# Nexus — Indexer Engine Design Spec (Sub-project 2)

**Date:** 2026-07-02
**Status:** Approved (design), pending implementation plan
**Depends on:** Sub-project 1 (Foundation) — complete and merged to `master`.

---

## 1. Purpose

Fill Prowlarr's role in Nexus: manage indexers and search across them. This
sub-project implements the `provider.Indexer` contract declared in Foundation and
delivers the engine plus its REST API. There is no UI yet — that lands in
Sub-project 6.

## 2. Scope decisions (settled during brainstorming)

- **Protocols:** Newznab (usenet) + Torznab (torrent) only. These are one protocol
  family (Torznab is a superset of Newznab), so they share a single client.
- **No Cardigann.** The YAML tracker-scraping engine (Prowlarr's 500+ private
  trackers) is explicitly deferred to its own future sub-project. The
  `provider.Indexer` registry seam keeps that door open with no rework.
- **Consumer-only.** Nexus queries indexers for its own internal use. It does
  **not** expose an aggregated Torznab/Newznab endpoint for external apps
  (Prowlarr's proxy role). Matches the "unified single app, not an aggregator"
  vision.
- **Generic configuration.** Indexers are added by type + base URL + API key;
  capabilities are auto-discovered via `t=caps`. No curated preset catalog.
- **Typed-search params at the protocol level now** (`tvsearch`/`movie` with
  season/episode/imdbid/tvdbid), but this sub-project only exercises plain text +
  category search end-to-end. Typed search becomes fully useful once media/
  automation (Sub-projects 4–5) can resolve IDs.

## 3. Architecture & module boundaries

All indexer code lives in `internal/indexer` and imports `internal/core/*` only.
It persists through `core/store`, and communicates with the rest of the app via
the event bus and shared store — never by importing other feature modules.

### 3.1 Two Foundation touch-ups this sub-project requires

1. **Module route mounting.** Extend
   `api.NewRouter(deps, spa, mounts ...func(chi.Router))`. Each `mount` is invoked
   inside the authenticated `/api/v1` group so feature modules can attach
   sub-routers. `core/api` gains **no** feature imports; the composition root
   (`cmd/nexus/main.go`) does the wiring. The variadic signature is
   backward-compatible: zero mounts reproduces today's behavior, and existing
   `api` tests are unaffected.
2. **Indexer persistence in `core/store`.** Add
   `internal/core/store/indexer_store.go` + migration `0002_indexers.sql`. This
   matches how `users`/`sessions`/`tasks` already live in `core/store` (the `db`
   handle is private to that package, so it is the consistent home).

### 3.2 Package layout

```
internal/indexer/
  indexer.go     # package doc + Service: owns the live set of configured indexer
                 #   clients, rebuilt from store on config change (reload())
  newznab.go     # NewznabClient implements provider.Indexer; Protocol flag
  caps.go        # capabilities discovery + parsing (t=caps)
  request.go     # builds search/tvsearch/movie request URLs from provider.Query
  parse.go       # parses Newznab/Torznab XML (RSS) -> []provider.Release
  search.go      # SearchService: concurrent fan-out, aggregate, dedupe, sort
  ratelimit.go   # per-indexer minimum-interval limiter
  health.go      # health-check Command (implements command.Command)
  api.go         # chi sub-router: indexer CRUD, test, caps, manual search
  testdata/      # recorded caps + search XML fixtures
  *_test.go
```

### 3.3 Component responsibilities

- **Service** — source of truth for live indexer clients; reloads its client set
  from the store after any config change (called in-package by the API handlers);
  hands the current set to `SearchService`.
- **NewznabClient** — one configured indexer; performs caps/search HTTP + parsing
  behind `provider.Indexer`. Protocol flag selects Torznab attribute parsing and
  the default release protocol.
- **SearchService** — fan-out across enabled clients, merge/dedupe/sort, collect
  per-indexer errors (partial success).
- **health Command** — periodic caps ping; updates status and emits an event.
- **api** — the HTTP surface.

## 4. Data model & contracts

### 4.1 `indexers` table (migration `0002_indexers.sql`) → `store.Indexer`

| Column | Purpose |
|--------|---------|
| `id` (PK), `name` | user's label |
| `implementation` | `newznab` \| `torznab` (protocol flag) |
| `base_url`, `api_key` | endpoint + credential |
| `enabled`, `priority` | on/off; ranking tie-breaker (default 25, lower = preferred) |
| `categories` | JSON array of enabled Newznab category IDs (empty = all) |
| `settings` | JSON blob for future per-indexer fields (forward-proof) |
| `caps` | JSON cache of last capabilities discovery |
| `status`, `last_check`, `fail_message` | health state |
| `created_at`, `updated_at` | timestamps |

Store methods (same style as existing): `CreateIndexer`, `GetIndexer`,
`ListIndexers`, `UpdateIndexer`, `DeleteIndexer`, `SetIndexerStatus`.

### 4.2 Extended shared contracts in `core/provider` (backward-compatible)

```go
type SearchType string // "search" | "tvsearch" | "movie"
type Protocol   string // "usenet" | "torrent"

type Query struct {
    Type           SearchType
    Term           string
    Categories     []int
    Season, Episode *int      // typed-search params (tvsearch)
    IMDbID         string      // typed-search params (movie)
    TVDBID, TMDBID int
    Limit, Offset  int
    Kind           MediaKind   // existing field, retained
}

type Release struct {
    Title, DownloadURL, InfoURL string
    Size              int64
    IndexerID         string
    Categories        []int
    PublishDate       time.Time
    GUID              string
    Protocol          Protocol
    Seeders, Leechers *int      // torrent-only; nil for usenet
}
```

The Foundation `provider_test.go` stub still compiles (added fields are additive).

### 4.3 Capabilities

Parsed from `t=caps`, cached in `indexers.caps`: supported search modes
(`search`/`tvsearch`/`movie`), the params each mode accepts, the indexer's category
list, and result limits (default/max). Used to validate a query before dispatch
and to skip indexers that don't support a requested mode.

### 4.4 Persistence scope

Only indexer **configs** and the caps cache are stored. **Search results are
transient** — returned live, never persisted. Release history and grabs belong to
the download-client and history work in later sub-projects. (YAGNI.)

## 5. Runtime behavior

### 5.1 Manual search (`GET /api/v1/search`)

1. Handler parses `query`, `type`, `categories`, `limit`/`offset`, optional
   `indexerIds` → builds a `provider.Query`.
2. `SearchService` selects enabled clients (priority-sorted), **skips any whose
   cached caps don't support the requested mode/params**, then fans out
   concurrently with a per-indexer timeout; each call passes through that
   indexer's rate limiter.
3. Collect results **and** per-indexer errors → aggregate → dedupe → sort:
   - *Dedupe:* by `(Protocol, GUID)`, falling back to normalized `Title`+`Size`;
     keep the copy from the higher-priority indexer.
   - *Sort:* `PublishDate` desc, then indexer priority. (Quality/scoring ranking is
     deferred to the media sub-project.)
4. Response `{ "releases": [...], "indexerErrors": [{indexerId, message}] }` —
   **partial success:** one indexer failing never fails the whole search (HTTP 200
   with an errors array, even when `releases` is empty).

Manual search is a **direct synchronous service call** with a context timeout —
**not** a queued command. The Foundation command queue is fire-and-forget
(`Enqueue` returns a task id, not results), so it is reserved for health checks and
later automatic/RSS searches.

### 5.2 Protocol + caps

One `NewznabClient` builds `…/api?t=<mode>&apikey=…&q=…&cat=…&season=&ep=&imdbid=…`.
Newznab and Torznab share the endpoint and parser; Torznab additionally reads
`torznab:attr` seeders/peers/magnet and defaults `Protocol=torrent` (Newznab →
`usenet`). Result XML is Newznab-style RSS (`<item>` with `<enclosure>`, `<guid>`,
`<pubDate>`, and `newznab:attr`/`torznab:attr` elements).

### 5.3 Rate limiting

Per-indexer minimum request interval (config default ~2s) and concurrency 1, so
Nexus never hammers an indexer into a ban.

### 5.4 Health

A `health.Command` runs on a scheduler interval (default 15 min): for each enabled
indexer it pings `t=caps`; on success `status=ok`, on failure it records
`fail_message` and emits `IndexerStatusChanged` (event name `"indexer.status"`),
which the WebSocket hub forwards live. Simple consecutive-failure flag; no
auto-disable. (Escalating backoff is a noted later enhancement.) The `test`
endpoints run the same check on demand.

### 5.5 Error handling

Typed errors — `ErrIndexerUnavailable`, `ErrAuthFailed`, `ErrInvalidResponse`,
`ErrUnsupportedSearch` — map through the existing `WriteError` JSON envelope.
During fan-out, per-indexer errors are captured into the response rather than
propagated as a 500.

## 6. API surface (all auth-guarded)

| Method + path | Purpose |
|---------------|---------|
| `GET /api/v1/indexer` | list configured indexers |
| `POST /api/v1/indexer` | create (validates + discovers caps) |
| `GET /api/v1/indexer/{id}` | get one |
| `PUT /api/v1/indexer/{id}` | update |
| `DELETE /api/v1/indexer/{id}` | delete |
| `POST /api/v1/indexer/{id}/test` | test connectivity of a saved indexer; update status |
| `POST /api/v1/indexer/test` | test an unsaved config (body) — for the add flow |
| `GET /api/v1/indexer/schema` | describe indexer types + config fields (future UI) |
| `GET /api/v1/search` | manual aggregated search |

## 7. Testing (all offline, CGO-free, deterministic)

- **Unit:** request-URL builder per search type; caps parser; result parser with
  recorded Newznab + Torznab XML fixtures in `testdata/`; dedupe/sort; rate-limiter
  timing.
- **Integration:** `httptest.Server`s as fake indexers — one healthy, one failing —
  asserting aggregation, dedupe, and partial-error behavior; API handler tests
  through the mounted router.
- No real network access in any test.

## 8. Acceptance criteria

1. A Newznab and a Torznab indexer can be created, tested, listed, updated, and
   deleted via the API; caps are discovered and cached on create/test.
2. `GET /api/v1/search` returns aggregated, deduped, sorted releases across all
   enabled indexers, with per-indexer errors surfaced and partial success honored.
3. Torznab results include seeders/leechers; Newznab results are marked usenet.
4. Health checks update indexer status and emit `indexer.status` events observable
   on the WebSocket.
5. Per-indexer rate limiting is enforced.
6. `CGO_ENABLED=0 go build ./...` succeeds and `go test ./...` passes.
7. Module boundaries hold: `internal/indexer` imports only `internal/core/*`.

## 9. Out of scope (explicit)

- Cardigann / YAML tracker definitions and private-tracker login/captcha flows.
- External-facing aggregated Torznab/Newznab server.
- Curated indexer preset catalog.
- Release scoring / quality ranking (media sub-project).
- Persisted release history / grabbing (download-client + history sub-projects).
- Escalating health backoff / auto-disable.

## 10. Notes & deviations

- Extending `provider.Query`/`Release` is additive and does not break the
  Foundation stub test.
- `api.NewRouter` gains a variadic `mounts` parameter — the only Foundation
  signature change; existing callers/tests are unaffected.
- Module path `github.com/hellboundg/nexus` throughout.

### 10.1 Decisions settled during implementation (2026-07-02/03)

- **Caps-less indexers are searchable by default.** When an indexer's `caps`
  cache is empty (freshly added, or caps discovery has not yet run),
  `Service.Reload` builds its client with permissive, symmetric capabilities
  (`Search`, `TVSearch`, `MovieSearch` all `true`) so the indexer is usable
  immediately; the scheduled health check refines this once real caps are
  fetched.
- **The indexer API key is write-only in the config API.** `store.Indexer.APIKey`
  is tagged `json:"-"`, so it is never returned by the indexer CRUD endpoints
  (list/get/create/update); it is only ever *set* via the request payload.
- **API-key redaction scope is the indexer config, NOT release URLs.** Newznab/
  Torznab grab and info URLs (`Release.DownloadURL` / `Release.InfoURL`) carry
  the indexer's `apikey` query parameter by protocol necessity — the download
  client (sub-project 3) needs it to fetch the release. These URLs are returned
  by `GET /api/v1/search`. This is an accepted, conscious scope: the entire
  `/api/v1` surface is behind admin API-key auth, so this is not an external
  leak. Hiding the key entirely would require routing grabs through a Nexus
  proxy, which is deferred to the Download-clients sub-project.
