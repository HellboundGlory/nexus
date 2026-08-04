---
id: architecture-overview
title: Architecture Overview
type: knowledge
stability: evolving
# applies_to:
#   - internal/**  — lets tooling warn when this doc goes stale
summary: >
  Nexus is a modular monolith — one Go binary that replaces Prowlarr + Sonarr +
  Radarr, serving a REST + WebSocket API and an embedded React SPA on a single
  port over SQLite. The single most important fact before changing anything:
  feature modules may depend only on `internal/core/*` (plus the leaf packages),
  never on a sibling feature module.
---

# Architecture Overview

## The shape of it

- `cmd/nexus/` — composition root: wires modules, starts the server.
- `internal/core/*` — shared infrastructure:
  `api` (chi router, auth middleware, WebSocket hub), `auth` (sessions + bcrypt),
  `command` (background runner), `config`, `database` (SQLite + versioned
  migrations 0001–0010), `events` (in-process bus: `Publish` / `PublishAsync` /
  `Subscribe`), `provider` (interfaces for indexer / download client / metadata),
  `scheduler`, `store` (data-access layer over SQLite), `version`.
- Feature modules (each owns an `api.go` REST surface + engine):
  `internal/indexer`, `internal/downloadclient`, `internal/media`,
  `internal/automation`, `internal/importing`.
- Leaf packages: `internal/parsing` (release-name parser), `internal/quality`
  (13 ranked definitions + decision engine), `internal/naming` (rename tokens).
- `web/` — React SPA; source under `src/`, committed build under `dist/`
  embedded into the binary via `web/embed.go` (`//go:embed dist`), served with an
  index.html fallback for client-side routes.

**Module boundary rule** (README + enforced by convention): everything under
`internal/<feature>` may depend only on `internal/core/*` (plus `parsing` /
`quality` / `naming` for the media pipeline), never on a sibling feature module.
Modules reach each other only through the `internal/core/provider` interfaces
wired at the composition root. External integrations (indexers, download
clients, metadata) sit behind these provider interfaces and are compiled in —
adding one means implementing an interface, not touching core.

## How data moves

One representative end-to-end path — search, grab, monitor a release:

1. The UI calls `POST /api/v1/automation/search/{media}{id}` (or the interactive
   search dialog calls `GET /api/v1/search`).
2. `internal/automation` fans the search out to enabled indexers through
   `internal/indexer`: Newznab/Torznab clients run under per-indexer rate
   limiting, results are deduped and sorted, and status/health events are
   published on the event bus and broadcast to the WebSocket hub
   (`internal/core/api/ws.go`) so the Dashboard updates live.
3. The grab is routed to a download client by protocol + priority (with an
   explicit override). `internal/downloadclient` performs a **server-side grab**:
   it fetches the `.nzb`/`.torrent` bytes itself (so an indexer's API key never
   leaves the server) while magnet links pass through untouched; SABnzbd and
   qBittorrent are supported.
4. A queue monitor polls each client (~1 min) and streams `download.status`
   events. On completion `internal/importing` attributes the download back to
   the library item it was grabbed for, checks it against the quality decision,
   renames via token templates, hardlinks (falling back to copy) it into the
   root folder, and records it in history.

## Invariants

- **Module boundary:** `internal/<feature>` never imports a sibling feature
  module; cross-feature capability flows through `internal/core/provider`
  interfaces injected at the composition root.
- **Every database write goes through the `store` layer** — feature code talks to
  SQLite only via `store.Store` (`internal/core/store/store.go`), not `*sql.DB`.
- **Stored credentials are write-only.** Indexer API keys, download-client
  passwords, and the TMDb key are accepted on create/update but never
  serialized back in any API response (README "Stored credentials" note; the
  config API enforces this).
- **`web/dist` is committed and must never drift** — it is embedded at build
  time and `make verify-web` (`git diff --exit-code web/dist`) fails on any diff.
- **One port serves everything** — the REST API, the `/api/v1/ws` WebSocket
  stream, and the embedded SPA.
- **The event log / history is append-only in spirit** — queue and history
  records are written, not edited in place (see importing/history model).

## Constraints that aren't visible in the code

- **Pure-Go SQLite ⇒ no CGO.** `modernc.org/sqlite` keeps the build at
  `CGO_ENABLED=0`, which is what makes a single static binary and clean
  cross-compilation possible — and also why the **race detector is unavailable**
  (verify concurrency with `go test -count=N`, not `-race`).
- **Building the binary does not require Node.** Because `web/dist` is committed
  and embedded, a plain `go build -o nexus ./cmd/nexus` needs Node only if you
  rebuild the UI.
- **TMDb key gates media discovery.** Add-media search returns nothing until
  `NEXUS_TMDB_API_KEY` (a TMDb v3 key) is set; metadata sits behind the
  `MetadataProvider` interface so no code hardcodes TMDb.
- **Configuration is entirely environment-driven** (`NEXUS_*` variables). There
  is no config-file parser; a new setting means a new env var plus config wiring
  and, for encrypted values, a write-only path.
- **Single-administrator model.** Auth is one admin user (session cookie or
  `X-Api-Key`); this is a deliberate deployment boundary, not an oversight.

## Deliberately not done

- **No music (Lidarr) or books (Readarr).** The media model (`series → seasons →
  episodes`, `movies`) is purpose-built for TV + movies and deliberately not
  generalized (README scope boundaries).
- **No upstream *arr dependency.** Nexus is a full reimplementation, not a
  frontend aggregator over Prowlarr/Sonarr/Radarr.
- **No tagged release yet.** Status is feature-complete, pre-release; the schema
  and API may still change and you run from source (`make build`).
- **No per-connection backpressure on the WebSocket** — the hub drops a slow
  client (non-blocking `send` with `default`) rather than blocking the
  broadcaster, trading out-of-order delivery for liveness (`ws.go`).
