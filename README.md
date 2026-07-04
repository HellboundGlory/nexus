# Nexus

A single, self-hosted binary that replaces **Prowlarr + Sonarr + Radarr** with one
unified engine and interface. Nexus manages TV series, movies, and the indexers and
download clients that feed them — the combined job of those three tools — behind one
REST + WebSocket API on one port.

This is a **full reimplementation**, not a frontend aggregator: Nexus owns its own
engine and does not require the upstream *arr apps to run.

> **Status: active development.** The backend engine is being built sub-project by
> sub-project. Foundation, the indexer engine, and download clients are complete and
> merged; media management, automation, and the web UI are still to come (see
> [Roadmap](#roadmap)). There is no tagged release yet — expect the schema and API to
> change.

## Features

**Working today (backend):**

- **Indexer engine** — Newznab/Torznab clients (Usenet + torrent), capability
  discovery with caching, concurrent search fan-out across indexers (dedupe + sort),
  per-indexer rate limiting, and a scheduled health check that reports status live over
  WebSocket.
- **Download clients** — SABnzbd (Usenet) and qBittorrent (torrent). Nexus performs
  **server-side grab**: it fetches `.nzb`/`.torrent` bytes itself (so an indexer's API
  key never leaves the server) while magnet links pass through untouched. Releases are
  routed to a client by protocol + priority, with an explicit client override. A queue
  monitor polls each client (~1 min) and streams `download.status` events over
  WebSocket.
- **Foundation** — SQLite persistence with versioned migrations, an in-process event
  bus, a background command/scheduler runner, single-admin authentication (session
  cookie or API key), structured logging with rotation, and an embedded single-page-app
  shell served on the same port as the API.

**Planned:** media library management, automated search/grab/import automation, and the
full React web UI. See the [Roadmap](#roadmap).

## Architecture

Nexus is a **modular monolith**: one Go binary composed of internal modules that
**never import each other directly**. They communicate through an in-process event bus
and a shared database. External integrations (indexers, download clients) sit behind
**provider interfaces** that are compiled in — adding a new one means implementing an
interface, not touching the core.

Module boundary rule: everything under `internal/<feature>` may depend only on
`internal/core/*`, never on a sibling feature module.

| Layer     | Choice                                             |
| --------- | -------------------------------------------------- |
| Language  | Go (1.26+)                                          |
| HTTP      | `go-chi/chi` router; REST + `gorilla/websocket`     |
| Database  | SQLite via `modernc.org/sqlite` (pure Go, no CGO)   |
| Auth      | bcrypt password hashing (`golang.org/x/crypto`)     |
| Logging   | `log/slog` with `lumberjack` rotation               |
| Packaging | single static binary; UI embedded via `embed.FS`    |

Because the SQLite driver is pure Go, Nexus builds with `CGO_ENABLED=0` and
cross-compiles cleanly to a single static binary.

## Project layout

```
cmd/nexus/                  Composition root (main): wires modules, starts the server
internal/core/
  api/                      HTTP router, auth middleware, WebSocket hub
  auth/                     Sessions + password hashing
  command/                  Background command runner + manager
  config/                   Environment-driven configuration
  database/                 SQLite open + migrations (0001_init, 0002_indexers, 0003_download_clients)
  events/                   In-process event bus (Publish / PublishAsync / Subscribe)
  provider/                 Provider interfaces (Indexer, DownloadClient, ...)
  scheduler/                Periodic command scheduling
  store/                    Data-access layer over SQLite
  version/                  Build version
internal/indexer/           Indexer engine (Newznab/Torznab, search, health, REST)
internal/downloadclient/    SABnzbd + qBittorrent clients, grab, routing, queue monitor, REST
internal/media/             Media management (placeholder — sub-project 4)
web/                        Embedded SPA shell (dist/index.html placeholder — full UI is sub-project 6)
docs/superpowers/           Design specs and implementation plans
```

## Getting started

### Requirements

- Go **1.26** or newer

### Build & run

```bash
# Build a static binary
make build          # → ./nexus   (CGO_ENABLED=0 go build -o nexus ./cmd/nexus)

# Or run directly
make run            # go run ./cmd/nexus
```

By default Nexus listens on `0.0.0.0:9494` and stores its database and logs under
`./data`. On first start it creates the initial admin user; if `NEXUS_ADMIN_PASSWORD`
is not set, a random password is generated and written to the log. If `NEXUS_API_KEY`
is not set, a random API key is generated at startup.

### Run the tests

```bash
make test           # go test ./...
```

The race detector requires a C toolchain and is therefore unavailable in a
`CGO_ENABLED=0` setup; verify concurrency with `go test -count=N` instead.

## Configuration

All configuration is via environment variables:

| Variable               | Default     | Description                                              |
| ---------------------- | ----------- | ------------------------------------------------------- |
| `NEXUS_DATA_DIR`       | `./data`    | Directory for the SQLite database and log files.        |
| `NEXUS_HOST`           | `0.0.0.0`   | Bind address.                                           |
| `NEXUS_PORT`           | `9494`      | Listen port.                                            |
| `NEXUS_URL_BASE`       | *(empty)*   | URL path prefix for reverse-proxy subpath hosting.      |
| `NEXUS_LOG_LEVEL`      | `info`      | Log level (`debug`, `info`, `warn`, `error`).           |
| `NEXUS_API_KEY`        | *(random)*  | API key for `X-Api-Key` auth. Generated if unset.       |
| `NEXUS_ADMIN_USER`     | `admin`     | Initial admin username (used only on first start).      |
| `NEXUS_ADMIN_PASSWORD` | *(random)*  | Initial admin password. Logged if generated.            |

## API overview

One port serves both the API and the embedded UI.

- `GET  /health` — liveness (unauthenticated).
- `POST /api/v1/auth/login`, `POST /api/v1/auth/logout` — session cookie auth.

Everything below `/api/v1` (other than login/logout) requires authentication —
either the session cookie or the `X-Api-Key` header:

- `GET  /api/v1/system/status` — version and system info.
- `GET  /api/v1/ws` — WebSocket stream (task/indexer/download status events).
- **Indexers:** `GET|POST /api/v1/indexer`, `GET /api/v1/indexer/schema`,
  `POST /api/v1/indexer/test`, `GET|PUT|DELETE /api/v1/indexer/{id}`,
  `POST /api/v1/indexer/{id}/test`, `GET /api/v1/search`.
- **Download clients:** `GET|POST /api/v1/downloadclient`,
  `GET /api/v1/downloadclient/schema`, `POST /api/v1/downloadclient/test`,
  `GET|PUT|DELETE /api/v1/downloadclient/{id}`,
  `POST /api/v1/downloadclient/{id}/test`, `POST /api/v1/download` (grab),
  `GET /api/v1/queue`, `DELETE /api/v1/queue/{clientId}/{itemId}`.

Stored credentials (indexer API keys, download-client passwords) are **write-only** in
the config API — they are accepted on create/update but never serialized back in any
response.

## Roadmap

| # | Sub-project        | Status        |
| - | ------------------ | ------------- |
| 1 | Foundation         | ✅ Complete    |
| 2 | Indexer engine     | ✅ Complete    |
| 3 | Download clients   | ✅ Complete    |
| 4 | Media management   | ⏳ Planned     |
| 5 | Automation         | ⏳ Planned     |
| 6 | Web UI (React)     | ⏳ Planned     |

Design specs and implementation plans for each sub-project live under
[`docs/superpowers/`](docs/superpowers/).

### Scope boundaries

- **In scope:** TV series (Sonarr's role), movies (Radarr's role), indexer management
  (Prowlarr's role).
- **Out of scope:** music (Lidarr) and books (Readarr) — the media model is
  purpose-built for TV + movies, not generalized.
- **Deployment:** a single self-hosted binary/container that embeds the UI and serves
  everything on one port, with a single admin user.
