# Nexus Automation 5a — Decision Maker & Wanted/Missing Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/automation`: a pure decision maker that ranks acceptable releases, plus wanted/missing search strategies (movie + TV season-pack/episode-fallback), manual + scheduled search commands, a REST API, and composition-root wiring — the layer that chooses releases for monitored items and hands them to 4c's `importing.Enqueue`.

**Architecture:** A pure `Decide()` filters+ranks candidate releases; thin per-target strategies build a `provider.Query`, call an injected `Searcher`, run `Decide`, and enqueue the best via an injected `Enqueuer`, falling through on grab failure. Commands wrap the strategies for the worker pool; a REST sub-router dispatches them. `automation` reaches the concrete indexer/importing services only through the `Searcher`/`Enqueuer`/`Dispatcher` interfaces wired in `cmd/nexus/main.go` — the same pattern `importing` uses.

**Tech Stack:** Go (pure, `CGO_ENABLED=0`), `chi` v5 router, SQLite via `modernc.org/sqlite`, existing `internal/core/*` (store, provider, events, command, scheduler, api), `internal/parsing`, `internal/quality`, `internal/importing`.

## Global Constraints

- Go toolchain is NOT on the session PATH: prefix every Go command with `export PATH="/c/Program Files/Go/bin:$PATH"`.
- `go test -race` is unavailable (no CGO/C compiler) — verify concurrency with `-count=N` if ever needed. 5a has no new concurrency.
- Module path root: `github.com/hellboundg/nexus`.
- Dependency boundary (DIRECT imports only): `automation` may import `internal/core/*`, `internal/parsing`, `internal/quality`, `internal/importing`. It must NOT directly import `internal/indexer`, `internal/downloadclient`, `internal/media`, or `internal/naming`. (Transitive `naming` via `importing` is expected and allowed.)
- `command.Reporter` is `interface{ Progress(pct int, msg string) }`; there is no exported NopReporter — tests define a local `nopReporter`.
- Store constructor for tests: `db, _ := database.Open(t.TempDir()+"/t.db"); database.Migrate(db); st := store.New(db)`.
- All new files start with `package automation`. Keep files focused per §File Structure.
- Commit after every task with the exact message shown.

## File Structure

- `internal/automation/decide.go` — `Candidate`, `Decide()`, the comparer (pure; no I/O).
- `internal/automation/automation.go` — `Service`, `Searcher`/`Enqueuer` interfaces, `NewService`, `SearchCompleted` event, `emit`.
- `internal/automation/config.go` — `Config`, `DefaultConfig`, `Service.Config`/`SetConfig` (settings key `automation.config`).
- `internal/automation/search.go` — wanted/missing strategies (movie + TV) + `MissingSweep`.
- `internal/automation/command.go` — `command.Command` wrappers + `MissingSearchCommand`.
- `internal/automation/api.go` — `Dispatcher` interface + REST sub-router.
- `internal/parsing/parser.go` — enabling change: recognize season-pack titles (Task 4).
- `cmd/nexus/main.go` — wire the service, adapter, scheduled sweep, API mount, WS forward (Task 8).
- Tests: `decide_test.go`, `config_test.go`, `search_test.go`, `command_test.go`, `api_test.go`.

---

### Task 1: Decision maker (pure core)

**Files:**
- Create: `internal/automation/decide.go`
- Test: `internal/automation/decide_test.go`

**Interfaces:**
- Consumes: `parsing.Parse(title string, kind provider.MediaKind) parsing.ParsedRelease`; `quality.Decide(p parsing.ParsedRelease, profile store.QualityProfile) quality.Decision` (`.Accepted bool`); `quality.Compare(a, b parsing.ParsedRelease, profile store.QualityProfile) int` (+1 if a better); `provider.Release` (`.Title`, `.Protocol`, `.Seeders *int`, `.PublishDate time.Time`, `.Size int64`); `provider.ProtocolTorrent`, `provider.ProtocolUsenet`.
- Produces: `type Candidate struct { Release provider.Release; Parsed parsing.ParsedRelease }`; `func Decide(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile) []Candidate` (accepted only, ranked best-first). Later tasks call `Decide` and read `Candidate.Release`/`Candidate.Parsed`.

- [ ] **Step 1: Write the failing tests**

```go
package automation

import (
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// hdProfile allows WEBDL-1080p(7) and Bluray-1080p(9); Bluray outranks WEBDL.
func hdProfile() store.QualityProfile {
	return store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{
			{QualityID: 7, Allowed: true}, {QualityID: 9, Allowed: true},
		},
	}
}

func seedersPtr(n int) *int { return &n }

func TestDecideDropsDisallowedQuality(t *testing.T) {
	rel := provider.Release{Title: "The.Show.S01E01.720p.BluRay.x264-GRP", Protocol: provider.ProtocolUsenet}
	got := Decide([]provider.Release{rel}, provider.KindTV, hdProfile())
	if len(got) != 0 {
		t.Fatalf("Bluray-720p is not in the HD profile; want 0 candidates, got %d", len(got))
	}
}

func TestDecideRanksHigherQualityFirst(t *testing.T) {
	web := provider.Release{Title: "The.Show.S01E01.1080p.WEB-DL.x264-GRP", Protocol: provider.ProtocolUsenet}
	blu := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", Protocol: provider.ProtocolUsenet}
	got := Decide([]provider.Release{web, blu}, provider.KindTV, hdProfile())
	if len(got) != 2 {
		t.Fatalf("want 2 accepted, got %d", len(got))
	}
	if got[0].Release.Title != blu.Title {
		t.Fatalf("Bluray-1080p should rank first, got %q", got[0].Release.Title)
	}
}

func TestDecideTorrentSeedersTiebreak(t *testing.T) {
	low := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", Protocol: provider.ProtocolTorrent, Seeders: seedersPtr(3)}
	high := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-OTHER", Protocol: provider.ProtocolTorrent, Seeders: seedersPtr(50)}
	got := Decide([]provider.Release{low, high}, provider.KindTV, hdProfile())
	if len(got) != 2 || got[0].Release.Seeders == nil || *got[0].Release.Seeders != 50 {
		t.Fatalf("more seeders should rank first, got %+v", got)
	}
}

func TestDecideUsenetAgeThenSizeTiebreak(t *testing.T) {
	older := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", Protocol: provider.ProtocolUsenet, PublishDate: time.Unix(1000, 0), Size: 100}
	newer := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-NEW", Protocol: provider.ProtocolUsenet, PublishDate: time.Unix(2000, 0), Size: 100}
	got := Decide([]provider.Release{older, newer}, provider.KindTV, hdProfile())
	if len(got) != 2 || !got[0].Release.PublishDate.Equal(time.Unix(2000, 0)) {
		t.Fatalf("newer usenet should rank first, got %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestDecide -v`
Expected: FAIL — `undefined: Decide` / `undefined: Candidate`.

- [ ] **Step 3: Write the implementation**

```go
package automation

import (
	"sort"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/quality"
)

// Candidate is a release that passed the profile's accept gate, paired with its
// parsed attributes so callers can inspect season/episode/quality without
// re-parsing.
type Candidate struct {
	Release provider.Release
	Parsed  parsing.ParsedRelease
}

// Decide parses each release, drops any whose resolved quality is not allowed by
// the profile, and returns the accepted candidates ranked best-first. It performs
// no I/O and is the single ranking authority shared by every search strategy.
func Decide(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile) []Candidate {
	var out []Candidate
	for _, r := range releases {
		p := parsing.Parse(r.Title, kind)
		if !quality.Decide(p, profile).Accepted {
			continue
		}
		out = append(out, Candidate{Release: r, Parsed: p})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return compare(out[i], out[j], profile) > 0
	})
	return out
}

// compare orders two accepted candidates: +1 if a is better, -1 if b is better,
// 0 if indistinguishable. Chain (first non-zero wins): profile quality (rank +
// revision) → torrent seeders (more) → usenet age (newer) → size (larger).
// Season-pack vs single-episode selection is handled by the search strategy
// (filtering), not here, so this comparer stays context-free.
func compare(a, b Candidate, profile store.QualityProfile) int {
	if c := quality.Compare(a.Parsed, b.Parsed, profile); c != 0 {
		return c
	}
	if a.Release.Protocol == provider.ProtocolTorrent && b.Release.Protocol == provider.ProtocolTorrent {
		as, bs := seeders(a.Release), seeders(b.Release)
		if as != bs {
			if as > bs {
				return 1
			}
			return -1
		}
	}
	if a.Release.Protocol == provider.ProtocolUsenet && b.Release.Protocol == provider.ProtocolUsenet {
		if !a.Release.PublishDate.Equal(b.Release.PublishDate) {
			if a.Release.PublishDate.After(b.Release.PublishDate) {
				return 1
			}
			return -1
		}
	}
	if a.Release.Size != b.Release.Size {
		if a.Release.Size > b.Release.Size {
			return 1
		}
		return -1
	}
	return 0
}

func seeders(r provider.Release) int {
	if r.Seeders != nil {
		return *r.Seeders
	}
	return 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestDecide -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/automation/decide.go internal/automation/decide_test.go
git commit -m "feat(automation): pure release decision maker (filter + rank)"
```

---

### Task 2: Service, interfaces, and config

**Files:**
- Create: `internal/automation/automation.go`, `internal/automation/config.go`
- Test: `internal/automation/config_test.go`

**Interfaces:**
- Consumes: `store.Store` (`GetSetting(ctx, key) (string, bool, error)`, `SetSetting(ctx, key, value string) error`); `events.Bus` (`PublishAsync(ctx, events.Event)`); `importing.EnqueueRequest`, `store.QueueItem`.
- Produces:
  - `type Searcher interface { Search(ctx context.Context, q provider.Query) ([]provider.Release, error) }`
  - `type Enqueuer interface { Enqueue(ctx context.Context, req importing.EnqueueRequest) (store.QueueItem, error) }`
  - `type Service struct { ... }` with unexported fields `store *store.Store`, `search Searcher`, `enqueue Enqueuer`, `bus *events.Bus`
  - `func NewService(st *store.Store, search Searcher, enq Enqueuer, bus *events.Bus) *Service`
  - `type SearchCompleted struct { Kind string; ID int64; Grabbed int }` with `Name() string`
  - `type Config struct { MissingSearchIntervalHours int; MissingSearchBatchSize int }`
  - `func DefaultConfig() Config`; `func (s *Service) Config(ctx) (Config, error)`; `func (s *Service) SetConfig(ctx, Config) error`
  - Later tasks read `s.store`, `s.search`, `s.enqueue` and call `s.emit`.

- [ ] **Step 1: Write the failing test**

```go
package automation

import (
	"context"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return store.New(db)
}

func TestConfigDefaultsWhenAbsent(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	got, err := svc.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultConfig() {
		t.Fatalf("want defaults %+v, got %+v", DefaultConfig(), got)
	}
	if got.MissingSearchIntervalHours != 6 || got.MissingSearchBatchSize != 100 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	ctx := context.Background()
	want := Config{MissingSearchIntervalHours: 12, MissingSearchBatchSize: 25}
	if err := svc.SetConfig(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestConfig -v`
Expected: FAIL — `undefined: NewService` / `undefined: Config`.

- [ ] **Step 3: Write the implementation**

`internal/automation/automation.go`:

```go
// Package automation is Nexus's acquisition brain: it chooses releases for
// monitored library items and hands them to the import pipeline. It imports only
// internal/core/*, internal/parsing, internal/quality, and internal/importing;
// indexers are reached through the Searcher interface and the command manager
// through the Dispatcher interface, both wired at the composition root.
package automation

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
)

// Searcher runs an aggregated indexer search. Satisfied by an adapter over
// *indexer.Service that returns its releases plus a non-fatal aggregate error.
type Searcher interface {
	Search(ctx context.Context, q provider.Query) ([]provider.Release, error)
}

// Enqueuer decides+grabs a chosen release for a target item and records the
// tracking row. Satisfied by *importing.Service.
type Enqueuer interface {
	Enqueue(ctx context.Context, req importing.EnqueueRequest) (store.QueueItem, error)
}

// Service owns the search strategies and the missing-item sweep.
type Service struct {
	store   *store.Store
	search  Searcher
	enqueue Enqueuer
	bus     *events.Bus
}

func NewService(st *store.Store, search Searcher, enq Enqueuer, bus *events.Bus) *Service {
	return &Service{store: st, search: search, enqueue: enq, bus: bus}
}

func (s *Service) emit(ctx context.Context, ev events.Event) {
	if s.bus != nil {
		s.bus.PublishAsync(ctx, ev)
	}
}

// SearchCompleted is emitted when a search entrypoint finishes. ID is the target
// id of that entrypoint (movieId for movies; seriesId for series/season searches;
// episodeId for an episode search).
type SearchCompleted struct {
	Kind    string `json:"kind"` // "tv" or "movie"
	ID      int64  `json:"id"`
	Grabbed int    `json:"grabbed"`
}

func (SearchCompleted) Name() string { return "automation.search.completed" }
```

`internal/automation/config.go`:

```go
package automation

import (
	"context"
	"encoding/json"
)

const configSettingKey = "automation.config"

// Config controls the scheduled missing-item sweep. The interval is read at
// startup to register the scheduler; a change takes effect on next startup.
type Config struct {
	MissingSearchIntervalHours int `json:"missingSearchIntervalHours"`
	MissingSearchBatchSize     int `json:"missingSearchBatchSize"`
}

// DefaultConfig is applied when no config has been saved. Deliberately
// conservative because RSS sync (5b) does not exist yet.
func DefaultConfig() Config {
	return Config{MissingSearchIntervalHours: 6, MissingSearchBatchSize: 100}
}

// Config returns the persisted config, or DefaultConfig if none is saved. Any
// non-positive field is replaced with its default so a bad value can't disable
// the sweep or make it unbounded.
func (s *Service) Config(ctx context.Context) (Config, error) {
	raw, ok, err := s.store.GetSetting(ctx, configSettingKey)
	if err != nil {
		return Config{}, err
	}
	if !ok {
		return DefaultConfig(), nil
	}
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Config{}, err
	}
	d := DefaultConfig()
	if c.MissingSearchIntervalHours <= 0 {
		c.MissingSearchIntervalHours = d.MissingSearchIntervalHours
	}
	if c.MissingSearchBatchSize <= 0 {
		c.MissingSearchBatchSize = d.MissingSearchBatchSize
	}
	return c, nil
}

// SetConfig persists the sweep config.
func (s *Service) SetConfig(ctx context.Context, c Config) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, configSettingKey, string(b))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestConfig -v`
Expected: PASS (2 tests). `go build ./internal/automation/` also succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/automation/automation.go internal/automation/config.go internal/automation/config_test.go
git commit -m "feat(automation): service, searcher/enqueuer interfaces, sweep config"
```

---

### Task 3: Movie wanted/missing search

**Files:**
- Create: `internal/automation/search.go`
- Test: `internal/automation/search_test.go`

**Interfaces:**
- Consumes: `store.Store` (`GetMovie(ctx, id) (*store.Movie, error)`, `MediaFileForMovie(ctx, movieID) (*store.MediaFile, error)`, `GetQualityProfile(ctx, id) (store.QualityProfile, error)`); `store.Movie` fields `ID`, `Title`, `IMDbID string`, `TMDBID int`, `Monitored bool`, `QualityProfileID *int64`; `provider.Query{Type, Kind, Term, IMDbID, TMDBID}`, `provider.SearchMovie`, `provider.KindMovie`; `importing.EnqueueRequest`, `importing.ErrNoProfile`; `Decide` and `Candidate` from Task 1.
- Produces: `func (s *Service) SearchMovie(ctx context.Context, movieID int64) (int, error)` (returns grabbed count; emits `SearchCompleted{Kind:"movie"}`); unexported helpers `profileFor`, `enqueueBest`, `movieQuery`. Task 5 reuses `enqueueBest` and `profileFor`; Task 6 calls `SearchMovie`.

- [ ] **Step 1: Write the failing tests**

```go
package automation

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
)

type fakeSearcher struct {
	lastQuery provider.Query
	releases  []provider.Release
	err       error
}

func (f *fakeSearcher) Search(_ context.Context, q provider.Query) ([]provider.Release, error) {
	f.lastQuery = q
	return f.releases, f.err
}

type fakeEnqueuer struct {
	reqs  []importing.EnqueueRequest
	errOn func(importing.EnqueueRequest) error // optional per-request error
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, req importing.EnqueueRequest) (store.QueueItem, error) {
	f.reqs = append(f.reqs, req)
	if f.errOn != nil {
		if err := f.errOn(req); err != nil {
			return store.QueueItem{}, err
		}
	}
	return store.QueueItem{ID: int64(len(f.reqs))}, nil
}

func seedMovie(t *testing.T, st *store.Store, monitored bool, withProfile bool) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := st.CreateMovie(ctx, store.Movie{TMDBID: 42, IMDbID: "tt42", Title: "The Film", Year: 2020, Monitored: monitored})
	if err != nil {
		t.Fatal(err)
	}
	if withProfile {
		prof, err := st.CreateQualityProfile(ctx, hdProfile())
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetMovieQualityProfileID(ctx, id, &prof.ID); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestSearchMovieEnqueuesBest(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.WEB-DL.x264-GRP", DownloadURL: "u1", Protocol: provider.ProtocolUsenet, IndexerID: "nz"},
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u2", Protocol: provider.ProtocolUsenet, IndexerID: "nz"},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchMovie(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("want 1 grab, got n=%d reqs=%d", n, len(fe.reqs))
	}
	if fe.reqs[0].DownloadURL != "u2" {
		t.Fatalf("Bluray should be chosen, got %q", fe.reqs[0].DownloadURL)
	}
	if fe.reqs[0].MediaKind != provider.KindMovie || fe.reqs[0].MovieID != id {
		t.Fatalf("bad enqueue request: %+v", fe.reqs[0])
	}
	if fs.lastQuery.Type != provider.SearchMovie || fs.lastQuery.IMDbID != "tt42" || fs.lastQuery.TMDBID != 42 {
		t.Fatalf("bad query: %+v", fs.lastQuery)
	}
}

func TestSearchMovieSkipsWhenNotMonitored(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, false, true)
	fs := &fakeSearcher{}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchMovie(context.Background(), id)
	if err != nil || n != 0 {
		t.Fatalf("unmonitored movie must not search; n=%d err=%v", n, err)
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("no search should have run, got query %+v", fs.lastQuery)
	}
}

func TestSearchMovieFallsThroughOnGrabFailure(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-BEST", DownloadURL: "best", Protocol: provider.ProtocolUsenet},
		{Title: "The.Film.2020.1080p.WEB-DL.x264-NEXT", DownloadURL: "next", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{errOn: func(r importing.EnqueueRequest) error {
		if r.DownloadURL == "best" {
			return errors.New("grab boom")
		}
		return nil
	}}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchMovie(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 2 || fe.reqs[1].DownloadURL != "next" {
		t.Fatalf("should fall through to next candidate: n=%d reqs=%+v", n, fe.reqs)
	}
}

func TestSearchMovieNoProfileStops(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, false) // monitored, but no quality profile
	fs := &fakeSearcher{}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchMovie(context.Background(), id)
	if err != nil || n != 0 {
		t.Fatalf("no-profile movie should skip cleanly; n=%d err=%v", n, err)
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("no search without a profile, got %+v", fs.lastQuery)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestSearchMovie -v`
Expected: FAIL — `svc.SearchMovie undefined`.

- [ ] **Step 3: Write the implementation**

`internal/automation/search.go`:

```go
package automation

import (
	"context"
	"errors"
	"log/slog"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
)

// SearchMovie searches for a monitored, file-less movie and enqueues the best
// acceptable release. Returns the number grabbed (0 or 1). Skips silently when
// the movie is unmonitored, already has a file, or has no quality profile.
func (s *Service) SearchMovie(ctx context.Context, movieID int64) (int, error) {
	n, err := s.searchMovie(ctx, movieID)
	s.emit(ctx, SearchCompleted{Kind: "movie", ID: movieID, Grabbed: n})
	return n, err
}

func (s *Service) searchMovie(ctx context.Context, movieID int64) (int, error) {
	m, err := s.store.GetMovie(ctx, movieID)
	if err != nil {
		return 0, err
	}
	if !m.Monitored {
		return 0, nil
	}
	if f, err := s.store.MediaFileForMovie(ctx, m.ID); err != nil {
		return 0, err
	} else if f != nil {
		return 0, nil // already have a file
	}
	profile, ok, err := s.profileFor(ctx, m.QualityProfileID)
	if err != nil || !ok {
		return 0, err
	}
	releases, err := s.search.Search(ctx, movieQuery(m))
	if err != nil {
		slog.Warn("automation: movie search had indexer errors", "movieId", m.ID, "err", err)
	}
	cands := Decide(releases, provider.KindMovie, profile)
	grabbed, err := s.enqueueBest(ctx, cands, func(c Candidate) importing.EnqueueRequest {
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

func movieQuery(m *store.Movie) provider.Query {
	q := provider.Query{Type: provider.SearchMovie, Kind: provider.KindMovie, Term: m.Title}
	if m.IMDbID != "" {
		q.IMDbID = m.IMDbID
	}
	if m.TMDBID != 0 {
		q.TMDBID = m.TMDBID
	}
	return q
}

// profileFor loads the assigned quality profile. ok=false (no error) means the
// item has no profile assigned and cannot be decided.
func (s *Service) profileFor(ctx context.Context, profileID *int64) (store.QualityProfile, bool, error) {
	if profileID == nil {
		return store.QualityProfile{}, false, nil
	}
	p, err := s.store.GetQualityProfile(ctx, *profileID)
	if err != nil {
		return store.QualityProfile{}, false, err
	}
	return p, true, nil
}

// enqueueBest tries candidates best-first, returning true once one is grabbed.
// A grab failure (network/reject) falls through to the next candidate;
// importing.ErrNoProfile is terminal for the item (nothing more to try).
func (s *Service) enqueueBest(ctx context.Context, cands []Candidate, req func(Candidate) importing.EnqueueRequest) (bool, error) {
	for _, c := range cands {
		if _, err := s.enqueue.Enqueue(ctx, req(c)); err == nil {
			return true, nil
		} else if errors.Is(err, importing.ErrNoProfile) {
			return false, nil
		} else {
			slog.Warn("automation: enqueue failed, trying next candidate", "title", c.Release.Title, "err", err)
		}
	}
	return false, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestSearchMovie -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/automation/search.go internal/automation/search_test.go
git commit -m "feat(automation): movie wanted/missing search with fall-through"
```

---

### Task 4: Parser season-pack recognition (enabling change to 4b)

**Files:**
- Modify: `internal/parsing/parser.go`
- Test: `internal/parsing/parser_test.go` (add tests; create if absent)

**Why:** The 4b parser only sets `Season` when at least one episode is present
(`reSeasonEp` requires `SxxExx`). A bare season-pack title (`Sxx`, `Season xx`)
parses to `Season==0`, which would break Task 5's season-pack filter
(`Parsed.Season==n && len(Parsed.Episodes)==0`). This task adds an additive
season-pack branch: when no `SxxExx` match is found for a TV title, recognize a
season-only pattern and set `Season` with `Episodes` empty. Existing `SxxExx`
behavior is unchanged.

**Interfaces:**
- Consumes: existing `Parse(title string, kind provider.MediaKind) parsing.ParsedRelease`, `reSeasonEp`.
- Produces: `Parse` now returns `Season>0, len(Episodes)==0` for season-pack titles (TV kind). Task 5's pack filter relies on this.

- [ ] **Step 1: Write the failing tests**

```go
// add to internal/parsing/parser_test.go
package parsing

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestParseSeasonPack(t *testing.T) {
	cases := []struct {
		title  string
		season int
	}{
		{"The.Show.S01.1080p.BluRay.x264-GRP", 1},
		{"The.Show.Season.2.1080p.WEB-DL-GRP", 2},
		{"The.Show.S03.COMPLETE.1080p.BluRay-GRP", 3},
	}
	for _, c := range cases {
		p := Parse(c.title, provider.KindTV)
		if p.Season != c.season || len(p.Episodes) != 0 {
			t.Fatalf("%q: want season=%d episodes=[], got season=%d episodes=%v",
				c.title, c.season, p.Season, p.Episodes)
		}
	}
}

func TestParseSeasonPackDoesNotBreakEpisodes(t *testing.T) {
	p := Parse("The.Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV)
	if p.Season != 1 || len(p.Episodes) != 1 || p.Episodes[0] != 1 {
		t.Fatalf("SxxExx must be unchanged, got season=%d episodes=%v", p.Season, p.Episodes)
	}
}

func TestParseNoSeasonStaysZero(t *testing.T) {
	p := Parse("Some.Movie.2020.1080p.BluRay.x264-GRP", provider.KindTV)
	if p.Season != 0 || len(p.Episodes) != 0 {
		t.Fatalf("non-season title should stay season=0, got season=%d episodes=%v", p.Season, p.Episodes)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/parsing/ -run TestParseSeason -v`
Expected: FAIL — `TestParseSeasonPack` fails (season=0 for `S01`).

- [ ] **Step 3: Add the season-pack regex and branch**

In `internal/parsing/parser.go`, add to the `var (...)` regex block (next to `reSeasonEp`):

```go
	reSeasonPack = regexp.MustCompile(`(?i)\bS(?:eason)?[ ._-]?(\d{1,2})\b`)
```

Then in the `if kind == provider.KindTV {` block, extend the season match so the
pack pattern is a fallback when no episode match is found:

```go
	if kind == provider.KindTV {
		if m := reSeasonEp.FindStringSubmatch(title); m != nil {
			p.Season = atoi(m[1])
			for _, em := range reEpNums.FindAllStringSubmatch(m[2], -1) {
				p.Episodes = append(p.Episodes, atoi(em[1]))
			}
			if m[3] != "" { // range end, e.g. E10-E12
				end := atoi(m[3])
				for e := p.Episodes[len(p.Episodes)-1] + 1; e <= end; e++ {
					p.Episodes = append(p.Episodes, e)
				}
			}
		} else if m := reSeasonPack.FindStringSubmatch(title); m != nil {
			// Season pack: a season is named but no episode → whole-season release.
			p.Season = atoi(m[1])
		}
	} else {
```

(Leave the rest of the `else` movie branch exactly as-is.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/parsing/ -run TestParse -v`
Expected: PASS (new tests + existing parser tests all green).

- [ ] **Step 5: Run the full suite to confirm no 4b/4c regression**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./...`
Expected: all packages PASS (the parser change is additive; `SxxExx` behavior is unchanged, so `internal/quality` and `internal/importing` stay green).

- [ ] **Step 6: Commit**

```bash
git add internal/parsing/parser.go internal/parsing/parser_test.go
git commit -m "feat(parsing): recognize season-pack titles (Sxx / Season xx) for automation"
```

---

### Task 5: TV wanted/missing search (season-pack + episode fallback)

**Files:**
- Modify: `internal/automation/search.go`
- Test: `internal/automation/search_test.go` (add tests)

**Interfaces:**
- Consumes: `store.Store` (`GetSeries(ctx, id) (*store.Series, error)`, `ListSeasons(ctx, seriesID) ([]store.Season, error)`, `ListEpisodes(ctx, seriesID) ([]store.Episode, error)`, `MediaFileForEpisode(ctx, episodeID) (*store.MediaFile, error)`, `GetEpisode(ctx, id) (*store.Episode, error)`); `store.Series` (`ID`, `Title`, `TMDBID`, `Monitored`, `QualityProfileID`); `store.Season` (`SeasonNumber`, `Monitored`); `store.Episode` (`ID`, `SeriesID`, `SeasonNumber`, `EpisodeNumber`, `Monitored`); `provider.SearchTV`, `provider.KindTV`, `provider.Query{Season *int, Episode *int}`; `Decide`, `Candidate`, `enqueueBest`, `profileFor` from earlier tasks.
- Produces: `func (s *Service) SearchSeries(ctx, seriesID int64) (int, error)`; `func (s *Service) SearchSeason(ctx, seriesID int64, seasonNumber int) (int, error)`; `func (s *Service) SearchEpisode(ctx, episodeID int64) (int, error)` (each emits `SearchCompleted{Kind:"tv"}`). Task 6 calls `SearchSeries`.

- [ ] **Step 1: Write the failing tests**

```go
// append to search_test.go

func seedSeries(t *testing.T, st *store.Store, monitored bool, epCount int) (seriesID int64, epIDs []int64) {
	t.Helper()
	ctx := context.Background()
	prof, err := st.CreateQualityProfile(ctx, hdProfile())
	if err != nil {
		t.Fatal(err)
	}
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 7, Title: "The Show", Monitored: monitored})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesQualityProfileID(ctx, sid, &prof.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSeason(ctx, store.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= epCount; i++ {
		if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: i, Monitored: true}); err != nil {
			t.Fatal(err)
		}
	}
	eps, _ := st.ListEpisodes(ctx, sid)
	for _, e := range eps {
		epIDs = append(epIDs, e.ID)
	}
	return sid, epIDs
}

func TestSearchSeasonFullyMissingPrefersPack(t *testing.T) {
	st := newStore(t)
	sid, epIDs := seedSeries(t, st, true, 3)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "single", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(context.Background(), sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "pack" {
		t.Fatalf("fully-missing season should grab the pack once: n=%d reqs=%+v", n, fe.reqs)
	}
	if len(fe.reqs[0].EpisodeIDs) != 3 {
		t.Fatalf("pack should carry all 3 missing episode ids, got %v", fe.reqs[0].EpisodeIDs)
	}
	if fs.lastQuery.Season == nil || *fs.lastQuery.Season != 1 || fs.lastQuery.Episode != nil {
		t.Fatalf("pack search query should set season and not episode: %+v", fs.lastQuery)
	}
	_ = epIDs
}

func TestSearchSeasonNoPackFallsBackToEpisodes(t *testing.T) {
	st := newStore(t)
	sid, _ := seedSeries(t, st, true, 2)
	// Searcher returns only per-episode singles regardless of query (no pack).
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01E02.1080p.BluRay.x264-GRP", DownloadURL: "e2", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(context.Background(), sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(fe.reqs) != 2 {
		t.Fatalf("no acceptable pack should fall back to 2 per-episode grabs: n=%d reqs=%d", n, len(fe.reqs))
	}
	for _, r := range fe.reqs {
		if len(r.EpisodeIDs) != 1 {
			t.Fatalf("per-episode grab should carry exactly one episode id, got %v", r.EpisodeIDs)
		}
	}
}

func TestSearchSeasonPartiallyMissingSearchesEpisodesOnly(t *testing.T) {
	st := newStore(t)
	sid, epIDs := seedSeries(t, st, true, 2)
	// Give episode 1 a media file so the season is only partially missing.
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{MediaKind: "tv", EpisodeID: &epIDs[0], RelativePath: "e1.mkv", QualityID: 9}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E02.1080p.BluRay.x264-GRP", DownloadURL: "e2", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(context.Background(), sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].EpisodeIDs[0] != epIDs[1] {
		t.Fatalf("only the missing episode 2 should be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
	if fs.lastQuery.Episode == nil {
		t.Fatalf("partially-missing season must use per-episode queries: %+v", fs.lastQuery)
	}
}

func TestSearchEpisodeSingle(t *testing.T) {
	st := newStore(t)
	sid, epIDs := seedSeries(t, st, true, 2)
	_ = sid
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchEpisode(context.Background(), epIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].EpisodeIDs[0] != epIDs[0] {
		t.Fatalf("episode search should grab that one episode: n=%d reqs=%+v", n, fe.reqs)
	}
}

func TestSearchEpisodeSkipsWhenFiled(t *testing.T) {
	st := newStore(t)
	_, epIDs := seedSeries(t, st, true, 1)
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{MediaKind: "tv", EpisodeID: &epIDs[0], RelativePath: "e1.mkv", QualityID: 9}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchEpisode(context.Background(), epIDs[0])
	if err != nil || n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("already-filed episode must be skipped: n=%d err=%v reqs=%d", n, err, len(fe.reqs))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestSearchSeason -v`
Expected: FAIL — `svc.SearchSeason undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/automation/search.go`:

```go
// SearchSeries searches every monitored season of a monitored series for its
// missing episodes. Returns the total grabbed.
func (s *Service) SearchSeries(ctx context.Context, seriesID int64) (int, error) {
	n, err := s.searchSeries(ctx, seriesID)
	s.emit(ctx, SearchCompleted{Kind: "tv", ID: seriesID, Grabbed: n})
	return n, err
}

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
	total := 0
	for _, sn := range seasons {
		if !sn.Monitored {
			continue
		}
		n, err := s.searchSeason(ctx, se, sn.SeasonNumber, eps, profile)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// SearchSeason searches one season of a series.
func (s *Service) SearchSeason(ctx context.Context, seriesID int64, seasonNumber int) (int, error) {
	n, err := s.searchSeasonEntry(ctx, seriesID, seasonNumber)
	s.emit(ctx, SearchCompleted{Kind: "tv", ID: seriesID, Grabbed: n})
	return n, err
}

func (s *Service) searchSeasonEntry(ctx context.Context, seriesID int64, seasonNumber int) (int, error) {
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
	eps, err := s.store.ListEpisodes(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	return s.searchSeason(ctx, se, seasonNumber, eps, profile)
}

// searchSeason searches a single season. If every monitored episode in the
// season is missing, it tries a full-season pack first (enqueued with all
// missing episode ids); otherwise, or if no acceptable pack is found, it falls
// back to per-episode searches. eps is the full episode list for the series.
func (s *Service) searchSeason(ctx context.Context, se *store.Series, seasonNumber int, eps []store.Episode, profile store.QualityProfile) (int, error) {
	var monitored, missing []store.Episode
	for _, e := range eps {
		if e.SeasonNumber != seasonNumber || !e.Monitored {
			continue
		}
		monitored = append(monitored, e)
		f, err := s.store.MediaFileForEpisode(ctx, e.ID)
		if err != nil {
			return 0, err
		}
		if f == nil {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}
	// Fully missing → try a season pack first.
	if len(missing) == len(monitored) {
		releases, err := s.search.Search(ctx, tvQuery(se, seasonNumber, nil))
		if err != nil {
			slog.Warn("automation: season search had indexer errors", "seriesId", se.ID, "season", seasonNumber, "err", err)
		}
		var packs []Candidate
		for _, c := range Decide(releases, provider.KindTV, profile) {
			if c.Parsed.Season == seasonNumber && len(c.Parsed.Episodes) == 0 {
				packs = append(packs, c)
			}
		}
		ids := episodeIDs(missing)
		grabbed, err := s.enqueueBest(ctx, packs, func(c Candidate) importing.EnqueueRequest {
			return tvRequest(se.ID, ids, c)
		})
		if err != nil {
			return 0, err
		}
		if grabbed {
			return 1, nil
		}
		// no acceptable pack → fall through to per-episode
	}
	total := 0
	for _, e := range missing {
		n, err := s.searchEpisode(ctx, se, e, profile)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// SearchEpisode searches for one missing, monitored episode.
func (s *Service) SearchEpisode(ctx context.Context, episodeID int64) (int, error) {
	n, err := s.searchEpisodeEntry(ctx, episodeID)
	s.emit(ctx, SearchCompleted{Kind: "tv", ID: episodeID, Grabbed: n})
	return n, err
}

func (s *Service) searchEpisodeEntry(ctx context.Context, episodeID int64) (int, error) {
	e, err := s.store.GetEpisode(ctx, episodeID)
	if err != nil {
		return 0, err
	}
	se, err := s.store.GetSeries(ctx, e.SeriesID)
	if err != nil {
		return 0, err
	}
	if !se.Monitored || !e.Monitored {
		return 0, nil
	}
	profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
	if err != nil || !ok {
		return 0, err
	}
	return s.searchEpisode(ctx, se, *e, profile)
}

// searchEpisode searches one episode and enqueues the best covering release.
// Skips if the episode already has a file.
func (s *Service) searchEpisode(ctx context.Context, se *store.Series, e store.Episode, profile store.QualityProfile) (int, error) {
	f, err := s.store.MediaFileForEpisode(ctx, e.ID)
	if err != nil {
		return 0, err
	}
	if f != nil {
		return 0, nil
	}
	ep := e.EpisodeNumber
	releases, err := s.search.Search(ctx, tvQuery(se, e.SeasonNumber, &ep))
	if err != nil {
		slog.Warn("automation: episode search had indexer errors", "episodeId", e.ID, "err", err)
	}
	var covering []Candidate
	for _, c := range Decide(releases, provider.KindTV, profile) {
		if c.Parsed.Season == e.SeasonNumber && containsInt(c.Parsed.Episodes, e.EpisodeNumber) {
			covering = append(covering, c)
		}
	}
	grabbed, err := s.enqueueBest(ctx, covering, func(c Candidate) importing.EnqueueRequest {
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

func tvQuery(se *store.Series, season int, episode *int) provider.Query {
	q := provider.Query{Type: provider.SearchTV, Kind: provider.KindTV, Term: se.Title, Season: &season}
	if se.TMDBID != 0 {
		q.TMDBID = se.TMDBID
	}
	if episode != nil {
		q.Episode = episode
	}
	return q
}

func tvRequest(seriesID int64, episodeIDs []int64, c Candidate) importing.EnqueueRequest {
	return importing.EnqueueRequest{
		DownloadURL: c.Release.DownloadURL, Title: c.Release.Title,
		Protocol: c.Release.Protocol, IndexerID: c.Release.IndexerID,
		MediaKind: provider.KindTV, SeriesID: seriesID, EpisodeIDs: episodeIDs,
	}
}

func episodeIDs(eps []store.Episode) []int64 {
	out := make([]int64, len(eps))
	for i, e := range eps {
		out[i] = e.ID
	}
	return out
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run 'TestSearch' -v`
Expected: PASS (all movie + TV search tests).

- [ ] **Step 5: Commit**

```bash
git add internal/automation/search.go internal/automation/search_test.go
git commit -m "feat(automation): TV wanted/missing search (season-pack + episode fallback)"
```

---

### Task 6: Search commands + missing sweep

**Files:**
- Create: `internal/automation/command.go`
- Test: `internal/automation/command_test.go`

**Interfaces:**
- Consumes: `command.Command` (`Name() string`, `Run(ctx, command.Reporter) error`), `command.Reporter`; `store.Store` (`ListMovies(ctx) ([]store.Movie, error)`, `ListSeries(ctx) ([]store.Series, error)`, `MediaFileForMovie`); `Service.SearchMovie`/`SearchSeries`/`SearchSeason`/`SearchEpisode`/`Config`.
- Produces: `func NewSearchMovieCommand(svc *Service, movieID int64) command.Command` (and `...Series`/`...Season(svc, seriesID, seasonNumber)`/`...Episode(svc, episodeID)`); `func NewMissingSearchCommand(svc *Service) command.Command`; `func (s *Service) MissingSweep(ctx, batch int) (int, error)`. Task 7 constructs these; Task 8 schedules `NewMissingSearchCommand`.

- [ ] **Step 1: Write the failing tests**

```go
package automation

import (
	"context"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestSearchMovieCommandRuns(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	cmd := NewSearchMovieCommand(svc, id)
	if cmd.Name() != "SearchMovie" {
		t.Fatalf("bad name %q", cmd.Name())
	}
	if err := cmd.Run(context.Background(), nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if len(fe.reqs) != 1 {
		t.Fatalf("command should have grabbed one, got %d", len(fe.reqs))
	}
}

func TestMissingSweepRespectsBatchAndSkipsFiled(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	// Two monitored missing movies + one monitored movie that already has a file.
	seedMovie(t, st, true, true)
	seedMovie(t, st, true, true)
	filed := seedMovie(t, st, true, true)
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &filed, RelativePath: "m.mkv", QualityID: 9}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.MissingSweep(ctx, 1) // batch of 1 → only the first missing movie
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("batch=1 should process exactly one target: n=%d reqs=%d", n, len(fe.reqs))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run 'TestSearchMovieCommand|TestMissingSweep' -v`
Expected: FAIL — `NewSearchMovieCommand undefined` / `svc.MissingSweep undefined`.

- [ ] **Step 3: Write the implementation**

`internal/automation/command.go`:

```go
package automation

import (
	"context"
	"fmt"

	"github.com/hellboundg/nexus/internal/core/command"
)

type searchCommand struct {
	name string
	run  func(ctx context.Context) (int, error)
}

func (c *searchCommand) Name() string { return c.name }

func (c *searchCommand) Run(ctx context.Context, r command.Reporter) error {
	r.Progress(0, "searching")
	n, err := c.run(ctx)
	if err != nil {
		return err
	}
	r.Progress(100, fmt.Sprintf("%d grabbed", n))
	return nil
}

func NewSearchMovieCommand(svc *Service, movieID int64) command.Command {
	return &searchCommand{name: "SearchMovie", run: func(ctx context.Context) (int, error) {
		return svc.SearchMovie(ctx, movieID)
	}}
}

func NewSearchSeriesCommand(svc *Service, seriesID int64) command.Command {
	return &searchCommand{name: "SearchSeries", run: func(ctx context.Context) (int, error) {
		return svc.SearchSeries(ctx, seriesID)
	}}
}

func NewSearchSeasonCommand(svc *Service, seriesID int64, seasonNumber int) command.Command {
	return &searchCommand{name: "SearchSeason", run: func(ctx context.Context) (int, error) {
		return svc.SearchSeason(ctx, seriesID, seasonNumber)
	}}
}

func NewSearchEpisodeCommand(svc *Service, episodeID int64) command.Command {
	return &searchCommand{name: "SearchEpisode", run: func(ctx context.Context) (int, error) {
		return svc.SearchEpisode(ctx, episodeID)
	}}
}

// NewMissingSearchCommand is the scheduled sweep over monitored-missing items.
func NewMissingSearchCommand(svc *Service) command.Command {
	return &searchCommand{name: "MissingSearch", run: func(ctx context.Context) (int, error) {
		cfg, err := svc.Config(ctx)
		if err != nil {
			return 0, err
		}
		return svc.MissingSweep(ctx, cfg.MissingSearchBatchSize)
	}}
}
```

Append to `internal/automation/search.go`:

```go
// MissingSweep processes up to batch monitored targets that are missing files:
// first monitored movies without a file, then monitored series (each of which
// may fan out to several episode searches). Returns the total grabbed. A per-
// target error is not fatal to the sweep — it is logged and the sweep continues.
func (s *Service) MissingSweep(ctx context.Context, batch int) (int, error) {
	if batch <= 0 {
		batch = DefaultConfig().MissingSearchBatchSize
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
		if f != nil {
			continue
		}
		processed++
		n, err := s.SearchMovie(ctx, m.ID)
		if err != nil {
			slog.Warn("automation: sweep movie search failed", "movieId", m.ID, "err", err)
			continue
		}
		total += n
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
		processed++
		n, err := s.SearchSeries(ctx, se.ID)
		if err != nil {
			slog.Warn("automation: sweep series search failed", "seriesId", se.ID, "err", err)
			continue
		}
		total += n
	}
	return total, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run 'TestSearchMovieCommand|TestMissingSweep' -v`
Expected: PASS (2 tests). Full package: `go test ./internal/automation/ -v` all green.

- [ ] **Step 5: Commit**

```bash
git add internal/automation/command.go internal/automation/search.go internal/automation/command_test.go
git commit -m "feat(automation): search commands + scheduled missing sweep"
```

---

### Task 7: REST API

**Files:**
- Create: `internal/automation/api.go`
- Test: `internal/automation/api_test.go`

**Interfaces:**
- Consumes: `chi.Router`; `api.WriteJSON(w, status, v)`, `api.WriteError(w, status, code, msg)`; `command.Command`, `command.Manager` (via `Dispatcher`); `store.Store` (`GetMovie`, `GetSeries`, `GetEpisode`, `ErrNotFound`); `Service.Config`/`SetConfig`; command constructors from Task 5.
- Produces: `type Dispatcher interface { Enqueue(command.Command) (string, error) }`; `type API struct{...}`; `func NewAPI(svc *Service, d Dispatcher) *API`; `func (a *API) Mount(r chi.Router)`. Task 8 passes `mgr` (a `*command.Manager`) as the `Dispatcher` and `autoAPI.Mount` to the router.

- [ ] **Step 1: Write the failing test**

```go
package automation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/store"
)

type fakeDispatcher struct {
	last command.Command
}

func (f *fakeDispatcher) Enqueue(c command.Command) (string, error) {
	f.last = c
	return "task-1", nil
}

func newTestAPI(t *testing.T) (http.Handler, *store.Store, *fakeDispatcher) {
	t.Helper()
	st := newStore(t)
	svc := NewService(st, &fakeSearcher{}, &fakeEnqueuer{}, nil)
	fd := &fakeDispatcher{}
	r := chi.NewRouter()
	NewAPI(svc, fd).Mount(r)
	return r, st, fd
}

func TestAPISearchMovieDispatches(t *testing.T) {
	r, st, fd := newTestAPI(t)
	id := seedMovie(t, st, true, true)
	req := httptest.NewRequest(http.MethodPost, "/automation/search/movie/"+itoa(id), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d (%s)", w.Code, w.Body.String())
	}
	if fd.last == nil || fd.last.Name() != "SearchMovie" {
		t.Fatalf("expected SearchMovie dispatched, got %v", fd.last)
	}
	var body map[string]string
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["taskId"] != "task-1" {
		t.Fatalf("want taskId in body, got %v", body)
	}
}

func TestAPISearchMovieUnknownIs404(t *testing.T) {
	r, _, fd := newTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/automation/search/movie/999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown movie, got %d", w.Code)
	}
	if fd.last != nil {
		t.Fatalf("nothing should be dispatched for a bad id")
	}
}

func TestAPIConfigRoundTrip(t *testing.T) {
	r, _, _ := newTestAPI(t)
	put := httptest.NewRequest(http.MethodPut, "/automation/config",
		strings.NewReader(`{"missingSearchIntervalHours":8,"missingSearchBatchSize":10}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, put)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT config want 200, got %d", w.Code)
	}
	get := httptest.NewRequest(http.MethodGet, "/automation/config", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, get)
	var c Config
	if err := json.NewDecoder(w2.Body).Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.MissingSearchIntervalHours != 8 || c.MissingSearchBatchSize != 10 {
		t.Fatalf("config not persisted: %+v", c)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
```

> Note: add `"strconv"` to the test imports (used by `itoa`).

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestAPI -v`
Expected: FAIL — `NewAPI undefined` / `Dispatcher undefined`.

- [ ] **Step 3: Write the implementation**

`internal/automation/api.go`:

```go
package automation

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/store"
)

// Dispatcher enqueues a command onto the worker pool, returning its task id.
// Satisfied by *command.Manager.
type Dispatcher interface {
	Enqueue(command.Command) (string, error)
}

type API struct {
	svc      *Service
	dispatch Dispatcher
}

func NewAPI(svc *Service, d Dispatcher) *API { return &API{svc: svc, dispatch: d} }

func (a *API) Mount(r chi.Router) {
	r.Route("/automation", func(r chi.Router) {
		r.Post("/search/movie/{id}", a.searchMovie)
		r.Post("/search/series/{id}", a.searchSeries)
		r.Post("/search/series/{id}/season/{n}", a.searchSeason)
		r.Post("/search/episode/{id}", a.searchEpisode)
		r.Route("/config", func(r chi.Router) {
			r.Get("/", a.getConfig)
			r.Put("/", a.putConfig)
		})
	})
}

func pathInt64(r *http.Request, key string) (int64, bool) {
	v, err := strconv.ParseInt(chi.URLParam(r, key), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// dispatch validates the target exists (404 if not) then enqueues the command
// and writes 202 with the task id.
func (a *API) accept(w http.ResponseWriter, exists error, cmd command.Command) {
	if errors.Is(exists, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if exists != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	id, err := a.dispatch.Enqueue(cmd)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to dispatch")
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]string{"taskId": id})
}

func (a *API) searchMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	_, err := a.svc.store.GetMovie(r.Context(), id)
	a.accept(w, err, NewSearchMovieCommand(a.svc, id))
}

func (a *API) searchSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	_, err := a.svc.store.GetSeries(r.Context(), id)
	a.accept(w, err, NewSearchSeriesCommand(a.svc, id))
}

func (a *API) searchSeason(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	n, okN := pathInt64(r, "n")
	if !ok || !okN {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id or season")
		return
	}
	_, err := a.svc.store.GetSeries(r.Context(), id)
	a.accept(w, err, NewSearchSeasonCommand(a.svc, id, int(n)))
}

func (a *API) searchEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	_, err := a.svc.store.GetEpisode(r.Context(), id)
	a.accept(w, err, NewSearchEpisodeCommand(a.svc, id))
}

func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	c, err := a.svc.Config(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load config")
		return
	}
	api.WriteJSON(w, http.StatusOK, c)
}

func (a *API) putConfig(w http.ResponseWriter, r *http.Request) {
	var c Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.svc.SetConfig(r.Context(), c); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to save config")
		return
	}
	api.WriteJSON(w, http.StatusOK, c)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestAPI -v`
Expected: PASS (4 tests). Then `go vet ./internal/automation/` clean.

- [ ] **Step 5: Commit**

```bash
git add internal/automation/api.go internal/automation/api_test.go
git commit -m "feat(automation): REST API dispatching search commands + config"
```

---

### Task 8: Composition-root wiring + boundary verification

**Files:**
- Modify: `cmd/nexus/main.go`

**Interfaces:**
- Consumes: `indexer.Service.Search(ctx, provider.Query) indexer.SearchResult` (`.Releases`, `.IndexerErrors`); `automation.NewService`, `automation.NewAPI`, `automation.NewMissingSearchCommand`, `automation.Service.Config`; `importSvc` (`*importing.Service`, satisfies `automation.Enqueuer`); `mgr` (`*command.Manager`, satisfies `automation.Dispatcher`); `scheduler.Every`.
- Produces: a running, wired automation subsystem — scheduled sweep + mounted API + WS forwarding of `automation.search.completed`.

- [ ] **Step 1: Add the automation import**

In the import block of `cmd/nexus/main.go`, add (keep grouping/order consistent):

```go
	"github.com/hellboundg/nexus/internal/automation"
```

Also ensure `"fmt"` is imported (used by the adapter below).

- [ ] **Step 2: Add the Searcher adapter at the bottom of main.go**

Append near `dlQueueAdapter`:

```go
// autoSearchAdapter flattens indexer.Service.Search's SearchResult into the
// ([]provider.Release, error) shape automation.Searcher expects, without
// importing the indexer package into internal/automation. Per-indexer errors are
// surfaced as a non-fatal aggregate error; the releases that succeeded are still
// returned.
type autoSearchAdapter struct{ svc *indexer.Service }

func (a autoSearchAdapter) Search(ctx context.Context, q provider.Query) ([]provider.Release, error) {
	res := a.svc.Search(ctx, q)
	if len(res.IndexerErrors) > 0 {
		return res.Releases, fmt.Errorf("automation: %d indexer error(s)", len(res.IndexerErrors))
	}
	return res.Releases, nil
}
```

- [ ] **Step 3: Wire the service, scheduled sweep, and API**

After the `importCmd := importing.NewImportCommand(importSvc)` line (around line 105), add:

```go
	autoSvc := automation.NewService(st, autoSearchAdapter{svc: idxSvc}, importSvc, bus)
	autoAPI := automation.NewAPI(autoSvc, mgr)
	autoCfg, err := autoSvc.Config(ctx)
	if err != nil {
		return err
	}
```

In the scheduler block (after the `sch.Every(1*time.Minute, ... importCmd ...)` line), add:

```go
	sch.Every(time.Duration(autoCfg.MissingSearchIntervalHours)*time.Hour, func() command.Command {
		return automation.NewMissingSearchCommand(autoSvc)
	})
```

In the `api.NewRouter(...)` call, add `"automation.search.completed"` to the `WSForward` slice and append `autoAPI.Mount` to the variadic mounts:

```go
	router := api.NewRouter(api.Deps{
		Auth: authSvc, Store: st, Version: version.Version(), Bus: bus,
		WSForward: []string{"indexer.status", "download.status", "media.series.updated", "media.movie.updated", "import.completed", "queue.updated", "automation.search.completed"},
	}, web.Handler(), idxAPI.Mount, dlAPI.Mount, mediaAPI.Mount, qualityAPI.Mount, importAPI.Mount, autoAPI.Mount)
```

- [ ] **Step 4: Build, vet, and full test suite**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...
```
Expected: build + vet clean; all packages PASS (now 21 packages including `internal/automation`).

- [ ] **Step 5: Verify module boundary (direct imports)**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
go list -f '{{ join .Imports "\n" }}' ./internal/automation | grep -E 'internal/(indexer|downloadclient|media|naming)$' || echo "BOUNDARY OK"
```
Expected: prints `BOUNDARY OK` (no direct import of indexer/downloadclient/media/naming). If any line prints instead, a forbidden direct import slipped in — fix it before committing.

- [ ] **Step 6: Commit**

```bash
git add cmd/nexus/main.go
git commit -m "feat(automation): wire decision maker + wanted/missing search into composition root"
```

---

## Self-Review (completed during authoring)

- **Spec coverage:** §3.1 interfaces → Task 2/8; §3.2 decision maker → Task 1; §3.2 parser season-pack enabling change → Task 4; §3.3 movie/TV strategies → Tasks 3, 5; §3.4 commands + sweep → Task 6; §3.5 config → Task 2; §3.6 REST API (202, 404 pre-validation, config) → Task 7; §3.7 events → Tasks 2/8 (`SearchCompleted` + WSForward); §8 acceptance criteria 1–8 → Tasks 1–8. No gaps.
- **Placeholder scan:** no TBD/TODO; every code step shows complete code.
- **Type consistency:** `Candidate`/`Decide` (T1) reused unchanged in T3/T5; parser season-pack (T4) sets `Season>0, len(Episodes)==0` that T5's pack filter reads; `Searcher`/`Enqueuer` (T2) implemented by adapters in T8; `enqueueBest`/`profileFor` defined in T3, reused in T5; `Dispatcher` (T7) satisfied by `*command.Manager` in T8; `SearchCompleted.Name()` matches the WSForward topic string `automation.search.completed`.

## Notes for the implementer

- `store.MediaFile` has `EpisodeID *int64` and `MovieID *int64` — the test fixtures set exactly one. Confirm field names against `internal/core/store/import_store.go` if the compiler complains.
- `parsing.Parse` derives season/episode from the title; the test titles (`S01E01`, `S01`) are chosen to parse deterministically. Bare season-pack titles (`S01`) only parse to `Season=1, len(Episodes)==0` **after Task 4** — do Task 4 before Task 5, or the season-pack tests will fail on `Season==0`.
- Keep `slog` usage at `Warn` for non-fatal indexer/grab errors — these are expected in normal operation and must not fail the command.
