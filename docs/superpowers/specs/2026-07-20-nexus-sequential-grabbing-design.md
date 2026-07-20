# Nexus — Sequential per-series grabbing and season-pack exhaustion (SP-B)

Date: 2026-07-20
Status: approved, not yet planned
Supersedes: nothing

## 1. Context

Nexus grabs releases from two independent automation paths:

- **The search path** — `Service.SearchSeries` → `searchSeason` → `searchEpisode`
  (`internal/automation/search.go`), driven by the scheduled `MissingSearch` sweep
  and by manual "search" buttons in the UI.
- **The RSS path** — `Service.RSSSync` → `rssPlaceTV` (`internal/automation/rss.go:350`),
  driven by the scheduled `RSSSync` poll.

Both bucket a series' missing episodes, try a full-season pack for any fully-missing
monitored season, and otherwise fall back to one grab per missing episode. Both call
the shared `enqueueBest` (`search.go:125`).

### The problems being solved

**#2 — automation fans out without limit.** `searchSeries` loops every monitored
season, and `searchSeason` loops every missing episode grabbing each one it can
(`search.go:265`). Adding a series with three unwatched seasons fires dozens of
simultaneous grabs into the download client. `rssPlaceTV` does the same thing
independently. The only existing restraint is `activeQueue`, which prevents grabbing
the *same* episode twice — it does nothing about grabbing forty *different* episodes
at once.

**#3 — season-pack fallback is premature.** `enqueueBest` returns as soon as a
candidate successfully **enqueues** (`search.go:128`), not when it successfully
**downloads**. If that season pack later fails in the client, `handleFailed`
(`internal/importing/command.go:44`) blocklists it and then calls
`researchAfterFailure`, which loops `ResearchEpisode` once per episode id on the row
(`command.go:83-87`). For a 10-episode pack that is 10 per-episode searches. The
second-best season pack is never tried; one failed pack permanently drops that season
to per-episode grabbing.

## 2. Goals / non-goals

**Goals**

- Limit how many downloads one series may have in flight at once, configurable,
  default 1.
- Apply that limit to **all three** automation grab paths (search, RSS, upgrade).
- When a season pack fails, try the next acceptable season pack before falling back
  to per-episode.
- Resume automatically when a slot frees, without waiting for the next scheduled
  sweep.

**Non-goals**

- Any limit on movies. `activeQueue` already prevents a second grab for a movie that
  has one in flight, and a movie is a single file — it is already sequential.
- Restricting manual/interactive grabs (see §3.3).
- Stalled-download detection (see §6.1). Explicitly deferred.
- Any change to `enqueueBest`'s candidate ordering or to `Decide`.

## 3. Load-bearing constraints

### 3.1 `store.ListQueue` must keep returning the *whole* queue

`activeQueue` (`search.go:87`) folds `store.ListQueue` into the in-flight id sets that
guard against duplicate grabs. SP-A already pinned this with
`TestListQueueReturnsAllRowsUnpaged` (60 rows seeded against a default page size of 50).

**This spec makes it load-bearing for a second, independent reason:** the new
per-series counter is derived from the same rows. If `ListQueue` were ever paginated,
the gate would under-count in-flight downloads and silently leak concurrency — the
exact failure it exists to prevent. Neither `ListQueue` nor `QueueByStatus` may gain
a default limit.

### 3.2 There are THREE TV grab paths, not one

| Path | Entry | Grab site |
|---|---|---|
| Search / missing sweep | `SearchSeries`, `SearchSeason`, `SearchEpisode` | `searchSeason` → `searchEpisode` (`search.go:219`) |
| RSS sync | `RSSSync` | `rssPlaceTV` (`rss.go:350`) |
| Upgrade sweep | `upgradeSweep` (`upgrade.go:92`) | `upgradeEpisode` (`upgrade.go:228`) |

The first two are parallel implementations of the same pack-then-episode strategy over
different candidate pools. The third grabs *upgrades* for episodes that already have a
file, looping episodes within a series.

All three call `enqueueBest` and all three must be gated. A gate installed in one leaves
the others ungated — this is the single most likely way to build this feature and have
it appear to work, since a passing search-path test proves nothing about the other two.

`upgradeSweep` is easy to overlook because its own `batch` limit caps *targets
processed*, not downloads started per series: one series can contribute many upgrade
grabs within a single batch slot.

### 3.3 Manual grabs are counted but never refused

Interactive/manual grabs call `importing.Enqueue` directly and do not pass through
`searchSeason` or `rssPlaceTV`. Placing the gate in those two paths therefore makes
manual grabs unrestricted **by construction** — no bypass flag is needed, and none
should be added. They still occupy a slot, because the counter is derived from queue
rows regardless of what created them.

### 3.4 The failure hook exists; the success hook does not

`researchAfterFailure` fires only from `handleFailed`. A *successful* import
(`command.go:30`) triggers nothing today. The success trigger is the one genuinely
new piece of wiring in this sub-project.

## 4. Design

### 4.1 Per-series in-flight counts

`activeQueue` gains a third return value:

```go
func (s *Service) activeQueue(ctx context.Context) (
    movies, episodes map[int64]struct{},
    seriesInFlight map[int64]int,
    err error,
)
```

`seriesInFlight[seriesID]` counts rows whose `Status` is `QueueGrabbed` or
`QueueImporting` and whose `SeriesID` is non-nil — the same filter already applied to
the other two sets, walked in the same loop.

All existing call sites are updated for the new arity. This is a compile-breaking
signature change with no behaviour change on its own, and should land as its own task.

### 4.2 The budget

A series' remaining budget is:

```go
func seriesBudget(limit, inFlight int) int
```

- `limit <= 0` → unlimited; returns a sentinel meaning "no cap" (`math.MaxInt`).
  **`0` is the explicit off switch for the whole feature**, and negatives behave the
  same way so no new config validation is required.
- otherwise → `max(0, limit-inFlight)`.

Note this deliberately does **not** follow the `MissingSweep` precedent of
`if batch <= 0 { batch = DefaultConfig()... }`. Here `<= 0` means unlimited, because
the user asked for an explicit way to disable the gate.

### 4.3 Applying the budget — search path

`searchSeries` computes the budget **once** for the series and threads it through all
its seasons, so the cap is per series rather than per season. `searchSeason`
decrements it on each successful grab and returns early at zero, in both the
season-pack branch and the per-episode loop.

`searchSeasonEntry` and `searchEpisodeEntry` (the single-season and single-episode
entry points behind the UI's search buttons) compute their own budget the same way.

The budget is threaded as an explicit `*int` (or a small `budget` struct with a
`take() bool` method) rather than as a return value, so a single counter is shared
across a season-pack phase and a per-episode phase without each caller re-deriving it.
The plan picks one; whichever it picks is used identically in §4.4 and §4.4b.

### 4.4 Applying the budget — RSS path

`rssPlaceTV` receives the series' budget and applies it identically across its
season-pack phase and its per-episode phase. `RSSSync` computes it per series from the
`seriesInFlight` map it already gets back from its existing `activeQueue` call
(`rss.go:275`).

### 4.4b Applying the budget — upgrade path

`upgradeSweep` (`upgrade.go:92`) computes a budget per series from the same
`seriesInFlight` map returned by its existing `activeQueue` call (`upgrade.go:107`),
and its episode loop stops grabbing upgrades for a series once that budget is spent.

Upgrades count against the same single limit as missing-episode grabs — the limit is
"downloads in flight for this series", and an upgrade is a download like any other.
Its existing per-sweep `batch` cap is unrelated and stays as-is: it limits how many
*targets* the sweep examines, not how many downloads one series may start.

### 4.5 The `ResearchSeries` hook

`importing.Researcher` (`internal/importing/importing.go:67`) gains one method:

```go
ResearchSeries(ctx context.Context, seriesID int64) error
```

satisfied by `automation.Service` as a thin wrapper over `SearchSeries`, mirroring the
existing `ResearchMovie`/`ResearchEpisode` wrappers in
`internal/automation/blocklist_filter.go:25-33`.

`ResearchEpisode` remains on the interface and keeps working, but after this change
nothing in the TV failure path calls it.

**`handleFailed`** — `researchAfterFailure` routes TV rows to `ResearchSeries(row.SeriesID)`
instead of looping `ResearchEpisode` per episode id. Movie rows are unchanged.

**Successful import** — `ImportCompleted` collects the series id of every row it
imports successfully and, after the loop, calls `ResearchSeries` **once per distinct
series**. Firing per row would launch several concurrent searches for one show, racing
each other for the single freed slot.

Movies deliberately get no success trigger: a movie that just imported has a file, so
`searchMovie` returns at its `already have a file` guard (`search.go:33`) — it would be
a guaranteed-useless indexer round-trip on every movie import.

### 4.6 Season-pack exhaustion

No dedicated code. It is an emergent consequence of §4.5 plus machinery that already
exists:

1. A season pack fails in the client.
2. `handleFailed` blocklists it, scoped to the series, and deletes the queue row.
3. `researchAfterFailure` calls `ResearchSeries`.
4. `searchSeason` recomputes missing episodes. Nothing imported, so every monitored
   episode is still missing → it re-enters the `len(missing) == len(monitored)`
   season-pack branch (`search.go:238`).
5. `filterBlocklisted` (inside `enqueueBest`) drops the pack that just failed.
6. The next-best pack is grabbed.
7. When packs are exhausted, `Decide` yields no pack candidates and control falls
   through to the existing per-episode loop.

The blocklist is the state. No new table, no migration, no second source of truth.

### 4.7 Configuration

`automation.Config` (`internal/automation/config.go:13`) gains:

```go
MaxConcurrentPerSeries int `json:"maxConcurrentPerSeries"`
```

`DefaultConfig()` sets it to `1`. It is surfaced in the existing automation settings
section in the frontend, alongside the other interval/batch fields. No new settings
tab, no schema change — the automation config is already persisted as a single blob.

### 4.8 Error handling summary

| Situation | Behaviour |
|---|---|
| `ResearchSeries` returns an error | Logged via `slog.Warn`, swallowed. An import must never fail because a follow-up search did. Matches today's `researchAfterFailure`. |
| Series already at its limit | Grab skipped silently. Not an error, not a history event. |
| `limit <= 0` | Gate disabled; behaviour identical to today. |
| Several rows for one series import in one tick | `ResearchSeries` fires once for that series. |
| Manual grab while at the limit | Always allowed. Occupies a slot, restraining later automation. |

## 5. Testing

### 5.1 The fixture trap that must be avoided

**A gate test must use a series with several missing episodes.** With a single missing
episode, "grabbed 1 because the gate stopped it" is indistinguishable from "grabbed
everything there was", and the test passes against a completely absent gate.

This is the same defect class as SP-A's orphaning bug, where `fakeQueue.Remove`
discarded its client id and both fixtures used the same value, making the production
code's deliberate choice of source untestable. **When code chooses between two
sources, or stops short of doing everything, the fixtures must make the two outcomes
visibly differ.**

### 5.2 Required coverage

- Series with 5 missing episodes, limit 1 → exactly 1 grab; limit 3 → exactly 3.
- Limit 0 → all 5 grabbed (gate disabled). Negative → same.
- The budget spans seasons: 2 fully-missing seasons, limit 1 → 1 grab total, not 1
  per season.
- A row created directly via `importing.Enqueue` (standing in for a manual grab)
  occupies the slot and suppresses the next automation grab.
- **RSS path gated** — its own test against `rssPlaceTV`, not inherited from the
  search-path test.
- **Upgrade path gated** — its own test against `upgradeSweep`: a series with several
  cutoff-unmet episodes and limit 1 yields exactly 1 upgrade grab. Also assert an
  in-flight *missing-episode* grab suppresses an upgrade grab for the same series,
  proving both kinds share one budget.
- **Pack exhaustion with two acceptable packs:** first fails → second is grabbed, and
  no per-episode grab occurs. Then the second fails → per-episode begins.
- `handleFailed` on a TV row calls `ResearchSeries` once, not `ResearchEpisode` per
  episode. `fakeResearcher` (`internal/importing/command_test.go:102`) gains a
  `series` slice.
- Successful import of 3 rows for one series in one tick → `ResearchSeries` called
  exactly once.
- Movie import success → no research call.
- `TestListQueueReturnsAllRowsUnpaged` continues to pass (§3.1).

## 6. Risks

### 6.1 Stalled downloads starve a series — ACCEPTED

A download that stalls indefinitely (no seeders, client never reports failure) holds
its series' slot forever, and nothing further for that show is grabbed. Today the
other episodes would still download. This is a real behaviour regression accepted
deliberately: SP-A shipped per-row queue removal, so clearing the stuck row is a
two-click fix that immediately frees the slot. Revisit only if it bites in production;
real stalled-download detection is its own sub-project.

### 6.2 Manual import does not fire the success trigger

The trigger lives in `ImportCompleted`'s loop. An import performed directly through
the API frees the slot but does not immediately trigger the next grab; the series
resumes on the next scheduled sweep. Accepted as a minor latency issue, not a
correctness one.

### 6.3 A slow series search on the import tick

`ImportCompleted` runs every 5s. `ResearchSeries` performs live indexer searches, so a
slow indexer lengthens that tick. The tick is already `command.Single`, so ticks cannot
overlap; the worst case is a delayed subsequent import, not pile-up.

## 7. Open questions

None. All decisions in §2-§4 were made explicitly during brainstorming:
per-series (not per-season) granularity; configurable with default 1; `<= 0` disables;
manual never refused but always counted; all three grab paths gated; success trigger
TV-only and deduped per tick; pack state carried by the blocklist; stalled detection
deferred.

One item was corrected during spec-writing rather than brainstorming: the design was
approved as gating "both" grab paths, before a call-site sweep of `activeQueue` found a
third (`upgradeSweep`). Gating it follows directly from the approved principle — the
limit is downloads in flight per series, and an upgrade is a download — but it was not
explicitly approved as a third path and is called out here for that reason.
