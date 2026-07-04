# Nexus — Media Library Design Spec (Sub-project 4a)

**Date:** 2026-07-04
**Status:** Approved (design), pending implementation plan
**Program context:** Sub-project 4 (Media management) from the foundation roadmap
(`2026-07-01-nexus-foundation-design.md` §4) is too large for a single spec. It is
decomposed into three slices, each with its own spec → plan → build cycle:

- **4a — Library core (this spec):** media data model, TMDb metadata provider, root
  folders, add/refresh/monitor, REST + WS.
- **4b — Parsing & quality:** release-name parser, quality profiles / custom formats,
  release decision/scoring engine.
- **4c — Import & rename:** link completed downloads to library items, rename via
  templates, move into root folders, file tracking, history.

Slices are built in order; 4b and 4c depend on 4a. This spec covers **4a only**.

---

## 1. Purpose

Give Nexus a persistent library of the TV series and movies a user wants to manage,
populated from an external metadata source. This is the spine the rest of media
management hangs on: parsing/quality (4b) scores releases *against* library items,
and import (4c) files completed downloads *into* library items. 4a establishes the
data model, the metadata provider interface (with one concrete provider), root
folders, and the add/refresh/monitor lifecycle — but performs no disk, parsing,
searching, or grabbing work.

## 2. Scope decisions (settled during brainstorming)

1. **Metadata provider: TMDb only, covering both TV and movies**, behind a generic
   `provider.MetadataProvider` interface. A single API key and one HTTP client cover
   both media kinds — the smallest integration surface to prove the interface and
   model. TVDB and others can be added later behind the same interface.
2. **Full library model, no files yet.** Model `series → seasons → episodes` and
   `movies`. Refresh pulls full episode lists from TMDb. **No** `episode_files` /
   `movie_files` tables and **no** disk scanning — those land in 4c.
3. **Monitoring: flag + simple add options.** A `monitored` boolean on
   series/season/episode and movie. At add time apply a monitor choice
   (`all` / `future` / `none` for series; monitored yes/no for movies) and expose
   toggle endpoints. No search/grab logic consumes it yet (that is sub-5).
4. **Quality profiles are deferred to 4b.** `series.quality_profile_id` and
   `movies.quality_profile_id` are nullable placeholder columns in 4a, wired to real
   profiles in 4b.
5. **Images are URLs, not downloads.** Store the TMDb poster/fanart URLs; do not
   download or cache artwork bytes in 4a.

## 3. Architecture & module boundaries

New engine package `internal/media`, depending only on `internal/core/*` — never on
`internal/indexer`, `internal/downloadclient`, or `internal/automation` (same rule
enforced for every feature module). It communicates outward only through the store,
the event bus, and its mounted REST routes.

### 3.1 Foundation touch-up this sub-project requires

- **`core/provider`:** add the `MetadataProvider` interface and its metadata result
  types (`MetadataResult`, `SeriesMetadata`, `SeasonMetadata`, `EpisodeMetadata`,
  `MovieMetadata`). Pure contract types; no behavior.
- **`core/store`:** add media store methods (`media_store.go`) and migration
  `0004_media.sql`. Reuse existing helpers (`boolToInt`, `rowScanner`) — do not
  redefine them.
- **`cmd/nexus/main.go`:** construct `media.NewService`, `media.NewAPI`,
  `media.NewRefresh`; register the refresh command with the scheduler; mount the
  media API sub-router; add `media.series.updated` and `media.movie.updated` to
  `api.Deps.WSForward`.

### 3.2 Package layout

```
internal/media/
  media.go        Service: add / refresh / list / monitor orchestration
  tmdb.go         TMDBClient implementing provider.MetadataProvider
  refresh.go      RefreshCommand (scheduler-driven metadata refresh)
  api.go          REST sub-router (Mount), request/response shaping
  errors.go       Typed errors
  testdata/       Recorded TMDb JSON fixtures
```

### 3.3 Component responsibilities

- **`Service`** owns all library mutations. It never talks HTTP to TMDb directly; it
  depends on a `provider.MetadataProvider` (the real `TMDBClient` in production, a
  fake in tests) and on the `Store`. Add/refresh/monitor logic and season/episode
  reconciliation live here.
- **`TMDBClient`** is the only component that knows TMDb's wire format. It maps TMDb
  JSON to the `provider` metadata types, applies rate limiting and short-TTL caching,
  and surfaces typed errors.
- **`RefreshCommand`** implements `command.Command`; a single persistent instance is
  registered with the scheduler (same pattern as the download-client monitor) so a
  periodic refresh-all runs (~12h). One-off refreshes go through the `Service`
  directly from the API.
- **`API`** validates input, calls the `Service`, and shapes JSON responses. It never
  reaches into the store or provider directly.

## 4. Data model & contracts

### 4.1 Tables (migration `0004_media.sql`)

**`root_folders`**

| column | type | notes |
|--------|------|-------|
| `id` | INTEGER PK | |
| `path` | TEXT NOT NULL UNIQUE | validated (exists + writable) on create |
| `created_at` | TEXT NOT NULL | RFC3339 |

**`series`**

| column | type | notes |
|--------|------|-------|
| `id` | INTEGER PK | |
| `tmdb_id` | INTEGER NOT NULL UNIQUE | |
| `title` | TEXT NOT NULL | |
| `sort_title` | TEXT NOT NULL | |
| `overview` | TEXT NOT NULL DEFAULT '' | |
| `status` | TEXT NOT NULL DEFAULT '' | e.g. `continuing` / `ended` |
| `first_aired` | TEXT | nullable RFC3339 date |
| `poster_url` | TEXT NOT NULL DEFAULT '' | TMDb URL |
| `fanart_url` | TEXT NOT NULL DEFAULT '' | TMDb URL |
| `root_folder_id` | INTEGER | FK → `root_folders(id)` |
| `quality_profile_id` | INTEGER | nullable placeholder (4b) |
| `monitored` | INTEGER NOT NULL DEFAULT 1 | bool |
| `added_at` | TEXT NOT NULL | |
| `last_refreshed_at` | TEXT | nullable |

**`seasons`** — `id` PK, `series_id` FK, `season_number` INT NOT NULL,
`monitored` INT NOT NULL DEFAULT 1. `UNIQUE(series_id, season_number)`.
`ON DELETE CASCADE` from series.

**`episodes`** — `id` PK, `series_id` FK, `season_number` INT NOT NULL,
`episode_number` INT NOT NULL, `tmdb_id` INT, `title` TEXT NOT NULL DEFAULT '',
`overview` TEXT NOT NULL DEFAULT '', `air_date` TEXT (nullable),
`monitored` INT NOT NULL DEFAULT 1. `UNIQUE(series_id, season_number, episode_number)`.
`ON DELETE CASCADE` from series.

**`movies`** — `id` PK, `tmdb_id` INT NOT NULL UNIQUE, `title`, `sort_title`,
`overview` DEFAULT '', `status` DEFAULT '', `year` INT, `release_date` TEXT (nullable),
`runtime` INT, `imdb_id` TEXT DEFAULT '', `poster_url` DEFAULT '',
`fanart_url` DEFAULT '', `root_folder_id` FK, `quality_profile_id` (nullable),
`monitored` INT NOT NULL DEFAULT 1, `added_at`, `last_refreshed_at` (nullable).

### 4.2 Store types

`store.Series`, `store.Season`, `store.Episode`, `store.Movie`, `store.RootFolder`
Go structs mirror the columns. Store methods (names indicative):
`CreateRootFolder / ListRootFolders / DeleteRootFolder`;
`CreateSeries / GetSeries / ListSeries / UpdateSeries / DeleteSeries`;
`UpsertSeason / ListSeasons / SetSeasonMonitored`;
`UpsertEpisodes(batch) / ListEpisodes / SetEpisodeMonitored`;
`CreateMovie / GetMovie / ListMovies / UpdateMovie / DeleteMovie / SetMovieMonitored`.
Upserts key on the `UNIQUE` constraints so refresh is idempotent.

### 4.3 `provider.MetadataProvider` contract

```go
type MetadataProvider interface {
    SearchTV(ctx, term string) ([]MetadataResult, error)
    SearchMovie(ctx, term string) ([]MetadataResult, error)
    TVDetails(ctx, tmdbID int) (SeriesMetadata, error)   // includes seasons + episodes
    MovieDetails(ctx, tmdbID int) (MovieMetadata, error)
}
```

- `MetadataResult` — `TMDBID, Title, Year, Overview, PosterURL, Kind`.
- `SeriesMetadata` — series fields + `[]SeasonMetadata`, each with `[]EpisodeMetadata`.
- `MovieMetadata` — movie fields (incl. `IMDbID`, `Runtime`, `ReleaseDate`).

These are pure data; the interface has no persistence concerns.

### 4.4 Credential handling

The TMDb API key follows the established write-only pattern: stored via config
(bootstrap env `NEXUS_TMDB_API_KEY`, consistent with other bootstrap keys), never
serialized back in any API response.

## 5. Runtime behavior

### 5.1 Lookup (add picker) — `GET /api/v1/media/lookup?term=&kind=`
Live TMDb search (`SearchTV` or `SearchMovie` by `kind`); returns `[]MetadataResult`.
No persistence. This is what the UI's "add series/movie" search box calls.

### 5.2 Add — `POST /api/v1/series` / `POST /api/v1/movies`
Body: `{ tmdbId, rootFolderId, monitored | monitorOption }`. The service:
1. Rejects duplicates (`tmdb_id` already present) → `ErrAlreadyExists`.
2. Validates the root folder exists.
3. Fetches details from the provider; persists the series/movie.
4. For series: persists seasons + episodes; applies the monitor option
   (`all` = every episode monitored; `future` = only unaired episodes monitored;
   `none` = none). For movies: sets `monitored` from the request.
5. Publishes `media.series.updated` / `media.movie.updated`.

### 5.3 Refresh — `POST /api/v1/series/{id}/refresh` and scheduled
Re-pulls details from the provider and **reconciles**: new seasons/episodes are
inserted (upsert on the unique key), existing titles/air-dates/overviews/status are
updated, `last_refreshed_at` is set. Monitored flags set by the user are preserved
(reconcile updates descriptive fields, not user monitoring choices); newly discovered
episodes inherit their season's monitored state. A single persistent `RefreshCommand`
registered with the scheduler refreshes all monitored library items (~12h). Emits the
same update events. Removing an item that no longer exists upstream is **not** done
automatically (out of scope — see §7).

### 5.4 Monitor toggles
`PUT /series/{id}/monitor`, `PUT /season/{id}/monitor`, `PUT /episode/{id}/monitor`,
`PUT /movies/{id}/monitor` — body `{ monitored: bool }`. Toggling a series **cascades**
to all its seasons and episodes; toggling a season cascades to that season's episodes;
an episode toggle affects only itself. Emits update events.

### 5.5 Events → WebSocket
`media.series.updated` and `media.movie.updated` carry the affected item id + kind.
Published **asynchronously** (`bus.PublishAsync`) so a slow/blocking subscriber can
never stall a refresh under a lock — applying the sub-3 emit-under-lock lesson.

### 5.6 Error handling
Typed errors in `errors.go`: `ErrNotFound` (unknown local id — maps 404),
`ErrAlreadyExists` (duplicate add — 409), `ErrInvalidRootFolder` (missing/unwritable
path — 400), `ErrProviderUnavailable` (TMDb network/5xx/auth failure — 502),
`ErrProviderNotConfigured` (no API key — 400/503). The API maps sentinels to status
codes; unknown errors → 500.

## 6. API surface (all auth-guarded, under `/api/v1`)

- `GET  /media/lookup?term=&kind=tv|movie`
- `GET|POST /series`, `GET|PUT|DELETE /series/{id}`, `POST /series/{id}/refresh`,
  `PUT /series/{id}/monitor`, `PUT /season/{id}/monitor`, `PUT /episode/{id}/monitor`
- `GET|POST /movies`, `GET|PUT|DELETE /movies/{id}`, `POST /movies/{id}/refresh`,
  `PUT /movies/{id}/monitor`
- `GET|POST /rootfolder`, `DELETE /rootfolder/{id}`

`GET /series/{id}` and `GET /movies/{id}` return the item with its nested
seasons/episodes (series). Consistent JSON error envelope from `core/api`.

## 7. Testing (all offline, CGO-free, deterministic)

- **`TMDBClient`** against an `httptest.Server` serving recorded JSON fixtures from
  `testdata/` — verify search mapping, TV details (seasons + episodes), movie details,
  rate-limit call shape, and error mapping (non-200 / auth failure →
  `ErrProviderUnavailable`).
- **Store** against a temp SQLite DB — CRUD + upsert idempotency (refresh twice yields
  no duplicate seasons/episodes) + cascade delete.
- **`Service`** with a **fake `MetadataProvider`** — add (dedupe, monitor options
  all/future/none, root-folder validation), refresh reconciliation (new episode added,
  title updated, user monitored flags preserved), monitor cascade.
- **API** via the mounted router — status codes for happy/sad paths (200/201/400/404/
  409), credential never present in responses.
- No network; no `-race` (no CGO — verify with `-count=N`).

## 8. Acceptance criteria

1. Migration `0004_media.sql` applies cleanly; idempotency test expects 4 migrations.
2. `GET /media/lookup` returns TMDb search results for a term (via fixture).
3. Adding a series persists it plus its seasons and episodes, honoring the monitor
   option; adding a movie persists it; duplicates are rejected with 409.
4. Refresh reconciles metadata idempotently (no duplicate rows on repeat) and updates
   descriptive fields while preserving user monitored choices.
5. Monitor toggles work at series/season/episode/movie level.
6. Root folder create validates the path; list/delete work.
7. `media.series.updated` / `media.movie.updated` reach the WebSocket.
8. The TMDb API key never appears in any response body.
9. `internal/media` imports only `internal/core/*`.
10. Full sweep green: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...`.

## 9. Out of scope (explicit — later slices / sub-projects)

- File/disk records, disk scanning, rename, import (**4c**).
- Quality profiles, custom formats, release scoring (**4b**); `quality_profile_id`
  is a nullable placeholder only.
- Searching, grabbing, RSS sync, wanted/missing, calendar, upgrades (**sub-5**).
- Artwork download/caching (URLs only).
- Additional metadata providers (TVDB etc.) — the interface allows them; only TMDb
  ships in 4a.
- Automatic deletion of items removed upstream.
- Multi-user / per-user libraries (single admin, per foundation).

## 10. Notes & deviations

- **Provider-interface reuse:** `MetadataProvider` mirrors the shape of the existing
  `Indexer`/`DownloadClient` provider contracts — pure data in, typed results out,
  concrete client isolates the wire format. Consistent with the modular-monolith
  boundary.
- **Async WS forwarding is deliberate** (see §5.5) — the download-client monitor's
  synchronous emit-under-lock was flagged as latent coupling; media refresh adopts
  `PublishAsync` from the start.
- **Quality-profile columns now, wired later:** carrying the nullable FK in `0004`
  avoids a second media-table migration when 4b lands.
