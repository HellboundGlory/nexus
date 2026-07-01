# Nexus — Design Spec

**Date:** 2026-07-01
**Status:** Approved (design), pending implementation plan
**This document:** Overall vision & roadmap, plus the detailed design for Sub-project 1 (Foundation).

---

## 1. Vision

Nexus is a single, modern, self-hosted web application that replaces the separate
Prowlarr, Sonarr, and Radarr apps with one unified interface and one engine. It
manages TV series, movies, and the indexers that feed them — the combined job of
those three tools — behind a sleek, real-time UI.

This is a **full reimplementation**, not a frontend aggregator over the existing
apps. Nexus owns its own engine; the three upstream apps are not required to run it.

### Scope boundaries

- **In scope:** TV series (Sonarr's role), movies (Radarr's role), indexer
  management (Prowlarr's role).
- **Out of scope:** music (Lidarr), books (Readarr). The media model is **not**
  generalized to accommodate them — it is purpose-built for TV + movies.
- **Deployment:** a single self-hosted binary/container that embeds the UI and
  serves everything on one port. **Single admin user** with login.

### Reality check (why this is a program, not a single spec)

Sonarr and Radarr are the same codebase forked; Prowlarr shares that lineage.
"Three apps" is really **one shared engine with three feature surfaces** — which is
why merging them is coherent. The genuinely deep pieces that make this large:

- **Release-name parsing** — turning strings like
  `The.Show.S02E05.1080p.WEB-DL.DDP5.1.H.264-GROUP` into structured, scored
  metadata. Thousands of rules and years of edge cases.
- **Indexer definitions** — Prowlarr's value is a community repo of 500+ indexer
  definitions (Cardigann YAML). The engine is buildable; recreating every
  definition is an ongoing, community-scale effort, not a one-time write.

## 2. Technology stack

| Layer | Choice | Rationale |
|-------|--------|-----------|
| Backend | **Go** | App is I/O-bound (HTTP to indexers/download clients + DB); Go gives near-Rust throughput with far faster velocity and strong concurrency. |
| Frontend | **React + TypeScript + Tailwind + shadcn/ui** | Fastest proven path to a sleek, modern dashboard; large ecosystem; same framework the real *arr apps use. |
| Transport | **REST + WebSocket** | REST for CRUD/actions; WebSocket for live activity, download progress, and notifications. |
| Database | **SQLite** via `modernc.org/sqlite` (pure Go) | Keeps single-binary cross-compilation clean (no CGO); matches how the *arr apps store data. |
| Packaging | **Single Go binary / one Docker container** | UI embedded via `embed.FS`; one port serves UI + API. |

## 3. Architecture: modular monolith

One Go binary composed of internal modules that **never import each other
directly**. They communicate through an in-process **event bus** and a shared DB.
External integrations sit behind **provider interfaces** (compiled in, not
dynamically loaded), so adding an indexer, download client, or metadata source
means implementing an interface — not touching the core.

Rejected alternatives:

- **Microservices** — network hops, deployment and distributed-state complexity
  for zero benefit at single-container self-hosted scale.
- **Dynamic plugin system** — Go's runtime plugins are platform-brittle and
  version-fragile. Provider interfaces give ~90% of the flexibility with none of
  the pain; the interface design leaves the door open to plugins later.

## 4. Roadmap: sub-projects

Each is independently buildable and gets its own spec → plan → build cycle. Built
in order; everything stands on Foundation.

1. **Foundation** — config, database, task scheduler, event bus, HTTP API
   skeleton, auth, logging, SPA serving, provider-interface contracts.
   **(Detailed below.)**
2. **Indexer engine** (Prowlarr's role) — Torznab/Newznab protocol, definition
   loader, search aggregation.
3. **Download-client integration** — usenet (SABnzbd/NZBGet) + torrent
   (qBittorrent/etc.) adapters, queue monitoring.
4. **Media management** — metadata providers (TVDB/TMDb), root folders, the
   release-name parser, quality profiles / custom formats, import & rename.
5. **Automation** — monitored items, RSS sync, wanted/missing search, calendar,
   upgrades.
6. **Unified web UI** — the sleek dashboard tying it all together.

---

## 5. Sub-project 1: Foundation — detailed design

The base every other module is built on. No indexer/download/media behavior yet —
those modules ship as stub packages that declare their provider interfaces.

### 5.1 Project layout

```
nexus/
├── cmd/nexus/            # main() — wiring, startup, graceful shutdown
├── internal/
│   ├── core/
│   │   ├── config/       # bootstrap config (host, port, data dir, log level)
│   │   ├── database/     # SQLite connection, migrations, sqlc queries
│   │   ├── events/       # in-process event bus (typed pub/sub)
│   │   ├── scheduler/    # cron + interval tasks, command queue
│   │   ├── logging/      # slog setup, console + rotating file
│   │   ├── auth/         # single-admin login, sessions, API key
│   │   └── api/          # HTTP router, middleware, WebSocket hub
│   ├── indexer/          # stub interfaces now (sub-project 2)
│   ├── downloadclient/   # stub interfaces now (sub-project 3)
│   ├── media/            # stub now (sub-project 4)
│   └── automation/       # stub now (sub-project 5)
└── web/                  # React app; built assets embedded via embed.FS
```

Rule: modules communicate only through `core/events` and the DB. `core` is the
only shared dependency.

### 5.2 Configuration

Two layers, mirroring the *arr approach:

- **Bootstrap** (env vars + optional `config.yaml`) — the minimum needed before
  the DB exists: data directory, bind host/port, URL base, log level, and the
  auto-generated API key.
- **Runtime settings** — everything else (indexer settings, quality profiles,
  etc., added in later sub-projects) lives in a `settings` table, editable from
  the UI.

Precedence: **env var → config file → built-in default**.

### 5.3 Database & migrations

- SQLite via `modernc.org/sqlite` (pure Go, no CGO).
- Schema managed by **embedded, versioned migration files**, run automatically on
  startup.
- Queries generated with **sqlc** — compile-time-checked SQL, no reflection ORM.
- Foundation tables: `schema_migrations`, `settings`, `users`, `sessions`,
  `tasks` (command/job history), and an optional `events` audit table.
- A `Store` interface wraps queries so modules depend on an interface, not raw SQL.

### 5.4 Event bus

In-process typed pub/sub in `core/events`. Publishers emit typed events (e.g.
`MediaGrabbed`, `DownloadCompleted`); subscribers register handlers. Supports:

- **Synchronous** handlers — ordering guaranteed.
- **Async** handlers — fire-and-forget with panic recovery.

This is the decoupling spine, and it also feeds the WebSocket hub for live UI
updates.

### 5.5 Task scheduler & command queue

Two responsibilities in one module:

- **Scheduler** — recurring jobs on cron/interval (RSS sync, health checks, etc.,
  registered by later modules).
- **Command queue** — the *arr "Command" pattern: discrete jobs (e.g. "Search for
  episode", "Refresh series") run in a worker pool, report **progress**, and are
  queryable/visible in the UI. Commands persist to the `tasks` table so history
  survives restarts.

### 5.6 HTTP API & WebSocket

- **Router:** `chi`. REST under `/api/v1/...` with a consistent JSON error
  envelope.
- **Middleware:** panic recovery, request logging, auth.
- **Real-time:** a **WebSocket hub** at `/api/v1/ws` broadcasts selected
  event-bus events to connected clients — the equivalent of the SignalR feed the
  *arr UIs use for live activity/progress.
- **Foundation endpoints:** `/api/v1/system/status`, `/health`, auth endpoints,
  and the WebSocket. Later modules mount their own sub-routers.

### 5.7 Auth

- Single admin account. First-run setup creates the admin (username + password,
  hashed with **argon2id**).
- Browser sessions via a secure, HTTP-only cookie.
- Programmatic access via the auto-generated **API key** (request header).
- Login / logout endpoints, auth middleware, change-password path.
- Multi-user is out of scope, but the `users` table leaves room.

### 5.8 Logging & errors

- Structured logging with stdlib **`log/slog`** — console (dev) + rotating file
  (prod), level from config.
- Typed domain errors map to API responses through **one** translation layer, so
  handlers stay thin.

### 5.9 SPA serving

The built React app is embedded with `embed.FS` and served at `/`, with SPA
fallback (unknown non-`/api` paths → `index.html` for client-side routing). One
binary, one port, serves both UI and API.

### 5.10 Provider interfaces (declared, not implemented)

Foundation declares the Go interfaces later modules implement — `Indexer`,
`DownloadClient`, `MetadataProvider` — plus a **registry** pattern for each. No
implementations yet; this nails the contracts so sub-projects 2–4 slot in cleanly.

### 5.11 Testing

- Table-driven unit tests per package.
- Integration tests against a temp SQLite file.
- Provider interfaces let later modules test against fakes.

---

## 6. Foundation acceptance criteria

The Foundation sub-project is done when:

1. `nexus` starts from a single binary, reads bootstrap config, creates its data
   dir + SQLite DB, and runs migrations automatically.
2. First-run flow creates the admin account; login issues a session cookie; the
   API key authenticates programmatic requests.
3. `GET /api/v1/system/status` and `/health` respond; unknown UI routes fall back
   to the embedded SPA's `index.html`.
4. A demonstration command can be enqueued, runs in the worker pool, reports
   progress, persists to `tasks`, and pushes progress to a connected WebSocket
   client via the event bus.
5. `Indexer`, `DownloadClient`, and `MetadataProvider` interfaces + registries
   compile and are covered by fake-based tests.
6. Structured logs write to console and a rotating file at the configured level.
7. Graceful shutdown drains in-flight commands and closes the DB cleanly.
