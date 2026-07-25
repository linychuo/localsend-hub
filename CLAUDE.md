# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

LocalSend Hub is a Go receiver for the LocalSend protocol v2. It runs as **two independent processes** that share state through files on disk — there is no in-process communication between them.

- `main.go` → core service (HTTPS, port 53317, file reception + multicast discovery)
- `cmd/admin/main.go` → admin service (HTTP, port 53318, web UI + settings)

Both binaries are built from the same `internal/` packages but instantiated separately. The Docker `entrypoint.sh` launches both in the same container; `wait -n` exits the container if the core service dies, but keeps the core running if only the admin service dies.

## Build & Run

```bash
# Build both binaries
go build -o localsend-hub . && go build -o localsend-hub-admin ./cmd/admin

# Run locally (use two terminals — they are separate processes)
./localsend-hub         # core
./localsend-hub-admin   # admin UI at http://localhost:53318

# Tests (only the state package has tests today)
go test ./internal/state/...

# Docker (builds + runs both services)
docker compose up -d
```

The build uses `CGO_ENABLED=0` and the pure-Go `modernc.org/sqlite` driver — no C toolchain needed. Binaries are ~9MB static.

## Architecture

### Cross-process state sharing

The two processes coordinate through three on-disk artifacts, not through memory:

1. **`localsend_config.json`** (path from `state.GetConfigPath()`: `LOCALSEND_CONFIG_PATH` env → `/app/config/...` if `/app/config` exists → cwd). The core service writes it **on change** via `State.Save()` (triggered by `SetDeviceIdentity`/`SetReceiveDir`); the admin service **polls** it every 2s (`admin_state.go` `watchInterval`) and reloads. Admin writes go through the same file — there is no API call from admin to core.
2. **`localsend_logs.db`** (SQLite, path from `db.GetDBPath()`). The core service owns the schema and writes transfer logs; the admin service opens it **read-only** to display logs. If the admin starts before the core, log loading silently fails and retries on next request.
3. **`received/`** directory — written only by core, read by admin for the file list/download endpoints.

When changing config-handling code, remember: a setting edited in the admin UI is not seen by the core until the core's next 2s polling tick (or restart for env-var-only settings). Env vars (`LOCALSEND_*`) override config file values **at core startup only** — see `applyEnvOverrides` in `internal/state/state.go`.

### Core service (`internal/core/server.go`)

- Generates an RSA-2048 self-signed TLS cert **in memory** at startup (`generateCert`). The device `fingerprint` reported to LocalSend clients is `SHA-256(certificate DER)` uppercased hex — it is **not stable across restarts** because the cert is regenerated each run.
- Implements LocalSend v2 endpoints under `/api/localsend/v2/...` (plus v1 aliases for `info`/`register`). See README for the full table.
- Upload flow: `prepare-upload` → `State.RegisterSession` (stores `FileMeta` + tokens) → `upload` validates token, streams body through `CancellableReader` so `/cancel` can interrupt an in-flight transfer.
- Files are saved to `{ReceiveDir}/{senderFingerprint}/YYYY/MM/{filename}`. The sender fingerprint comes from `req.Info["fingerprint"]` in the prepare-upload request. Duplicates are renamed with a `_timestamp` suffix. `filepath.Base()` is applied to all incoming filenames to prevent path traversal.

### Admin service (`internal/admin/server.go`)

- HTTP server on `0.0.0.0:53318` (intentionally LAN-accessible — there is no auth).
- Web assets in `internal/admin/web/` are embedded via `go:embed` (vanilla HTML/CSS/JS, no framework, no build step).
- Reads the SQLite log DB read-only. Writes to config go to the JSON file and rely on the core's polling to pick them up.

### State packages (`internal/state/`)

- `state.go` — `State` struct, used **only by core**. Holds in-memory session/token/cancel maps that are not persisted.
- `admin_state.go` — `AdminState` struct, used **only by admin**. Polls config file.
- `shared.go` — `ConfigData` and `LogEntry` types used by both.
- `persistence.go` — `GetConfigPath()` + file load/save for `State`.
- `admin_provider.go` — interface abstraction so admin handlers don't depend on the core `State` type.

When adding a new config field: update `ConfigData` in `shared.go`, the save snapshot in `persistence.go`, the load logic in both `state.go` (`loadFromFile`) and `admin_state.go` (`loadFromConfigFile`), and the env-var override in `applyEnvOverrides` if it should be env-overridable.

## Conventions

- Comments throughout the codebase are in Chinese; match the surrounding style when editing.
- All shared mutable state is guarded by `sync.Mutex` — preserve this when adding fields to `State`/`AdminState`.
- The `received/`, `data/`, `localsend_config.json`, `*.db` paths are gitignored. Don't commit generated files.
- Branch model: `master` is the current Go implementation; `java-legacy` is archived.
