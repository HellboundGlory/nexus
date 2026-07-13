# Nexus

A single, self-hosted binary that replaces **Prowlarr + Sonarr + Radarr** with one
unified engine and interface. Nexus manages TV series, movies, and the indexers and
download clients that feed them — the combined job of those three tools — behind one
REST + WebSocket API and an embedded web UI on one port.

This is a **full reimplementation**, not a frontend aggregator: Nexus owns its own
engine and does not require the upstream *arr apps to run.

> **Status: feature-complete, pre-release.** All six roadmap sub-projects — foundation,
> indexer engine, download clients, media management, automation, and the React web UI —
> are built and merged (see [Roadmap](#roadmap)). There is no tagged release yet and the
> schema and API may still change; expect to run from source.

## Features

- **Indexer engine** — Newznab/Torznab clients (Usenet + torrent), capability discovery
  with caching, concurrent search fan-out across indexers (dedupe + sort), per-indexer
  rate limiting, and a scheduled health check that reports status live over WebSocket.
- **Download clients** — SABnzbd (Usenet) and qBittorrent (torrent). Nexus performs
  **server-side grab**: it fetches `.nzb`/`.torrent` bytes itself (so an indexer's API
  key never leaves the server) while magnet links pass through untouched. Releases are
  routed to a client by protocol + priority, with an explicit client override. A queue
  monitor polls each client (~1 min) and streams `download.status` events.
- **Media library** — TV series → seasons → episodes and movies, with metadata from
  **TMDb** (both TV and movies) behind a `MetadataProvider` interface. Root folders,
  a monitored flag with add options (all / future / none), refresh reconciliation that
  preserves user monitor choices, and cascading monitor toggles down to the episode.
- **Parsing & quality** — a release-name parser (source / resolution / codec / revision,
  plus TV season+episode and movie year/edition/group) and 13 built-in ranked quality
  definitions. Quality profiles with an allowed set, a cutoff, and an upgrades toggle
  drive a stateless accept/reject/upgrade decision engine.
- **Automation** — scheduled and manual search that finds monitored, missing items and
  grabs the best acceptable release; **RSS sync** that polls indexer feeds and matches
  releases back to monitored items (id-first, then title/year); and an **upgrade sweep**
  that re-searches items whose existing file ranks below the profile cutoff, guarded by a
  history-based cooldown. In-flight items in the queue are skipped to avoid re-grabbing.
- **Import & rename** — completed downloads are attributed back to the library item they
  were grabbed for, checked against the quality decision, renamed via token templates,
  hardlinked (falling back to copy) into the root folder, tracked as files, and recorded
  in history. Season packs match each video file's parsed episode against the grab.
- **Web UI** — an embedded React single-page app served on the same port: Dashboard with
  a live activity feed, Movies and TV libraries with detail pages, a Calendar of upcoming
  episodes and movies, an Activity view (download/import queue + history), and Settings
  for indexers, download clients, quality profiles, root folders, naming, and scheduling.
- **Foundation** — SQLite persistence with versioned migrations, an in-process event bus,
  a background command/scheduler runner, single-admin authentication (session cookie or
  API key), and structured logging with rotation.

## Architecture

Nexus is a **modular monolith**: one Go binary composed of internal feature modules that
**never import each other directly**. They communicate through an in-process event bus
and a shared database, and reach each other's capabilities only through small
consumer-defined interfaces wired at the composition root. External integrations
(indexers, download clients, metadata) sit behind **provider interfaces** that are
compiled in — adding a new one means implementing an interface, not touching the core.

Module boundary rule: everything under `internal/<feature>` may depend only on
`internal/core/*` (plus, for the media pipeline, the leaf `parsing` / `quality` /
`naming` packages), never on a sibling feature module.

| Layer     | Choice                                                   |
| --------- | -------------------------------------------------------- |
| Language  | Go (1.26+)                                               |
| HTTP      | `go-chi/chi` router; REST + `gorilla/websocket`          |
| Database  | SQLite via `modernc.org/sqlite` (pure Go, no CGO)        |
| Auth      | bcrypt password hashing (`golang.org/x/crypto`)          |
| Logging   | `log/slog` with `lumberjack` rotation                    |
| Web UI    | Vite + React 19 + TypeScript + Tailwind v4 + shadcn/ui, TanStack Query, React Router |
| Packaging | single static binary; UI embedded via `embed.FS`         |

Because the SQLite driver is pure Go, Nexus builds with `CGO_ENABLED=0` and
cross-compiles cleanly to a single static binary. The compiled UI is committed under
`web/dist` and embedded, so building the binary does not require Node.

## Project layout

```
cmd/nexus/                  Composition root (main): wires modules, starts the server
internal/core/
  api/                      HTTP router, auth middleware, WebSocket hub
  auth/                     Sessions + password hashing
  command/                  Background command runner + manager
  config/                   Environment-driven configuration
  database/                 SQLite open + migrations (0001_init … 0006_import)
  events/                   In-process event bus (Publish / PublishAsync / Subscribe)
  provider/                 Provider interfaces (Indexer, DownloadClient, MetadataProvider, ...)
  scheduler/                Periodic command scheduling
  store/                    Data-access layer over SQLite
  version/                  Build version
internal/indexer/           Indexer engine (Newznab/Torznab, search, health, REST)
internal/downloadclient/    SABnzbd + qBittorrent clients, grab, routing, queue monitor, REST
internal/media/             Library model, TMDb provider, refresh, root folders, calendar, REST
internal/parsing/           Release-name parser (leaf)
internal/quality/           Quality definitions, profiles, decision engine, REST (leaf-ish)
internal/naming/            Rename token templates (leaf)
internal/importing/         Grab tracking, import pipeline, queue + history, REST
internal/automation/        Missing search, RSS sync, upgrade sweep, REST
web/                        React SPA (source under src/, committed build under dist/)
docs/superpowers/           Design specs and implementation plans
```

## Getting started

### Requirements

- Go **1.26** or newer
- Node (only to rebuild the web UI; the committed `web/dist` bundle means a plain Go
  build needs no Node)

### Build & run

```bash
# Build a static binary (rebuilds the web bundle first, then compiles Go)
make build          # → ./nexus

# Or, since web/dist is committed, build just the Go binary (no Node needed)
CGO_ENABLED=0 go build -o nexus ./cmd/nexus

# Run directly
make run            # go run ./cmd/nexus
```

By default Nexus listens on `0.0.0.0:9494`, stores its database and logs under `./data`,
and serves the UI at `http://localhost:9494/`. On first start it creates the initial
admin user; if `NEXUS_ADMIN_PASSWORD` is not set, a random password is generated and
written to the log. If `NEXUS_API_KEY` is not set, a random API key is generated at
startup. TMDb-backed metadata lookups require `NEXUS_TMDB_API_KEY` (a TMDb v3 key) — the
Add-media search returns nothing until it is set.

### Docker

Prebuilt multi-arch images (`linux/amd64`, `linux/arm64`) are published to GHCR:

```bash
docker run -d --name nexus -p 9494:9494 \
  -e NEXUS_ADMIN_PASSWORD=change-me \
  -e NEXUS_TMDB_API_KEY=your-tmdb-v3-key \
  -v nexus-data:/data \
  ghcr.io/hellboundglory/nexus:latest
```

Or use the example [`docker-compose.yml`](docker-compose.yml) with a `.env` (copy
from [`.env.example`](.env.example)). Full instructions, the environment-variable
reference, and volume-permission notes are in [docs/docker.md](docs/docker.md).

### Web UI development

```bash
make web            # production build into web/dist (npm ci + vite build)
make web-dev        # Vite dev server on :5173, proxying /api and /ws to :9494
make web-test       # frontend unit tests (Vitest)
```

`make build` depends on `web`, and a drift guard (`git diff --exit-code web/dist`) keeps
the committed bundle in sync with the source.

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
| `NEXUS_TMDB_API_KEY`   | *(empty)*   | TMDb v3 API key for metadata lookups. Write-only.       |
| `NEXUS_ADMIN_USER`     | `admin`     | Initial admin username (used only on first start).      |
| `NEXUS_ADMIN_PASSWORD` | *(random)*  | Initial admin password. Logged if generated.            |

## API overview

One port serves the REST API, the WebSocket stream, and the embedded UI.

- `GET  /health` — liveness (unauthenticated).
- `POST /api/v1/auth/login`, `POST /api/v1/auth/logout` — session cookie auth.

Everything else below `/api/v1` requires authentication — either the session cookie or
the `X-Api-Key` header:

- `GET  /api/v1/system/status` — version and system info.
- `GET  /api/v1/ws` — WebSocket stream (indexer / download / media / import / automation
  status events).
- **Indexers:** `GET|POST /api/v1/indexer`, `GET /api/v1/indexer/schema`,
  `POST /api/v1/indexer/test`, `GET|PUT|DELETE /api/v1/indexer/{id}`,
  `POST /api/v1/indexer/{id}/test`, `GET /api/v1/search`.
- **Download clients:** `GET|POST /api/v1/downloadclient`,
  `GET /api/v1/downloadclient/schema`, `POST /api/v1/downloadclient/test`,
  `GET|PUT|DELETE /api/v1/downloadclient/{id}`, `POST /api/v1/downloadclient/{id}/test`,
  `POST /api/v1/download` (server-side grab).
- **Library:** `GET /api/v1/media/lookup`, `GET|POST /api/v1/series` and `/movies`,
  `GET|DELETE /api/v1/series|movies/{id}`, `POST /{id}/refresh`, `PUT /{id}/monitor`,
  `PUT /{id}/qualityprofile`, `PUT /api/v1/season|episode/{id}/monitor`,
  `GET|POST|DELETE /api/v1/rootfolder`, `GET /api/v1/calendar?start=&end=`.
- **Quality:** `GET /api/v1/quality/definitions`,
  `GET|POST /api/v1/qualityprofile`, `GET|PUT|DELETE /api/v1/qualityprofile/{id}`,
  `POST /api/v1/quality/parse`.
- **Queue, import & history:** `GET /api/v1/queue`, `DELETE /api/v1/queue/{id}`,
  `POST /api/v1/queue/{id}/import`, `GET /api/v1/history`,
  `GET|PUT /api/v1/config/naming`.
- **Automation:** `POST /api/v1/automation/search/{movie|series|episode}/{id}` (and
  `/series/{id}/season/{n}`), `GET|PUT /api/v1/automation/config`.

Stored credentials (indexer API keys, download-client passwords, the TMDb key) are
**write-only** in the config API — they are accepted on create/update but never
serialized back in any response.

## Roadmap

| # | Sub-project        | Status        |
| - | ------------------ | ------------- |
| 1 | Foundation         | ✅ Complete    |
| 2 | Indexer engine     | ✅ Complete    |
| 3 | Download clients   | ✅ Complete    |
| 4 | Media management   | ✅ Complete    |
| 5 | Automation         | ✅ Complete    |
| 6 | Web UI (React)     | ✅ Complete    |

Design specs and implementation plans for each sub-project live under
[`docs/superpowers/`](docs/superpowers/).

### Scope boundaries

- **In scope:** TV series (Sonarr's role), movies (Radarr's role), indexer management
  (Prowlarr's role).
- **Out of scope:** music (Lidarr) and books (Readarr) — the media model is
  purpose-built for TV + movies, not generalized.
- **Deployment:** a single self-hosted binary/container that embeds the UI and serves
  everything on one port, with a single admin user.
