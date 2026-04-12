# LocalSend Hub - LocalSend Receiver

> **High-performance, production-ready LocalSend protocol receiver** built in Go. Receive files from any LocalSend-compatible device (iOS, Android, Windows, macOS, Linux) on your NAS or server seamlessly.

[![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/docker-supported-2496ED?logo=docker)](Dockerfile)
[![Binary Size](https://img.shields.io/badge/binary%20size-~9MB-green)](README.md)
[![Docker Image](https://github.com/linychuo/localsend-hub/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/linychuo/localsend-hub/pkgs/container/localsend-hub)

## ✨ Features

- **🔐 Native HTTPS** — Auto-generates RSA-2048 self-signed TLS certificates at startup. No external reverse proxy needed.
- **📡 UDP Multicast Discovery** — Automatically announces availability to the local network using LocalSend protocol v2.
- **📦 Seamless File Reception** — Stream-based file reception with automatic duplicate renaming (`file_timestamp.ext`).
- **🛡️ Admin Dashboard** — Modern web UI at `0.0.0.0:53318` for monitoring transfers, managing files, and configuring settings.
- **♻️ Resource Efficient** — Capped log buffer (1000 entries), ~9MB static binary, ~10MB memory footprint.
- **🐳 Docker Ready** — Multi-stage build with health checks, resource limits, and volume persistence.
- **🚀 Zero Dependencies** — Single static binary with no runtime dependencies. Deploy anywhere.

## 🚀 Quick Start

### Option 1: Run Binary Directly

```bash
# Build from source
go build -o localsend-hub . && go build -o localsend-hub-admin ./cmd/admin

# Run core service (file reception)
./localsend-hub

# Run admin service (management UI) - in another terminal
./localsend-hub-admin
```

### Option 2: Docker

```bash
# Build and run (starts both services)
./docker.sh
# or: docker compose up -d

# Access admin panel
open http://localhost:53318
```

### Option 3: Docker Compose (Recommended)

```bash
docker compose up -d
```

This mounts `./data/received` and `./data/config` for persistent storage.

## 📋 Default Configuration

| Setting | Value | Description |
|---------|-------|-------------|
| **Core Port** | `53317` (HTTPS) | LocalSend receiver endpoint |
| **Admin Port** | `53318` (HTTP) | Management dashboard (accessible from LAN) |
| **Device Alias** | `LocalSend Hub` | Visible name on local network |
| **Device Type** | `server` | Reported to LocalSend clients |
| **Receive Directory** | `./received` | Where files are saved |
| **Multicast Address** | `224.0.0.167:53317` | LocalSend discovery protocol |
| **Max Logs** | `1000` | Ring buffer, oldest dropped |

## 🏗️ Architecture

LocalSend Hub uses a **dual-service decoupled architecture** for fault isolation:

```
                   ┌───────────────────────────────────────────┐
                   │           Container / Host                │
                   │                                           │
  ┌────────────┐   │  ┌──────────────┐      ┌──────────────┐   │
  │ LocalSend  │   │  │ Core Service │      │ Admin Service│   │
  │ Clients    │◄──┤  │ Port 53317   │      │ Port 53318   │   │
  │ (Mobile)   │   │  │ (HTTPS)      │      │ (HTTP/Local) │   │
  └────────────┘   │  │              │      │              │   │
                   │  │ - TLS Gen    │      │ - Web UI     │   │
        ┌──────────┤  │ - File Save  │      │ - API        │   │
        │          │  │ - Multicast  │      │ - Config     │   │
        ▼          │  └──────────────┘      └──────────────┘   │
  Shared Config File            ┌──────────────────┐            │
  (localsend_config.json)       │ Shared SQLite DB │            │
  - Config  - Device Identity   │ (Transfer Logs)  │            │
                                └──────────────────┘            │
└───────────────────────────────────────────────────────────────┘
```

**Key Benefits:**
- **Fault Isolation**: If Admin Service crashes, Core Service continues receiving files
- **Independent Scaling**: Each service can be deployed/scaled separately
- **Clean Separation**: Clear boundaries between file reception and management concerns
- **Consistent Logs**: Both services share the same SQLite database, no stale data

## 📁 Project Structure

```
.
├── main.go                     # Core service entry point
├── cmd/
│   └── admin/
│       └── main.go             # Admin service entry point
├── internal/                   # Private implementation (not importable)
│   ├── state/                  # 💾 Thread-safe config, sessions
│   │   ├── state.go            # Core service state management
│   │   ├── admin_state.go      # Admin service state management
│   │   ├── shared.go           # Shared types (LogEntry, ConfigData)
│   │   └── admin_provider.go   # Interface for cross-process state
│   │   └── persistence.go      # JSON config file I/O
│   ├── db/                     # 🗄️ SQLite database layer
│   │   └── logdb.go            # Transfer logs persistence
│   ├── discovery/              # 📡 UDP multicast announcer
│   │   └── multicast.go        # Periodic network discovery
│   ├── core/                   # 🌐 HTTPS server + LocalSend handlers
│   │   └── server.go           # TLS cert gen, API endpoints
│   └── admin/                  # 🛡️ Admin panel + embedded web UI
│       ├── server.go           # HTTP server with go:embed
│       └── web/                # Frontend assets
│           ├── index.html      # Dashboard HTML
│           ├── style.css       # Dark/light theme CSS
│           └── app.js          # Vanilla JS frontend
├── Dockerfile                  # Multi-stage Docker build (both binaries)
├── docker-compose.yml          # Docker Compose configuration
├── entrypoint.sh               # Docker entrypoint (starts both services)
├── go.mod                      # Module definition
└── received/                   # [Git-ignored] File storage directory
```

## 🔌 API Endpoints

### Core API (Port 53317 - HTTPS)

Implements the [LocalSend Protocol v2](https://github.com/localsend/protocol).

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/localsend/v2/info` | `GET` | Device info (JSON) |
| `/api/localsend/v2/register` | `POST` | Sender registration (returns info) |
| `/api/localsend/v2/prepare-upload` | `POST` | Session preparation (returns session ID + tokens) |
| `/api/localsend/v2/upload` | `POST` | File upload (octet-stream) |

### Admin API (Port 53318 - HTTP, LAN Accessible)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | `GET` | Admin dashboard |
| `/api/logs` | `GET` | Transfer logs (reverse chronological) |
| `/api/logs` | `DELETE` | Clear all logs |
| `/api/identity` | `GET` | Current device identity |
| `/api/identity` | `POST` | Update alias/model/type |
| `/api/config` | `POST` | Update receive directory |
| `/api/files` | `GET` | List received files with metadata |
| `/files/{filename}` | `GET` | Download a received file |

## 🔧 Configuration

### Configuration Priority

LocalSend Hub uses a simple layered configuration:

1. **Built-in defaults** — loaded first
2. **Config file** — overrides defaults (if file exists)
3. **Environment variables** — overrides everything (if set at startup)

Settings changed via the Admin UI are automatically saved to the config file. Environment variables take effect at container startup and override config file values.

### Environment Variables (Docker)

| Variable | Default | Description |
|----------|---------|-------------|
| `LOCALSEND_RECEIVE_DIR` | `/app/received` | Directory to save received files |
| `LOCALSEND_PORT` | `53317` | HTTPS listener port |
| `LOCALSEND_ADMIN_PORT` | `53318` | Admin panel port |
| `LOCALSEND_DEVICE_NAME` | `LocalSend Hub` | Device name shown to senders |
| `LOCALSEND_DEVICE_TYPE` | `server` | Device type |
| `LOCALSEND_MAX_LOGS` | `1000` | Max log entries |
| `LOCALSEND_CONFIG_PATH` | *(auto)* | Custom config file path (optional) |

### Config File

LocalSend Hub persists configuration to `localsend_config.json`. In Docker, the default path is `/app/config/localsend_config.json` (mounted via volume). The file is auto-created on first run and saved every 15 seconds or on config change.

Transfer logs are stored in a separate SQLite database (`localsend_logs.db`), ensuring real-time consistency between both services without file polling.

```json
{
  "receiveDir": "./received",
  "corePort": 53317,
  "adminPort": 53318,
  "alias": "LocalSend Hub",
  "deviceModel": "LocalSend Hub Server",
  "deviceType": "server",
  "maxLogs": 1000
}
```

## 🛡️ Security

| Feature | Implementation |
|---------|----------------|
| **Transport Encryption** | RSA-2048 self-signed TLS (auto-generated, 10-year validity) |
| **Device Fingerprint** | SHA-256 hash of TLS certificate DER bytes |
| **Path Traversal Prevention** | `filepath.Base()` strips directory components from filenames |
| **Admin Panel Isolation** | Binds to `0.0.0.0` — accessible from LAN; consider adding auth for remote access |
| **Thread Safety** | All shared state protected by `sync.Mutex` |

## 🐳 Docker Deployment

### GitHub Container Registry (Recommended)

Pull the official image from GitHub Container Registry:

```bash
# Pull latest stable release
docker pull ghcr.io/linychuo/localsend-hub:latest

# Or pull a specific version
docker pull ghcr.io/linychuo/localsend-hub:v1.0.0
```

### Quick Deploy

```bash
# Using official image from GHCR
docker run -d \
  --name localsend-hub \
  --network host \
  -v $(pwd)/data/received:/app/received \
  -v $(pwd)/data/config:/app/config \
  -v $(pwd)/data/sqlite:/app/data \
  ghcr.io/linychuo/localsend-hub:latest
```

Or using Docker Compose:

```bash
docker compose up -d
```

### Manual Build

```bash
docker build -t localsend-hub .
docker run -d \
  --name localsend-hub \
  -p 53317:53317 \
  -p 53318:53318 \
  -v $(pwd)/data/received:/app/received \
  -v $(pwd)/data/config:/app/config \
  -v $(pwd)/data/sqlite:/app/data \
  localsend-hub
```

### LAN Discovery

For multicast to work across your local network, use `network_mode: host` in Docker Compose:

```yaml
services:
  localsend-hub:
    network_mode: host
    volumes:
      - ./data/received:/app/received
      - ./data/config:/app/config
      - ./data/sqlite:/app/data
```

> ⚠️ **Note**: `network_mode: host` removes network isolation. Only use on trusted networks.

### Health Check

The container includes an automatic health check that verifies the admin API is responsive every 30 seconds.

## 🔍 Troubleshooting

### Device Not Discovered on LAN

- Ensure UDP port `53317` is not blocked by firewall rules.
- If using Docker, verify multicast routing with `network_mode: host`.
- Some routers block multicast; check your network configuration.

### Admin Panel Not Accessible

- The admin panel binds to `0.0.0.0:53318` and is accessible from LAN.
- For secure remote access, use SSH tunneling: `ssh -L 53318:localhost:53318 user@server`
- Or add authentication (Basic Auth) before exposing to untrusted networks.

### Files Not Saving

- Verify the `received/` directory exists and has write permissions.
- Check logs via admin panel: `http://localhost:53318`

## 📊 Resource Usage

| Metric | Value |
|--------|-------|
| Binary Size | ~9 MB |
| Memory (Idle) | ~10 MB |
| CPU (Idle) | < 0.1% |
| Docker Image | ~15 MB (Alpine-based) |
| Startup Time | < 100 ms |

## 🗺️ Roadmap

| Feature | Status |
|---------|--------|
| SHA-256 file integrity verification | 🔲 Planned |
| PIN-based authentication | 🔲 Planned |
| Session TTL expiration | 🔲 Planned |
| Rate limiting | 🔲 Planned |
| Concurrent upload limits | 🔲 Planned |
| Atomic file writes (temp + rename) | 🔲 Planned |
| Cloud storage backend | 🔲 Planned |
| mDNS/Bonjour discovery | 🔲 Planned |

See [requirements.md](requirments.md) for detailed feature specifications.

## 📚 References

- [LocalSend Protocol Specification](https://github.com/localsend/protocol)
- [Architecture Design Document](DESIGN.md)
- [Feature Requirements](requirments.md)

## 📄 License

MIT License. See [LICENSE](LICENSE) for details.

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Commit your changes (`git commit -m 'Add my feature'`)
4. Push to the branch (`git push origin feature/my-feature`)
5. Open a Pull Request

## 🙏 Acknowledgments

- [LocalSend](https://github.com/localsend/localsend) — For the open protocol specification
- Go standard library — For the robust `net/http`, `crypto/tls`, and `net` packages
