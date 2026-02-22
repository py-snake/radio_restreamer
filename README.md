# Radio Restreamer

A high-performance, multi-stream radio restreamer written in Go that allows you to relay multiple radio streams through proxies with automatic reconnection and ICY metadata stripping.

## Features

- **Multi-Stream Support**: Relay multiple radio streams simultaneously
- **Proxy Support**: SOCKS4, SOCKS4a, SOCKS5, HTTP, and HTTPS proxy support
- **Automatic Reconnection**: Exponential backoff with configurable delays
- **ICY Metadata Stripping**: Clean audio streams without metadata corruption
- **Live Statistics**: Real-time web dashboard with stream metrics
- **Concurrent Listeners**: Handle multiple simultaneous connections per stream
- **Graceful Shutdown**: Proper cleanup on application termination
- **Cross-Platform**: Runs on Linux, Windows, and macOS

## Quick Start

### Prerequisites

- Go 1.25.7 or later
- Access to radio stream URLs
- Proxy servers (optional, for geo-restricted streams)

### Installation

1. **Clone the repository**:
```bash
git clone https://github.com/your-username/radio_restreamer.git
cd radio_restreamer
```

2. **Build the application**:
```bash
go build -o radio_restreamer
```

3. **Configure your streams** (edit `config.json`):
```json
{
  "admin_port": 9000,
  "streams": [
    {
      "name": "My Radio Station",
      "source_url": "http://stream.example.com/radio",
      "port": 8080,
      "proxy": {
        "type": "direct"
      },
      "reconnect_delay_secs": 3,
      "max_reconnect_delay_secs": 60,
      "dial_timeout_secs": 30,
      "max_listeners": 0
    }
  ]
}
```

4. **Run the application**:
```bash
./radio_restreamer
```

5. **Access the dashboard** at `http://localhost:9000`

## Configuration

### Main Configuration (config.json)

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `admin_port` | integer | Yes | - | Port for the admin dashboard (1-65535) |
| `streams` | array | Yes | - | Array of stream configurations |

### Stream Configuration

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Display name for the stream |
| `source_url` | string | Yes | - | Source radio stream URL |
| `port` | integer | Yes | - | Local port to serve the restream (1-65535) |
| `proxy.type` | string | No | `direct` | Proxy type: `direct`, `socks4`, `socks4a`, `socks5`, `http`, `https` |
| `proxy.address` | string | Conditional | - | Proxy server address (required if proxy type not `direct`) |
| `proxy.username` | string | No | - | Proxy authentication username |
| `proxy.password` | string | No | - | Proxy authentication password |
| `reconnect_delay_secs` | integer | No | 3 | Initial delay before reconnecting (seconds) |
| `max_reconnect_delay_secs` | integer | No | 60 | Maximum reconnection delay (seconds) |
| `dial_timeout_secs` | integer | No | 30 | Connection timeout (seconds) |
| `max_listeners` | integer | No | 0 | Maximum simultaneous listeners (0 = unlimited) |

### Proxy Types

- **`direct`**: Direct connection (no proxy)
- **`socks4`**: SOCKS4 proxy
- **`socks4a`**: SOCKS4a proxy (supports DNS resolution)
- **`socks5`**: SOCKS5 proxy
- **`http`**: HTTP proxy
- **`https`**: HTTPS proxy (TLS-encrypted)

## Usage

### Command Line Options

```bash
./radio_restreamer -config /path/to/config.json
```

| Option | Description | Default |
|--------|-------------|---------|
| `-config` | Path to configuration file | `config.json` |

### Listening to Streams

Once running, you can access your restreamed radio stations using:

- **VLC Media Player**: `http://localhost:PORT`
- **Web Browser**: `http://localhost:PORT`
- **Audio Players**: Any player that supports HTTP streams

### Admin Dashboard

The admin dashboard provides real-time statistics for all streams:

- **Live Listeners**: Current number of connected listeners
- **Bandwidth**: Real-time bytes per second
- **Total Relayed**: Cumulative bytes relayed since start
- **Reconnect Count**: Number of reconnection attempts
- **Connection Status**: Current stream state (connected, connecting, reconnecting, idle)

Access the dashboard at: `http://localhost:ADMIN_PORT`

## API Endpoints

### GET `/api/stats`

Returns JSON statistics for all streams:

```json
[
  {
    "name": "Example FM",
    "source_url": "http://stream.example.com/radio",
    "port": 8080,
    "status": "connected",
    "listener_count": 5,
    "total_bytes_relayed": 104857600,
    "current_bps": 128000,
    "reconnect_count": 2
  }
]
```

### GET `/`

Serves the admin dashboard HTML interface.

## Architecture

### Core Components

1. **StreamManager**: Manages individual radio stream connections
2. **Proxy Handler**: Handles proxy connections for geo-restricted streams
3. **ICY Stripper**: Removes ICY metadata blocks from audio streams
4. **Listener Handler**: Manages HTTP clients listening to streams
5. **Admin Server**: Provides monitoring and statistics

### Data Flow

1. Connects to upstream radio stream through configured proxy
2. Strips ICY metadata blocks at the relay level
3. Broadcasts clean audio data to all connected listeners
4. Maintains connection with automatic reconnection on failures
5. Provides real-time statistics via admin interface

## Building from Source

### Development Build

```bash
go build -o radio_restreamer
```

### Production Build (with optimizations)

```bash
go build -ldflags="-s -w" -o radio_restreamer
```

### Cross-Platform Builds

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o radio_restreamer-linux-amd64

# Windows
GOOS=windows GOARCH=amd64 go build -o radio_restreamer-windows-amd64.exe

# macOS
GOOS=darwin GOARCH=amd64 go build -o radio_restreamer-darwin-amd64
```

## Docker Support

### Building Docker Image

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o radio_restreamer

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/radio_restreamer .
COPY config.json .
CMD ["./radio_restreamer"]
```

### Docker Compose Example

```yaml
version: '3.8'
services:
  radio-restreamer:
    build: .
    ports:
      - "9000:9000"  # Admin dashboard
      - "8080:8080"  # Stream 1
      - "8081:8081"  # Stream 2
    volumes:
      - ./config.json:/root/config.json
    restart: unless-stopped
```

## Troubleshooting

### Common Issues

1. **"stream not yet available, please retry"**
   - The upstream stream is not connected yet
   - Check source URL and proxy configuration
   - Wait for automatic reconnection

2. **"stream at capacity"**
   - Maximum listener limit reached
   - Increase `max_listeners` in configuration or set to 0 for unlimited

3. **Connection timeouts**
   - Check proxy server availability
   - Verify network connectivity
   - Increase `dial_timeout_secs` if needed

4. **Audio artifacts or corruption**
   - ICY metadata stripping may be misconfigured
   - Check if upstream stream uses ICY metadata
   - Verify Content-Type headers

### Logging

The application provides detailed logging:
- Connection attempts and status changes
- Listener connections and disconnections
- Error conditions and reconnection attempts
- Bandwidth statistics

### Performance Monitoring

Monitor these key metrics:
- **Listener count**: Should be stable during normal operation
- **Reconnect count**: Should be low (indicates stable upstream)
- **Bandwidth**: Should match expected stream bitrate
- **Memory usage**: Should be stable over time

## Security Considerations

- **Admin Port**: Restrict access to admin dashboard port (default: 9000)
- **Proxy Credentials**: Store securely, avoid committing to version control
- **Listener Limits**: Set appropriate `max_listeners` to prevent resource exhaustion
- **Network Access**: Ensure proper firewall rules for required ports

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues and questions:
1. Check the troubleshooting section above
2. Review the configuration examples
3. Check application logs for error details
4. Open an issue on GitHub with relevant details

## Acknowledgments

- Built with Go standard library and `golang.org/x/net/proxy`
- Inspired by the need for reliable radio stream restreaming
- Community contributions and feedback welcome

---

**Note**: Ensure you have proper rights to restream any radio content and comply with applicable laws and regulations.