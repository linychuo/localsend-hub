The user wants a comprehensive project summary based on our conversation. Let me compile all the important context, decisions, and progress into a markdown summary.# Project Summary

## Overall Goal
Refactor LocalSend Hub from a single-process architecture into a **dual-service architecture** where the core file reception service and admin management service run as independent processes, providing fault isolation so that admin service crashes do not interrupt file transfers.

## Key Knowledge

### Architecture
- **Core Service** (`main.go`, port 53317 HTTPS): Handles LocalSend protocol (multicast discovery, TLS cert generation, file reception)
- **Admin Service** (`cmd/admin/main.go`, port 53318 HTTP): Web dashboard for logs, config, file management
- **Cross-process communication**: Shared JSON config file (`localsend_config.json`), admin polls every 2s for changes
- **Fault isolation**: If admin crashes → core continues receiving files. If core crashes → Docker container exits (monitored by `entrypoint.sh`)

### Technology Stack
- Go 1.21+, standard library only (`net/http`, `crypto/tls`, `embed`)
- Frontend: Vanilla JS + CSS via `go:embed`
- Docker: Alpine 3.19, `dumb-init` for PID 1 process management, multi-stage build (~15MB image)

### State Package Design
| File | Purpose |
|------|---------|
| `internal/state/state.go` | Core service state (includes sessions map) |
| `internal/state/admin_state.go` | Admin service state with config file watcher |
| `internal/state/shared.go` | Shared types: `LogEntry`, `ConfigData` |
| `internal/state/admin_provider.go` | `AdminStateProvider` interface (both implement) |

### Project Structure
```
cmd/              # Each subdir = one binary
  admin/main.go   # Admin service entry
internal/         # Private packages
  state/          # State management (dual design)
  discovery/      # UDP multicast
  core/           # HTTPS server
  admin/          # Admin HTTP server (uses AdminStateProvider interface)
main.go           # Core service entry
Dockerfile        # Builds both binaries
docker-compose.yml # Runs both services in one container
entrypoint.sh     # Starts + monitors both processes
```

### Configuration Priority
1. Code defaults → 2. Config file → 3. Environment variables (highest)

### Build Commands
```bash
go build -o localsend-hub .                    # Core service
go build -o localsend-hub-admin ./cmd/admin    # Admin service
docker compose up -d                           # Docker (starts both)
```

### User Preferences
- User prefers minimal, essential files only ("锦上添花" files should be removed)
- Removed: `CASAOS_GUIDE.md`, `DOCKER_GUIDE.md`, `README_DOCKER.md`, `USAGE_GUIDE.md`, `build.sh`, `docker.sh`
- Retained: `README.md`, `QWEN.md`, `DESIGN.md`, `requirments.md`, `Dockerfile`, `docker-compose.yml`, `entrypoint.sh`
- All CasaOS references purged from the project

## Recent Actions

1. **[DONE]** Refactored state package — split into `State` (core) and `AdminState` (admin) with shared types and `AdminStateProvider` interface
2. **[DONE]** Created `cmd/admin/main.go` — standalone admin binary
3. **[DONE]** Updated `main.go` — core service only (removed admin startup)
4. **[DONE]** Updated `internal/admin/server.go` — uses `AdminStateProvider` interface instead of concrete `*state.State`
5. **[DONE]** Updated `Dockerfile` — builds both binaries, uses `dumb-init`
6. **[DONE]** Updated `entrypoint.sh` — starts both services as background processes, monitors exits
7. **[DONE]** Updated `docker-compose.yml` — health check targets core HTTPS, removed CasaOS labels
8. **[DONE]** Deleted redundant files — CasaOS guides, Docker guides, convenience scripts (build.sh, docker.sh)
9. **[DONE]** Rewrote `DESIGN.md` and `requirments.md` — updated for dual-service architecture
10. **[DONE]** Committed and pushed to remote (`master` branch, commit `63eeead`)

## Current Plan

All refactoring tasks are **DONE**. The project is in a clean, working state with:
- Two independently compilable binaries
- Docker image containing both services in one container
- Fault isolation between core and admin services
- Minimal, clean documentation (no CasaOS, no redundant files)

No immediate next steps. Awaiting user feedback or new feature requests.

---

## Summary Metadata
**Update time**: 2026-04-12T10:52:04.236Z 
