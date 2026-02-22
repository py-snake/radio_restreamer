package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// ProxyConfig holds proxy connection settings for a stream.
type ProxyConfig struct {
	// Type is one of: direct, socks4, socks4a, socks5, http, https
	Type     string `json:"type"`
	Address  string `json:"address"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// StreamConfig defines a single radio stream to relay.
type StreamConfig struct {
	Name      string      `json:"name"`
	SourceURL string      `json:"source_url"`
	Port      int         `json:"port"`
	Proxy     ProxyConfig `json:"proxy"`
	// ReconnectDelaySecs is the initial delay before reconnecting (seconds).
	ReconnectDelaySecs int `json:"reconnect_delay_secs"`
	// MaxReconnectDelaySecs caps the exponential backoff (seconds).
	MaxReconnectDelaySecs int `json:"max_reconnect_delay_secs"`
	// DialTimeoutSecs is the timeout for the proxy dial + upstream response
	// headers. 0 uses the default (30 s).
	DialTimeoutSecs int `json:"dial_timeout_secs"`
	// MaxListeners caps simultaneous listeners. 0 means unlimited.
	MaxListeners int `json:"max_listeners"`
}

// Config is the top-level configuration structure.
type Config struct {
	AdminPort int            `json:"admin_port"`
	Streams   []StreamConfig `json:"streams"`
}

// LoadConfig reads and parses a JSON config file from the given path.
func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.AdminPort <= 0 || c.AdminPort > 65535 {
		return fmt.Errorf("config: admin_port %d is invalid", c.AdminPort)
	}
	if len(c.Streams) == 0 {
		return fmt.Errorf("config: no streams defined")
	}

	ports := make(map[int]string)
	for i, s := range c.Streams {
		if s.Name == "" {
			return fmt.Errorf("config: stream[%d] missing name", i)
		}
		if s.SourceURL == "" {
			return fmt.Errorf("config: stream %q missing source_url", s.Name)
		}
		if s.Port <= 0 || s.Port > 65535 {
			return fmt.Errorf("config: stream %q has invalid port %d", s.Name, s.Port)
		}
		if s.Port == c.AdminPort {
			return fmt.Errorf("config: stream %q port %d conflicts with admin_port", s.Name, s.Port)
		}
		if other, dup := ports[s.Port]; dup {
			return fmt.Errorf("config: stream %q and %q share port %d", s.Name, other, s.Port)
		}
		ports[s.Port] = s.Name

		// An empty or omitted proxy type is treated as "direct".
		if s.Proxy.Type == "" {
			c.Streams[i].Proxy.Type = "direct"
		}
		switch s.Proxy.Type {
		case "direct":
			// No address required.
		case "socks4", "socks4a", "socks5", "http", "https":
			if s.Proxy.Address == "" {
				return fmt.Errorf("config: stream %q missing proxy.address", s.Name)
			}
		default:
			return fmt.Errorf("config: stream %q has unknown proxy type %q (want direct, socks4, socks4a, socks5, http, https)", s.Name, s.Proxy.Type)
		}

		if s.ReconnectDelaySecs <= 0 {
			c.Streams[i].ReconnectDelaySecs = 3
		}
		if s.MaxReconnectDelaySecs <= 0 {
			c.Streams[i].MaxReconnectDelaySecs = 60
		}
		if s.DialTimeoutSecs <= 0 {
			c.Streams[i].DialTimeoutSecs = 30
		}
	}
	return nil
}
