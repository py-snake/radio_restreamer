package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// TestNewDialerDirect tests direct proxy connection
func TestNewDialerDirect(t *testing.T) {
	cfg := ProxyConfig{Type: "direct"}
	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("Failed to create direct dialer: %v", err)
	}

	if dialer == nil {
		t.Fatal("Dialer should not be nil")
	}
}

// TestNewDialerSOCKS5 tests SOCKS5 proxy dialer creation
func TestNewDialerSOCKS5(t *testing.T) {
	cfg := ProxyConfig{
		Type:    "socks5",
		Address: "127.0.0.1:1080",
	}

	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("Failed to create SOCKS5 dialer: %v", err)
	}

	if dialer == nil {
		t.Fatal("Dialer should not be nil")
	}
}

// TestNewDialerSOCKS5WithAuth tests SOCKS5 proxy with authentication
func TestNewDialerSOCKS5WithAuth(t *testing.T) {
	cfg := ProxyConfig{
		Type:     "socks5",
		Address:  "127.0.0.1:1080",
		Username: "user",
		Password: "pass",
	}

	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("Failed to create SOCKS5 dialer with auth: %v", err)
	}

	if dialer == nil {
		t.Fatal("Dialer should not be nil")
	}
}

// TestNewDialerHTTP tests HTTP proxy dialer creation
func TestNewDialerHTTP(t *testing.T) {
	cfg := ProxyConfig{
		Type:    "http",
		Address: "127.0.0.1:8080",
	}

	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("Failed to create HTTP dialer: %v", err)
	}

	if dialer == nil {
		t.Fatal("Dialer should not be nil")
	}

	// Verify it's an httpConnectDialer
	if _, ok := dialer.(*httpConnectDialer); !ok {
		t.Error("Expected httpConnectDialer type")
	}
}

// TestNewDialerHTTPS tests HTTPS proxy dialer creation
func TestNewDialerHTTPS(t *testing.T) {
	cfg := ProxyConfig{
		Type:    "https",
		Address: "127.0.0.1:8443",
	}

	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("Failed to create HTTPS dialer: %v", err)
	}

	if dialer == nil {
		t.Fatal("Dialer should not be nil")
	}

	// Verify it's an httpConnectDialer
	if _, ok := dialer.(*httpConnectDialer); !ok {
		t.Error("Expected httpConnectDialer type")
	}
}

// TestNewDialerInvalidType tests invalid proxy type handling
func TestNewDialerInvalidType(t *testing.T) {
	cfg := ProxyConfig{Type: "invalid-type"}

	_, err := NewDialer(cfg)
	if err == nil {
		t.Error("Expected error for invalid proxy type")
	}
}

// TestDialContextFunc tests the DialContextFunc wrapper
func TestDialContextFunc(t *testing.T) {
	cfg := ProxyConfig{Type: "direct"}
	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("Failed to create dialer: %v", err)
	}

	// Test with timeout
	dialFunc := DialContextFunc(dialer, 5*time.Second)
	if dialFunc == nil {
		t.Fatal("Dial function should not be nil")
	}

	// Test without timeout
	dialFuncNoTimeout := DialContextFunc(dialer, 0)
	if dialFuncNoTimeout == nil {
		t.Fatal("Dial function without timeout should not be nil")
	}
}

// TestDialContextFuncWithContextCancellation tests context cancellation
func TestDialContextFuncWithContextCancellation(t *testing.T) {
	cfg := ProxyConfig{Type: "direct"}
	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("Failed to create dialer: %v", err)
	}

	dialFunc := DialContextFunc(dialer, 5*time.Second)

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Attempt to dial with cancelled context
	conn, err := dialFunc(ctx, "tcp", "example.com:80")
	if err == nil {
		conn.Close()
		t.Error("Expected error when dialing with cancelled context")
	}
}

// TestHTTPConnectDialerInterface tests that httpConnectDialer implements required interfaces
func TestHTTPConnectDialerInterface(t *testing.T) {
	cfg := ProxyConfig{
		Type:    "http",
		Address: "127.0.0.1:8080",
	}

	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("Failed to create dialer: %v", err)
	}

	// Test that it implements proxy.Dialer
	if _, ok := dialer.(proxy.Dialer); !ok {
		t.Error("Dialer should implement proxy.Dialer interface")
	}

	// Test that it implements DialContext
	if httpDialer, ok := dialer.(*httpConnectDialer); ok {
		// Test Dial method (should delegate to DialContext)
		conn, err := httpDialer.Dial("tcp", "example.com:80")
		if err == nil {
			conn.Close()
		}
		// We expect this to fail since there's no real proxy server,
		// but it shouldn't panic
	}
}

// TestProxyConfigValidation tests proxy configuration validation
func TestProxyConfigValidation(t *testing.T) {
	testCases := []struct {
		name        string
		cfg         ProxyConfig
		expectError bool
	}{
		{
			name: "valid direct proxy",
			cfg:  ProxyConfig{Type: "direct"},
		},
		{
			name: "valid SOCKS5 proxy",
			cfg:  ProxyConfig{Type: "socks5", Address: "127.0.0.1:1080"},
		},
		{
			name: "valid SOCKS5 with auth",
			cfg:  ProxyConfig{Type: "socks5", Address: "127.0.0.1:1080", Username: "user", Password: "pass"},
		},
		{
			name: "valid HTTP proxy",
			cfg:  ProxyConfig{Type: "http", Address: "127.0.0.1:8080"},
		},
		{
			name: "valid HTTPS proxy",
			cfg:  ProxyConfig{Type: "https", Address: "127.0.0.1:8443"},
		},
		{
			name:        "invalid proxy type",
			cfg:         ProxyConfig{Type: "invalid"},
			expectError: true,
		},
		{
			name:        "SOCKS5 without address",
			cfg:         ProxyConfig{Type: "socks5"},
			expectError: true,
		},
		{
			name:        "HTTP without address",
			cfg:         ProxyConfig{Type: "http"},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDialer(tc.cfg)
			if tc.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestDialerConcurrentAccess tests concurrent access to dialers
func TestDialerConcurrentAccess(t *testing.T) {
	cfg := ProxyConfig{Type: "direct"}
	dialer, err := NewDialer(cfg)
	if err != nil {
		t.Fatalf("Failed to create dialer: %v", err)
	}

	dialFunc := DialContextFunc(dialer, 5*time.Second)

	// Test concurrent access
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			conn, err := dialFunc(ctx, "tcp", "example.com:80")
			if err == nil {
				conn.Close()
			}
			// We expect this to fail, but it shouldn't panic
		}()
	}
	wg.Wait()
}
