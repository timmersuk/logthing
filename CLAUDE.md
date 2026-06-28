# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commonly Used Commands

### Build & Run
- **Build frontend**: `pnpm --dir frontend build` (must be done before building/running the Go server, as assets are embedded)
- **Install frontend deps**: `pnpm --dir frontend install`
- **Build binaries**: `make build` (builds `frontend`, then compiles `bin/logthing` and `bin/syslogsend`)
- **Run frontend dev server**: `pnpm --dir frontend dev`
- **Run the service locally**: `go run ./cmd/server` (Note: requires `LOGTHING_USERNAME` and `LOGTHING_PASSWORD` environment variables to be set)
- **Send a test syslog message**: `make syslogsend` (or use `go run ./cmd/syslogsend -network udp -addr 127.0.0.1:5514 -message "test"`)
- **Run with Docker Compose**: `make compose-up` (sets up service with a local storage bind mount)

### Testing
- **Run all tests**: `make test` (builds the frontend first since the Go server embeds `internal/web/dist`)
- **Run a specific Go package's tests**: `go test ./internal/storage`
- **Run a single Go test**: `go test -run ^TestFileStore_Append$ ./internal/storage`

## High-Level Architecture

Logthing is a single-binary Go service that receives syslog messages, stores them locally as daily newline-delimited JSON (NDJSON) files, and serves both a REST API and an embedded React frontend for inspection.

### Go Backend (`internal/`)
- `syslog/`: Wraps `go-syslog.v2` to receive and parse incoming UDP/TCP syslog packets.
- `storage/`: Append-only local storage. Writes messages to disk partitioned by UTC date and source (`data/messages/YYYY/MM/DD/<source>.ndjson`). Scans these files to answer API queries.
- `model/`: Shared data structures, mainly the core `Message` representation.
- `api/`: REST API handlers (`/api/v1/messages`, `import`, `test-event`). Secured by HTTP Basic Auth.
- `realtime/`: Server-Sent Events (SSE) hub for streaming newly arrived messages to connected UI clients.
- `web/`: Uses `go:embed` to serve the Vite-built frontend assets (`internal/web/dist`).
- `config/`: Application configuration, sourced exclusively from environment variables.
- `openapi/` & `swaggerui/`: Embedded Swagger UI and OpenAPI specifications.

### Frontend (`frontend/`)
- A Vite + React application.
- Uses polling and SSE (`/api/v1/messages/stream`) to display live syslog updates in a scrollable table.
- `vite.config.ts` outputs the build directly into `internal/web/dist` so it can be compiled into the Go binary.