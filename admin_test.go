package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestAdminServerHandleStats tests the admin stats API handler
func TestAdminServerHandleStats(t *testing.T) {
	// Create test stream managers
	config1 := StreamConfig{
		Name:      "Test Stream 1",
		SourceURL: "http://example.com/stream1",
		Port:      8001,
		Proxy:     ProxyConfig{Type: "direct"},
	}

	config2 := StreamConfig{
		Name:      "Test Stream 2",
		SourceURL: "http://example.com/stream2",
		Port:      8002,
		Proxy:     ProxyConfig{Type: "direct"},
	}

	sm1, _ := NewStreamManager(config1)
	sm2, _ := NewStreamManager(config2)

	// Create admin server with test managers
	adminServer := &AdminServer{
		port:     8080,
		managers: []*StreamManager{sm1, sm2},
	}

	// Test stats API
	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()

	adminServer.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Check response content type
	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Expected JSON content type, got %s", contentType)
	}

	// Check CORS headers
	corsHeader := w.Header().Get("Access-Control-Allow-Origin")
	if corsHeader != "*" {
		t.Errorf("Expected CORS header '*', got '%s'", corsHeader)
	}

	// Parse response
	var response []StreamSnapshot
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("Expected 2 streams, got %d", len(response))
	}
}

// TestAdminServerHandleStatsEmptyStreams tests admin stats API with no streams
func TestAdminServerHandleStatsEmptyStreams(t *testing.T) {
	// Create admin server with no managers
	adminServer := &AdminServer{
		port:     8080,
		managers: []*StreamManager{},
	}

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()

	adminServer.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response []StreamSnapshot
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	if len(response) != 0 {
		t.Errorf("Expected 0 streams, got %d", len(response))
	}
}

// TestStreamSnapshotJSON tests JSON marshaling of StreamSnapshot
func TestStreamSnapshotJSON(t *testing.T) {
	snapshot := StreamSnapshot{
		Name:           "Test Stream",
		SourceURL:      "http://example.com/stream",
		Port:           8080,
		Status:         "connected",
		ListenerCount:  5,
		TotalBytesIn:   1000000,
		InputBPS:       128000,
		TotalBytesOut:  900000,
		OutputBPS:      115200,
		ReconnectCount: 2,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Failed to marshal StreamSnapshot: %v", err)
	}

	// Unmarshal back
	var unmarshaled StreamSnapshot
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal StreamSnapshot: %v", err)
	}

	// Verify all fields match
	if snapshot.Name != unmarshaled.Name {
		t.Errorf("Name mismatch: %s != %s", snapshot.Name, unmarshaled.Name)
	}
	if snapshot.SourceURL != unmarshaled.SourceURL {
		t.Errorf("SourceURL mismatch: %s != %s", snapshot.SourceURL, unmarshaled.SourceURL)
	}
	if snapshot.Port != unmarshaled.Port {
		t.Errorf("Port mismatch: %d != %d", snapshot.Port, unmarshaled.Port)
	}
	if snapshot.Status != unmarshaled.Status {
		t.Errorf("Status mismatch: %s != %s", snapshot.Status, unmarshaled.Status)
	}
	if snapshot.ListenerCount != unmarshaled.ListenerCount {
		t.Errorf("ListenerCount mismatch: %d != %d", snapshot.ListenerCount, unmarshaled.ListenerCount)
	}
	if snapshot.TotalBytesIn != unmarshaled.TotalBytesIn {
		t.Errorf("TotalBytesIn mismatch: %d != %d", snapshot.TotalBytesIn, unmarshaled.TotalBytesIn)
	}
	if snapshot.InputBPS != unmarshaled.InputBPS {
		t.Errorf("InputBPS mismatch: %d != %d", snapshot.InputBPS, unmarshaled.InputBPS)
	}
	if snapshot.TotalBytesOut != unmarshaled.TotalBytesOut {
		t.Errorf("TotalBytesOut mismatch: %d != %d", snapshot.TotalBytesOut, unmarshaled.TotalBytesOut)
	}
	if snapshot.OutputBPS != unmarshaled.OutputBPS {
		t.Errorf("OutputBPS mismatch: %d != %d", snapshot.OutputBPS, unmarshaled.OutputBPS)
	}
	if snapshot.ReconnectCount != unmarshaled.ReconnectCount {
		t.Errorf("ReconnectCount mismatch: %d != %d", snapshot.ReconnectCount, unmarshaled.ReconnectCount)
	}
}

// TestAdminServerConcurrentAccess tests concurrent access to admin stats
func TestAdminServerConcurrentAccess(t *testing.T) {
	// Create test managers
	managers := make([]*StreamManager, 5)
	for i := 0; i < 5; i++ {
		config := StreamConfig{
			Name:      "Test Stream " + string(rune('A'+i)),
			SourceURL: "http://example.com/stream" + string(rune('A'+i)),
			Port:      8000 + i,
			Proxy:     ProxyConfig{Type: "direct"},
		}
		sm, _ := NewStreamManager(config)
		managers[i] = sm
	}

	adminServer := &AdminServer{
		port:     8080,
		managers: managers,
	}

	// Test concurrent access
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/stats", nil)
			w := httptest.NewRecorder()
			adminServer.handleStats(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
			}
		}()
	}
	wg.Wait()
}

// TestAdminServerHandleIndex tests the admin index page handler
func TestAdminServerHandleIndex(t *testing.T) {
	adminServer := &AdminServer{
		port:     8080,
		managers: []*StreamManager{},
	}

	// Test root path
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	adminServer.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Check response content type
	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Expected HTML content type, got %s", contentType)
	}

	// Check that response contains expected HTML
	if !strings.Contains(w.Body.String(), "Radio Restreamer") {
		t.Error("Expected response to contain 'Radio Restreamer'")
	}

	// Test non-root path (should return 404)
	req2 := httptest.NewRequest("GET", "/nonexistent", nil)
	w2 := httptest.NewRecorder()

	adminServer.handleIndex(w2, req2)

	if w2.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w2.Code)
	}
}

// TestAdminServerNewAdminServer tests AdminServer creation
func TestAdminServerNewAdminServer(t *testing.T) {
	// Create test managers
	managers := make([]*StreamManager, 2)
	for i := 0; i < 2; i++ {
		config := StreamConfig{
			Name:      "Test Stream " + string(rune('A'+i)),
			SourceURL: "http://example.com/stream" + string(rune('A'+i)),
			Port:      8000 + i,
			Proxy:     ProxyConfig{Type: "direct"},
		}
		sm, _ := NewStreamManager(config)
		managers[i] = sm
	}

	adminServer := NewAdminServer(8080, managers)

	if adminServer == nil {
		t.Fatal("AdminServer should not be nil")
	}

	if adminServer.port != 8080 {
		t.Errorf("Expected port 8080, got %d", adminServer.port)
	}

	if len(adminServer.managers) != 2 {
		t.Errorf("Expected 2 managers, got %d", len(adminServer.managers))
	}
}

// TestAdminServerStatsCacheControl tests cache control headers
func TestAdminServerStatsCacheControl(t *testing.T) {
	adminServer := &AdminServer{
		port:     8080,
		managers: []*StreamManager{},
	}

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()

	adminServer.handleStats(w, req)

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Expected Cache-Control 'no-cache', got '%s'", cacheControl)
	}
}
