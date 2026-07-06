# Nexus Automation: RSS Sync (Sub-project 5b) Design

**Status:** Approved (brainstorm complete). Ready for implementation plan.
**Depends on:** 5a (decision maker `automation.Decide` + `Searcher`/`Enqueuer`
interfaces + queue-dedup guard), 4a (media library + monitored flags), 4b
(parsing & quality), 4c (import pipeline / `importing.Enqueue`), sub-2 (indexer
search).

## 1. Goal

Give Nexus the second slice of its **automation brain**: **RSS sync**. Where 5a's
wanted/missing search is *targeted* (it starts from a monitored item and asks
indexers for it), RSS sync is *release-driven*: it periodically polls each enabled
indexer's "latest" feed, and for each recent release it reverse-matches to a
monitored library item and grabs the best acceptable one. This is how Nexus picks
up new releases as they are posted, without waiting for the conservative 6h
missing sweep.

5b is deliberately a **re-composition of 5a**: it reuses `automation.Decide`, the
`Searcher`/`Enqueuer` interfaces, and the queue-dedup guard. The genuinely new
logic is the **reverse matcher** (release → monitored item) and the
**group-by-target** pipeline. Upgrades of items that already have a file remain
out of scope (that is 5c).

Sub-5 decomposition (unchanged):

- **5a — Decision maker + wanted/missing search.** DONE, merged.
- **5b — RSS sync.** (This document.)
- **5c — Upgrade / cutoff-unmet search.** Targeted search for items that *have* a
  file below the profile cutoff, gated by `quality.IsUpgrade`.

## 2. Settled decisions (from brainstorming)

1. **ID-first matching, title+year fallback, drop-on-ambiguous.** Indexer feeds
   routinely carry `tmdbid`/`imdbid`/`tvdbid` newznab/torznab attrs; the current
   parser reads `category`/`size`/`seeders`/`peers` but **drops** the id attrs.
   5b captures them on `provider.Release` and matches on exact id when present,
   falling back to a normalized title (+ year for movies, + first-aired year for
   TV) only when no id matches. An ambiguous match (a title that resolves to more
   than one monitored item, or a release whose kind cannot be determined) is
   dropped and logged. This turns the classic false-positive cases (The Office
   US/UK, same-title-different-year films, remakes) into exact-id hits.
2. **Poll all enabled indexers, no migration.** RSS sync fans out to every enabled
   indexer via the existing aggregated `Searcher.Search` with a generic empty-term
   query (which Newznab/Torznab treat as the latest-items feed). No per-indexer
   "enable RSS" flag and no schema change in this slice; such a toggle can be added
   later if a user needs to exclude a specific indexer from RSS.
3. **Reuse everything reusable from 5a.** `automation.Decide` (parse → drop
   rejects → rank best-first), the `Searcher`/`Enqueuer` consumer interfaces, and
   the `activeQueue` (in-flight) + `MediaFileFor*` (already-filed) guards are all
   reused verbatim. No new package; 5b lands in `internal/automation`.
4. **Group by target, then Decide+enqueueBest per target.** RSS is release-driven
   but the guards are target-driven. Processing `for each release: match → Decide →
   enqueue` would let two feed items covering the same episode both grab, because
   the `activeQueue` snapshot taken at poll start does not reflect the row just
   written. Instead 5b collects `map[target][]Candidate` across the whole feed,
   then per target applies the skip-guards once and runs `Decide` + `enqueueBest`
   over that target's candidates — picking the *best* of duplicate releases rather
   than the first seen, exactly mirroring 5a's per-target flow.
5. **No persisted "seen GUID" cache (YAGNI).** Correctness against duplicate grabs
   comes from the same two guards 5a's sweep already relies on: a target that is
   in-flight (`download_queue` row `grabbed`/`importing`) or already has a
   `media_file` is skipped. Re-parsing the feed each poll is wasteful but correct;
   a seen-cache would be a pure optimization. **Known limitation:** a release that
   grabs-then-fails is re-considered every poll (more visible at a 15-min interval
   than at 6h) — deferred to a future failed-release / blocklist mechanism.
6. **Config: enabled flag + minute-granularity interval.** RSS runs much more
   frequently than the sweep, so it gets its own fields on `automation.config`:
   `rssSyncEnabled` (default true) and `rssSyncIntervalMinutes` (default 15). The
   scheduler registration is read at startup and gated on the enabled flag,
   consistent with how the sweep interval is applied today.

## 3. Architecture

```
internal/automation/
  automation.go   # Service, consumer interfaces, NewService  (existing; +RSSCompleted event)
  decide.go       # PURE decision maker (existing, reused unchanged)
  search.go       # 5a wanted/missing strategies (existing; activeQueue/profileFor/enqueueBest reused)
  rss.go          # NEW: RSSSync pipeline + reverse matcher + group-by-target
  command.go      # +NewRSSSyncCommand
  config.go       # +rssSyncEnabled / +rssSyncIntervalMinutes
  api.go          # unchanged (RSS is scheduled-only; no manual endpoint this slice)

internal/core/provider/provider.go   # +Release.TMDBID / .IMDbID / .TVDBID (additive)
internal/indexer/parse.go            # +capture tmdbid/imdbid/tvdbid attrs (additive)

cmd/nexus/main.go                    # register RSS scheduler job (gated on enabled); +WSForward event
```

Verified dependency set is **unchanged**: `automation → internal/core/* +
internal/parsing + internal/quality + internal/importing`. The new `Release` id
fields live in `internal/core/provider`; the parser change lives in
`internal/indexer` — neither adds an import to `automation`.

### 3.1 Release identity capture (`provider` + `indexer`)

Additive fields on `provider.Release`:

```go
type Release struct {
    // ... existing fields ...
    TMDBID int    // from newznab/torznab attr "tmdbid" (0 when absent)
    IMDbID string // from attr "imdbid", normalized without the "tt" prefix ("" when absent)
    TVDBID int    // from attr "tvdbid" (0 when absent)
}
```

`indexer/parse.go` gains three cases in its existing attr switch (`tmdbid`,
`imdbid`, `tvdbid`), parsed the same way `season`/`ep` ids would be. `imdbid` is
stored with any leading `tt` stripped so it compares directly against
`store.Movie.IMDbID`'s stored form. All fields are optional; every existing
consumer (search, health, dedupe) ignores them.

### 3.2 Feed fetch (reuse `Searcher`)

RSS sync does **not** need a new interface. A latest-items feed is a generic
empty-term query, which the indexer layer already supports (`buildSearchURL` with
`t=search` and no `q`; `Supports` gates on `caps.Search`). `RSSSync` calls:

```go
releases, err := s.search.Search(ctx, provider.Query{Type: provider.SearchGeneric, Limit: rssFeedLimit})
```

`indexer.Service.Search` fans out to all enabled indexers, dedupes by GUID/title,
and returns releases sorted newest-first; the `autoSearchAdapter` in `main.go`
already flattens `SearchResult` → `([]provider.Release, error)`, surfacing
per-indexer errors as a non-fatal aggregate. A bounded `rssFeedLimit` (constant,
e.g. 100) keeps each indexer's response size sane. Partial indexer failure and
zero releases are both non-fatal.

### 3.3 Reverse matcher (the new logic, `rss.go`)

Per release, resolve a **kind** then a **target**:

**Kind routing.** By Newznab category first: `2000–2999 → movie`, `5000–5999 →
TV`. When no category is present, fall back to a parse heuristic: parse once as TV
— if `Season > 0` treat as TV, else if a `Year` parses treat as movie; otherwise
**undecidable → drop** (logged).

**Movie target.** Resolve against monitored movies in priority order:
1. `release.TMDBID` == `movie.TMDBID` (both non-zero).
2. `release.IMDbID` == `movie.IMDbID` (both non-empty).
3. normalized `parsed.Title` + `parsed.Year` == normalized `movie.Title` +
   `movie.Year`.

**TV target.** Resolve the *series* against monitored series:
1. `release.TMDBID` == `series.TMDBID` (both non-zero). Our metadata source is
   TMDB, so `series.TMDBID` is populated.
2. normalized `parsed.Title`, disambiguated by first-aired year when the title
   alone is ambiguous.

   *Honest caveat:* many TV feeds carry `tvdbid` rather than `tmdbid`, and Nexus
   does not store a TVDB id on `series`, so TV matches frequently land on the
   title(+year) fallback. This is acceptable for the slice; adding a TVDB id to
   the series model is a possible future enhancement, not required here.

   Once the series is resolved, the release's `parsed.Season` / `parsed.Episodes`
   select the covered episodes within that series (same parsing the 5a strategies
   use). A season-pack release (`Season > 0`, empty `Episodes`) targets the
   `(series, season)`; an episode release targets the specific covered episode(s).

**Normalization** (matcher-local, pure): lowercase, drop non-alphanumeric,
collapse whitespace. A normalized title that maps to more than one monitored item
is **ambiguous → drop**.

### 3.4 Group-by-target + Decide + enqueueBest (`rss.go`)

After matching, releases are bucketed by resolved target, then processed with the
same guards and selection as 5a:

1. **Movies** — `map[movieID][]Candidate`. Per movie: skip if it has a file
   (`MediaFileForMovie`) or is in-flight (`activeQueue` movies set); else
   `Decide(cands, KindMovie, profile)` → `enqueueBest` with a movie
   `EnqueueRequest`.
2. **TV season packs** — `map[{seriesID,season}][]Candidate`, considered only when
   **every** monitored episode of that season is missing and not in-flight (5a's
   fully-missing rule). Per bucket: `Decide` (keep full-season packs) →
   `enqueueBest` with `EpisodeIDs = <all missing in season>`. Episodes covered by a
   grabbed pack are marked handled for this poll (pack beats per-episode, matching
   5a precedence).
3. **TV episodes** — for each still-unhandled monitored-missing-and-not-in-flight
   episode, gather the candidate releases covering it, `Decide` → `enqueueBest`
   with `EpisodeIDs = [episodeID]`.

`activeQueue`, `profileFor`, `enqueueBest`, `Decide`, `tvRequest`, and the movie
`EnqueueRequest` builder are all reused from 5a's `search.go`. Per-target grab
failure falls through to the next candidate; `importing.ErrNoProfile` is terminal
for that target (skip, log). The `activeQueue` snapshot is taken once at the start
of the poll and grouping happens before any enqueue, so within-poll duplicates are
resolved by *selection* (best candidate) rather than racing the guard.

### 3.5 Config (`config.go`)

`automation.config` settings JSON gains two fields (existing sweep fields
unchanged):

```json
{
  "missingSearchIntervalHours": 6,
  "missingSearchBatchSize": 100,
  "rssSyncEnabled": true,
  "rssSyncIntervalMinutes": 15
}
```

`DefaultConfig()` sets `RSSSyncEnabled: true`, `RSSSyncIntervalMinutes: 15`. A
non-positive `rssSyncIntervalMinutes` clamps to the default (same guard the sweep
fields use, so a bad value can't produce an unbounded scheduler). `rssSyncEnabled`
is respected as stored; when the key is absent entirely, the default (enabled)
applies. Editable via the existing `GET/PUT /api/v1/automation/config`; interval
and enabled changes take effect on next startup, documented as such (consistent
with the sweep interval today).

### 3.6 Command, scheduler, event

- **Command** — `NewRSSSyncCommand(svc)` returns a `command.Command` named
  `RSSSync` that runs `svc.RSSSync(ctx)` and reports progress
  ("polling", "N grabbed"), mirroring the existing search-command wrapper.
- **Scheduler** — `main.go` registers
  `sch.Every(time.Duration(cfg.RSSSyncIntervalMinutes)*time.Minute, …)` **only when
  `cfg.RSSSyncEnabled`**. Same-instance factory pattern as the sweep / queue
  monitor / `ImportCompleted`.
- **Event** — a new `automation.rss.completed` event
  `{considered, matched, grabbed}` is emitted when a poll finishes and added to
  `Deps.WSForward` in `main.go`. Individual grabs already emit `grabbed` history +
  `queue.updated` inside `importing.Enqueue`; RSS does not duplicate those.

## 4. Data flow (one RSS poll)

```
scheduler → RSSSyncCommand.Run
  → automation.RSSSync(ctx)
      → Searcher.Search(SearchGeneric, limit)          # aggregated latest feed, all enabled indexers
      → build monitored library index (movies + series, by id and normalized title)
      → for each release:
            kind = routeKind(category | parse heuristic)     # drop if undecidable
            target = match(release, kind)                     # id-first, title+year fallback; drop if ambiguous
            append Candidate{Release, Parsed} to buckets[target]
      → activeQueue snapshot (in-flight movies/episodes)
      → movies:        per movieID     → skip filed/in-flight → Decide → enqueueBest
      → TV packs:      per (series,season) fully-missing → Decide(packs) → enqueueBest(all missing) → mark handled
      → TV episodes:   per unhandled missing episode → Decide → enqueueBest
  → emit automation.rss.completed{considered, matched, grabbed}

importing.Enqueue (4c): re-decides, grabs bytes, writes download_queue row +
  grabbed history + queue.updated; 4c ImportCompleted later imports the file.
```

As in 5a, `Decide` runs in automation for ranking/selection and
`importing.Enqueue` independently re-runs `quality.Decide` as its own accept gate.

## 5. Error handling

- Partial indexer failure → log, proceed with the releases that returned; zero
  releases is not an error.
- Undecidable kind, no matching monitored item, or ambiguous match → skip that
  release (logged), continue the poll.
- Already-filed or in-flight target → skipped by the same guards as 5a; a
  partially-in-flight season is handled per-episode rather than as a pack.
- Grab failure on the best candidate → fall through to the next; only if all
  acceptable candidates for a target fail is that target left this poll.
- `ErrNoProfile` from `Enqueue` (target has no quality profile) → skip that
  target, log; the poll continues.
- A target with an unresolved id but a confident title match is grabbed; the
  double-check in `importing.Enqueue` is the final safety gate before bytes move.

## 6. Testing

- **`provider` / `indexer` parse tests** — a feed item carrying
  `tmdbid`/`imdbid`/`tvdbid` attrs surfaces them on `Release` (imdb `tt` prefix
  stripped); absence yields zero values; existing attrs still parse.
- **`rss_test.go` — matcher** — table tests: movie by tmdbid, by imdbid, by
  title+year fallback; series by tmdbid, by title+first-aired-year fallback;
  kind routing by category and by parse-heuristic fallback; ambiguous title →
  drop; undecidable kind → drop.
- **`rss_test.go` — pipeline** (fake `Searcher`/`Enqueuer`): group-by-target picks
  the best of two duplicate releases covering one episode (single grab); a
  fully-missing season prefers a pack and marks its episodes handled (no
  per-episode double-grab); filed / in-flight / unmonitored targets skipped;
  grab-failure fall-through; `considered/matched/grabbed` counts correct.
- **`config_test.go`** — RSS defaults when key absent; non-positive interval
  clamps; enabled respected; round-trip load/save preserving the sweep fields.
- **`command_test.go`** — `RSSSync` command runs via a local `nopReporter` and
  reports the grabbed count.
- Boundary check in review: `go list -f '{{ .Imports }}'` confirms `automation`'s
  direct imports still include no `indexer`/`downloadclient`/`media`/`naming`.

## 7. Out of scope (5b)

- Upgrade / cutoff-unmet search (5c) — RSS grabs only for *missing* monitored
  items; a release better than an existing file is ignored this slice.
- Persisted seen-release cache and failed-release / blocklist (known-limitation
  above).
- Per-indexer "enable RSS" toggle (migration) — poll-all-enabled this slice.
- TVDB id on the series model (would strengthen TV id-matching).
- A manual "trigger RSS now" API endpoint (RSS is scheduled-only this slice; can
  be a trivial follow-up).
- Custom formats, delay profiles, release-restriction lists (no data model yet).

## 8. Acceptance criteria

1. `provider.Release` carries `TMDBID`/`IMDbID`/`TVDBID`, populated by the Newznab
   parser from the corresponding feed attrs (imdb `tt`-stripped), and dropped
   gracefully when absent.
2. `RSSSync` polls all enabled indexers via one generic empty-term aggregated
   search and reverse-matches each release to a monitored item using id-first,
   title+year fallback, dropping ambiguous/undecidable releases.
3. Candidates are grouped by resolved target; per target the item is skipped when
   filed or in-flight, and the *best* acceptable release is enqueued via
   `importing.Enqueue` (best-of-duplicates, not first-seen), with season packs
   preferred over per-episode for a fully-missing season.
4. RSS sync is scheduled at `rssSyncIntervalMinutes` (default 15) and registered
   only when `rssSyncEnabled` (default true); both editable via `automation.config`
   with the same clamping guarantees as the sweep fields.
5. `automation.rss.completed{considered, matched, grabbed}` reaches the WS; grabs
   record history via 4c.
6. Module boundaries verified: `automation` imports only `core/*` + `parsing` +
   `quality` + `importing`.
7. Green: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...`.
