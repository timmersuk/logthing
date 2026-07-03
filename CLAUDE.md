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

### Docker & Release
- **Build Docker image locally**: `make docker-build IMAGE=timmersuk/logthing TAG=0.1.0`
- **Push multi-arch image to Docker Hub**: `make docker-buildx-push IMAGE=timmersuk/logthing TAG=v0.1.0`
- **Tag and push release**: `make release-tag TAG=v0.1.0` (requires clean worktree on main branch)

## Configuration Environment Variables

| Env var | Default | Description |
| --- | --- | --- |
| `LOGTHING_USERNAME` | required | Username for HTTP Basic Auth. |
| `LOGTHING_PASSWORD` | required | Password for HTTP Basic Auth. |
| `LOGTHING_HTTP_ADDR` | `:8080` | HTTP listen address for the REST API and frontend. |
| `LOGTHING_SYSLOG_UDP_ADDR` | `:5514` | UDP syslog listen address. Set to empty string to disable. |
| `LOGTHING_SYSLOG_TCP_ADDR` | `:5514` | TCP syslog listen address. Set to empty string to disable. |
| `LOGTHING_SYSLOG_FORMAT` | `automatic` | Parser format: `automatic`, `rfc5424`, `rfc3164`, or `rfc6587`. |
| `LOGTHING_DATA_DIR` | `data/messages` | Root directory for local message files. |

Port 514 normally requires elevated privileges; the default syslog port is `5514` for local development.

## REST API Endpoints

Interactive Swagger UI at `/swagger-ui/`. Raw OpenAPI spec at `/swagger.json`. All require Basic Auth except healthcheck.

| Method | Path | Query Params | Description |
| --- | --- | --- | --- |
| `GET` | `/healthcheck` | none | Basic service health response (no auth required). |
| `GET` | `/api/v1/messages` | `q`, `host`, `limit`, `offset`, `since`, `until` | Returns latest-first messages from local storage. |
| `GET` | `/api/v1/messages/stream` | none | Streams newly appended messages as Server-Sent Events (SSE). |
| `POST` | `/api/v1/messages/import` | none | Imports newline-delimited JSON (NDJSON) messages into local storage. |
| `POST` | `/api/v1/test-event` | none | Sends one server-side RFC5424 test event to the configured syslog destination. |

Query parameters:
- `host`: exact syslog hostname filter (can be repeated for multiple hosts)
- `q`: search query string
- `limit`: defaults to 200, capped at 2000
- `offset`: defaults to 0, used for paging
- `since`/`until`: RFC3339 timestamps filtering on `received_at`

### Message Response Schema

```json
{
  "data": [
    {
      "id": "f2e4a19c6f50429bbf7afc1fcb705fa6",
      "received_at": "2026-06-19T12:00:00Z",
      "timestamp": "2026-06-19T12:00:00Z",
      "transport": "udp/tcp",
      "source": "127.0.0.1:51234",
      "priority": 13,
      "facility": 1,
      "severity": 5,
      "hostname": "edge-1",
      "app_name": "router",
      "proc_id": "123",
      "msg_id": "abc",
      "tag": "router",
      "message": "link changed"
    }
  ],
  "meta": {
    "count": 1,
    "limit": 50,
    "offset": 0,
    "has_more": false
  }
}
```

### Import Format

Import accepts NDJSON in the request body. Blank lines are skipped, and invalid JSON/timestamps/facilities/severities return `400` with the failing line number. Supported fields: `timestamp`, `host`, `program`, `pid`, `severity`, `facility`, `message`. Facility and severity may be syslog names (e.g., `daemon`, `warning`) or numeric values.

## High-Level Architecture

Logthing is a single-binary Go service that receives syslog messages, stores them locally as daily newline-delimited JSON (NDJSON) files, and serves both a REST API and an embedded React frontend for inspection.

### Storage Layout

Messages are written as one JSON object per line under `LOGTHING_DATA_DIR`, partitioned by UTC date and sender source:

```text
data/messages/YYYY/MM/DD/<source>.ndjson
```

The source filename is derived from the `source` field reported by go-syslog. When it's `host:port`, the port is stripped so transient UDP source ports don't create one file per datagram. Characters unsafe for file paths are replaced with `_`, and messages without a source use `unknown`. Query reads scan all matching source files and return messages in latest-first order.

### Go Backend (`internal/`)
- `syslog/`: Wraps `go-syslog.v2` to receive and parse incoming UDP/TCP syslog packets. Also includes the CLI sender tool (`cmd/syslogsend`).
- `storage/`: Append-only local storage (NDJSON). Writes messages partitioned by date/source; scans files for API queries.
- `model/`: Shared data structures, mainly the core `Message` representation.
- `api/`: REST API handlers with HTTP Basic Auth middleware. Exposes `/api/v1/*` endpoints including SSE streaming and NDJSON import.
- `realtime/`: Server-Sent Events (SSE) hub for streaming newly arrived messages to connected UI clients.
- `web/`: Uses `go:embed` to serve the Vite-built frontend assets (`internal/web/dist`).
- `config/`: Application configuration, sourced exclusively from environment variables.
- `openapi/` & `swaggerui/`: Embedded Swagger UI and OpenAPI specifications at `/swagger-ui/` and `/swagger.json`.

### Frontend (`frontend/`)
- A Vite + React application.
- Uses polling and SSE (`/api/v1/messages/stream`) to display live syslog updates in a scrollable table.
- `vite.config.ts` outputs the build directly into `internal/web/dist` so it can be compiled into the Go binary.
- Live updates work only on page 1; other pages use manual pagination via query params.