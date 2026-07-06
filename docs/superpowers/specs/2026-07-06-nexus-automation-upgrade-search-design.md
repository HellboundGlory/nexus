# Nexus Automation: Upgrade / Cutoff-Unmet Search (Sub-project 5c) Design

**Status:** Approved (brainstorm complete). Ready for implementation plan.
**Depends on:** 5a (decision maker `automation.Decide` + `Searcher`/`Enqueuer`
interfaces + `enqueueBest`/`activeQueue`/`profileFor`/`tvRequest` + queue-dedup
guard), 4a (media library + monitored flags), 4b (parsing & quality, in
particular `quality.IsUpgrade`), 4c (import pipeline / `importing.Enqueue` +
grab-tracking `download_queue`, `media_files.quality_id`, `history`), sub-2
(indexer search).

## 1. Goal

Give Nexus the third and final slice of its **automation brain**: **upgrade /
cutoff-unmet search**. Where 5a's missing sweep is *targeted at items with no
file* and 5b's RSS sync is *release-driven for missing items*, 5c is *targeted at
items that already HAVE a file whose quality ranks below their profile's cutoff*.
For each such item it searches indexers for a better release and grabs the best
candidate that would be a genuine upgrade over the existing file. This is how
Nexus progressively replaces an early low-quality grab with the quality the user
actually asked for.

5c is deliberately an **inversion of the 5a missing sweep**: it reuses
`automation.Decide`, `enqueueBest`, `activeQueue`, `profileFor`, `tvRequest`, and
the `Searcher`/`Enqueuer` interfaces verbatim. The single new *selection* rule is
the upgrade filter, and the single new *safety* mechanism is a cooldown guard
against re-grabbing the same release. The import pipeline's existing upgrade gate
(`internal/importing/importer.go` — `quality.IsUpgrade` at import time) remains
the final backstop.

Sub-5 decomposition (now complete with this slice):

- **5a — Decision maker + wanted/missing search.** DONE, merged + pushed.
- **5b — RSS sync.** DONE, merged + pushed.
- **5c — Upgrade / cutoff-unmet search.** (This document.)

Calendar remains deferred to sub-6 (UI), per the roadmap decision.

## 2. Settled decisions (from brainstorming)

1. **Scheduled-sweep only.** 5c adds a scheduled `UpgradeSweep` + config + event.
   The existing manual `Search{Movie,Series,Season,Episode}` commands stay
   **missing-only** (they still skip any item that already has a file).
   Interactive per-item upgrade search is deferred to sub-6 (UI), consistent with
   how the calendar was deferred. This keeps the slice minimal and avoids changing
   established manual-search semantics.

2. **Per-episode upgrades only; no season-pack upgrades.** Each episode file has
   its own independent quality, so a whole-season pack upgrade would require every
   episode in the pack to individually be an upgrade — meaningfully more logic for
   a mixed-quality season. TV upgrade search is therefore per-episode. Season-pack
   upgrades are explicit backlog.

3. **History-based cooldown guard (no new migration).** Every grab already writes
   a `"grabbed"` row to the `history` table (`event_type='grabbed'`, `source_title`,
   `series_id`/`movie_id`, `quality_id`, `created_at`) via
   `importing.Enqueue`. The cooldown guard reuses that append-only log: a candidate
   whose `(item, normalized-title)` was grabbed within the cooldown window is
   skipped. No new table, no new write path. (Rejected alternative: a dedicated
   `grab_blocklist` migration — more precise per-episode but duplicates state the
   history log already carries.) The TV grabbed-history row records `series_id`
   but not `episode_id`, so the TV guard is series+title granularity — which is the
   behavior we want (a release title already grabbed for a series is not re-grabbed
   for it within the window).

4. **Local below-cutoff pre-filter before any indexer call.** Items already at or
   above their profile cutoff (or under a profile with upgrades disabled) never
   trigger a search. This caps indexer load and caps how often the re-grab guard
   can even be exercised.

## 3. The re-grab loop (the risk this slice must not introduce)

Concrete failure without a guard: a release whose *title* claims Bluray-1080p but
whose *file* resolves to 720p, with cutoff = 1080p. The sweep grabs it (title →
`IsUpgrade` true); the importer imports it as 720p (still an upgrade over the old
SDTV file, so accepted); the stored `media_files.quality_id` becomes 720p — still
below cutoff. Next interval the same release is still on the indexer, its title
still resolves to 1080p, `IsUpgrade(1080p, 720p)` is still true → it is grabbed
again → 720p again → forever, one wasted grab + download per cycle. `activeQueue`
does **not** close this: it only guards `grabbed`/`importing` rows, and a
completed-but-still-below-cutoff item is fair game every cycle.

The cooldown guard (§2.3) closes it: once that release title has been grabbed for
that item, it is not re-grabbed for `UpgradeGrabCooldownHours` (default 7 days),
so the loop fires at most once per window rather than once per sweep. A genuinely
*different*, better release is unaffected and can still be grabbed.

Note: 5a's missing sweep has the same latent hole but it is benign there — once a
file lands, the item leaves the missing set. Upgrades are where it recurs, hence
the guard lives in 5c.

## 4. Boundary: RSS does not upgrade

5b's RSS sync reverse-matches monitored **missing** items only. A better-quality
release that appears on an indexer feed for an item that already has a
below-cutoff file will **not** be grabbed by RSS — only this 5c sweep catches it.
This is an intentional scope line (the roadmap phrases 5c as "cutoff-unmet
search"), stated so it is not mistaken later for a bug.

## 5. Components

### 5.1 `internal/quality` — `CutoffUnmet` (leaf addition)

```go
// CutoffUnmet reports whether an existing file of quality existingID is eligible
// for an upgrade under the profile: upgrades enabled AND the existing quality
// ranks strictly below the profile cutoff. It is IsUpgrade's cutoff arm made
// available without a candidate, for use as a pre-search filter. Qualities absent
// from the profile rank below all present ones (profileRank returns -1).
func CutoffUnmet(existingID int, profile store.QualityProfile) bool
```

Semantics: `profile.UpgradeAllowed && existingRank < cutoffRank`, where ranks come
from the same `profileRank` used by `IsUpgrade`, guaranteeing the pre-filter and
the per-candidate `IsUpgrade` agree.

### 5.2 `internal/core/store` — `GrabbedSince` (additive read, no migration)

```go
// GrabbedSince returns "grabbed" history events created at or after since,
// newest first. Used by the automation upgrade sweep to build its cooldown guard.
func (s *Store) GrabbedSince(ctx context.Context, since time.Time) ([]HistoryEvent, error)
```

`SELECT id, event_type, media_kind, series_id, episode_id, movie_id, source_title,
quality_id, message, created_at FROM history WHERE event_type='grabbed' AND
created_at >= ? ORDER BY id DESC`.

### 5.3 `internal/automation` — `upgrade.go`

- **Cooldown set.** Built once per sweep from `GrabbedSince(now - cooldown)`:
  a set keyed by `(itemKey, normalize(sourceTitle))`, where `itemKey` is
  `"movie:<id>"` or `"series:<id>"`. `normalize` reuses the title normalization
  already used by the matcher/decide paths (lowercase, punctuation/space folding).

- **Upgrade candidate filter** (the one new selection rule), applied to the output
  of the reused `Decide`:

  ```
  keep candidate c for item with existing file f iff
    quality.IsUpgrade(quality.Resolve(c.Parsed).ID, f.QualityID, profile)   // real upgrade over the file
    AND !cooldownSet.has(itemKey, normalize(c.Release.Title))               // not recently grabbed
  ```

  `Resolve(...).ID` and `f.QualityID` are both quality-definition IDs (verified
  against `importer.go`, which stores `mf.QualityID = quality.Resolve(parsed).ID`
  and gates with `IsUpgrade(q.ID, existing.QualityID, profile)`), so
  `IsUpgrade`'s `profileRank` lookups line up. The kept candidates are handed to
  the reused `enqueueBest`.

- **`UpgradeSweep(ctx, batch) (int, error)`** — mirrors `MissingSweep`, inverted on
  file presence:
  1. Build the cooldown set.
  2. **Movies:** for each monitored movie that **has** a file
     (`MediaFileForMovie != nil`), load its profile; skip if
     `!CutoffUnmet(file.QualityID, profile)`; skip if the movie is in
     `activeQueue`; otherwise search (`movieQuery`), `Decide`, apply the upgrade
     filter, `enqueueBest`.
  3. **Series → episodes:** for each monitored series, for each monitored episode
     that **has** a file below cutoff and is not in `activeQueue`, run the same
     per-episode flow (`tvQuery` with the episode, `Decide`, upgrade filter
     restricted to covering candidates, `enqueueBest` with `tvRequest`).
  4. Batch-bounded (`processed >= batch` stops the sweep) and per-target-error
     tolerant (logged and skipped), identical to `MissingSweep`.
  5. Returns the total grabbed.

- **Event.** `UpgradeCompleted{Grabbed int}` with `Name() =
  "automation.upgrade.completed"`, emitted at the end of the sweep.

### 5.4 `internal/automation` — command + config

- `NewUpgradeSearchCommand(svc *Service) command.Command` — mirrors
  `NewRSSSyncCommand`; runs `svc.UpgradeSweep(ctx, cfg.UpgradeSearchBatchSize)`.
- `Config` gains four fields (all additive; non-positive → default in `Config()`,
  same pattern as existing fields):

  | field | default | note |
  |---|---|---|
  | `UpgradeSearchEnabled` | `true` | scheduler gated on it (like `RSSSyncEnabled`) |
  | `UpgradeSearchIntervalHours` | `12` | sweep cadence |
  | `UpgradeSearchBatchSize` | `100` | targets per sweep (mirrors missing) |
  | `UpgradeGrabCooldownHours` | `168` | 7-day re-grab guard window |

  `UpgradeSearchEnabled` is a plain bool; like `RSSSyncEnabled` it is not subject
  to the non-positive-fallback rule.

### 5.5 `cmd/nexus/main.go` — wiring

- After the RSS block: `if autoCfg.UpgradeSearchEnabled { sch.Every(
  time.Duration(autoCfg.UpgradeSearchIntervalHours)*time.Hour, func() command.Command {
  return automation.NewUpgradeSearchCommand(autoSvc) }) }`.
- Add `"automation.upgrade.completed"` to the `WSForward` list.
- No new REST route: the sweep is scheduled-only, and the four new config fields
  ride along on the existing `GET/PUT /api/v1/automation/config` automatically.

## 6. Data flow

```
scheduler (every UpgradeSearchIntervalHours, if enabled)
  → NewUpgradeSearchCommand → UpgradeSweep(batch)
      cooldown set ← store.GrabbedSince(now - cooldownHours)
      for each monitored movie WITH a file (bounded by batch):
        profile ← profileFor(movie.QualityProfileID)         // skip if none
        if !CutoffUnmet(file.QualityID, profile): continue    // pre-filter, no indexer call
        if movie in activeQueue: continue
        releases ← Searcher.Search(movieQuery(movie))
        cands    ← Decide(releases, KindMovie, profile)
        cands    ← filter: IsUpgrade(Resolve(c).ID, file.QualityID, profile) && !cooldown
        enqueueBest(cands) → importing.Enqueue → grabbed queue row + "grabbed" history
      for each monitored series → each monitored episode WITH a file below cutoff:
        (same flow via tvQuery/tvRequest, covering-episode candidates only)
      emit automation.upgrade.completed{Grabbed} → WS
  ... later, when the download completes ...
  importCmd → importer.importFile → quality.IsUpgrade gate (final backstop):
      if new file is an upgrade over existing → replace; else history "not an upgrade", file untouched
```

## 7. Module boundaries

Unchanged. `internal/automation` continues to import only `internal/core/*`,
`internal/parsing`, `internal/quality`, and `internal/importing`. The new helpers
live in `internal/quality` (`CutoffUnmet`, a leaf-level pure function over
`store.QualityProfile`) and `internal/core/store` (`GrabbedSince`). No new
cross-package edges; verified post-build via direct-imports as with 5a/5b.

## 8. Testing

- `quality.CutoffUnmet`: table test — below-cutoff → true; at/above-cutoff → false;
  `UpgradeAllowed=false` → false; quality absent from profile → true (ranks below
  all present).
- `store.GrabbedSince`: inserts across the boundary time return only rows at/after
  `since`, newest-first, `event_type='grabbed'` only.
- `automation.UpgradeSweep` (in-memory store + fake `Searcher`/`Enqueuer`):
  - (a) monitored movie with a below-cutoff file grabs the best upgrade release.
  - (b) at-cutoff item is skipped **without any `Searcher.Search` call** (assert
    the fake searcher was not invoked).
  - (c) a candidate whose resolved quality is not an upgrade over the file is
    filtered out (not enqueued).
  - (d) re-grab loop: a candidate whose title matches a `"grabbed"` history row for
    the same item within the cooldown window is skipped; the same title *outside*
    the window is allowed.
  - (e) an item already in `activeQueue` (grabbed/importing) is skipped.
  - (f) a profile with `UpgradeAllowed=false` never upgrades.
  - (g) TV per-episode: an episode with a below-cutoff file grabs a covering
    upgrade; a fully-below-cutoff season does **not** attempt a pack (per-episode
    only).
- Config round-trip test extended for the four new fields incl. non-positive
  fallback (and `UpgradeSearchEnabled=false` disabling the scheduled sweep).

## 9. Acceptance criteria

1. With `UpgradeSearchEnabled=true`, the scheduler runs `UpgradeSweep` every
   `UpgradeSearchIntervalHours`; with it `false`, no upgrade job is registered.
2. Only monitored items with a file whose quality is below their profile cutoff,
   under a profile with upgrades enabled, are considered — and no indexer search is
   issued for items that fail that pre-filter.
3. A release is grabbed only if it is a real upgrade over the existing file
   (`IsUpgrade`) and has not been grabbed for that item within the cooldown window.
4. The same mislabeled release is not re-grabbed on every sweep (loop closed to at
   most once per cooldown window).
5. Items currently in flight (`grabbed`/`importing`) are never re-grabbed.
6. `automation.upgrade.completed` is emitted per sweep and forwarded over the
   WebSocket.
7. No new migration; `go build ./...`, `go vet ./...`, `go test ./...` all green;
   module boundaries unchanged.

## 10. Backlog (explicit, deferred)

- Season-**pack** upgrades (all-episodes-upgrade logic for a mixed-quality season).
- Interactive / manual per-item upgrade search (sub-6 UI).
- RSS-driven upgrades (would require 5b's matcher to also consider owned
  below-cutoff items).
- 5a's latent missing-sweep re-grab hole (benign; noted for completeness).
- 5a's `searchSeason` multi-episode double-grab (pre-existing, tracked from 5b).
