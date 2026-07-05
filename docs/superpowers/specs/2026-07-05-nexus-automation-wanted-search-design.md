# Nexus Automation: Decision Maker & Wanted/Missing Search (Sub-project 5a) Design

**Status:** Approved (brainstorm complete). Ready for implementation plan.
**Depends on:** 4a (media library + monitored flags), 4b (parsing & quality), 4c
(import pipeline / `importing.Enqueue`), sub-2 (indexer search).

## 1. Goal

Give Nexus the first slice of its **automation brain**: the layer that *chooses*
releases for monitored library items and hands them to 4c's import pipeline. 5a
covers the **decision maker** (parse candidates → drop rejects → rank best-first)
and **wanted/missing search** (find monitored items that have no file yet, search
indexers for them, enqueue the best acceptable release). RSS sync (5b) and
upgrade / cutoff-unmet search (5c) are separate slices built on the same core.

Sub-5 as a whole was decomposed into three dependency-ordered slices, mirroring
4a/4b/4c:

- **5a — Decision maker + wanted/missing search.** (This document.)
- **5b — RSS sync.** Untargeted periodic poll of recent releases, parse, match to
  a monitored item, enqueue. Reuses 5a's decision maker.
- **5c — Upgrade / cutoff-unmet search.** Targeted search for items that *have* a
  file below the profile cutoff, gated by `quality.IsUpgrade`.

**Calendar** (roadmap §4 originally listed it under sub-5) is a read-only view
with no decision logic and is deferred to **sub-6 (UI)**, where it is rendered.

## 2. Settled decisions (from brainstorming)

1. **One primitive, three strategies.** Every acquisition mode reduces to *get
   candidate releases → pick the best acceptable one → `importing.Enqueue`*. 5a
   builds the shared decision maker plus the wanted/missing candidate source
   (targeted `indexer.Search`). This mirrors Sonarr/Radarr's
   `DownloadDecisionMaker` + `NzbSearchService` split.
2. **Boundaries via consumer-defined interfaces.** `automation` declares narrow
   `Searcher` and `Enqueuer` interfaces satisfied by the concrete
   `*indexer.Service` / `*importing.Service` in the composition root — the same
   pattern `importing` uses with `Grabber`/`QueueReader`. Media, quality-profile,
   and history reads go directly through `*store.Store`. `automation` never
   imports `indexer`, `downloadclient`, `media`, or `naming`.
3. **TV search granularity = season-pack + episode fallback.** If a monitored
   season is *entirely* missing, search the season pack first (fewer grabs);
   otherwise (or if no acceptable pack is found) search individual missing
   episodes.
4. **Selection = rank + fall through on failure.** Rank accepted candidates
   (quality first, then tiebreaks); attempt to enqueue the best; on grab failure
   try the next candidate. Grounded in Sonarr's `DownloadDecisionComparer`,
   subset to fields Nexus has (no custom formats / delay profiles yet).
5. **Triggers = manual commands (primary) + a conservative scheduled sweep.**
   Manual per-item/season/series/movie search commands are the main path. A
   scheduled `MissingSearch` command runs on a configurable interval (default 6h,
   deliberately conservative because RSS does not exist until 5b) over a bounded
   batch of monitored-but-missing items so indexers are not hammered.
6. **Stateless orchestration.** 5a adds no migration. It reads existing tables
   (series/seasons/episodes/movies, media_files, quality_profiles) and writes only
   through 4c's `Enqueue` (which records the `download_queue` row + history). The
   only new persisted state is one settings key, `automation.config` (sweep
   interval + batch size).

## 3. Architecture

```
internal/automation/
  automation.go   # Service struct, consumer interfaces, NewService
  decide.go       # PURE decision maker: Decide() + comparer chain
  search.go       # wanted/missing strategies (movie, TV season/episode)
  command.go      # command.Command wrappers + scheduled MissingSearch
  config.go       # automation.config settings load/save (interval, batch)
  api.go          # REST sub-router mounted into authed /api/v1
```

Verified dependency set: `automation → internal/core/* (store, provider, events,
command, scheduler) + internal/parsing + internal/quality + internal/importing`.
No import of `indexer`, `downloadclient`, `media`, or `naming`. The
`indexer.Service.Search(...) indexer.SearchResult` → `([]provider.Release, error)`
flattening lives in an adapter in `cmd/nexus/main.go`.

### 3.1 Consumer-defined interfaces

```go
// Searcher runs an aggregated indexer search. Satisfied by an adapter over
// *indexer.Service that returns res.Releases and a joined non-fatal error.
type Searcher interface {
    Search(ctx context.Context, q provider.Query) ([]provider.Release, error)
}

// Enqueuer decides+grabs a chosen release for a target item and records the
// tracking row. Satisfied by *importing.Service.
type Enqueuer interface {
    Enqueue(ctx context.Context, req importing.EnqueueRequest) (store.QueueItem, error)
}
```

Per-indexer errors from `indexer.Search` are already surfaced in `SearchResult`;
the adapter logs them and still returns whatever releases succeeded (partial
results are usable). A search returning zero releases is not an error.

### 3.2 Decision maker (pure core)

```go
type Candidate struct {
    Release provider.Release
    Parsed  parsing.ParsedRelease
}

// Decide parses each release, drops rejects via quality.Decide against the
// profile, and returns the accepted candidates ranked best-first.
func Decide(releases []provider.Release, kind provider.MediaKind,
    profile store.QualityProfile) []Candidate
```

Comparer chain (applied as a stable sort, best-first; first non-zero wins):

1. **Quality** — `quality.Compare(a.Parsed, b.Parsed, profile)` (profile rank,
   then revision). Higher is better.
2. **Torrent seeders** — when both are torrents, more `Release.Seeders` is better.
3. **Usenet age** — when both are usenet, newer `PublishDate` is better (for
   missing search newer is a safe default; matches Sonarr's age preference).
4. **Size** — larger is better, as a final tiebreak.

Season-pack vs single-episode selection is **not** a comparer key — it is handled
by the search strategy (which filters candidates to packs or singles depending on
context), so the comparer stays context-free. `Decide` performs no I/O and is
exhaustively table-tested.

**Parser dependency (enabling change to 4b):** the 4b release-name parser
(`internal/parsing`) currently recognizes a season only when at least one episode
is present (`SxxExx`); a bare season-pack title (`Sxx`, `Season xx`) parses to
`Season==0`. Season-pack search therefore requires a small additive extension to
the parser to set `Season` (with `Episodes` empty) for season-pack titles. This
is an enabling change scoped into 5a, fully covered by parser tests, and does not
alter existing `SxxExx` behavior.

### 3.3 Wanted/missing strategies (`search.go`)

All strategies build a `provider.Query`, call `Searcher.Search`, run `Decide`,
then enqueue candidates best-first, falling through to the next on grab failure.

**Movie** (`searchMovie`):
- Skip unless the movie is `Monitored` and `MediaFileForMovie == nil`.
- `Query{Type: SearchMovie, Kind: KindMovie, Term: title, IMDbID, TMDBID}`.
- `Decide` → attempt each candidate: `Enqueue(EnqueueRequest{MediaKind: movie,
  MovieID, DownloadURL, Title, Protocol, IndexerID})` until one succeeds.

**TV** (`searchSeries` / `searchSeason` / `searchEpisode`):
- Compute the set of monitored episodes with no `MediaFileForEpisode`, grouped by
  season number (only within monitored seasons of a monitored series).
- For a season where **every** monitored episode is missing:
  1. Season-pack search: `Query{Type: SearchTV, Kind: KindTV, Season: &n,
     TVDBID/TMDBID, Term}`.
  2. From `Decide`, keep only full-season packs; attempt to enqueue the best with
     `EnqueueRequest{MediaKind: tv, SeriesID, EpisodeIDs: <all missing in season>,
     ...}`, falling through on failure.
  3. If no acceptable pack, fall through to per-episode search for that season.
- For a partially-missing season (or the fallback above): per-episode search
  `Query{Type: SearchTV, Season: &n, Episode: &e, ...}` and enqueue each with
  `EpisodeIDs: [episodeID]`.
- `searchEpisode` / `searchSeason` are the same logic scoped to one episode / one
  season; `searchSeries` iterates the series' monitored seasons.

Episode→Query id fields: TVDBID/TMDBID come from the `Series` row; the adapter
passes whatever the series has (4a stores `TMDBID`). `Term` is the series title
(+ `SxxEyy` for per-episode) as a fallback for caps-less/text indexers.

### 3.4 Triggers (`command.go`)

- **Manual commands** — `SearchMovieCommand`, `SearchSeriesCommand`,
  `SearchSeasonCommand`, `SearchEpisodeCommand`, each implementing
  `command.Command` (`Name()` + `Run(ctx, Reporter)`), reporting progress
  ("searching", "N grabbed"). Dispatched from the API.
- **Scheduled sweep** — `MissingSearchCommand`: enumerates monitored-but-missing
  items (movies + episodes) up to the configured batch size, runs the appropriate
  strategy for each, reports progress. Registered with the scheduler at the
  configured interval (default 6h). Same-instance factory pattern as the queue
  monitor / `ImportCompleted`.

### 3.5 Config (`config.go`)

`automation.config` settings JSON:

```json
{ "missingSearchIntervalHours": 6, "missingSearchBatchSize": 100 }
```

Loaded on startup (defaults applied when the key is absent), editable via
`GET/PUT /api/v1/automation/config`. Interval change takes effect on next startup
(consistent with how other scheduled intervals are handled today); documented as
such.

### 3.6 REST API (`api.go`)

Mounted into the authed `/api/v1` sub-router in `main.go`:

- `POST /automation/search/movie/{id}` → `SearchMovieCommand`
- `POST /automation/search/series/{id}` → `SearchSeriesCommand`
- `POST /automation/search/series/{id}/season/{n}` → `SearchSeasonCommand`
- `POST /automation/search/episode/{id}` → `SearchEpisodeCommand`
- `GET  /automation/config` / `PUT /automation/config`

Search endpoints dispatch the command onto the existing worker pool via
`command.Manager.Enqueue(Command) (id, error)` and return `202 Accepted` with the
task id; progress is visible over the existing task/WS channels. The API is given
a minimal `Dispatcher interface { Enqueue(command.Command) (string, error) }`
(satisfied by `*command.Manager`) rather than the concrete manager — this is the
first API-driven command dispatch in the codebase (4c's synchronous
`POST /queue/{id}/import` is fine because import is quick; indexer fan-out is not).
Consistent JSON error envelope; unknown/absent ids → 404. The handler validates
the target id exists (via store) *before* enqueuing so a bad id returns 404 rather
than a silently-failing background task.

### 3.7 Events

- `automation.search.completed` — `{kind, id, grabbed}` emitted when a search
  command finishes → WS via `Deps.WSForward`.
- Individual grabs already emit `grabbed` history + `queue.updated` inside
  `importing.Enqueue`; automation does not duplicate them.

## 4. Data flow (wanted/missing, TV season)

```
scheduler/API → SearchSeasonCommand.Run
  → search.searchSeason(seriesID, n)
      → store: series, monitored episodes, MediaFileForEpisode (find missing)
      → all missing? Searcher.Search(tvsearch, season=n)
          → Decide(releases, KindTV, profile)  [pure]
          → keep full-season packs, best-first
          → Enqueuer.Enqueue(EpisodeIDs=allMissing)   ── fall through on error ──┐
      → else / no pack: per-episode Searcher.Search(season=n, episode=e)         │
          → Decide → Enqueuer.Enqueue(EpisodeIDs=[e])  ── fall through on error ─┤
  → emit automation.search.completed{tv, seriesID, grabbed}                      │
                                                                                 │
importing.Enqueue (4c): re-decides, grabs bytes, writes download_queue row + ────┘
  grabbed history + queue.updated;  4c ImportCompleted later imports the file.
```

Note: `Decide` runs in automation for *ranking/selection*; `importing.Enqueue`
re-runs `quality.Decide` as its own accept gate. This double-check is intentional
and cheap — automation picks, importing independently validates before grabbing.

## 5. Error handling

- Partial indexer failure → log, proceed with the releases that returned.
- Zero releases / all rejected → item stays missing, not an error; recorded in the
  command result as "0 grabbed".
- Grab failure on the best candidate → fall through to the next; only if all
  acceptable candidates fail is the item left missing this run.
- **Already in flight → skip (no duplicate grabs).** A media file does not exist
  until 4c imports a completed download, so between grab and import (and for
  stalled downloads that never import) an item still reads as "missing". Before
  searching, automation checks the download queue (`store.ListQueue`, rows in
  `grabbed`/`importing`) and skips any target already in flight — a movie whose
  id is queued, or an episode whose id appears in a queued row's `EpisodeIDs`. A
  fully-missing-season determination also treats queued episodes as not-missing,
  so a partially-in-flight season searches per-episode rather than re-grabbing a
  pack. This makes the scheduled sweep idempotent and prevents concurrent
  manual+scheduled searches from double-grabbing.
- `ErrNoProfile` from `Enqueue` (item has no quality profile) → skip that item,
  log a warning; the sweep continues with the rest of the batch.
- Missing/deleted item id on a manual command → 404.

## 6. Testing

- **`decide_test.go`** — table tests for accept/reject filtering and the full
  comparer chain (quality ordering, season-pack preference, seeders, age, size
  tiebreaks, mixed protocols).
- **`search_test.go`** — fake `Searcher`/`Enqueuer`: assert the correct `Query`
  is built (season-pack vs per-episode, ids/term), the correct `EpisodeIDs` are
  enqueued, fully-vs-partially missing seasons branch correctly, and grab-failure
  fall-through picks the next candidate. Skips already-filed / unmonitored items.
- **`command_test.go`** — commands run via a local `nopReporter`; `MissingSearch`
  respects batch size and enumerates only monitored-missing items.
- **`config_test.go`** — defaults when key absent; round-trip load/save.
- **`api_test.go`** — route dispatch, 202 on search, 404 on bad id, config
  GET/PUT round-trip.
- Boundary check in review: `go list -f '{{ .Imports }}'` confirms `automation`'s
  **direct** imports include no `indexer`/`downloadclient`/`media`/`naming`.
  (Transitive `naming` via `importing` is expected and allowed.)

## 7. Out of scope (5a)

- RSS sync (5b) — untargeted periodic feed poll.
- Upgrade / cutoff-unmet search (5c) — searching items that already have a file.
- Calendar (sub-6).
- Custom formats, delay profiles, release-restriction lists, indexer priority in
  the comparer (no data model for them yet).
- Interactive/manual release selection UI (sub-6).
- Reactive "search on add" (series/movie just added → immediate search): can be a
  small follow-up once 5a lands; not required for the slice.

## 8. Acceptance criteria

1. `Decide` returns only profile-accepted candidates, ranked by the documented
   comparer chain; pure and fully unit-tested.
2. A manual movie/episode/season/series search builds the correct query, decides,
   and enqueues the best acceptable release via `importing.Enqueue`, falling
   through on grab failure.
3. A fully-missing monitored season prefers an acceptable season pack (enqueued
   with all missing episode ids) and falls back to per-episode when no pack is
   acceptable; a partially-missing season searches per-episode.
4. Already-filed or unmonitored items are skipped.
5. The scheduled `MissingSearch` runs at the configured interval over a bounded
   batch; interval/batch editable via `automation.config`.
6. `automation.search.completed` reaches the WS; grabs record history via 4c.
7. Module boundaries verified: `automation` imports only `core/*` + `parsing` +
   `quality` + `importing`.
8. Green: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...`.
