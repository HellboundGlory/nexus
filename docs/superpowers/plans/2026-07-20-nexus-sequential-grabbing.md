# Sequential per-series grabbing and season-pack exhaustion (SP-B) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Limit how many downloads one TV series may have in flight at once (configurable, default 1) across all three automation grab paths, and make a failed season pack try the next pack before dropping to per-episode grabbing.

**Architecture:** `activeQueue` gains a per-series in-flight count. Each automation grab path derives a `budget` from that count plus a new config field and stops grabbing for a series once spent. A single new `Researcher.ResearchSeries` hook — fired on download failure *and* (newly) on successful import — resumes the series when a slot frees; season-pack exhaustion then falls out for free, because the failed pack is already blocklisted and the re-run season search skips it.

**Tech Stack:** Go 1.x (stdlib + chi + database/sql/SQLite), React 19 + TypeScript + TanStack Query + Vite, Vitest + Testing Library.

Spec: `docs/superpowers/specs/2026-07-20-nexus-sequential-grabbing-design.md`

## Global Constraints

- **`store.ListQueue` and `store.QueueByStatus` MUST stay unpaged.** `activeQueue` folds them into both the duplicate-grab guard and (now) the per-series counter. A default limit would silently under-count in-flight downloads and leak concurrency. `TestListQueueReturnsAllRowsUnpaged` pins this — it must keep passing.
- **There are THREE automation TV grab paths, and all three must be gated:** `searchSeason`/`searchEpisode` (`internal/automation/search.go`), `rssPlaceTV` (`internal/automation/rss.go:350`), `upgradeEpisode` via `upgradeSweep` (`internal/automation/upgrade.go:92`). A passing test on one proves nothing about the other two; each needs its own test.
- **`MaxConcurrentPerSeries <= 0` means UNLIMITED (gate disabled).** It is the deliberate exception to `Service.Config()`'s clamp-every-non-positive-field rule. Do NOT add a normalizing `if c.MaxConcurrentPerSeries <= 0 { ... }` clause — that would silently convert the off switch back into a limit of 1.
- **Manual/interactive grabs are never refused.** They call `importing.Enqueue` directly and never pass through the gated paths, so this is true by construction. Do NOT add a bypass flag to `Enqueue`.
- **Movies are out of scope.** No movie gating, and no success-trigger research for movies.
- **A gate test must use a series with SEVERAL missing episodes.** With one missing episode, "grabbed 1 because the gate stopped it" is indistinguishable from "grabbed everything there was", and the test passes against a completely absent gate.
- Frontend: colours via CSS custom properties only, never hex literals. `web/dist` is rebuilt and committed ONLY in the final task.
- The real frontend typecheck is `cd web && npx tsc -p tsconfig.app.json --noEmit`. A bare `npx tsc --noEmit` is vacuous here (`web/tsconfig.json` is solution-style with `"files": []`) and always exits 0.

## Verified test harness (do not invent alternatives)

**`internal/automation`** — tests are INSIDE `package automation`, so all types are unqualified.
- `newStore(t)` → `*store.Store` (`config_test.go:11`)
- `hdProfile()` → `store.QualityProfile` (`config_test.go:12`)
- `NewService(st, search, enq, bus)` — pass `nil` for the bus: `NewService(st, fs, fe, nil)`
- `&fakeSearcher{releases: []provider.Release{...}}` — also records `lastQuery`, and has an optional `err` field (`search_test.go:14`)
- `&fakeEnqueuer{}` — records every request in `.reqs`; optional `errOn func(importing.EnqueueRequest) error` (`search_test.go:31`)
- `seedSeries(t, st, monitored bool, epCount int) (seriesID int64, epIDs []int64)` (`search_test.go:154`) — creates a series titled **"The Show"** (TMDBID 7) with an HD profile, **season 1 only**, and `epCount` monitored episodes. **It does NOT create a season 2** — any multi-season test must call `st.UpsertSeason`/`st.UpsertEpisode` itself.
- `seedMovie(t, st, monitored, withProfile bool) int64` (`search_test.go:48`)
- Queue rows are created with `st.EnqueueGrab(ctx, store.QueueItem{MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{ep}, Status: store.QueueGrabbed})` (`search_test.go:323`)
- Release title conventions: episode `"The.Show.S01E01.1080p.BluRay.x264-GRP"`, season pack `"The.Show.S01.1080p.BluRay.x264-GRP"` (`search_test.go:186-189`)

**`internal/importing`** — tests are INSIDE `package importing`.
- `newTestStore(t)` (`enqueue_test.go:63`), `newSvc(t)` (`:58`), `newSvcWithQueue(t, q QueueReader)` (`:86`)
- `seedSeriesWithProfile(t, st) (seriesID, epID int64)` (`enqueue_test.go:103`)
- `type fakeResearcher struct{ movies, episodes []int64 }` with `ResearchMovie`/`ResearchEpisode` (`command_test.go:100-110`) — **Task 6 adds a `series []int64` field and a `ResearchSeries` method.**
- `svc.SetResearcher(res)` (`command_test.go:134`)
- `fakeQueue` (`enqueue_test.go:26`) is the `QueueReader` fake.

---

### Task 1: `activeQueue` returns per-series in-flight counts

Compile-breaking arity change with no behaviour change. Landing it alone keeps the six call-site edits out of the diffs that contain real logic.

**Files:**
- Modify: `internal/automation/search.go` (`activeQueue` at :87, call sites at :35, :167, :208, :299)
- Modify: `internal/automation/rss.go:275`
- Modify: `internal/automation/upgrade.go:107`
- Test: `internal/automation/search_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func (s *Service) activeQueue(ctx context.Context) (movies, episodes map[int64]struct{}, seriesInFlight map[int64]int, err error)` — `seriesInFlight[seriesID]` is the number of `QueueGrabbed`/`QueueImporting` rows with a non-nil `SeriesID`.

- [ ] **Step 1: Write the failing test**

Add to `internal/automation/search_test.go`:

```go
func TestActiveQueueCountsRowsPerSeries(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 3)

	// Two in-flight rows for this series, one of each active status.
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[0]}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[1]}, Status: store.QueueImporting,
	}); err != nil {
		t.Fatal(err)
	}
	// A movie row must not be counted against any series.
	mid := seedMovie(t, st, true, true)
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "movie", MovieID: &mid, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(st, &fakeSearcher{}, &fakeEnqueuer{}, nil)
	_, _, inFlight, err := svc.activeQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := inFlight[sid]; got != 2 {
		t.Fatalf("want 2 in flight for series %d, got %d (map=%v)", sid, got, inFlight)
	}
	if len(inFlight) != 1 {
		t.Fatalf("only the series should appear, got %v", inFlight)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/automation/ -run TestActiveQueueCountsRowsPerSeries`
Expected: FAIL to COMPILE — `assignment mismatch: 4 variables but s.activeQueue returns 3 values`.

- [ ] **Step 3: Change the signature and populate the map**

In `internal/automation/search.go`, replace the `activeQueue` function:

```go
// activeQueue returns the sets of movie ids and episode ids that currently have
// an in-flight download-queue row (grabbed or importing), plus a per-series
// count of those rows. Such items were already grabbed but not yet imported (no
// media file exists yet), so a search must not grab them again — this makes the
// sweep idempotent and prevents duplicate grabs from concurrent manual+scheduled
// searches or stalled downloads. seriesInFlight additionally powers the
// per-series concurrency gate, which is why store.ListQueue must stay unpaged:
// a paginated read would under-count and silently leak concurrency.
func (s *Service) activeQueue(ctx context.Context) (movies, episodes map[int64]struct{}, seriesInFlight map[int64]int, err error) {
	rows, err := s.store.ListQueue(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	movies = map[int64]struct{}{}
	episodes = map[int64]struct{}{}
	seriesInFlight = map[int64]int{}
	for _, r := range rows {
		if r.Status != store.QueueGrabbed && r.Status != store.QueueImporting {
			continue
		}
		if r.MovieID != nil {
			movies[*r.MovieID] = struct{}{}
		}
		if r.SeriesID != nil {
			seriesInFlight[*r.SeriesID]++
		}
		for _, eid := range r.EpisodeIDs {
			episodes[eid] = struct{}{}
		}
	}
	return movies, episodes, seriesInFlight, nil
}
```

- [ ] **Step 4: Update all five other call sites**

Each gains one extra return. Use `_` everywhere for now — later tasks replace the blanks with real names.

- `search.go:35` → `activeMovies, _, _, err := s.activeQueue(ctx)`
- `search.go:167` → `_, activeEps, _, err := s.activeQueue(ctx)`
- `search.go:208` → `_, activeEps, _, err := s.activeQueue(ctx)`
- `search.go:299` → `_, activeEps, _, err := s.activeQueue(ctx)`
- `rss.go:275` → `activeMovies, activeEps, _, err := s.activeQueue(ctx)`
- `upgrade.go:107` → `activeMovies, activeEps, _, err := s.activeQueue(ctx)`

- [ ] **Step 5: Run the tests**

Run: `go build ./... && go test ./internal/automation/`
Expected: PASS, including the new test and every pre-existing one (no behaviour changed).

- [ ] **Step 6: Commit**

```bash
git add internal/automation/
git commit -m "feat(sp-b): activeQueue reports per-series in-flight counts"
```

---

### Task 2: `MaxConcurrentPerSeries` config field

**Files:**
- Modify: `internal/automation/config.go`
- Test: `internal/automation/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Config.MaxConcurrentPerSeries int` (`json:"maxConcurrentPerSeries"`), default `1`. `<= 0` means unlimited and MUST survive `Service.Config()` unmodified.

- [ ] **Step 1: Write the failing tests**

Add to `internal/automation/config_test.go`:

```go
func TestDefaultConfigLimitsOneDownloadPerSeries(t *testing.T) {
	if got := DefaultConfig().MaxConcurrentPerSeries; got != 1 {
		t.Fatalf("want default 1, got %d", got)
	}
}

// 0 is the documented off switch for the per-series gate. Every OTHER numeric
// field in Config() is clamped to its default when non-positive; this one must
// not be, or the off switch silently becomes a limit of 1.
func TestConfigPreservesZeroMaxConcurrentPerSeries(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	svc := NewService(st, &fakeSearcher{}, &fakeEnqueuer{}, nil)

	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 0
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxConcurrentPerSeries != 0 {
		t.Fatalf("0 must survive as 'unlimited', got %d", got.MaxConcurrentPerSeries)
	}
	// Sanity: a sibling field IS still clamped, proving the exemption is specific.
	if got.MissingSearchBatchSize != DefaultConfig().MissingSearchBatchSize {
		t.Fatalf("sibling clamp should be unchanged, got %d", got.MissingSearchBatchSize)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/automation/ -run 'TestDefaultConfigLimitsOneDownloadPerSeries|TestConfigPreservesZeroMaxConcurrentPerSeries'`
Expected: FAIL to COMPILE — `c.MaxConcurrentPerSeries undefined`.

- [ ] **Step 3: Add the field**

In `internal/automation/config.go`, add to the `Config` struct after `UpgradeGrabCooldownHours`:

```go
	// MaxConcurrentPerSeries caps how many downloads one TV series may have in
	// flight at once. Unlike every other numeric field here, it is deliberately
	// NOT clamped in Config(): <= 0 means unlimited and is the documented way to
	// disable the gate entirely.
	MaxConcurrentPerSeries int `json:"maxConcurrentPerSeries"`
```

And in `DefaultConfig()`, after `UpgradeGrabCooldownHours: 168,`:

```go
		MaxConcurrentPerSeries:     1,
```

**Do NOT add a clamp for it in `Config()`.** Leave that function's existing clauses untouched.

- [ ] **Step 4: Run them to verify they pass**

Run: `go test ./internal/automation/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/automation/
git commit -m "feat(sp-b): add MaxConcurrentPerSeries config, default 1"
```

---

### Task 3: The budget helper and the search-path gate

**Files:**
- Create: `internal/automation/budget.go`
- Create: `internal/automation/budget_test.go`
- Modify: `internal/automation/search.go` (`searchSeries`, `searchSeasonEntry`, `searchEpisodeEntry`, `searchSeason`, `searchEpisode`)
- Test: `internal/automation/search_test.go`

**Interfaces:**
- Consumes: Task 1's `seriesInFlight`; Task 2's `Config.MaxConcurrentPerSeries`.
- Produces:
  - `func newBudget(limit, inFlight int) *budget`
  - `func (b *budget) allows() bool`
  - `func (b *budget) take()`
  - `searchSeason(ctx, se, seasonNumber, eps, profile, activeEps, bud *budget) (int, error)`
  - `searchEpisode(ctx, se, e, profile, activeEps, bud *budget) (int, error)`

  Tasks 4 and 5 reuse `newBudget`/`allows`/`take` verbatim.

- [ ] **Step 1: Write the failing budget test**

Create `internal/automation/budget_test.go`:

```go
package automation

import "testing"

func TestNewBudget(t *testing.T) {
	cases := []struct {
		name          string
		limit         int
		inFlight      int
		wantTakes     int  // how many takes before allows() goes false
		wantUnlimited bool
	}{
		{name: "limit 1 nothing in flight", limit: 1, inFlight: 0, wantTakes: 1},
		{name: "limit 3 one in flight", limit: 3, inFlight: 1, wantTakes: 2},
		{name: "already at limit", limit: 1, inFlight: 1, wantTakes: 0},
		{name: "over limit clamps to zero", limit: 1, inFlight: 5, wantTakes: 0},
		{name: "zero disables the gate", limit: 0, inFlight: 99, wantUnlimited: true},
		{name: "negative disables the gate", limit: -1, inFlight: 99, wantUnlimited: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBudget(tc.limit, tc.inFlight)
			if tc.wantUnlimited {
				for i := 0; i < 1000; i++ {
					if !b.allows() {
						t.Fatalf("unlimited budget refused after %d takes", i)
					}
					b.take()
				}
				return
			}
			for i := 0; i < tc.wantTakes; i++ {
				if !b.allows() {
					t.Fatalf("want %d takes, refused at %d", tc.wantTakes, i)
				}
				b.take()
			}
			if b.allows() {
				t.Fatalf("want exactly %d takes, but it still allows more", tc.wantTakes)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/automation/ -run TestNewBudget`
Expected: FAIL to COMPILE — `undefined: newBudget`.

- [ ] **Step 3: Implement the budget**

Create `internal/automation/budget.go`:

```go
package automation

import "math"

// budget tracks how many more downloads automation may start for one series.
// It is shared across a whole series search (all seasons, packs and episodes)
// so the cap is per series, not per season.
type budget struct{ remaining int }

// newBudget derives the remaining allowance from the configured limit and how
// many rows the series already has in flight. A limit <= 0 disables the gate —
// that is the documented off switch, not a bad value to be clamped.
func newBudget(limit, inFlight int) *budget {
	if limit <= 0 {
		return &budget{remaining: math.MaxInt}
	}
	rem := limit - inFlight
	if rem < 0 {
		rem = 0
	}
	return &budget{remaining: rem}
}

func (b *budget) allows() bool { return b.remaining > 0 }

func (b *budget) take() {
	if b.remaining > 0 && b.remaining < math.MaxInt {
		b.remaining--
	}
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/automation/ -run TestNewBudget`
Expected: PASS (6 subtests).

- [ ] **Step 5: Write the failing gate tests**

Add to `internal/automation/search_test.go`. Note the fixture deliberately has FIVE missing episodes so "gate stopped it at 1" is distinguishable from "grabbed everything".

```go
// oneEpisodePerEp returns one release per episode of season 1, so a series
// search would grab every episode if nothing stopped it.
func episodeReleases(n int) []provider.Release {
	var rs []provider.Release
	for i := 1; i <= n; i++ {
		rs = append(rs, provider.Release{
			Title:       fmt.Sprintf("The.Show.S01E%02d.1080p.BluRay.x264-GRP", i),
			DownloadURL: fmt.Sprintf("e%d", i),
			Protocol:    provider.ProtocolUsenet,
		})
	}
	return rs
}

func TestSearchSeriesStopsAtPerSeriesLimit(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 5)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	// Default config → limit 1.

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("limit 1 must grab exactly 1 of 5 missing episodes: n=%d reqs=%d", n, len(fe.reqs))
	}
}

func TestSearchSeriesRespectsHigherLimit(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 5)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 3
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || len(fe.reqs) != 3 {
		t.Fatalf("limit 3 must grab exactly 3: n=%d reqs=%d", n, len(fe.reqs))
	}
}

func TestSearchSeriesUngatedWhenLimitZero(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 5)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 0
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || len(fe.reqs) != 5 {
		t.Fatalf("limit 0 disables the gate, want all 5: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// An in-flight row created directly (as a manual grab would) occupies the slot.
func TestSearchSeriesCountsExistingInFlightRow(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 5)
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[0]}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("series already at limit must grab nothing: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// The budget spans seasons: two fully-missing seasons must still yield 1 grab.
func TestSearchSeriesBudgetSpansSeasons(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 2) // creates season 1 only
	if err := st.UpsertSeason(ctx, store.Season{SeriesID: sid, SeasonNumber: 2, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err := st.UpsertEpisode(ctx, store.Episode{
			SeriesID: sid, SeasonNumber: 2, EpisodeNumber: i, Monitored: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "p1", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S02.1080p.BluRay.x264-GRP", DownloadURL: "p2", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("budget is per series, not per season: n=%d reqs=%d", n, len(fe.reqs))
	}
}
```

- [ ] **Step 6: Run them to verify they fail**

Run: `go test ./internal/automation/ -run TestSearchSeries`
Expected: FAIL — `TestSearchSeriesStopsAtPerSeriesLimit` reports 5 grabs where 1 was wanted (no gate exists yet). `TestSearchSeriesUngatedWhenLimitZero` already passes; that is expected and fine.

- [ ] **Step 7: Thread the budget through the search path**

In `internal/automation/search.go`:

**`searchSeries`** — read the config, build one budget, pass it to every season:

```go
func (s *Service) searchSeries(ctx context.Context, seriesID int64) (int, error) {
	se, err := s.store.GetSeries(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	if !se.Monitored {
		return 0, nil
	}
	profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
	if err != nil || !ok {
		return 0, err
	}
	seasons, err := s.store.ListSeasons(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	eps, err := s.store.ListEpisodes(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	_, activeEps, inFlight, err := s.activeQueue(ctx)
	if err != nil {
		return 0, err
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	bud := newBudget(cfg.MaxConcurrentPerSeries, inFlight[seriesID])
	total := 0
	for _, sn := range seasons {
		if !bud.allows() {
			break
		}
		if !sn.Monitored {
			continue
		}
		n, err := s.searchSeason(ctx, se, sn.SeasonNumber, eps, profile, activeEps, bud)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
```

**`searchSeasonEntry`** — same pattern; replace its `activeQueue` line and its final call:

```go
	_, activeEps, inFlight, err := s.activeQueue(ctx)
	if err != nil {
		return 0, err
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	bud := newBudget(cfg.MaxConcurrentPerSeries, inFlight[seriesID])
	return s.searchSeason(ctx, se, seasonNumber, eps, profile, activeEps, bud)
```

**`searchEpisodeEntry`** — same, keyed on `e.SeriesID`:

```go
	_, activeEps, inFlight, err := s.activeQueue(ctx)
	if err != nil {
		return 0, err
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	bud := newBudget(cfg.MaxConcurrentPerSeries, inFlight[e.SeriesID])
	return s.searchEpisode(ctx, se, *e, profile, activeEps, bud)
```

**`searchSeason`** — add the `bud *budget` parameter, guard the pack branch, and break the per-episode loop:

```go
func (s *Service) searchSeason(ctx context.Context, se *store.Series, seasonNumber int, eps []store.Episode, profile store.QualityProfile, activeEps map[int64]struct{}, bud *budget) (int, error) {
```

Immediately after the existing `if len(missing) == 0 { return 0, nil }`, add:

```go
	if !bud.allows() {
		return 0, nil
	}
```

Inside the `if len(missing) == len(monitored) {` pack branch, replace the existing `if grabbed { return 1, nil }` with:

```go
		if grabbed {
			bud.take()
			return 1, nil
		}
```

And replace the trailing per-episode loop with:

```go
	total := 0
	for _, e := range missing {
		if !bud.allows() {
			break
		}
		n, err := s.searchEpisode(ctx, se, e, profile, activeEps, bud)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
```

**`searchEpisode`** — add the parameter, guard before spending an indexer call, and take on grab:

```go
func (s *Service) searchEpisode(ctx context.Context, se *store.Series, e store.Episode, profile store.QualityProfile, activeEps map[int64]struct{}, bud *budget) (int, error) {
```

Add right after the existing `if _, queued := activeEps[e.ID]; queued { return 0, nil }`:

```go
	if !bud.allows() {
		return 0, nil
	}
```

and replace its `if grabbed { return 1, nil }` with:

```go
	if grabbed {
		bud.take()
		return 1, nil
	}
```

- [ ] **Step 8: Run the package suite**

Run: `go build ./... && go test ./internal/automation/`
Expected: PASS. If a pre-existing test now grabs fewer items than it used to, that test seeds multiple missing episodes and was relying on unlimited fan-out — set `MaxConcurrentPerSeries: 0` in that test's config to preserve its original intent, and say so in your report.

- [ ] **Step 9: Commit**

```bash
git add internal/automation/
git commit -m "feat(sp-b): gate the search path with a per-series budget"
```

---

### Task 4: Gate the RSS path

**Files:**
- Modify: `internal/automation/rss.go` (`RSSSync` at :228, `rssPlaceTV` at :350)
- Test: `internal/automation/rss_test.go`

**Interfaces:**
- Consumes: Task 3's `newBudget`/`allows`/`take` and its `episodeReleases(n int) []provider.Release` test helper (defined in `search_test.go`, same package — do not redefine it); Task 1's `seriesInFlight`.
- Produces: `rssPlaceTV(ctx, se, eps, ranked, activeEps, bud *budget) (int, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/automation/rss_test.go`, matching that file's existing style for building a feed:

```go
func TestRSSSyncStopsAtPerSeriesLimit(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedSeries(t, st, true, 5)
	// One release per episode: ungated, RSS would place all five.
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	// Default config → limit 1.

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 {
		t.Fatalf("RSS must respect the per-series limit: grabbed=%d reqs=%d", res.Grabbed, len(fe.reqs))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/automation/ -run TestRSSSyncStopsAtPerSeriesLimit`
Expected: FAIL — 5 grabbed, 1 wanted. **This failing test is the point of the task**: the search-path gate from Task 3 does nothing here, because `rssPlaceTV` is a separate implementation.

- [ ] **Step 3: Thread the budget through RSS**

In `RSSSync`, capture the counts (line ~275):

```go
	activeMovies, activeEps, inFlight, err := s.activeQueue(ctx)
```

Read the config once before the TV loop (place it just above `// TV: per series, ...`):

```go
	cfg, err := s.Config(ctx)
	if err != nil {
		return res, err
	}
```

and inside the TV loop, build a per-series budget and pass it down:

```go
		bud := newBudget(cfg.MaxConcurrentPerSeries, inFlight[seriesID])
		n, err := s.rssPlaceTV(ctx, se, eps, ranked, activeEps, bud)
```

In `rssPlaceTV`, add the parameter:

```go
func (s *Service) rssPlaceTV(ctx context.Context, se *store.Series, eps []store.Episode, ranked []Candidate, activeEps map[int64]struct{}, bud *budget) (int, error) {
```

Guard the season-pack loop — add as the first statement inside `for season, missing := range missingBySeason {`:

```go
		if !bud.allows() {
			break
		}
```

and call `bud.take()` at each point the function increments `grabbed`. Apply the same `if !bud.allows() { break }` guard as the first statement of the per-episode loop that follows.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/automation/`
Expected: PASS, including the pre-existing RSS tests.

- [ ] **Step 5: Commit**

```bash
git add internal/automation/
git commit -m "feat(sp-b): gate the RSS path with the same per-series budget"
```

---

### Task 5: Gate the upgrade path

**Files:**
- Modify: `internal/automation/upgrade.go` (`upgradeSweep` at :92, its episode loop at :173-197)
- Test: `internal/automation/upgrade_test.go`

**Interfaces:**
- Consumes: Task 3's `newBudget`/`allows`/`take` and its `episodeReleases(n int) []provider.Release` test helper (defined in `search_test.go`, same package — do not redefine it); Task 1's `seriesInFlight`.
- Produces: no new exported surface; `upgradeSweep`'s episode loop becomes budget-aware. Adds the `seedUpgradableSeries` test helper to `upgrade_test.go` (which must gain a `fmt` import).

- [ ] **Step 1: Write the failing tests**

Add to `internal/automation/upgrade_test.go`, following that file's existing pattern for seeding a series whose episodes have below-cutoff files:

```go
// seedUpgradableSeries seeds a monitored series whose every episode already has
// a WEBDL-1080p(7) file — below hdProfile's cutoff of 9 — so every episode is a
// valid upgrade target. Mirrors TestUpgradeSweepTVEpisode (upgrade_test.go:214).
func seedUpgradableSeries(t *testing.T, st *store.Store, epCount int) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, epCount)
	for i := range epIDs {
		id := epIDs[i]
		if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
			MediaKind: "tv", EpisodeID: &id,
			RelativePath: fmt.Sprintf("e%d.mkv", i+1), QualityID: 7,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return sid, epIDs
}

func TestUpgradeSweepStopsAtPerSeriesLimit(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedUpgradableSeries(t, st, 4)
	fs := &fakeSearcher{releases: episodeReleases(4)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	// Default config → limit 1.

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("upgrades must respect the per-series limit: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// Upgrades and missing-episode grabs share ONE budget.
func TestUpgradeSweepSharesBudgetWithInFlightGrab(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedUpgradableSeries(t, st, 4)
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[0]}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: episodeReleases(4)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("an in-flight grab must block upgrades for the same series: n=%d reqs=%d", n, len(fe.reqs))
	}
}
```

Note the exported `UpgradeSweep(ctx, batch)` is the method the existing tests call — `upgradeSweep` is the unexported inner function. `hdProfile()` defines qualities 7 and 9 with cutoff 9, which is why a quality-7 file is upgradable by a Bluray-1080p(9) release.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/automation/ -run TestUpgradeSweep`
Expected: FAIL — 4 grabs where 1 was wanted, and 4 where 0 was wanted.

- [ ] **Step 3: Thread the budget through the upgrade sweep**

In `upgradeSweep`, capture the counts (line ~107):

```go
	activeMovies, activeEps, inFlight, err := s.activeQueue(ctx)
```

`cfg` is already read at the top of the function. Inside the series loop, after the profile lookup and before the episode loop, build the budget:

```go
		bud := newBudget(cfg.MaxConcurrentPerSeries, inFlight[se.ID])
```

Then, as the first statement inside `for _, e := range eps {`:

```go
			if !bud.allows() {
				break
			}
```

and after a successful upgrade, spend it — replace `total += grabbed` in the episode loop with:

```go
			if grabbed > 0 {
				bud.take()
			}
			total += grabbed
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test ./internal/automation/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/automation/
git commit -m "feat(sp-b): gate the upgrade sweep with the per-series budget"
```

---

### Task 6: `ResearchSeries` — failure routing, success trigger, and pack exhaustion

The behavioural heart of the sub-project.

**Files:**
- Modify: `internal/importing/importing.go:67` (the `Researcher` interface)
- Modify: `internal/importing/command.go` (`ImportCompleted` at :14, `researchAfterFailure` at :73)
- Modify: `internal/automation/blocklist_filter.go` (add the `ResearchSeries` wrapper beside `ResearchMovie`/`ResearchEpisode` at :25-33)
- Test: `internal/importing/command_test.go`, `internal/automation/search_test.go`

**Interfaces:**
- Consumes: Task 3's gated `searchSeason` (pack exhaustion depends on it re-entering the pack branch).
- Produces: `Researcher.ResearchSeries(ctx context.Context, seriesID int64) error`, implemented by `*automation.Service`.

- [ ] **Step 1: Extend the fake and write the failing importing tests**

In `internal/importing/command_test.go`, change the fake:

```go
type fakeResearcher struct{ movies, episodes, series []int64 }

func (f *fakeResearcher) ResearchSeries(_ context.Context, id int64) error {
	f.series = append(f.series, id)
	return nil
}
```

(keep the existing `ResearchMovie` and `ResearchEpisode` methods unchanged), then add:

```go
// A failed TV row re-searches the SERIES, not each episode. For a season pack
// the per-episode variant fired one search per episode and dropped straight to
// per-episode grabbing, so the next-best pack was never tried.
func TestFailedTVDownloadResearchesSeriesNotEpisodes(t *testing.T) {
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	ctx := context.Background()
	sid, epID := seedSeriesWithProfile(t, st)
	res := &fakeResearcher{}
	svc.SetResearcher(res)

	q, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epID},
		SourceTitle: "The.Show.S01.1080p.BluRay.x264-GRP", Protocol: "usenet",
		QualityID: 9, Status: store.QueueGrabbed,
	})
	if err != nil {
		t.Fatal(err)
	}
	fq.items = []provider.DownloadItem{{
		ID: "nzo_1", DownloadClientID: "sab", Status: provider.StatusFailed,
		ErrorMessage: "unpack failed", Title: q.SourceTitle,
	}}

	if err := svc.ImportCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if len(res.series) != 1 || res.series[0] != sid {
		t.Fatalf("want ResearchSeries(%d), got series=%v", sid, res.series)
	}
	if len(res.episodes) != 0 {
		t.Fatalf("must not fire per-episode research for a TV row, got %v", res.episodes)
	}
}
```

Then the dedupe test. Three rows for ONE series complete in the same tick; the hook must fire once, not three times. Modelled on `TestImportCompletedScansGrabbedRows` (`command_test.go:17`), which is where the root-folder / profile / real-file-on-disk setup comes from — an import needs a real file to move:

```go
func TestImportCompletedResearchesSeriesOncePerTick(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rfID, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{
		TMDBID: 1, Title: "The Show", RootFolderID: &rfID, QualityProfileID: &prof.ID,
	})
	for i := 1; i <= 3; i++ {
		_ = st.UpsertEpisode(ctx, store.Episode{
			SeriesID: sid, SeasonNumber: 1, EpisodeNumber: i, Title: fmt.Sprintf("Ep%d", i),
		})
	}
	eps, _ := st.ListEpisodes(ctx, sid)

	res := &fakeResearcher{}
	svc.SetResearcher(res)

	var items []provider.DownloadItem
	for i, e := range eps {
		dl := t.TempDir()
		title := fmt.Sprintf("The.Show.S01E%02d.1080p.BluRay.x264-GRP", i+1)
		writeFile(t, filepath.Join(dl, title+".mkv"), 60*1024*1024)
		epID := e.ID
		if _, err := st.EnqueueGrab(ctx, store.QueueItem{
			DownloadClientID: "c1", ClientItemID: fmt.Sprintf("h%d", i+1), Protocol: "usenet",
			SourceTitle: title, MediaKind: "tv", SeriesID: &sid,
			EpisodeIDs: []int64{epID}, QualityID: 9, Status: store.QueueGrabbed,
		}); err != nil {
			t.Fatal(err)
		}
		items = append(items, provider.DownloadItem{
			ID: fmt.Sprintf("h%d", i+1), DownloadClientID: "c1",
			Status: provider.StatusCompleted, OutputPath: dl,
		})
	}
	fq.items = items

	if err := svc.ImportCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if len(res.series) != 1 || res.series[0] != sid {
		t.Fatalf("3 imports for one series must research it ONCE, got %v", res.series)
	}
}
```

This test needs `fmt` and `path/filepath` imported in `command_test.go`; `writeFile` lives in `importer_test.go:13` (same package).

Finally, prove movie rows never route to the series hook. Rather than build a movie-import fixture that does not exist in this package, add one assertion to the **existing** `TestReconcileFailedDownloadBlocklistsAndRetries` (`command_test.go:112`), which already sets up a movie row and a `fakeResearcher`:

```go
	if len(res.series) != 0 {
		t.Fatalf("a movie row must never route to ResearchSeries, got %v", res.series)
	}
```

The movie *success* case needs no test: `ImportCompleted` only records a series id under `if row.SeriesID != nil`, which is nil for every movie row, so it is structurally unreachable rather than merely untested. Spec §5.2 was amended to say this.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/importing/ -run 'TestFailedTVDownloadResearchesSeries|TestImportCompleted'`
Expected: FAIL to COMPILE first (`*fakeResearcher does not implement Researcher`) once the interface gains the method; before that, the routing test fails because `res.series` stays empty while `res.episodes` has one entry.

- [ ] **Step 3: Add the interface method**

In `internal/importing/importing.go`:

```go
type Researcher interface {
	ResearchMovie(ctx context.Context, movieID int64) error
	ResearchEpisode(ctx context.Context, episodeID int64) error
	ResearchSeries(ctx context.Context, seriesID int64) error
}
```

- [ ] **Step 4: Route failures to the series**

In `internal/importing/command.go`, replace `researchAfterFailure`:

```go
// researchAfterFailure triggers a fresh search for the failed target so
// automation can grab an alternative release, now that this one is blocklisted.
// TV rows re-search the whole SERIES rather than each episode: for a season pack
// the per-episode variant skipped straight to per-episode grabbing, so the
// next-best pack was never tried. Re-running the series search re-enters the
// season-pack branch with the failed pack blocklisted, which is what makes pack
// exhaustion work.
func (s *Service) researchAfterFailure(ctx context.Context, row store.QueueItem) {
	if s.researcher == nil {
		return
	}
	if row.MediaKind == string(provider.KindMovie) && row.MovieID != nil {
		if err := s.researcher.ResearchMovie(ctx, *row.MovieID); err != nil {
			slog.Warn("importing: re-search after failure failed", "movieId", *row.MovieID, "err", err)
		}
		return
	}
	if row.SeriesID != nil {
		if err := s.researcher.ResearchSeries(ctx, *row.SeriesID); err != nil {
			slog.Warn("importing: re-search after failure failed", "seriesId", *row.SeriesID, "err", err)
		}
	}
}
```

- [ ] **Step 5: Add the success trigger, deduped per tick**

In `internal/importing/command.go`, replace `ImportCompleted`:

```go
// ImportCompleted imports every grabbed queue row whose client item is
// completed, and handles rows whose client item has failed (blocklist + retry).
// After a successful TV import the series is re-searched, so the next episode is
// grabbed as soon as the slot frees rather than waiting for the next scheduled
// sweep. Series ids are collected and researched once per tick — firing per row
// would launch several concurrent searches racing for the same freed slot.
func (s *Service) ImportCompleted(ctx context.Context) error {
	rows, err := s.store.QueueByStatus(ctx, store.QueueGrabbed)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	items := s.queue.Queue(ctx).Items
	var imported []int64
	seen := map[int64]struct{}{}
	for _, row := range rows {
		it, ok := matchItem(items, row)
		if !ok {
			continue
		}
		switch it.Status {
		case provider.StatusCompleted:
			if err := s.ImportItem(ctx, row.ID); err != nil {
				return err
			}
			if row.SeriesID != nil {
				if _, dup := seen[*row.SeriesID]; !dup {
					seen[*row.SeriesID] = struct{}{}
					imported = append(imported, *row.SeriesID)
				}
			}
		case provider.StatusFailed:
			if err := s.handleFailed(ctx, row, it); err != nil {
				return err
			}
		}
	}
	s.researchImportedSeries(ctx, imported)
	return nil
}

// researchImportedSeries re-searches each series that just had an import land,
// so the freed concurrency slot is used immediately. Failures are logged and
// swallowed: an import must never fail because a follow-up search did.
func (s *Service) researchImportedSeries(ctx context.Context, seriesIDs []int64) {
	if s.researcher == nil {
		return
	}
	for _, id := range seriesIDs {
		if err := s.researcher.ResearchSeries(ctx, id); err != nil {
			slog.Warn("importing: re-search after import failed", "seriesId", id, "err", err)
		}
	}
}
```

- [ ] **Step 6: Implement the automation-side wrapper**

In `internal/automation/blocklist_filter.go`, beside the existing wrappers:

```go
func (s *Service) ResearchSeries(ctx context.Context, seriesID int64) error {
	_, err := s.SearchSeries(ctx, seriesID)
	return err
}
```

- [ ] **Step 7: Write the failing pack-exhaustion test**

Add to `internal/automation/search_test.go`. Two acceptable packs; blocklisting the first must yield the second, NOT a per-episode grab:

```go
func TestSearchSeasonTriesNextPackWhenFirstIsBlocklisted(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 3)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack1", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01.1080p.WEB-DL.x264-GRP", DownloadURL: "pack2", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "ep1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	// The first pack has already failed and been blocklisted, exactly as
	// handleFailed would leave things before calling ResearchSeries.
	if _, err := st.AddBlocklist(ctx, store.Blocklist{
		MediaKind: "tv", SeriesID: &sid,
		SourceTitle: "The.Show.S01.1080p.BluRay.x264-GRP",
		Protocol:    "usenet", Reason: "unpack failed",
	}); err != nil {
		t.Fatal(err)
	}

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("want exactly 1 grab, got n=%d reqs=%d", n, len(fe.reqs))
	}
	if fe.reqs[0].DownloadURL != "pack2" {
		t.Fatalf("must try the next PACK before per-episode, got %q", fe.reqs[0].DownloadURL)
	}
	if len(fe.reqs[0].EpisodeIDs) != 3 {
		t.Fatalf("a pack grab covers every missing episode, got %v", fe.reqs[0].EpisodeIDs)
	}
}

func TestSearchSeasonFallsBackToEpisodesWhenPacksExhausted(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 3)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack1", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "ep1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	if _, err := st.AddBlocklist(ctx, store.Blocklist{
		MediaKind: "tv", SeriesID: &sid,
		SourceTitle: "The.Show.S01.1080p.BluRay.x264-GRP",
		Protocol:    "usenet", Reason: "unpack failed",
	}); err != nil {
		t.Fatal(err)
	}

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("want exactly 1 grab, got n=%d reqs=%d", n, len(fe.reqs))
	}
	if fe.reqs[0].DownloadURL != "ep1" {
		t.Fatalf("with no packs left it must fall back per-episode, got %q", fe.reqs[0].DownloadURL)
	}
}
```

- [ ] **Step 8: Run everything**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS across all packages.

Both pack tests should pass **without any new production code beyond Steps 3-6** — that is the design claim (§4.6 of the spec: the blocklist is the state). If either fails, do NOT add pack-tracking state; report it, because it means the emergent behaviour the design depends on does not actually hold.

`cmd/nexus` also compiles against `Researcher`; if anything there implements the interface it needs the new method too. `go build ./...` will tell you.

- [ ] **Step 9: Commit**

```bash
git add internal/
git commit -m "feat(sp-b): ResearchSeries hook, success trigger, and pack exhaustion"
```

---

### Task 7: Frontend setting and bundle rebuild

**Files:**
- Modify: `web/src/features/settings/configTypes.ts`
- Modify: `web/src/features/settings/GeneralSection.tsx`
- Test: `web/src/features/settings/GeneralSection.test.tsx`
- Modify: `web/dist/**` (rebuild)

**Interfaces:**
- Consumes: Task 2's `maxConcurrentPerSeries` JSON field.
- Produces: nothing consumed by later tasks (final task).

- [ ] **Step 1: Add the field to the type**

In `web/src/features/settings/configTypes.ts`, add to `AutomationConfig` after `upgradeGrabCooldownHours`:

```ts
  maxConcurrentPerSeries: number
```

- [ ] **Step 2: Add the form field**

In `web/src/features/settings/GeneralSection.tsx`, append to `NUM_FIELDS`:

```tsx
  { key: "maxConcurrentPerSeries", label: "Max concurrent downloads per series (0 = unlimited)" },
```

- [ ] **Step 3: Update the section test**

Read `web/src/features/settings/GeneralSection.test.tsx` first. If it mocks an `AutomationConfig` object, add `maxConcurrentPerSeries` to that fixture — a missing key will make the new input render as `undefined` and may break an existing assertion. If it asserts a field count or snapshot, update it.

Then add a test asserting the new field renders and round-trips, following the file's existing style for the other numeric fields:

```tsx
it("renders the per-series concurrency limit", () => {
  // render with an automation config whose maxConcurrentPerSeries is 1
  expect(screen.getByLabelText(/max concurrent downloads per series/i)).toBeInTheDocument()
})
```

- [ ] **Step 4: Run the frontend suite and the REAL typecheck**

Run: `cd web && npx vitest run && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS, 0 type errors.

Do NOT use a bare `npx tsc --noEmit` — it typechecks nothing in this repo and always exits 0.

- [ ] **Step 5: Rebuild the bundle**

Run: `cd web && npm run build`
Expected: build succeeds and `web/dist` asset hashes change.

- [ ] **Step 6: Full-stack verification**

Run from the repo root: `go build ./... && go vet ./... && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 7: Commit**

```bash
git add web/src web/dist
git commit -m "feat(sp-b): surface the per-series download limit in settings"
```

---

## Verification Checklist

After Task 7, confirm each spec requirement:

- [ ] A series with 5 missing episodes and limit 1 yields exactly 1 grab; limit 3 yields 3; limit 0 yields 5 (spec §4.2, §4.3)
- [ ] The budget spans seasons — 2 fully-missing seasons, limit 1 → 1 grab total (spec §4.3)
- [ ] A directly-enqueued row (standing in for a manual grab) occupies the slot (spec §3.3)
- [ ] The RSS path is gated by its own test against `rssPlaceTV` (spec §4.4)
- [ ] The upgrade sweep is gated, and shares one budget with missing-episode grabs (spec §4.4b)
- [ ] `MaxConcurrentPerSeries: 0` survives `Service.Config()` un-normalized (spec §4.7)
- [ ] A failed TV row calls `ResearchSeries` once, never `ResearchEpisode` (spec §4.5)
- [ ] Three successful imports for one series in one tick → `ResearchSeries` called exactly once (spec §4.5)
- [ ] A successful movie import triggers no research (spec §4.5)
- [ ] A blocklisted first pack yields the second pack, not a per-episode grab; with packs exhausted it falls back per-episode (spec §4.6)
- [ ] `TestListQueueReturnsAllRowsUnpaged` still passes (spec §3.1)
- [ ] `go build ./... && go vet ./... && go test ./...` all pass
- [ ] `cd web && npx vitest run && npx tsc -p tsconfig.app.json --noEmit` pass
- [ ] `web/dist` rebuilt and committed
