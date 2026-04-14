# LocalSend Hub - LocalSend Receiver (Go Implementation)

## Project Overview

LocalSend Hub is a high-performance, production-ready **LocalSend protocol receiver** implemented in **Go**. It allows devices on the local network to send files to this NAS/Server seamlessly.

The project features a **dual-service architecture**:
1.  **Core Service (Port 53317, HTTPS)**: Handles LocalSend discovery (multicast) and file reception.
2.  **Admin Service (Port 53318, HTTP)**: A local-only web management console for viewing logs and changing settings.

These two services run as **separate processes** for fault isolation. If the admin service crashes, file transfers continue uninterrupted.

### Key Features
- **🔐 Native HTTPS**: Generates self-signed RSA-2048 certificates at startup. No external proxy needed.
- **📡 UDP Multicast**: Announces availability to the local network automatically.
- **📦 File Reception**: Saves incoming files with auto-rename support for duplicates. Files are organized into `{sender_fingerprint}/YYYY/MM/` subdirectories based on the sender device fingerprint and file's modified time metadata.
- **🛡️ Admin Console**: A modern web UI running on `0.0.0.0:53318` (LAN accessible) to view transfer logs and manage directories.
- **♻️ Auto-Cleanup**: Logs are capped (e.g., 1000 entries) to prevent memory leaks.
- **🚀 Single Binary**: Compiles to a static binary (~9MB) with zero dependencies.
- **🐳 Docker Ready**: Multi-stage build with health checks, resource limits, and volume persistence.
- **📦 GitHub Packages**: Official Docker images published to GitHub Container Registry (ghcr.io).

## Branches

| Branch | Description |
|--------|-------------|
| **master** | **Go implementation** (Current, Production-ready). |
| **java-legacy** | Legacy Java implementation (Archived). |

## Technology Stack

- **Language**: Go 1.25+
- **Standard Libs**: `crypto/tls`, `crypto/x509`, `net/http`, `net`, `embed`.
- **UI**: Embedded HTML/CSS/JS via `go:embed` (No external frontend framework, uses Vanilla JS).
- **Architecture**: Internal packages (`internal/`) for strict encapsulation.

## Default Configuration

| Setting | Value |
|---------|-------|
| **Core Port** | `53317` (HTTPS) |
| **Admin Port** | `53318` (HTTP, LAN accessible) |
| **Device Alias** | `LocalSend Hub` |
| **Device Type** | `server` |
| **Multicast** | `224.0.0.167:53317` |
| **Default Dir** | `./received` |
| **File Storage** | `received/{sender_fingerprint}/YYYY/MM/` (by sender fingerprint and file modified time) |
| **Max Logs** | `1000` (ring buffer) |

## Building and Running

### Option 1: Build Binaries

```bash
# Using build script (supports nix-shell fallback)

# Or compile manually
go build -o localsend-hub .
go build -o localsend-hub-admin ./cmd/admin

# Run core service (file reception)
./localsend-hub

# Run admin service (management UI) - in another terminal
./localsend-hub-admin
```

### Option 2: Docker

```bash
# Docker Compose (recommended) - runs both services in one container
docker compose up -d

# Or manual build
./docker.sh
```

## Project Structure

```text
.
├── main.go                     # Core service entry point
├── cmd/
│   └── admin/
│       └── main.go             # Admin service entry point
├── go.mod                      # Module definition
├── internal/                   # Private implementation details
│   ├── state/                  # 💾 Global config, thread-safe state
│   │   ├── state.go            # Core service state management
│   │   ├── admin_state.go      # Admin service state management
│   │   ├── shared.go           # Shared types (LogEntry, ConfigData)
│   │   ├── admin_provider.go   # Interface for cross-process state
│   │   └── persistence.go      # JSON config file I/O
│   ├── db/                     # 🗄️ SQLite database layer
│   │   └── logdb.go            # Transfer logs persistence
│   ├── discovery/              # 📡 UDP Multicast broadcasting
│   │   └── multicast.go        # Periodic network discovery
│   ├── core/                   # 🌐 HTTPS Server, TLS Cert Gen, LocalSend Handlers
│   │   └── server.go           # TLS cert gen, API endpoints
│   └── admin/                  # 🛡️ Admin Panel Server & Embedded Web UI
│       ├── server.go           # HTTP server with go:embed
│       ├── ui.go               # Legacy placeholder (no-op)
│       └── web/                # Professional web UI files
│           ├── index.html      # Main dashboard HTML
│           ├── style.css       # Professional dark/light theme CSS
│           └── app.js          # Frontend JavaScript logic
├── Dockerfile                  # Multi-stage Docker build (both binaries)
├── docker-compose.yml          # Unified Docker Compose
├── entrypoint.sh               # Docker entrypoint (starts both services)
├── localsend-hub                 # [Ignored] Core service binary (not in git)
├── localsend-hub-admin           # [Ignored] Admin service binary (not in git)
├── received/                   # [Ignored] Received files storage (not in git)
├── localsend_config.json         # [Ignored] Auto-generated config file (settings only)
├── localsend_logs.db*            # [Ignored] SQLite transfer logs database (not in git)
├── DESIGN.md                   # Architecture design doc
└── requirments.md              # Feature requirements
```

## API Endpoints

### Core API (Port 53317 - HTTPS)
| Endpoint | Method | Query Params | Body | Response |
|----------|--------|-------------|------|----------|
| `/api/localsend/v2/info` | GET | — | — | `{alias, version, deviceModel, deviceType, fingerprint, download}` |
| `/api/localsend/v2/register` | POST | — | `{alias, version, ...}` | `{alias, version, deviceModel, deviceType, fingerprint, download}` |
| `/api/localsend/v2/prepare-upload` | POST | `?pin=xxx` (optional) | `{info, files: {id: {id, fileName, size, fileType, sha256, preview, metadata}}}` | `{sessionId, files: {fileId: token}}` |
| `/api/localsend/v2/upload` | POST | `sessionId`, `fileId`, `token` | Binary (Octet-Stream) | 200 (no body) |
| `/api/localsend/v2/cancel` | POST | `sessionId` | — | 200 (no body) |

### Admin API (Port 53318 - HTTP, LAN Accessible)
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Serve Admin Web UI |
| `/api/logs` | GET / DELETE | View or clear transfer logs |
| `/api/identity` | GET / POST | Get/update device identity |
| `/api/config` | POST | Update receive directory |
| `/api/files` | GET | List received files with metadata |
| `/files/{filename}` | GET | Download a received file |

## Development Conventions

- **Internal Packages**: Code in `internal/` cannot be imported by external modules (Go enforces this).
- **Separated Services**: Core and Admin services run as separate processes for fault isolation.
- **Cross-Process State**: Services share a SQLite database (`localsend_logs.db`) for transfer logs, ensuring real-time consistency. Settings are stored in a shared JSON config file (`localsend_config.json`).
- **Concurrency**: Uses Goroutines for background tasks (Multicast loop, Servers, config file watcher).
- **Thread Safety**: `sync.Mutex` used in both `State` and `AdminState` to protect shared data.
- **Config Persistence**: Auto-saved every 15 seconds (core) and on admin config changes. The admin service watches for file modifications.
- **Database**: Pure Go SQLite (`modernc.org/sqlite`) with WAL mode for concurrent read/write support.

## References

- [README.md](README.md) - Main project documentation
- Requirements: `requirments.md`
- Design: `DESIGN.md`
- LocalSend Protocol: https://github.com/localsend/protocol
