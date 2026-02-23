package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// NewDialer builds a proxy.Dialer that routes connections through the
// configured proxy. Supports direct, socks4, socks4a, socks5, http, https.
func NewDialer(cfg ProxyConfig) (proxy.Dialer, error) {
	switch cfg.Type {
	case "direct":
		return proxy.Direct, nil

	case "socks4", "socks4a", "socks5":
		if cfg.Address == "" {
			return nil, fmt.Errorf("proxy: %s requires an address", cfg.Type)
		}
		var rawURL string
		if cfg.Username != "" {
			u := url.URL{
				Scheme: cfg.Type,
				User:   url.UserPassword(cfg.Username, cfg.Password),
				Host:   cfg.Address,
			}
			rawURL = u.String()
		} else {
			rawURL = fmt.Sprintf("%s://%s", cfg.Type, cfg.Address)
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("proxy: invalid URL %q: %w", rawURL, err)
		}
		d, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("proxy: %w", err)
		}
		return d, nil

	case "http", "https":
		if cfg.Address == "" {
			return nil, fmt.Errorf("proxy: %s requires an address", cfg.Type)
		}
		return &httpConnectDialer{cfg: cfg}, nil

	default:
		return nil, fmt.Errorf("proxy: unknown type %q", cfg.Type)
	}
}

// DialContextFunc wraps a proxy.Dialer into the function signature required
// by http.Transport.DialContext, with an optional per-call timeout.
// If the dialer natively supports DialContext (all x/net dialers do), it is
// used directly; otherwise a goroutine-based fallback is used.
func DialContextFunc(d proxy.Dialer, timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	type ctxDialer interface {
		DialContext(context.Context, string, string) (net.Conn, error)
	}
	if cd, ok := d.(ctxDialer); ok {
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			return cd.DialContext(ctx, network, addr)
		}
	}
	// Fallback: run Dial in a goroutine and honour context cancellation.
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		type result struct {
			conn net.Conn
			err  error
		}
		ch := make(chan result, 1)
		go func() {
			conn, err := d.Dial(network, addr)
			ch <- result{conn, err}
		}()
		select {
		case r := <-ch:
			return r.conn, r.err
		case <-ctx.Done():
			// Drain the goroutine and close the connection if it arrived late.
			select {
			case r := <-ch:
				if r.conn != nil {
					r.conn.Close()
				}
			default:
				// No connection received yet
			}
			return nil, ctx.Err()
		}
	}
}

// httpConnectDialer tunnels connections through an HTTP/HTTPS proxy using
// the CONNECT method. Implements both proxy.Dialer and DialContext.
type httpConnectDialer struct {
	cfg ProxyConfig
}

// Dial implements proxy.Dialer by delegating to DialContext.
func (d *httpConnectDialer) Dial(network, addr string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, addr)
}

// DialContext connects to the proxy, optionally upgrades to TLS, then issues
// a CONNECT request to tunnel through to addr.
func (d *httpConnectDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// Connect to the proxy itself.
	nd := &net.Dialer{}
	tcpConn, err := nd.DialContext(ctx, "tcp", d.cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("proxy: dial %s: %w", d.cfg.Address, err)
	}

	var conn net.Conn
	if d.cfg.Type == "https" {
		host, _, splitErr := net.SplitHostPort(d.cfg.Address)
		if splitErr != nil {
			host = d.cfg.Address
		}
		// InsecureSkipVerify: proxy TLS certs are not verified; the tunnelled
		// traffic carries its own security.
		tlsConn := tls.Client(tcpConn, &tls.Config{ServerName: host, InsecureSkipVerify: true}) //nolint:gosec
		if err = tlsConn.HandshakeContext(ctx); err != nil {
			tcpConn.Close()
			return nil, fmt.Errorf("proxy: TLS handshake with %s: %w", d.cfg.Address, err)
		}
		conn = tlsConn
	} else {
		conn = tcpConn
	}

	// Issue HTTP CONNECT to open a tunnel to the target.
	req := &http.Request{
		Method:     "CONNECT",
		URL:        &url.URL{Opaque: addr},
		Host:       addr,
		Header:     make(http.Header),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
	if d.cfg.Username != "" {
		req.SetBasicAuth(d.cfg.Username, d.cfg.Password)
	}

	if err = req.Write(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("proxy: CONNECT write: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("proxy: CONNECT response: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("proxy: CONNECT status %d %s", resp.StatusCode, resp.Status)
	}

	return conn, nil
}
