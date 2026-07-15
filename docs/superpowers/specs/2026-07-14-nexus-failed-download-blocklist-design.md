# Nexus — Failed-Download Handling + Blocklist (Wave C, Slice C1) Design

**Date:** 2026-07-14
**Status:** Approved (design)
**Scope:** Full-stack. New store table + migration, backend failure-handling +
blocklist, automation search filter, and an Activity "Blocklist" page.

## 1. Context

After the Wave A/B web-UI work and the Docker import fix, the user asked for
Sonarr/Radarr-style download-pipeline behaviour. Wave C decomposes into three
independent slices, built one at a time:

- **C1 — Failed-download handling + blocklist** (this doc).
- C2 — Live download progress in Activity. *Separate spec.*
- C3 — Interactive "pick a release" search. *Separate spec.*

Order: **C1 → C2 → C3** (C1 is the user's priority and the blocklist it builds
is reused by C3).

**Problem today.** `automation.enqueueBest` stops once a release is *grabbed*
(handed to SAB/qBit); it never learns whether the download later fails. The
download monitor emits `download.status` events but nothing consumes a
`StatusFailed`. So when an NZB fails (e.g. missing articles) Nexus does nothing:
no retry, no record, and the dead item lingers. There is **no blocklist** of any
kind. Separately, the importer sets a queue row's status to `imported` but never
removes it, so completed items linger in the Queue view (`GET /queue` returns all
rows via `store.ListQueue`).

**User decisions (locked):**
1. Blocklist scope = **the media item** (movie / series), not global.
2. Trigger = **explicit client failures only** (`provider.StatusFailed`); stalled
   torrents are out of scope for C1.
3. Queue is **transient** — imported/handled-failure rows leave the Queue and live
   only in History/Blocklist (Sonarr parity).
4. Manual control in C1 = **remove from blocklist** only (no manual
   "remove & blocklist" queue action yet).

**Relevant existing code (verified):**
- `internal/importing/importing.go` — `Service{ store, grab Grabber, queue
  QueueReader, bus }`; `QueueReader` exposes `Queue(ctx) []provider.DownloadItem`
  and `Remove(ctx, clientID, itemID, deleteData)`.
- `internal/importing/command.go` — `ImportCompleted(ctx)` sweeps `grabbed` rows,
  matches each to its live item via `matchItem`, imports the `StatusCompleted`
  ones; scheduled every 1 min in `cmd/nexus/main.go:156`.
- `internal/importing/importer.go` — `ImportItem`, and `fail()` (writes a
  `download_failed`/`import_failed` history event + sets queue status). History is
  already written on grab (`enqueue.go:59`), import (`importer.go:180`), and
  failure (`importer.go:285`).
- `internal/automation/search.go` — `searchMovie`/`searchSeason`/`searchEpisode`
  build candidates with `Decide(releases, kind, profile) []Candidate` then
  `enqueueBest`. `Candidate{ Release provider.Release; Parsed parsing.ParsedRelease }`.
- `internal/core/store/import_store.go` — `download_queue` table, statuses
  `grabbed/importing/imported/failed`, `ListQueue`, `QueueByStatus`,
  `SetQueueStatus`, `AddHistory`, `ListHistory`. `QueueItem` has `SourceTitle`,
  `MediaKind`, `MovieID`, `SeriesID`, `EpisodeIDs`, `QualityID`, `Protocol`.
- `web/src/features/activity/ActivityLayout.tsx` — tabs Queue + History over
  `/activity/{queue,history}`.

## 2. Data model — `blocklist` (migration 0007)

```sql
CREATE TABLE blocklist (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  media_kind   TEXT    NOT NULL,                 -- "movie" | "tv"
  movie_id     INTEGER REFERENCES movies(id)  ON DELETE CASCADE,
  series_id    INTEGER REFERENCES series(id)  ON DELETE CASCADE,
  source_title TEXT    NOT NULL,                 -- raw release name (display)
  norm_title   TEXT    NOT NULL,                 -- normalized key (matching)
  protocol     TEXT    NOT NULL DEFAULT '',
  quality_id   INTEGER NOT NULL DEFAULT 0,
  reason       TEXT    NOT NULL DEFAULT '',      -- client error message
  created_at   TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_blocklist_movie  ON blocklist(movie_id);
CREATE INDEX idx_blocklist_series ON blocklist(series_id);

-- Queue becomes transient: clear pre-existing terminal rows so the Queue view
-- shows only active downloads immediately after upgrade.
DELETE FROM download_queue WHERE status IN ('imported','failed');
```

**Scope semantics.** A movie block sets `movie_id`; a TV block sets `series_id`.
For TV the block is keyed by `series_id + norm_title` (not per-episode-row): a
release name like `Show.S02E05.1080p...` already names its episode, so
series+title is effectively episode-specific while cleanly covering season packs
(one failed pack row → one block that its exact name won't be retried against).

**Normalization.** Add `store.NormReleaseTitle(s string) string` (lowercase; map
runs of non-alphanumeric to a single space; trim). It is used both when writing a
block (`norm_title`) and when filtering candidates, guaranteeing identical keys.

**Store methods** (`internal/core/store/blocklist_store.go`):
- `AddBlocklist(ctx, Blocklist) (int64, error)` — computes `norm_title` via
  `NormReleaseTitle(SourceTitle)`.
- `ListBlocklist(ctx) ([]Blocklist, error)` — newest first.
- `RemoveBlocklist(ctx, id int64) error` — `ErrNotFound` on 0 rows.
- `BlocklistedTitles(ctx, movieID *int64, seriesID *int64) (map[string]bool, error)`
  — normalized titles blocked for that target (queried by whichever id is set).
- `Blocklist` struct mirrors the columns.

## 3. Failure → retry flow

Extend the existing once-a-minute importing reconcile (rename `ImportCompleted`'s
body into a `Reconcile` that handles both terminal states; keep the scheduled
command). For each `grabbed` row matched to its live item `it`:

- `it.Status == StatusCompleted` → `ImportItem` (unchanged) → **on success, delete
  the queue row** (§4).
- `it.Status == StatusFailed` → **handle failure** (new `handleFailed(ctx, row, it)`):
  1. `AddHistory(download_failed, sourceTitle=row.SourceTitle, reason=it.ErrorMessage,
     movie/series ids, qualityId)`.
  2. `AddBlocklist(...)` scoped to `row.MovieID` / `row.SeriesID`, with
     `SourceTitle`, `Protocol`, `QualityID`, `reason = it.ErrorMessage`.
  3. `queue.Remove(ctx, it.DownloadClientID, it.ID, true)` — drop the dead item
     from SAB/qBit (best-effort; log on error, continue).
  4. `store.DeleteQueueItem(ctx, row.ID)` — the row is fully captured by
     history + blocklist.
  5. emit `DownloadFailedEvent{MediaKind, MovieID, SeriesID, EpisodeIDs}` (for WS
     UI refresh) and call the injected **`Researcher`** to re-search the target.

**Re-search seam (no import cycle).** `importing` defines the consumer interface;
`automation.Service` implements it; `main.go` wires it after both are constructed
(setter, because `automation` already depends on `importing` as its `Enqueuer`):

```go
// package importing
type Researcher interface {
    ResearchMovie(ctx context.Context, movieID int64) error
    ResearchEpisode(ctx context.Context, episodeID int64) error
}
func (s *Service) SetResearcher(r Researcher) { s.researcher = r }
```

`automation.Service.ResearchMovie/ResearchEpisode` are thin wrappers over the
existing `SearchMovie`/`SearchEpisode` (discard the count). On a failed movie →
`ResearchMovie(row.MovieID)`; on failed TV → `ResearchEpisode(id)` for each id in
`row.EpisodeIDs`. `researcher` is optional (nil → skip, e.g. in tests); calls are
best-effort (log on error). Re-search runs inside the background reconcile, so its
indexer latency is acceptable.

**Blocklist filter in search.** In `searchMovie`/`searchSeason`/`searchEpisode`,
after `Decide(...)` and before `enqueueBest`, drop blocklisted candidates:

```go
blocked, _ := s.store.BlocklistedTitles(ctx, moviePtr, seriesPtr)
cands = filterBlocklisted(cands, blocked) // keep c where !blocked[store.NormReleaseTitle(c.Release.Title)]
```

This makes the retry skip the just-failed release and pick the next best, and
makes every future search honour the blocklist. `filterBlocklisted` is a pure
helper (unit-tested).

## 4. Queue becomes transient

- `ImportItem` success path: after the `imported` history event, **delete the
  queue row** instead of `SetQueueStatus(..., imported)`. (`store.DeleteQueueItem`
  already exists for the queue DELETE endpoint; reuse it.)
- Failure path: row deleted in `handleFailed` (§3).
- Migration 0007 clears pre-existing `imported`/`failed` rows.
- `GET /queue` (`ListQueue`) is unchanged; with terminal rows gone it now returns
  only active (`grabbed`/`importing`) rows. No view-side filtering needed.

## 5. API — blocklist endpoints

Add to the importing API sub-router (it already owns `/queue` + `/history`):
- `GET /api/v1/blocklist` → `ListBlocklist` (200, `[]` when empty).
- `DELETE /api/v1/blocklist/{id}` → `RemoveBlocklist` (204; 404 on missing).

Enrich the list response with movie/series **title** for display (same
title-map approach the Activity History/Queue DTOs use), so the UI needn't
cross-reference. `DownloadFailedEvent` is added to the router's `WSForward` list
so the UI live-invalidates queue/history/blocklist on a failure.

## 6. UI — Activity third tab

- `ActivityLayout`: add a third tab `{ to: "/activity/blocklist", label:
  "Blocklist" }`; add the nested route.
- New `web/src/features/activity/BlocklistSection.tsx` — table of blocklisted
  releases: title, movie/show, quality, reason, date, and a **Remove** button
  (confirms, calls `DELETE /blocklist/{id}`, toasts, invalidates). Empty state
  "No blocklisted releases."
- New api hooks in `activity/api.ts`: `useBlocklist`, `useRemoveBlocklist`; extend
  `useActivityInvalidation` to also invalidate the blocklist query on the
  `download.failed`/`queue.updated` WS events.
- `types.ts`: `BlocklistEntry` (numeric wire shape mirroring the DTO; verify field
  types against the Go source per the project's recurring wire-shape lesson).
- Queue/History sections unchanged (Queue is now active-only by construction).

## 7. Testing

- **store**: blocklist add/list/remove; `BlocklistedTitles` returns the right set
  per movie vs series; `NormReleaseTitle` normalization cases; migration creates
  the table and deletes terminal queue rows.
- **importing reconcile**: a `grabbed` row whose live item is `StatusFailed` →
  writes a `download_failed` history event, adds a blocklist row scoped to the
  target, removes the client item, deletes the queue row, and calls the
  `Researcher` (fake) with the right target; a `StatusCompleted` row still imports
  and now deletes its queue row.
- **automation**: `filterBlocklisted` drops blocklisted candidates; `searchMovie`
  with a blocklisted top candidate grabs the next one instead.
- **frontend**: `BlocklistSection` renders entries + empty state; Remove calls the
  mutation and confirms; ActivityLayout shows three tabs and the blocklist route
  resolves; queue shows only active rows (fixture with no terminal rows).

## 8. Build & verify

- Follows the project flow: this spec → implementation plan (writing-plans) →
  SDD build.
- Migration is additive (0007); no destructive change beyond clearing terminal
  queue rows (captured in history).
- Full verify: `CGO_ENABLED=0 go build/vet/test ./...`, FE `vitest` + `tsc -b`,
  `web/dist` rebuilt (drift guard clean).
- Live browser AC on a seeded instance with a simulated failed download: the
  failed release appears on the Blocklist page with its reason, leaves the Queue,
  a re-search grabs a different release, and Remove un-blocks it.
- **ASK before pushing `master`.**

## 9. Out of scope (later)

- Stalled-torrent detection / timeouts (only explicit `StatusFailed` now).
- Live download progress in Activity (C2).
- Interactive "pick a release" search (C3).
- Manual "remove & blocklist" action on an active queue item.
- Global (cross-item) blocklist entries; per-indexer scoping.
- Configurable retry limits / max-blocklist age.
