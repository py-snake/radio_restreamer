package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestICYStripperBasic tests basic ICY metadata stripping functionality
func TestICYStripperBasic(t *testing.T) {
	stripper := newICYStripper(10) // metaint = 10 bytes
	buffer := &bytes.Buffer{}

	// Send 10 bytes of audio data
	audioData := []byte("1234567890")
	err := stripper.write(buffer, audioData)
	if err != nil {
		t.Fatalf("Failed to write audio data: %v", err)
	}

	if !bytes.Equal(buffer.Bytes(), audioData) {
		t.Errorf("Expected %q, got %q", audioData, buffer.Bytes())
	}
}

// TestICYStripperWithMetadata tests ICY metadata stripping with actual metadata blocks
func TestICYStripperWithMetadata(t *testing.T) {
	stripper := newICYStripper(5) // metaint = 5 bytes

	// Test data: 5 bytes audio + 1 byte metadata length (16 * 1 = 16 bytes metadata)
	testData := []byte("12345")                                // 5 bytes audio
	testData = append(testData, 0x01)                          // metadata length = 1 * 16 = 16
	testData = append(testData, []byte("metadata12345678")...) // 16 bytes metadata
	testData = append(testData, []byte("67890")...)            // next 5 bytes audio

	buffer := &bytes.Buffer{}
	err := stripper.write(buffer, testData)
	if err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}

	expected := []byte("1234567890") // Should get first 5 + next 5 bytes, skipping metadata
	if !bytes.Equal(buffer.Bytes(), expected) {
		t.Errorf("Expected %q, got %q", expected, buffer.Bytes())
	}
}

// TestICYStripperEmptyMetadata tests handling of empty metadata blocks
func TestICYStripperEmptyMetadata(t *testing.T) {
	stripper := newICYStripper(3)
	buffer := &bytes.Buffer{}

	// 3 bytes audio + empty metadata (length byte = 0)
	testData := []byte("abc")
	testData = append(testData, 0x00)             // empty metadata
	testData = append(testData, []byte("def")...) // next 3 bytes audio

	err := stripper.write(buffer, testData)
	if err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}

	expected := []byte("abcdef")
	if !bytes.Equal(buffer.Bytes(), expected) {
		t.Errorf("Expected %q, got %q", expected, buffer.Bytes())
	}
}

// TestStreamManagerCreation tests StreamManager creation with valid and invalid configs
func TestStreamManagerCreation(t *testing.T) {
	// Test valid configuration
	validConfig := StreamConfig{
		Name:      "Test Stream",
		SourceURL: "http://example.com/stream",
		Port:      8080,
		Proxy: ProxyConfig{
			Type: "direct",
		},
	}

	sm, err := NewStreamManager(validConfig)
	if err != nil {
		t.Fatalf("Failed to create StreamManager: %v", err)
	}
	if sm == nil {
		t.Fatal("StreamManager should not be nil")
	}

	// Test invalid proxy configuration
	invalidConfig := validConfig
	invalidConfig.Proxy.Type = "invalid-proxy-type"

	_, err = NewStreamManager(invalidConfig)
	if err == nil {
		t.Error("Expected error for invalid proxy type")
	}
}

// TestStreamManagerSnapshot tests the snapshot functionality
func TestStreamManagerSnapshot(t *testing.T) {
	config := StreamConfig{
		Name:      "Test Stream",
		SourceURL: "http://example.com/stream",
		Port:      8080,
		Proxy: ProxyConfig{
			Type: "direct",
		},
	}

	sm, err := NewStreamManager(config)
	if err != nil {
		t.Fatalf("Failed to create StreamManager: %v", err)
	}

	snapshot := sm.Snapshot()

	if snapshot.Name != config.Name {
		t.Errorf("Expected name %q, got %q", config.Name, snapshot.Name)
	}
	if snapshot.SourceURL != config.SourceURL {
		t.Errorf("Expected source URL %q, got %q", config.SourceURL, snapshot.SourceURL)
	}
	if snapshot.Port != config.Port {
		t.Errorf("Expected port %d, got %d", config.Port, snapshot.Port)
	}
	if snapshot.Status != "idle" {
		t.Errorf("Expected status 'idle', got %q", snapshot.Status)
	}
}

// TestStreamStatsConcurrency tests concurrent access to StreamStats
func TestStreamStatsConcurrency(t *testing.T) {
	stats := StreamStats{}

	// Set initial status
	stats.setStatus("connected")
	if stats.Status() != "connected" {
		t.Errorf("Expected status 'connected', got %q", stats.Status())
	}

	// Test concurrent access
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			stats.ListenerCount.Add(1)
			stats.TotalBytesIn.Add(int64(index))
			stats.setStatus(fmt.Sprintf("status-%d", index))
			_ = stats.Status()
		}(i)
	}
	wg.Wait()

	// Stats should still be accessible
	if stats.ListenerCount.Load() != 100 {
		t.Errorf("Expected 100 listeners, got %d", stats.ListenerCount.Load())
	}
}

// TestBroadcastConcurrency tests concurrent broadcasting to listeners
func TestBroadcastConcurrency(t *testing.T) {
	config := StreamConfig{
		Name:      "Test Stream",
		SourceURL: "http://example.com/stream",
		Port:      8080,
		Proxy:     ProxyConfig{Type: "direct"},
	}

	sm, err := NewStreamManager(config)
	if err != nil {
		t.Fatalf("Failed to create StreamManager: %v", err)
	}

	// Add multiple listeners
	listeners := make([]*listener, 10)
	for i := 0; i < 10; i++ {
		l, ok := sm.addListener()
		if !ok {
			t.Fatalf("Failed to add listener %d", i)
		}
		listeners[i] = l
	}

	// Test concurrent broadcasting
	var wg sync.WaitGroup
	data := []byte("test data")

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.broadcast(data)
		}()
	}
	wg.Wait()

	// Cleanup listeners
	for _, l := range listeners {
		sm.removeListener(l)
	}
}

// TestListenerChannelFull tests handling of slow consumers
func TestListenerChannelFull(t *testing.T) {
	config := StreamConfig{
		Name:      "Test Stream",
		SourceURL: "http://example.com/stream",
		Port:      8080,
		Proxy:     ProxyConfig{Type: "direct"},
	}

	sm, err := NewStreamManager(config)
	if err != nil {
		t.Fatalf("Failed to create StreamManager: %v", err)
	}

	// Add a listener but don't read from its channel
	l, ok := sm.addListener()
	if !ok {
		t.Fatal("Failed to add listener")
	}

	initialCount := sm.stats.ListenerCount.Load()

	// Fill up the channel completely
	data := []byte("x")
	for i := 0; i < listenerBufChunks*2; i++ {
		sm.broadcast(data)
	}

	// Listener should be removed due to slow consumer
	finalCount := sm.stats.ListenerCount.Load()
	if finalCount >= initialCount {
		t.Errorf("Expected listener count to decrease from %d, but got %d", initialCount, finalCount)
	}

	// Channel should be closed
	select {
	case <-l.done:
		// Expected - listener was dropped
	default:
		t.Error("Expected listener done channel to be closed")
	}
}

// TestHTTPListenerHandshake tests HTTP listener handshake functionality
func TestHTTPListenerHandshake(t *testing.T) {
	config := StreamConfig{
		Name:      "Test Stream",
		SourceURL: "http://example.com/stream",
		Port:      8080,
		Proxy:     ProxyConfig{Type: "direct"},
	}

	sm, err := NewStreamManager(config)
	if err != nil {
		t.Fatalf("Failed to create StreamManager: %v", err)
	}

	// Simulate upstream connection by setting headers
	sm.upHdrMu.Lock()
	sm.upHdrs = http.Header{
		"Content-Type": []string{"audio/mpeg"},
		"Icy-Name":     []string{"Test Station"},
	}
	sm.upHdrMu.Unlock()

	// Create test HTTP request
	req := httptest.NewRequest("GET", "/stream", nil)
	w := httptest.NewRecorder()

	// This should work now that we have upstream headers
	sm.handleListener(w, req)

	// Check response - should be internal server error since we can't hijack in test
	// (httptest.ResponseRecorder doesn't implement http.Hijacker)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// TestStreamManagerShutdown tests graceful shutdown
func TestStreamManagerShutdown(t *testing.T) {
	config := StreamConfig{
		Name:                  "Test Stream",
		SourceURL:             "http://example.com/stream",
		Port:                  0, // Use port 0 for faster startup (auto-assigned port)
		Proxy:                 ProxyConfig{Type: "direct"},
		ReconnectDelaySecs:    0, // No delay for faster shutdown in test
		MaxReconnectDelaySecs: 0,
	}

	sm, err := NewStreamManager(config)
	if err != nil {
		t.Fatalf("Failed to create StreamManager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Run should exit quickly due to timeout
	done := make(chan bool, 1) // Buffered channel to prevent goroutine leak
	go func() {
		sm.Run(ctx)
		done <- true
	}()

	select {
	case <-done:
		// Success - Run exited
	case <-time.After(300 * time.Millisecond):
		t.Error("StreamManager.Run did not exit within timeout")
	}
}

// TestBPSRateCalculation tests bandwidth rate calculation
func TestBPSRateCalculation(t *testing.T) {
	config := StreamConfig{
		Name:      "Test Stream",
		SourceURL: "http://example.com/stream",
		Port:      8080,
		Proxy:     ProxyConfig{Type: "direct"},
	}

	sm, err := NewStreamManager(config)
	if err != nil {
		t.Fatalf("Failed to create StreamManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start BPS ticker
	go sm.bpsTicker(ctx)

	// Wait for the first tick to fire and establish prevIn=0 / prevOut=0 baseline.
	time.Sleep(1100 * time.Millisecond)

	// Add data between tick 1 and tick 2.
	sm.stats.TotalBytesIn.Add(1000)
	sm.stats.TotalBytesOut.Add(500)

	// Wait for tick 2 to compute the delta (1000 and 500 respectively).
	// Read before tick 3 fires so the values have not been reset to zero yet.
	time.Sleep(1100 * time.Millisecond)

	inputBPS := sm.stats.InputBPS.Load()
	outputBPS := sm.stats.OutputBPS.Load()
	totalBytesIn := sm.stats.TotalBytesIn.Load()
	totalBytesOut := sm.stats.TotalBytesOut.Load()

	if inputBPS == 0 && totalBytesIn > 0 {
		t.Errorf("Input BPS should be non-zero (TotalBytesIn: %d, InputBPS: %d)", totalBytesIn, inputBPS)
	}
	if outputBPS == 0 && totalBytesOut > 0 {
		t.Errorf("Output BPS should be non-zero (TotalBytesOut: %d, OutputBPS: %d)", totalBytesOut, outputBPS)
	}

	cancel() // Stop the ticker
}

// TestMinFunction tests the min utility function
func TestMinFunction(t *testing.T) {
	testCases := []struct {
		a, b, expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{0, 5, 0},
		{5, 0, 0},
		{-1, 1, -1},
		{1, -1, -1},
	}

	for _, tc := range testCases {
		result := min(tc.a, tc.b)
		if result != tc.expected {
			t.Errorf("min(%d, %d) = %d, expected %d", tc.a, tc.b, result, tc.expected)
		}
	}
}

// TestRelayLoopBackoff tests reconnection backoff logic
func TestRelayLoopBackoff(t *testing.T) {
	config := StreamConfig{
		Name:                  "Test Stream",
		SourceURL:             "http://example.com/stream",
		Port:                  8080,
		Proxy:                 ProxyConfig{Type: "direct"},
		ReconnectDelaySecs:    1,
		MaxReconnectDelaySecs: 10,
	}

	sm, err := NewStreamManager(config)
	if err != nil {
		t.Fatalf("Failed to create StreamManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start relay loop in background
	go sm.relayLoop(ctx)

	// Wait a bit for reconnect attempts
	time.Sleep(1500 * time.Millisecond)

	// Check that reconnect count increased
	if sm.stats.ReconnectCount.Load() == 0 {
		t.Error("Expected reconnect count to increase")
	}

	cancel()
}

// Helper function for testing
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
