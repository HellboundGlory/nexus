# Nexus Automation: Upgrade / Cutoff-Unmet Search (5c) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a scheduled sweep that upgrades monitored library items whose existing file ranks below their quality-profile cutoff, grabbing the best genuine-upgrade release while guarding against a re-grab loop.

**Architecture:** 5c inverts 5a's missing sweep on file-presence. A new `Service.UpgradeSweep` iterates monitored items that *have* a file below cutoff, reuses the existing `Decide`/`enqueueBest`/`activeQueue`/`profileFor`/`tvRequest` machinery, adds one new selection rule (upgrade filter: `quality.IsUpgrade` over the existing file's quality) and one new safety mechanism (a history-based cooldown guard). The import pipeline's existing `quality.IsUpgrade` gate is the final backstop. No new migration.

**Tech Stack:** Go, `modernc.org/sqlite`, go-chi, existing `internal/automation`, `internal/quality`, `internal/core/store`, `internal/importing`, `internal/core/scheduler`.

## Global Constraints

- Go toolchain not on PATH: prefix every Go command with `export PATH="/c/Program Files/Go/bin:$PATH"`.
- `-race` is unavailable (no CGO/C compiler) — verify concurrency with `-count=N` only.
- Build/verify with `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...`.
- Module boundary: `internal/automation` imports only `internal/core/*`, `internal/parsing`, `internal/quality`, `internal/importing`. New helpers live in `internal/quality` (leaf over `store.QualityProfile`) and `internal/core/store`. No new cross-package edges.
- No new migration. The cooldown guard reuses the existing `history` table.
- Quality-definition IDs are the shared currency: `MediaFile.QualityID`, `quality.Resolve(parsed).ID`, and every argument to `quality.IsUpgrade`/`CutoffUnmet` are all definition IDs (verified against `internal/importing/importer.go`, which stores `mf.QualityID = quality.Resolve(parsed).ID` and gates with `IsUpgrade(q.ID, existing.QualityID, profile)`).
- Config bool fields (`RSSSyncEnabled`, `UpgradeSearchEnabled`) are NOT subject to the non-positive-fallback rule; only int fields are clamped.
- `hdProfile()` (in `internal/automation/decide_test.go`) allows WEBDL-1080p(7) + Bluray-1080p(9), `CutoffQualityID: 9`, `UpgradeAllowed: true`. `upProfile(bool)` (in `internal/quality/upgrade_test.go`) has the same 7/9 ladder, cutoff 9.

---

### Task 1: `quality.CutoffUnmet` pre-filter

**Files:**
- Modify: `internal/quality/decision.go` (add `CutoffUnmet` after `IsUpgrade`)
- Test: `internal/quality/upgrade_test.go` (add `TestCutoffUnmet`; reuse existing `upProfile`)

**Interfaces:**
- Consumes: `store.QualityProfile`, the existing unexported `profileRank(profile, qualityID) (rank int, allowed bool)` in `decision.go`.
- Produces: `func CutoffUnmet(existingID int, profile store.QualityProfile) bool` — true iff upgrades are enabled AND `existingID` ranks strictly below the profile cutoff. Used by the sweep as a pre-search filter so at-cutoff items never hit an indexer.

- [ ] **Step 1: Write the failing test**

Add to `internal/quality/upgrade_test.go`:

```go
func TestCutoffUnmet(t *testing.T) {
	p := upProfile(true) // 7 < 9, cutoff 9, upgrades on
	if !CutoffUnmet(7, p) {
		t.Fatal("quality below cutoff should be cutoff-unmet")
	}
	if CutoffUnmet(9, p) {
		t.Fatal("quality at cutoff should NOT be cutoff-unmet")
	}
	if CutoffUnmet(7, upProfile(false)) {
		t.Fatal("upgrades disabled -> never cutoff-unmet")
	}
	if !CutoffUnmet(999, p) {
		t.Fatal("quality absent from profile ranks below all -> cutoff-unmet")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/quality/ -run TestCutoffUnmet -v`
Expected: FAIL — `undefined: CutoffUnmet`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/quality/decision.go` immediately after `IsUpgrade`:

```go
// CutoffUnmet reports whether an existing file of quality existingID is eligible
// for an upgrade under the profile: upgrades enabled AND the existing quality
// ranks strictly below the profile cutoff. It is IsUpgrade's cutoff arm made
// available without a candidate, for use as a pre-search filter. Qualities absent
// from the profile rank below all present ones (profileRank returns -1).
func CutoffUnmet(existingID int, profile store.QualityProfile) bool {
	if !profile.UpgradeAllowed {
		return false
	}
	existingRank, _ := profileRank(profile, existingID)
	cutoffRank, _ := profileRank(profile, profile.CutoffQualityID)
	return existingRank < cutoffRank
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/quality/ -run TestCutoffUnmet -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/quality/decision.go internal/quality/upgrade_test.go
git commit -m "feat(quality): CutoffUnmet pre-filter for upgrade search"
```

---

### Task 2: `store.GrabbedSince` history read

**Files:**
- Modify: `internal/core/store/import_store.go` (add `GrabbedSince` + a `sqliteTimeLayout` const, after `ListHistory`)
- Test: `internal/core/store/import_store_test.go` (add `TestGrabbedSince`)

**Interfaces:**
- Consumes: existing `HistoryEvent` struct and the `history` table (columns per migration `0006_import.sql`); `newImportTestStore(t)` and `i64(v)` helpers already in the test file.
- Produces: `func (s *Store) GrabbedSince(ctx context.Context, since time.Time) ([]HistoryEvent, error)` — returns `event_type='grabbed'` rows with `created_at >= since`, newest-first. `since` is bound in SQLite's `CURRENT_TIMESTAMP` string format so the TEXT comparison is chronologically correct.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/store/import_store_test.go` (the file already imports `context`, `testing`; add `"time"` to its import block):

```go
func TestGrabbedSince(t *testing.T) {
	st := newImportTestStore(t)
	ctx := context.Background()
	// movie_id/series_id left nil to avoid FK constraints; we only test the
	// event_type + time filtering here.
	if err := st.AddHistory(ctx, HistoryEvent{EventType: "grabbed", MediaKind: "movie", SourceTitle: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddHistory(ctx, HistoryEvent{EventType: "imported", MediaKind: "movie", SourceTitle: "B"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GrabbedSince(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SourceTitle != "A" || got[0].EventType != "grabbed" {
		t.Fatalf("want only the grabbed row A, got %+v", got)
	}
	future, err := st.GrabbedSince(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(future) != 0 {
		t.Fatalf("future since should return no rows, got %d", len(future))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/core/store/ -run TestGrabbedSince -v`
Expected: FAIL — `st.GrabbedSince undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/core/store/import_store.go` immediately after `ListHistory` (the file already imports `time`):

```go
// sqliteTimeLayout matches SQLite's CURRENT_TIMESTAMP text format so a bound
// time compares correctly (lexicographically == chronologically) against
// created_at values written by the DB default.
const sqliteTimeLayout = "2006-01-02 15:04:05"

// GrabbedSince returns "grabbed" history events created at or after since,
// newest first. Used by the automation upgrade sweep to build its cooldown guard.
func (s *Store) GrabbedSince(ctx context.Context, since time.Time) ([]HistoryEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, media_kind, series_id, episode_id, movie_id, source_title, quality_id, message, created_at
		 FROM history WHERE event_type = 'grabbed' AND created_at >= ? ORDER BY id DESC`,
		since.UTC().Format(sqliteTimeLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEvent
	for rows.Next() {
		var h HistoryEvent
		if err := rows.Scan(&h.ID, &h.EventType, &h.MediaKind, &h.SeriesID, &h.EpisodeID, &h.MovieID,
			&h.SourceTitle, &h.QualityID, &h.Message, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/core/store/ -run TestGrabbedSince -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/store/import_store.go internal/core/store/import_store_test.go
git commit -m "feat(store): GrabbedSince history read for upgrade cooldown"
```

---

### Task 3: Config fields for the upgrade sweep

**Files:**
- Modify: `internal/automation/config.go` (`Config` struct, `DefaultConfig`, `Config()` clamps)
- Test: `internal/automation/config_test.go` (add two tests; update `TestConfigRoundTrip` and `TestConfigRSSRoundTrip`)

**Interfaces:**
- Consumes: existing `Config`/`DefaultConfig`/`Service.Config`/`Service.SetConfig`.
- Produces: four new `Config` fields — `UpgradeSearchEnabled bool` (default `true`), `UpgradeSearchIntervalHours int` (default `12`), `UpgradeSearchBatchSize int` (default `100`), `UpgradeGrabCooldownHours int` (default `168`). Non-positive ints clamp to their default; the bool does not clamp.

- [ ] **Step 1: Write the failing tests**

In `internal/automation/config_test.go`, first UPDATE the two whole-struct round-trip tests so they account for the new clamped defaults.

In `TestConfigRoundTrip`, after the existing `want.RSSSyncIntervalMinutes = 15` line and before the `if got != want` check, add:

```go
	want.UpgradeSearchIntervalHours = 12
	want.UpgradeSearchBatchSize = 100
	want.UpgradeGrabCooldownHours = 168
	// UpgradeSearchEnabled stays false (persisted zero value, bool not clamped)
```

In `TestConfigRSSRoundTrip`, change the `want` literal to also carry the upgrade defaults it will read back:

```go
	want := Config{
		MissingSearchIntervalHours: 6, MissingSearchBatchSize: 100,
		RSSSyncEnabled: true, RSSSyncIntervalMinutes: 30,
		UpgradeSearchIntervalHours: 12, UpgradeSearchBatchSize: 100, UpgradeGrabCooldownHours: 168,
	}
```

Then add two new tests:

```go
func TestConfigUpgradeDefaults(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	got, err := svc.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpgradeSearchEnabled {
		t.Fatal("upgrade search should default enabled")
	}
	if got.UpgradeSearchIntervalHours != 12 || got.UpgradeSearchBatchSize != 100 || got.UpgradeGrabCooldownHours != 168 {
		t.Fatalf("unexpected upgrade defaults: %+v", got)
	}
}

func TestConfigUpgradeClamps(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	ctx := context.Background()
	if err := svc.SetConfig(ctx, Config{
		MissingSearchIntervalHours: 6, MissingSearchBatchSize: 100,
		RSSSyncEnabled: true, RSSSyncIntervalMinutes: 15,
		UpgradeSearchEnabled: false, UpgradeSearchIntervalHours: 0,
		UpgradeSearchBatchSize: 0, UpgradeGrabCooldownHours: 0,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.UpgradeSearchIntervalHours != 12 || got.UpgradeSearchBatchSize != 100 || got.UpgradeGrabCooldownHours != 168 {
		t.Fatalf("non-positive upgrade ints should clamp to defaults, got %+v", got)
	}
	if got.UpgradeSearchEnabled {
		t.Fatal("explicit UpgradeSearchEnabled=false must be respected")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run 'TestConfigUpgrade|TestConfigRoundTrip|TestConfigRSSRoundTrip' -v`
Expected: FAIL — `unknown field UpgradeSearchEnabled in struct literal` (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/automation/config.go`, extend the struct:

```go
type Config struct {
	MissingSearchIntervalHours int  `json:"missingSearchIntervalHours"`
	MissingSearchBatchSize     int  `json:"missingSearchBatchSize"`
	RSSSyncEnabled             bool `json:"rssSyncEnabled"`
	RSSSyncIntervalMinutes     int  `json:"rssSyncIntervalMinutes"`
	UpgradeSearchEnabled       bool `json:"upgradeSearchEnabled"`
	UpgradeSearchIntervalHours int  `json:"upgradeSearchIntervalHours"`
	UpgradeSearchBatchSize     int  `json:"upgradeSearchBatchSize"`
	UpgradeGrabCooldownHours   int  `json:"upgradeGrabCooldownHours"`
}
```

Extend `DefaultConfig`:

```go
func DefaultConfig() Config {
	return Config{
		MissingSearchIntervalHours: 6,
		MissingSearchBatchSize:     100,
		RSSSyncEnabled:             true,
		RSSSyncIntervalMinutes:     15,
		UpgradeSearchEnabled:       true,
		UpgradeSearchIntervalHours: 12,
		UpgradeSearchBatchSize:     100,
		UpgradeGrabCooldownHours:   168,
	}
}
```

In `Config()`, add clamps after the existing `RSSSyncIntervalMinutes` clamp and before `return c, nil`:

```go
	if c.UpgradeSearchIntervalHours <= 0 {
		c.UpgradeSearchIntervalHours = d.UpgradeSearchIntervalHours
	}
	if c.UpgradeSearchBatchSize <= 0 {
		c.UpgradeSearchBatchSize = d.UpgradeSearchBatchSize
	}
	if c.UpgradeGrabCooldownHours <= 0 {
		c.UpgradeGrabCooldownHours = d.UpgradeGrabCooldownHours
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run 'TestConfig' -v`
Expected: PASS (all config tests, including the two updated round-trips).

- [ ] **Step 5: Commit**

```bash
git add internal/automation/config.go internal/automation/config_test.go
git commit -m "feat(automation): upgrade-search config fields (enabled/interval/batch/cooldown)"
```

---

### Task 4: Upgrade event, cooldown set, and candidate filter

**Files:**
- Create: `internal/automation/upgrade.go` (event + `cooldownSet`/keys + `buildCooldownSet` + `upgradeCandidates`; the sweep is added in Task 5)
- Test: `internal/automation/upgrade_test.go` (pure-helper tests)

**Interfaces:**
- Consumes: `store.HistoryEvent`, `store.QualityProfile`, `provider.Release`, the package-local `Candidate` (from `decide.go`), `normTitle` (from `rss.go`), `quality.Resolve`/`quality.IsUpgrade`.
- Produces (for Task 5 and wiring):
  - `type UpgradeCompleted struct { Grabbed int }` with `Name() string = "automation.upgrade.completed"`.
  - `func movieKey(id int64) string` / `func seriesKey(id int64) string` — item keys.
  - `type cooldownSet` with `func buildCooldownSet(events []store.HistoryEvent) cooldownSet` and method `has(itemKey, title string) bool`.
  - `func upgradeCandidates(cands []Candidate, existingQualityID int, profile store.QualityProfile, itemKey string, cs cooldownSet) []Candidate`.

- [ ] **Step 1: Write the failing tests**

Create `internal/automation/upgrade_test.go`:

```go
package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

func i64p(v int64) *int64 { return &v }

func TestUpgradeCompletedName(t *testing.T) {
	if (UpgradeCompleted{}).Name() != "automation.upgrade.completed" {
		t.Fatalf("bad event name %q", (UpgradeCompleted{}).Name())
	}
}

func TestBuildCooldownSetAndHas(t *testing.T) {
	events := []store.HistoryEvent{
		{EventType: "grabbed", MovieID: i64p(5), SourceTitle: "The.Film.2020.1080p.BluRay.x264-GRP"},
		{EventType: "grabbed", SeriesID: i64p(9), SourceTitle: "The.Show.S01E01.1080p.WEB-DL.x264-GRP"},
		{EventType: "grabbed", SourceTitle: "orphan-no-ids"}, // ignored: no movie/series id
	}
	cs := buildCooldownSet(events)
	if !cs.has(movieKey(5), "The.Film.2020.1080p.BluRay.x264-GRP") {
		t.Fatal("recent movie grab should be in cooldown set")
	}
	if !cs.has(seriesKey(9), "The.Show.S01E01.1080p.WEB-DL.x264-GRP") {
		t.Fatal("recent series grab should be in cooldown set")
	}
	if cs.has(movieKey(6), "The.Film.2020.1080p.BluRay.x264-GRP") {
		t.Fatal("different movie must not match")
	}
	if cs.has(movieKey(5), "Some.Other.Title") {
		t.Fatal("different title must not match")
	}
}

func TestUpgradeCandidatesFiltersNonUpgradesAndCooldown(t *testing.T) {
	p := hdProfile() // 7 & 9, cutoff 9
	mkCand := func(title string) Candidate {
		return Candidate{Release: provider.Release{Title: title}, Parsed: parsing.Parse(title, provider.KindMovie)}
	}
	web := mkCand("The.Film.2020.1080p.WEB-DL.x264-GRP") // quality 7
	blu := mkCand("The.Film.2020.1080p.BluRay.x264-GRP") // quality 9
	// Existing file is WEBDL-1080p(7); only the Bluray(9) is an upgrade.
	out := upgradeCandidates([]Candidate{web, blu}, 7, p, movieKey(1), cooldownSet{})
	if len(out) != 1 || out[0].Release.Title != blu.Release.Title {
		t.Fatalf("only the Bluray upgrade should survive, got %+v", out)
	}
	// Put the Bluray title on cooldown for this movie -> nothing survives.
	cs := buildCooldownSet([]store.HistoryEvent{
		{EventType: "grabbed", MovieID: i64p(1), SourceTitle: blu.Release.Title},
	})
	out = upgradeCandidates([]Candidate{web, blu}, 7, p, movieKey(1), cs)
	if len(out) != 0 {
		t.Fatalf("cooldown should suppress the only upgrade, got %+v", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run 'TestUpgradeCompletedName|TestBuildCooldownSet|TestUpgradeCandidates' -v`
Expected: FAIL — `undefined: UpgradeCompleted` / `buildCooldownSet` / `upgradeCandidates` (compile error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/automation/upgrade.go`:

```go
package automation

import (
	"fmt"

	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/quality"
)

// UpgradeCompleted is emitted when an upgrade / cutoff-unmet sweep finishes.
type UpgradeCompleted struct {
	Grabbed int `json:"grabbed"`
}

func (UpgradeCompleted) Name() string { return "automation.upgrade.completed" }

func movieKey(id int64) string  { return fmt.Sprintf("movie:%d", id) }
func seriesKey(id int64) string { return fmt.Sprintf("series:%d", id) }

type cooldownKey struct {
	item  string
	title string
}

// cooldownSet is the set of (itemKey, normalized release title) pairs grabbed
// within the cooldown window. A candidate matching one must not be re-grabbed —
// this closes the mislabel re-grab loop (a release whose title claims a quality
// its file does not deliver would otherwise be grabbed every sweep).
type cooldownSet map[cooldownKey]struct{}

// buildCooldownSet keys grabbed-history events by their item and normalized
// source title. TV grabbed rows carry series_id (not episode_id), so TV keys are
// series-level; events with neither a movie nor a series id are ignored.
func buildCooldownSet(events []store.HistoryEvent) cooldownSet {
	cs := make(cooldownSet, len(events))
	for _, e := range events {
		var item string
		switch {
		case e.MovieID != nil:
			item = movieKey(*e.MovieID)
		case e.SeriesID != nil:
			item = seriesKey(*e.SeriesID)
		default:
			continue
		}
		cs[cooldownKey{item: item, title: normTitle(e.SourceTitle)}] = struct{}{}
	}
	return cs
}

func (cs cooldownSet) has(itemKey, title string) bool {
	_, ok := cs[cooldownKey{item: itemKey, title: normTitle(title)}]
	return ok
}

// upgradeCandidates keeps only candidates that are a genuine upgrade over the
// existing file's quality AND were not grabbed for this item within the cooldown
// window. Input is assumed already ranked best-first by Decide; order is
// preserved.
func upgradeCandidates(cands []Candidate, existingQualityID int, profile store.QualityProfile, itemKey string, cs cooldownSet) []Candidate {
	var out []Candidate
	for _, c := range cands {
		newID := quality.Resolve(c.Parsed).ID
		if !quality.IsUpgrade(newID, existingQualityID, profile) {
			continue
		}
		if cs.has(itemKey, c.Release.Title) {
			continue
		}
		out = append(out, c)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run 'TestUpgradeCompletedName|TestBuildCooldownSet|TestUpgradeCandidates' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/automation/upgrade.go internal/automation/upgrade_test.go
git commit -m "feat(automation): upgrade event, cooldown set, and candidate filter"
```

---

### Task 5: `UpgradeSweep` (movies + per-episode TV)

**Files:**
- Modify: `internal/automation/upgrade.go` (add `UpgradeSweep` + private `upgradeSweep`/`upgradeMovie`/`upgradeEpisode`; extend the import block)
- Test: `internal/automation/upgrade_test.go` (add sweep tests)

**Interfaces:**
- Consumes: `Service` (from `automation.go`), `s.store`, `s.search`, `s.emit`; reused helpers `Decide` (decide.go), `enqueueBest`/`activeQueue`/`profileFor`/`movieQuery`/`tvQuery`/`tvRequest`/`containsInt` (search.go); `store.GrabbedSince` (Task 2); `quality.CutoffUnmet` (Task 1); `buildCooldownSet`/`upgradeCandidates`/`movieKey`/`seriesKey`/`UpgradeCompleted` (Task 4); `importing.EnqueueRequest`; `provider.KindMovie`/`KindTV`.
- Produces: `func (s *Service) UpgradeSweep(ctx context.Context, batch int) (int, error)` — the scheduled sweep; returns total grabbed and emits `UpgradeCompleted`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/automation/upgrade_test.go` (add `"context"` and `"github.com/hellboundg/nexus/internal/importing"` are NOT needed here; the tests only need `context`, `store`, `provider`, `testing` — `context` and `testing` plus the existing `store`/`provider` imports. Add `"context"` to the import block):

```go
func fileMovie(t *testing.T, st *store.Store, qualityID int) int64 {
	t.Helper()
	id := seedMovie(t, st, true, true) // monitored, hdProfile (7/9, cutoff 9, upgrades on)
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{
		MediaKind: "movie", MovieID: &id, RelativePath: "m.mkv", QualityID: qualityID,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestUpgradeSweepGrabsUpgrade(t *testing.T) {
	st := newStore(t)
	fileMovie(t, st, 7) // existing WEBDL-1080p(7), below cutoff 9
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "blu" {
		t.Fatalf("below-cutoff movie should grab the Bluray upgrade: n=%d reqs=%+v", n, fe.reqs)
	}
}

func TestUpgradeSweepSkipsAtCutoffWithoutSearching(t *testing.T) {
	st := newStore(t)
	fileMovie(t, st, 9) // already at cutoff
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.2160p.BluRay.x265-GRP", DownloadURL: "uhd", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("at-cutoff item must not be grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("at-cutoff item must not trigger an indexer search, got %+v", fs.lastQuery)
	}
}

func TestUpgradeSweepRejectsNonUpgrade(t *testing.T) {
	st := newStore(t)
	fileMovie(t, st, 7)
	// Only a same-quality WEBDL-1080p(7) is offered -> accepted by profile but not an upgrade.
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.WEB-DL.x264-OTHER", DownloadURL: "web", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("non-upgrade must not be grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
	if fs.lastQuery.Type == "" {
		t.Fatalf("below-cutoff item should still have been searched")
	}
}

func TestUpgradeSweepSkipsRecentlyGrabbed(t *testing.T) {
	st := newStore(t)
	id := fileMovie(t, st, 7)
	title := "The.Film.2020.1080p.BluRay.x264-GRP"
	if err := st.AddHistory(context.Background(), store.HistoryEvent{
		EventType: "grabbed", MediaKind: "movie", MovieID: &id, SourceTitle: title,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: title, DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("release grabbed within cooldown must not be re-grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
}

func TestUpgradeSweepSkipsInFlight(t *testing.T) {
	st := newStore(t)
	id := fileMovie(t, st, 7)
	if _, err := st.EnqueueGrab(context.Background(), store.QueueItem{
		MediaKind: "movie", MovieID: &id, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("in-flight item must not be re-grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("in-flight item must not trigger a search, got %+v", fs.lastQuery)
	}
}

func TestUpgradeSweepRespectsUpgradesDisabled(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 5000, IMDbID: "tt5000", Title: "The Film", Year: 2020, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	p := hdProfile()
	p.Name = "NoUpgrade"
	p.UpgradeAllowed = false
	prof, err := st.CreateQualityProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMovieQualityProfileID(ctx, mid, &prof.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &mid, RelativePath: "m.mkv", QualityID: 7}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 || fs.lastQuery.Type != "" {
		t.Fatalf("upgrades-disabled profile must never search or grab: n=%d reqs=%d q=%+v", n, len(fe.reqs), fs.lastQuery)
	}
}

func TestUpgradeSweepTVEpisode(t *testing.T) {
	st := newStore(t)
	sid, epIDs := seedSeries(t, st, true, 1)
	_ = sid
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{
		MediaKind: "tv", EpisodeID: &epIDs[0], RelativePath: "e1.mkv", QualityID: 7,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].EpisodeIDs[0] != epIDs[0] {
		t.Fatalf("below-cutoff episode should grab a covering upgrade: n=%d reqs=%+v", n, fe.reqs)
	}
	if fs.lastQuery.Episode == nil {
		t.Fatalf("TV upgrade search must be per-episode, got %+v", fs.lastQuery)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestUpgradeSweep -v`
Expected: FAIL — `svc.UpgradeSweep undefined` (compile error).

- [ ] **Step 3: Write minimal implementation**

Update the import block of `internal/automation/upgrade.go` to:

```go
import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
	"github.com/hellboundg/nexus/internal/quality"
)
```

Append to `internal/automation/upgrade.go`:

```go
// UpgradeSweep processes up to batch monitored targets that already have a file
// ranking below their profile cutoff: monitored movies first, then each
// monitored episode of each monitored series. For each it searches, keeps only
// releases that are a genuine upgrade over the existing file and are not on
// cooldown, and grabs the best. Returns the total grabbed; a per-target error is
// logged and the sweep continues.
func (s *Service) UpgradeSweep(ctx context.Context, batch int) (int, error) {
	n, err := s.upgradeSweep(ctx, batch)
	s.emit(ctx, UpgradeCompleted{Grabbed: n})
	return n, err
}

func (s *Service) upgradeSweep(ctx context.Context, batch int) (int, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	if batch <= 0 {
		batch = cfg.UpgradeSearchBatchSize
	}
	since := time.Now().Add(-time.Duration(cfg.UpgradeGrabCooldownHours) * time.Hour)
	events, err := s.store.GrabbedSince(ctx, since)
	if err != nil {
		return 0, err
	}
	cs := buildCooldownSet(events)

	activeMovies, activeEps, err := s.activeQueue(ctx)
	if err != nil {
		return 0, err
	}

	processed, total := 0, 0

	movies, err := s.store.ListMovies(ctx)
	if err != nil {
		return 0, err
	}
	for _, m := range movies {
		if processed >= batch {
			return total, nil
		}
		if !m.Monitored {
			continue
		}
		f, err := s.store.MediaFileForMovie(ctx, m.ID)
		if err != nil {
			return total, err
		}
		if f == nil {
			continue // no file → that is the missing sweep's job
		}
		profile, ok, err := s.profileFor(ctx, m.QualityProfileID)
		if err != nil {
			return total, err
		}
		if !ok || !quality.CutoffUnmet(f.QualityID, profile) {
			continue
		}
		if _, queued := activeMovies[m.ID]; queued {
			continue
		}
		processed++
		grabbed, err := s.upgradeMovie(ctx, m, f, profile, cs)
		if err != nil {
			slog.Warn("automation: upgrade movie search failed", "movieId", m.ID, "err", err)
			continue
		}
		total += grabbed
	}

	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return total, err
	}
	for _, se := range series {
		if processed >= batch {
			return total, nil
		}
		if !se.Monitored {
			continue
		}
		profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
		if err != nil {
			return total, err
		}
		if !ok {
			continue
		}
		eps, err := s.store.ListEpisodes(ctx, se.ID)
		if err != nil {
			return total, err
		}
		for _, e := range eps {
			if processed >= batch {
				return total, nil
			}
			if !e.Monitored {
				continue
			}
			f, err := s.store.MediaFileForEpisode(ctx, e.ID)
			if err != nil {
				return total, err
			}
			if f == nil || !quality.CutoffUnmet(f.QualityID, profile) {
				continue
			}
			if _, queued := activeEps[e.ID]; queued {
				continue
			}
			processed++
			grabbed, err := s.upgradeEpisode(ctx, &se, e, f, profile, cs)
			if err != nil {
				slog.Warn("automation: upgrade episode search failed", "episodeId", e.ID, "err", err)
				continue
			}
			total += grabbed
		}
	}
	return total, nil
}

func (s *Service) upgradeMovie(ctx context.Context, m store.Movie, f *store.MediaFile, profile store.QualityProfile, cs cooldownSet) (int, error) {
	releases, err := s.search.Search(ctx, movieQuery(&m))
	if err != nil {
		slog.Warn("automation: upgrade movie search had indexer errors", "movieId", m.ID, "err", err)
	}
	cands := upgradeCandidates(Decide(releases, provider.KindMovie, profile), f.QualityID, profile, movieKey(m.ID), cs)
	_, grabbed, err := s.enqueueBest(ctx, cands, func(c Candidate) importing.EnqueueRequest {
		return importing.EnqueueRequest{
			DownloadURL: c.Release.DownloadURL, Title: c.Release.Title,
			Protocol: c.Release.Protocol, IndexerID: c.Release.IndexerID,
			MediaKind: provider.KindMovie, MovieID: m.ID,
		}
	})
	if err != nil {
		return 0, err
	}
	if grabbed {
		return 1, nil
	}
	return 0, nil
}

func (s *Service) upgradeEpisode(ctx context.Context, se *store.Series, e store.Episode, f *store.MediaFile, profile store.QualityProfile, cs cooldownSet) (int, error) {
	ep := e.EpisodeNumber
	releases, err := s.search.Search(ctx, tvQuery(se, e.SeasonNumber, &ep))
	if err != nil {
		slog.Warn("automation: upgrade episode search had indexer errors", "episodeId", e.ID, "err", err)
	}
	var covering []Candidate
	for _, c := range Decide(releases, provider.KindTV, profile) {
		if c.Parsed.Season == e.SeasonNumber && containsInt(c.Parsed.Episodes, e.EpisodeNumber) {
			covering = append(covering, c)
		}
	}
	covering = upgradeCandidates(covering, f.QualityID, profile, seriesKey(se.ID), cs)
	_, grabbed, err := s.enqueueBest(ctx, covering, func(c Candidate) importing.EnqueueRequest {
		return tvRequest(se.ID, []int64{e.ID}, c)
	})
	if err != nil {
		return 0, err
	}
	if grabbed {
		return 1, nil
	}
	return 0, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestUpgradeSweep -v`
Expected: PASS (all seven sweep tests).

- [ ] **Step 5: Run the whole automation package to catch regressions**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -count=1`
Expected: PASS (ok).

- [ ] **Step 6: Commit**

```bash
git add internal/automation/upgrade.go internal/automation/upgrade_test.go
git commit -m "feat(automation): UpgradeSweep for cutoff-unmet items (movies + per-episode TV)"
```

---

### Task 6: Command constructor + scheduler wiring + WS forwarding

**Files:**
- Modify: `internal/automation/command.go` (add `NewUpgradeSearchCommand`)
- Test: `internal/automation/command_test.go` (add `TestUpgradeSearchCommandRuns`)
- Modify: `cmd/nexus/main.go` (schedule the sweep when enabled; add the event to `WSForward`)

**Interfaces:**
- Consumes: `Service.Config`, `Service.UpgradeSweep`, the package-local `searchCommand` type; `command.Command`; `scheduler`, `automation`, `command` in `main.go`.
- Produces: `func NewUpgradeSearchCommand(svc *Service) command.Command` (command name `"UpgradeSearch"`).

- [ ] **Step 1: Write the failing test**

Add to `internal/automation/command_test.go`:

```go
func TestUpgradeSearchCommandRuns(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true)
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{
		MediaKind: "movie", MovieID: &id, RelativePath: "m.mkv", QualityID: 7,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	cmd := NewUpgradeSearchCommand(svc)
	if cmd.Name() != "UpgradeSearch" {
		t.Fatalf("bad name %q", cmd.Name())
	}
	if err := cmd.Run(context.Background(), nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if len(fe.reqs) != 1 {
		t.Fatalf("upgrade command should have grabbed one, got %d", len(fe.reqs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestUpgradeSearchCommandRuns -v`
Expected: FAIL — `undefined: NewUpgradeSearchCommand` (compile error).

- [ ] **Step 3: Write minimal implementation**

Add to `internal/automation/command.go` after `NewRSSSyncCommand`:

```go
// NewUpgradeSearchCommand is the scheduled upgrade / cutoff-unmet sweep over
// monitored items whose existing file ranks below the profile cutoff.
func NewUpgradeSearchCommand(svc *Service) command.Command {
	return &searchCommand{name: "UpgradeSearch", run: func(ctx context.Context) (int, error) {
		cfg, err := svc.Config(ctx)
		if err != nil {
			return 0, err
		}
		return svc.UpgradeSweep(ctx, cfg.UpgradeSearchBatchSize)
	}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestUpgradeSearchCommandRuns -v`
Expected: PASS.

- [ ] **Step 5: Wire the scheduler and WS forwarding in `cmd/nexus/main.go`**

In the scheduler block, immediately after the existing RSS `if autoCfg.RSSSyncEnabled { ... }` block (around line 130), add:

```go
		if autoCfg.UpgradeSearchEnabled {
			sch.Every(time.Duration(autoCfg.UpgradeSearchIntervalHours)*time.Hour, func() command.Command {
				return automation.NewUpgradeSearchCommand(autoSvc)
			})
		}
```

In the `api.NewRouter(api.Deps{ ... WSForward: []string{...}})` call, append `"automation.upgrade.completed"` to the `WSForward` slice so it reads:

```go
		WSForward: []string{"indexer.status", "download.status", "media.series.updated", "media.movie.updated", "import.completed", "queue.updated", "automation.search.completed", "automation.rss.completed", "automation.upgrade.completed"},
```

- [ ] **Step 6: Build, vet, and run the full test suite**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build ./... && go vet ./... && go test ./... -count=1
```
Expected: build clean, vet clean, all packages `ok`.

- [ ] **Step 7: Verify module boundaries are unchanged**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./internal/automation/`
Expected: imports are limited to `internal/core/*`, `internal/parsing`, `internal/quality`, `internal/importing`, and standard library — no `internal/indexer`, `internal/downloadclient`, `internal/media`, or `internal/naming`.

- [ ] **Step 8: Commit**

```bash
git add internal/automation/command.go internal/automation/command_test.go cmd/nexus/main.go
git commit -m "feat(automation): schedule upgrade sweep + forward upgrade.completed to WS"
```

---

## Self-Review

**Spec coverage:**
- §5.1 `quality.CutoffUnmet` → Task 1. ✓
- §5.2 `store.GrabbedSince` → Task 2. ✓
- §5.3 cooldown set + upgrade filter → Task 4; `UpgradeSweep` movies + per-episode TV + event → Task 5. ✓
- §5.4 command + 4 config fields → Task 3 (config) + Task 6 (command). ✓
- §5.5 main.go scheduler + WSForward, no new REST route → Task 6. ✓
- §3 re-grab loop guard → cooldown in Tasks 2/4/5 (test `TestUpgradeSweepSkipsRecentlyGrabbed`). ✓
- §4 RSS-upgrade boundary → design-level scope; nothing to implement. ✓
- §8 tests (a)-(g) → Task 5 tests map: (a) `GrabsUpgrade`, (b) `SkipsAtCutoffWithoutSearching`, (c) `RejectsNonUpgrade`, (d) `SkipsRecentlyGrabbed`, (e) `SkipsInFlight`, (f) `RespectsUpgradesDisabled`, (g) `TVEpisode`. ✓
- §9 acceptance criteria → covered by Tasks 1-6 + the build/vet/test/boundary gates in Task 6. ✓

**Placeholder scan:** No TBD/TODO/"handle edge cases"; every code step shows full code. ✓

**Type consistency:** `UpgradeSweep(ctx, batch)`, `CutoffUnmet(existingID, profile)`, `GrabbedSince(ctx, since)`, `buildCooldownSet(events)`, `upgradeCandidates(cands, existingQualityID, profile, itemKey, cs)`, `movieKey`/`seriesKey`, `NewUpgradeSearchCommand(svc)` are used identically across tasks. Config field names match between Task 3, Task 5 (`UpgradeSearchBatchSize`, `UpgradeGrabCooldownHours`), and Task 6 (`UpgradeSearchEnabled`, `UpgradeSearchIntervalHours`, `UpgradeSearchBatchSize`). ✓

**Note for the implementer:** `context` must be added to `internal/automation/upgrade_test.go`'s import block in Task 5 (Task 4 created it without `context`); `time` must be added to `internal/core/store/import_store_test.go`'s import block in Task 2.
