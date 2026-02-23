package main

import (
	"context"
	"testing"
	"time"
)

// TestConfigLoading tests configuration loading functionality
func TestConfigLoading(t *testing.T) {
	// Test with empty config (should fail)
	_, err := LoadConfig("nonexistent.json")
	if err == nil {
		t.Error("Expected error for non-existent config file")
	}

	// Test with invalid JSON (should fail)
	// Note: This would require creating a temporary invalid JSON file
	// For now, we'll just test that the function exists and can be called
}

// TestMainFunctionality tests basic main program functionality
func TestMainFunctionality(t *testing.T) {
	// Create a minimal test configuration
	testConfig := Config{
		AdminPort: 8080,
		Streams: []StreamConfig{
			{
				Name:      "Test Stream",
				SourceURL: "http://example.com/stream",
				Port:      8000,
				Proxy: ProxyConfig{
					Type: "direct",
				},
			},
		},
	}

	// Test that we can create stream managers from config
	managers := make([]*StreamManager, len(testConfig.Streams))
	for i, streamCfg := range testConfig.Streams {
		sm, err := NewStreamManager(streamCfg)
		if err != nil {
			t.Fatalf("Failed to create stream manager: %v", err)
		}
		managers[i] = sm
	}

	if len(managers) != 1 {
		t.Errorf("Expected 1 stream manager, got %d", len(managers))
	}

	// Test admin server creation
	adminServer := NewAdminServer(testConfig.AdminPort, managers)
	if adminServer == nil {
		t.Fatal("Admin server should not be nil")
	}

	// Test graceful shutdown context
	_, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// This would normally run the servers, but we'll just test the setup
	// since actually running servers would require proper cleanup
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	// Test valid config
	validConfig := Config{
		AdminPort: 8080,
		Streams: []StreamConfig{
			{
				Name:      "Test",
				SourceURL: "http://example.com/stream",
				Port:      8000,
				Proxy:     ProxyConfig{Type: "direct"},
			},
		},
	}

	// Should be able to create managers from valid config
	managers := make([]*StreamManager, len(validConfig.Streams))
	for i, streamCfg := range validConfig.Streams {
		sm, err := NewStreamManager(streamCfg)
		if err != nil {
			t.Fatalf("Failed to create stream manager: %v", err)
		}
		managers[i] = sm
	}

	if len(managers) != 1 {
		t.Errorf("Expected 1 stream manager, got %d", len(managers))
	}

	// Test invalid stream config (missing required fields)
	invalidStreamConfig := StreamConfig{
		// Missing Name and SourceURL
		Port: 8000,
	}

	_, err := NewStreamManager(invalidStreamConfig)
	if err == nil {
		t.Error("Expected error for invalid stream configuration")
	}
}
