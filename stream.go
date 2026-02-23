package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
)

const chunkSize = 8192       // bytes read per iteration from upstream
const listenerBufChunks = 64 // channel depth per listener (~512 KB buffer)

// icyStripper filters ICY inline metadata blocks out of a byte stream,
// writing only the audio bytes to the destination writer.
// ICY streams interleave a metadata block after every metaint audio bytes.
// Each metadata block starts with one length byte (value × 16 = actual length),
// followed by that many bytes of metadata text.
type icyStripper struct {
	metaint       int
	audioLeft     int  // audio bytes remaining before the next metadata block
	needMetaLen   bool // true when the next byte is the metadata length indicator
	metaBytesLeft int  // metadata bytes still to skip
}

func newICYStripper(metaint int) *icyStripper {
	return &icyStripper{metaint: metaint, audioLeft: metaint}
}

// write passes audio bytes through to w, silently discarding metadata blocks.
func (s *icyStripper) write(w io.Writer, chunk []byte) error {
	for len(chunk) > 0 {
		switch {
		case s.metaBytesLeft > 0:
			n := min(s.metaBytesLeft, len(chunk))
			chunk = chunk[n:]
			s.metaBytesLeft -= n
			if s.metaBytesLeft == 0 {
				s.audioLeft = s.metaint
			}

		case s.needMetaLen:
			if len(chunk) == 0 {
				return nil
			}
			metaLen := int(chunk[0]) * 16
			chunk = chunk[1:]
			s.needMetaLen = false
			if metaLen > 0 {
				s.metaBytesLeft = metaLen
			} else {
				s.audioLeft = s.metaint
			}

		default:
			n := min(s.audioLeft, len(chunk))
			if n > 0 {
				if _, err := w.Write(chunk[:n]); err != nil {
					return err
				}
				chunk = chunk[n:]
				s.audioLeft -= n
				if s.audioLeft == 0 {
					s.needMetaLen = true
				}
			}
		}
	}
	return nil
}

// StreamStats holds live metrics for a single stream. All numeric fields
// are accessed atomically so they can be read from the admin handler without
// holding the stream lock.
type StreamStats struct {
	ListenerCount  atomic.Int64
	TotalBytesIn   atomic.Int64 // raw bytes received from upstream
	InputBPS       atomic.Int64 // upstream bytes/sec, refreshed every second
	TotalBytesOut  atomic.Int64 // bytes enqueued to listener channels
	OutputBPS      atomic.Int64 // listener aggregate bytes/sec, refreshed every second
	ReconnectCount atomic.Int64

	mu     sync.RWMutex
	status string
}

func (s *StreamStats) setStatus(v string) {
	s.mu.Lock()
	s.status = v
	s.mu.Unlock()
}

func (s *StreamStats) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// StreamSnapshot is a point-in-time copy of StreamStats for JSON encoding.
type StreamSnapshot struct {
	Name           string `json:"name"`
	SourceURL      string `json:"source_url"`
	Port           int    `json:"port"`
	Status         string `json:"status"`
	ListenerCount  int64  `json:"listener_count"`
	TotalBytesIn   int64  `json:"total_bytes_in"`
	InputBPS       int64  `json:"input_bps"`
	TotalBytesOut  int64  `json:"total_bytes_out"`
	OutputBPS      int64  `json:"output_bps"`
	ReconnectCount int64  `json:"reconnect_count"`
}

// listener represents a single connected HTTP client.
type listener struct {
	ch   chan []byte
	done chan struct{} // closed when the listener is dropped (slow consumer)
}

// StreamManager manages the upstream connection for one radio stream and fans
// received data out to all connected HTTP listeners.
type StreamManager struct {
	cfg    StreamConfig
	dialer proxy.Dialer
	stats  StreamStats

	// ctx is the root context; cancelled on shutdown.
	// Set once in Run() before any goroutines that use it are started.
	ctxMu sync.RWMutex
	ctx   context.Context

	mu        sync.Mutex
	listeners map[*listener]struct{}

	// upHdrs caches Content-Type and icy-* station headers (not icy-metaint,
	// which is stripped at the relay level) from the upstream response.
	upHdrMu sync.RWMutex
	upHdrs  http.Header // nil until first successful upstream connect
}

// NewStreamManager creates a StreamManager and resolves the proxy dialer.
func NewStreamManager(cfg StreamConfig) (*StreamManager, error) {
	d, err := NewDialer(cfg.Proxy)
	if err != nil {
		return nil, fmt.Errorf("stream %q: %w", cfg.Name, err)
	}
	sm := &StreamManager{
		cfg:       cfg,
		dialer:    d,
		listeners: make(map[*listener]struct{}),
	}
	sm.stats.setStatus("idle")
	return sm, nil
}

// Snapshot returns a point-in-time copy of the stream's stats.
func (sm *StreamManager) Snapshot() StreamSnapshot {
	return StreamSnapshot{
		Name:           sm.cfg.Name,
		SourceURL:      sm.cfg.SourceURL,
		Port:           sm.cfg.Port,
		Status:         sm.stats.Status(),
		ListenerCount:  sm.stats.ListenerCount.Load(),
		TotalBytesIn:   sm.stats.TotalBytesIn.Load(),
		InputBPS:       sm.stats.InputBPS.Load(),
		TotalBytesOut:  sm.stats.TotalBytesOut.Load(),
		OutputBPS:      sm.stats.OutputBPS.Load(),
		ReconnectCount: sm.stats.ReconnectCount.Load(),
	}
}

// Run starts the upstream relay loop and the per-stream HTTP server.
// It blocks until ctx is cancelled and all servers have shut down.
func (sm *StreamManager) Run(ctx context.Context) {
	// Store ctx before starting goroutines that read it.
	sm.ctxMu.Lock()
	sm.ctx = ctx
	sm.ctxMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sm.serveListeners(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		sm.bpsTicker(ctx)
	}()
	sm.relayLoop(ctx)
	wg.Wait()
}

// bpsTicker refreshes InputBPS and OutputBPS every second until ctx is cancelled.
func (sm *StreamManager) bpsTicker(ctx context.Context) {
	prevIn := sm.stats.TotalBytesIn.Load()
	prevOut := sm.stats.TotalBytesOut.Load()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			curIn := sm.stats.TotalBytesIn.Load()
			curOut := sm.stats.TotalBytesOut.Load()
			sm.stats.InputBPS.Store(curIn - prevIn)
			sm.stats.OutputBPS.Store(curOut - prevOut)
			prevIn = curIn
			prevOut = curOut
		}
	}
}

// relayLoop connects to the upstream, reads data, and broadcasts to listeners.
// On any error it waits with exponential backoff then retries.
func (sm *StreamManager) relayLoop(ctx context.Context) {
	initDelay := time.Duration(sm.cfg.ReconnectDelaySecs) * time.Second
	maxDelay := time.Duration(sm.cfg.MaxReconnectDelaySecs) * time.Second
	delay := initDelay

	for {
		if ctx.Err() != nil {
			sm.stats.setStatus("stopped")
			return
		}

		sm.stats.setStatus("connecting")
		log.Printf("[%s] connecting to %s via %s %s",
			sm.cfg.Name, sm.cfg.SourceURL, sm.cfg.Proxy.Type, sm.cfg.Proxy.Address)

		start := time.Now()
		err := sm.relay(ctx)

		if ctx.Err() != nil {
			sm.stats.setStatus("stopped")
			return
		}

		// If the session ran long enough to be considered stable, reset the
		// backoff so the next reconnect is fast rather than waiting at the
		// last accumulated delay.
		if time.Since(start) >= initDelay {
			delay = initDelay
		}

		sm.stats.setStatus("reconnecting")
		sm.stats.ReconnectCount.Add(1)
		log.Printf("[%s] relay error: %v — reconnecting in %s", sm.cfg.Name, err, delay)

		// Use a timer instead of time.After so it can be stopped immediately
		// when ctx is cancelled, rather than leaking until the delay expires.
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			sm.stats.setStatus("stopped")
			return
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

// relay performs a single upstream connection and relay session.
// It returns when the upstream closes, errors, or ctx is cancelled.
//
// ICY inline metadata is stripped here, at the relay level, before
// broadcasting. This ensures every listener — regardless of when it connects —
// always receives clean audio bytes aligned from the true start of each
// audio block. A per-listener stripper would be initialised at position 0
// even when the stream is mid-block, corrupting the bitstream.
func (sm *StreamManager) relay(ctx context.Context) error {
	dialTimeout := time.Duration(sm.cfg.DialTimeoutSecs) * time.Second

	transport := &http.Transport{
		DialContext:           DialContextFunc(sm.dialer, dialTimeout),
		ResponseHeaderTimeout: dialTimeout,
		DisableKeepAlives:     true,
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport}

	req, err := http.NewRequestWithContext(ctx, "GET", sm.cfg.SourceURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Icy-MetaData", "1")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upstream GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	// Build cached headers for listener handshakes.
	// icy-metaint is intentionally excluded: ICY inline metadata is stripped
	// at this relay level, so there is nothing for listeners to parse.
	cached := make(http.Header)
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/mpeg"
	}
	cached.Set("Content-Type", ct)
	for k, vs := range resp.Header {
		lower := strings.ToLower(k)
		if strings.HasPrefix(lower, "icy-") && lower != "icy-metaint" {
			cached[k] = vs
		}
	}

	// Create a relay-level ICY stripper if the upstream carries metadata.
	var relayStrip *icyStripper
	if metaintStr := resp.Header.Get("Icy-Metaint"); metaintStr != "" {
		if n, err := strconv.Atoi(metaintStr); err == nil && n > 0 {
			relayStrip = newICYStripper(n)
		}
	}

	sm.upHdrMu.Lock()
	sm.upHdrs = cached
	sm.upHdrMu.Unlock()

	sm.stats.setStatus("connected")
	log.Printf("[%s] connected — content-type=%s icy-name=%q icy-metaint=%s (stripping=%v)",
		sm.cfg.Name, ct, cached.Get("Icy-Name"), resp.Header.Get("Icy-Metaint"), relayStrip != nil)

	buf := make([]byte, chunkSize)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			sm.stats.TotalBytesIn.Add(int64(n))
			var toSend []byte
			if relayStrip != nil {
				var stripped bytes.Buffer
				_ = relayStrip.write(&stripped, buf[:n])
				toSend = stripped.Bytes()
			} else {
				toSend = make([]byte, n)
				copy(toSend, buf[:n])
			}
			if len(toSend) > 0 {
				sm.broadcast(toSend)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return fmt.Errorf("upstream closed stream")
			}
			return fmt.Errorf("upstream read: %w", readErr)
		}
	}
}

// broadcast sends a chunk to all registered listeners. If a listener's
// channel is full, that listener is dropped (slow consumer).
func (sm *StreamManager) broadcast(chunk []byte) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for l := range sm.listeners {
		select {
		case l.ch <- chunk:
			sm.stats.TotalBytesOut.Add(int64(len(chunk)))
		default:
			// Slow consumer — signal disconnect and remove immediately.
			close(l.done)
			delete(sm.listeners, l)
			sm.stats.ListenerCount.Add(-1)
		}
	}
}

// addListener registers a new listener. Returns (listener, true) on success,
// or (nil, false) if the max listener cap has been reached.
func (sm *StreamManager) addListener() (*listener, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.cfg.MaxListeners > 0 && len(sm.listeners) >= sm.cfg.MaxListeners {
		return nil, false
	}
	l := &listener{
		ch:   make(chan []byte, listenerBufChunks),
		done: make(chan struct{}),
	}
	sm.listeners[l] = struct{}{}
	sm.stats.ListenerCount.Add(1)
	return l, true
}

// removeListener deregisters a listener if it hasn't already been removed.
func (sm *StreamManager) removeListener(l *listener) {
	sm.mu.Lock()
	if _, ok := sm.listeners[l]; ok {
		delete(sm.listeners, l)
		sm.stats.ListenerCount.Add(-1)
	}
	sm.mu.Unlock()
}

// upstreamHeaders returns a snapshot of the cached upstream headers.
// Returns nil if not yet connected.
func (sm *StreamManager) upstreamHeaders() http.Header {
	sm.upHdrMu.RLock()
	defer sm.upHdrMu.RUnlock()
	return sm.upHdrs
}

// serveListeners starts an HTTP server on the configured port and shuts it
// down cleanly when ctx is cancelled.
func (sm *StreamManager) serveListeners(ctx context.Context) {
	addr := fmt.Sprintf("0.0.0.0:%d", sm.cfg.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           http.HandlerFunc(sm.handleListener),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("[%s] listener server shutdown: %v", sm.cfg.Name, err)
		}
	}()

	log.Printf("[%s] restream server on %s", sm.cfg.Name, addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[%s] listener server: %v", sm.cfg.Name, err)
	}
}

// handleListener handles an incoming HTTP request from a listener client.
//
// The connection is hijacked so we can write a raw HTTP/1.0 response directly
// to the TCP socket. HTTP/1.0 has no chunked transfer encoding, giving the
// client a continuous unframed byte stream — exactly what Icecast servers
// provide and what audio players and browsers expect.
func (sm *StreamManager) handleListener(w http.ResponseWriter, r *http.Request) {
	// Return 503 if the upstream has never connected. After the first
	// successful connection, upHdrs is retained across reconnects so
	// in-progress listeners ride through brief outages without dropping.
	upHdrs := sm.upstreamHeaders()
	if upHdrs == nil {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "stream not yet available, please retry", http.StatusServiceUnavailable)
		return
	}

	// Check listener cap before hijacking.
	l, accepted := sm.addListener()
	if !accepted {
		http.Error(w, "stream at capacity", http.StatusServiceUnavailable)
		return
	}
	defer sm.removeListener(l)

	// Hijack to take raw control of the TCP connection.
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("[%s] hijack: %v", sm.cfg.Name, err)
		return
	}
	defer conn.Close()

	// Close the raw connection when the server shuts down or this listener
	// is dropped by the broadcaster (slow consumer).
	go func() {
		sm.ctxMu.RLock()
		ctx := sm.ctx
		sm.ctxMu.RUnlock()

		select {
		case <-ctx.Done():
		case <-l.done:
		}
		conn.Close()
	}()

	// Write HTTP/1.0 response headers.
	var hdr strings.Builder
	hdr.WriteString("HTTP/1.0 200 OK\r\n")
	hdr.WriteString("Content-Type: " + upHdrs.Get("Content-Type") + "\r\n")
	hdr.WriteString("Cache-Control: no-cache\r\n")
	hdr.WriteString("Pragma: no-cache\r\n")
	hdr.WriteString("Access-Control-Allow-Origin: *\r\n")
	// Forward ICY station headers (icy-name, icy-description, icy-genre, …).
	// icy-metaint is absent from upHdrs (stripped at relay level).
	for k, vs := range upHdrs {
		if strings.HasPrefix(strings.ToLower(k), "icy-") && len(vs) > 0 {
			hdr.WriteString(k + ": " + vs[0] + "\r\n")
		}
	}
	hdr.WriteString("\r\n")

	if _, err := io.WriteString(conn, hdr.String()); err != nil {
		return
	}

	log.Printf("[%s] listener connected: %s", sm.cfg.Name, r.RemoteAddr)
	defer log.Printf("[%s] listener disconnected: %s", sm.cfg.Name, r.RemoteAddr)

	// Stream audio. Write errors (client disconnect, slow-consumer close,
	// shutdown) all cause a natural exit.
	sm.ctxMu.RLock()
	ctx := sm.ctx
	sm.ctxMu.RUnlock()

	for {
		select {
		case <-ctx.Done():
			return
		case <-l.done:
			return
		case chunk, ok := <-l.ch:
			if !ok {
				return
			}
			if _, err := conn.Write(chunk); err != nil {
				return
			}
		}
	}
}
