# Nexus — Download Clients Design Spec (Sub-project 3)

**Date:** 2026-07-03
**Status:** Approved (design), pending implementation plan
**Depends on:** Sub-project 1 (Foundation) and Sub-project 2 (Indexer engine) — both
complete and merged to `master`.

---

## 1. Purpose

Fill the download-client role in Nexus: send releases to a download client and
monitor the resulting queue. This sub-project implements the
`provider.DownloadClient` contract declared in Foundation and delivers the engine
plus its REST API. There is no UI yet — that lands in Sub-project 6.

It also closes the **grab-proxy** follow-up recorded during Sub-project 2 (indexer
design spec §10.1): releases are fetched server-side so the indexer API key never
leaves Nexus.

## 2. Scope decisions (settled during brainstorming)

- **Two clients this round:** **SABnzbd** (usenet) + **qBittorrent** (torrent) —
  the two most common clients, one per protocol, exercised end-to-end. NZBGet,
  Transmission, Deluge, etc. are deferred; they slot in later via the
  `provider.DownloadClient` registry seam with no rework.
- **Server-side fetch (grab-proxy).** Nexus fetches the `.nzb`/`.torrent` bytes
  itself and hands **content** to the client; magnet links pass through as URLs.
  The indexer API key (already embedded in `Release.DownloadURL` from search)
  never reaches the download client. Matches the *arr* apps
  (`UsenetClientBase`/`TorrentClientBase` in the Sonarr reference).
- **Client configs only.** Only download-client **configs** are persisted. The
  live queue is read by **polling the clients**; individual grabs/items are not
  stored. Grab/download history and import tracking belong to the media/history
  sub-projects. Mirrors Sub-project 2's "configs stored, results transient" stance.
- **Routing by protocol then priority.** A release carries a `Protocol`; the grab
  is routed to the highest-priority enabled client of that protocol (lower
  `priority` number wins, default 25 — same convention as indexers). The API also
  accepts an explicit `clientId` override for manual grabs.
- **Interface surface:** grab + list + remove + test. Import/rename hooks
  (`GetImportItem`/`MarkAsImported` in *arr*) are deferred to the media
  sub-project that owns the import pipeline.

## 3. Architecture & module boundaries

All download-client code lives in `internal/downloadclient` and imports
`internal/core/*` only. It persists through `core/store`, runs its queue monitor
through the Foundation command scheduler, and communicates with the rest of the
app via the event bus and shared store — never by importing other feature modules
(including `internal/indexer`).

**Why no `internal/indexer` import is needed:** the release's `DownloadURL`
returned by `GET /api/v1/search` already carries the indexer's `apikey` query
parameter, so a grab is a plain authenticated HTTP GET of that URL. The
download-client module does not need indexer internals to fetch release content.

### 3.1 Foundation touch-up this sub-project requires

**Extend the `core/provider` download contract.** The Foundation `DownloadRequest`
struct and `DownloadClient` interface are extended (see §4.2). This is the only
Foundation change. Unlike Sub-project 2's purely additive `Query`/`Release`
extensions, extending the `DownloadClient` interface changes its method set, so the
Foundation `provider_test.go` fake download client is updated to satisfy the new
interface. No other Foundation package changes:

- `api.NewRouter(deps, spa, mounts ...func(chi.Router))` already accepts variadic
  mounts (added in Sub-project 2) — the download API mounts through it, no change.
- `WSForward` already exists on `api.Deps` — the composition root adds
  `"download.status"` to the forwarded list, no core change.
- The scheduler already runs `command.Command`s on an interval — the composition
  root registers the queue monitor, no core change.

### 3.2 Package layout

```
internal/downloadclient/
  downloadclient.go  # package doc + Service: owns the live set of configured
                     #   clients, rebuilt from store on config change (reload());
                     #   exposes Grab() and Queue()
  sabnzbd.go         # SABnzbdClient implements provider.DownloadClient (usenet)
  qbittorrent.go     # QBittorrentClient implements provider.DownloadClient (torrent)
  grab.go            # server-side fetch: GET .nzb/.torrent bytes; magnet passthrough
  monitor.go         # queue-poll Command (implements command.Command): diff + emit
  errors.go          # typed errors
  api.go             # chi sub-router: client CRUD, test, schema, grab, queue, remove
  testdata/          # recorded SAB + qBittorrent API JSON fixtures
  *_test.go
```

### 3.3 Component responsibilities

- **Service** — source of truth for the live client set; reloads its clients from
  the store after any config change (called in-package by the API handlers). Owns
  `Grab(ctx, req)` (route → fetch → add) and `Queue(ctx)` (fan-out `Items()` across
  enabled clients).
- **SABnzbdClient / QBittorrentClient** — one configured client each; perform the
  HTTP calls + parsing behind `provider.DownloadClient`. `Add` receives a
  `DownloadRequest` whose content has already been fetched by `grab.go` (or a magnet
  URL); the client just submits it.
- **grab.go** — shared server-side fetch: given a routed release, returns the
  `.nzb`/`.torrent` bytes (plain GET, 30s timeout) or recognizes a `magnet:` URL and
  passes it through untouched.
- **monitor Command** — periodic queue poll; diffs against last-seen state and emits
  `DownloadStatusChanged` (event name `"download.status"`), which the WebSocket hub
  forwards live.
- **api** — the HTTP surface.

## 4. Data model & contracts

### 4.1 `download_clients` table (migration `0003_download_clients.sql`) → `store.DownloadClient`

| Column | Purpose |
|--------|---------|
| `id` (PK), `name` | user's label |
| `implementation` | `sabnzbd` \| `qbittorrent` |
| `protocol` | `usenet` \| `torrent` (derived from implementation; stored for routing) |
| `host`, `port`, `use_ssl`, `url_base` | endpoint (first-class columns — both clients need them) |
| `username` | qBittorrent login (empty for SABnzbd) |
| `api_key` | credential: SABnzbd API key **or** qBittorrent password; write-only (`json:"-"`) |
| `category` | download category sent to the client (folder routing for future import) |
| `enabled`, `priority` | on/off; routing tie-breaker (default 25, lower = preferred) |
| `settings` | JSON blob for future per-client fields (forward-proof) |
| `status`, `last_check`, `fail_message` | health state |
| `created_at`, `updated_at` | timestamps |

Store methods (same style as `indexer_store.go`): `CreateDownloadClient`,
`GetDownloadClient`, `ListDownloadClients`, `UpdateDownloadClient`,
`DeleteDownloadClient`, `SetDownloadClientStatus`.

The single `api_key` column holds SABnzbd's API key or qBittorrent's password; it
is tagged `json:"-"` so the config API never returns it (write-only), mirroring the
indexer `APIKey` decision (indexer design spec §10.1).

### 4.2 Extended shared contracts in `core/provider`

```go
type DownloadStatus string // "queued" | "downloading" | "completed" | "paused" | "failed" | "warning"

// DownloadItem is one entry in a client's queue/history snapshot.
type DownloadItem struct {
    ID               string         // client's item id/hash
    Title            string
    Status           DownloadStatus
    Progress         float64        // 0..100
    Size             int64          // total bytes
    Downloaded       int64          // bytes fetched so far
    DownloadClientID string         // which configured client owns this item
    Protocol         Protocol
    ErrorMessage     string
}

// DownloadRequest is a grab: extends the Foundation stub {URL, Title}.
type DownloadRequest struct {
    URL       string   // release download url (may be a magnet: link)
    Title     string
    Protocol  Protocol
    IndexerID string   // attribution
    Category  string   // overrides the client's default category when set
    Content   []byte   // pre-fetched .nzb/.torrent bytes; nil for magnet links
}

type DownloadClient interface {
    ID() string
    Protocol() Protocol
    Add(ctx context.Context, d DownloadRequest) (string, error) // returns client item id
    Items(ctx context.Context) ([]DownloadItem, error)
    Remove(ctx context.Context, id string, deleteData bool) error
    Test(ctx context.Context) error
}
```

The Foundation `provider_test.go` fake is updated to implement the new method set.

### 4.3 Persistence scope

Only client **configs** are stored. **Queue state is transient** — polled live from
the clients, never persisted. Grab history, blocklisting, and import tracking belong
to later sub-projects. (YAGNI.)

## 5. Runtime behavior

### 5.1 Grab (`POST /api/v1/download`)

Search results are transient (never persisted, per Sub-project 2 §4.4), so the
caller submits the release fields in the request body.

1. Handler parses the release (`downloadUrl`, `title`, `protocol`, `indexerId`,
   optional `category`) and an optional `clientId` override → builds a
   `provider.DownloadRequest`.
2. **Route:** if `clientId` is given, use that client (must be enabled); otherwise
   pick the highest-priority enabled client whose `Protocol` matches the release.
   No matching client → `ErrUnsupportedProtocol`.
3. **Fetch (server-side):** if `downloadUrl` is a `magnet:` link, pass it through
   unchanged (`Content` stays nil). Otherwise GET the URL (30s timeout) and put the
   `.nzb`/`.torrent` bytes in `Content`. A 404/410 → `ErrReleaseUnavailable`; other
   failures → `ErrClientUnavailable`.
4. **Submit:** call the routed client's `Add(ctx, req)`; return
   `{ "id": "<clientItemId>", "downloadClientId": "<id>" }`.

Grab is a **direct synchronous service call** with a context timeout — not a queued
command (same rationale as manual search in Sub-project 2 §5.1: the command queue is
fire-and-forget and returns a task id, not a result).

### 5.2 Client protocols

- **SABnzbd** (usenet) — JSON API at `{scheme}://{host}:{port}{url_base}/api` keyed
  by `apikey`. Grab: `mode=addfile` (multipart upload of the `.nzb` content) with
  `cat={category}`. Queue/history: `mode=queue` + `mode=history` (output=json)
  mapped to `[]DownloadItem` (`downloading`/`queued` from queue; `completed`/`failed`
  from history). Test: `mode=version` (or `mode=get_config`) authenticates the key.
- **qBittorrent** (torrent) — WebUI API v2 at `{scheme}://{host}:{port}{url_base}`.
  Cookie auth: `POST /api/v2/auth/login` (username+password) → `SID` cookie reused
  for subsequent calls. Grab: `POST /api/v2/torrents/add` — magnet via `urls`, or the
  fetched `.torrent` bytes via multipart `torrents`, with `category={category}`.
  Queue: `GET /api/v2/torrents/info` mapped to `[]DownloadItem` (state → status,
  `progress`×100, `size`/`completed`). Remove: `POST /api/v2/torrents/delete`
  (`deleteFiles`). Test: login succeeds + `GET /api/v2/app/version`.

### 5.3 Queue monitoring (`GET /api/v1/queue`, monitor command)

- **On-demand:** `GET /api/v1/queue` fans out `Items()` across enabled clients and
  returns `{ "items": [...], "clientErrors": [{clientId, message}] }` — **partial
  success:** one client failing never fails the whole snapshot (HTTP 200 with an
  errors array), mirroring search aggregation in Sub-project 2 §5.1.
- **Live:** a `monitor.Command` runs on a scheduler interval (default 1 min). It
  polls each enabled client's `Items()`, diffs against the last-seen snapshot, and
  emits `DownloadStatusChanged` (event name `"download.status"`) for changed/new/
  removed items. The WebSocket hub forwards `"download.status"` live (composition
  root adds it to `WSForward`).

### 5.4 Remove (`DELETE /api/v1/queue/{clientId}/{itemId}`)

Calls the named client's `Remove(ctx, itemId, deleteData)`; `deleteData` comes from a
query parameter (default false). Used for manual queue cleanup.

### 5.5 Error handling

Typed errors — `ErrClientUnavailable`, `ErrAuthFailed`, `ErrInvalidResponse`,
`ErrUnsupportedProtocol`, `ErrReleaseUnavailable` — map through the existing
`WriteError` JSON envelope. During queue fan-out, per-client errors are captured
into the response rather than propagated as a 500.

## 6. API surface (all auth-guarded)

| Method + path | Purpose |
|---------------|---------|
| `GET /api/v1/downloadclient` | list configured clients |
| `POST /api/v1/downloadclient` | create (validates + tests connectivity) |
| `GET /api/v1/downloadclient/{id}` | get one |
| `PUT /api/v1/downloadclient/{id}` | update |
| `DELETE /api/v1/downloadclient/{id}` | delete |
| `POST /api/v1/downloadclient/{id}/test` | test a saved client; update status |
| `POST /api/v1/downloadclient/test` | test an unsaved config (body) — for the add flow |
| `GET /api/v1/downloadclient/schema` | describe client types + config fields (future UI) |
| `POST /api/v1/download` | grab a release (body: release fields + optional `clientId`) |
| `GET /api/v1/queue` | live aggregated queue snapshot (partial success) |
| `DELETE /api/v1/queue/{clientId}/{itemId}` | remove a queue item (`?deleteData=`) |

## 7. Testing (all offline, CGO-free, deterministic)

- **Unit:** SABnzbd request builder + queue/history parser against recorded JSON
  fixtures; qBittorrent auth/add/info parser against recorded fixtures; grab magnet
  passthrough vs. byte-fetch; routing (protocol + priority + explicit override);
  monitor diff logic (new/changed/removed).
- **Integration:** `httptest.Server`s as fake SABnzbd and qBittorrent, plus a fake
  indexer serving `.nzb` bytes for the grab-fetch path — asserting end-to-end grab,
  queue aggregation with one healthy + one failing client (partial success), and
  remove. API handler tests through the mounted router.
- No real network access in any test. `go test -race` is unavailable in this
  environment (no C compiler); concurrency is verified with `-count=N`.

## 8. Acceptance criteria

1. A SABnzbd and a qBittorrent client can be created, tested, listed, updated, and
   deleted via the API; the credential is write-only (never returned).
2. `POST /api/v1/download` grabs a release: usenet fetches the `.nzb` server-side and
   uploads content to SABnzbd; torrent passes a magnet through and fetches a
   `.torrent` server-side when needed; the indexer API key never leaves Nexus.
3. Routing selects the correct client by protocol + priority, honoring an explicit
   `clientId` override.
4. `GET /api/v1/queue` returns an aggregated snapshot across enabled clients with
   per-client errors surfaced and partial success honored.
5. The monitor command polls the queue and emits `download.status` events observable
   on the WebSocket.
6. A queue item can be removed via the API.
7. `CGO_ENABLED=0 go build ./...` succeeds and `go test ./...` passes.
8. Module boundaries hold: `internal/downloadclient` imports only `internal/core/*`.

## 9. Out of scope (explicit)

- Additional clients (NZBGet, Transmission, Deluge, rTorrent, etc.).
- Grab/download **history** persistence, blocklisting, and failed-download handling.
- Import, rename, and hardlink/copy of completed downloads (media sub-project).
- Per-item queue controls (pause/resume/set-priority) and remote path mappings.
- Category/quality-driven client selection beyond protocol + priority.
- The indexer's per-indexer rate limiter on the grab fetch (grabs are low-volume/
  manual; a plain timed GET is used — see §10).

## 10. Notes & deviations

- **Grab fetch uses a plain 30s-timeout GET**, not the indexer module's per-indexer
  rate limiter. Grabs are low-volume and user/automation-initiated (not fan-out), and
  reusing the limiter would require importing `internal/indexer`, breaking the module
  boundary. Noted as a future enhancement if grab volume ever warrants it.
- **`category` is a per-client config field** (with an optional per-grab override) so
  downloads land in the right folder for the future import pipeline, even though no
  import logic exists yet.
- **Extending `provider.DownloadClient` changes its method set.** No existing fake
  breaks (Foundation's `provider_test.go` only fakes `Indexer`); the plan *adds* a
  fake download client to prove the extended contract.
- **qBittorrent category pre-existence (live-use caveat).** `/torrents/add` with a
  non-existent `category` does not error but may not file the download as expected;
  real deployments call `createCategory` first. Out of scope here (tests use a fake
  server); noted as a follow-up for when running against a live qBittorrent.
- Module path `github.com/hellboundg/nexus` throughout.
