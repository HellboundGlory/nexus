# Nexus Automation 5b — RSS Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add RSS sync to `internal/automation`: a scheduled job that polls every enabled indexer's latest feed, reverse-matches each release to a monitored *missing* library item (id-first, title+year fallback), and grabs the best acceptable release via 4c's `importing.Enqueue` — reusing 5a's `Decide`, the `Searcher`/`Enqueuer` interfaces, and the queue-dedup guard.

**Architecture:** RSS sync is release-driven where 5a is target-driven. `RSSSync` runs one aggregated generic (empty-term) search, builds an in-memory index of monitored movies/series, reverse-matches each release to a target (dropping ambiguous/undecidable), buckets candidates by target, then per target applies the same file/in-flight guards + `Decide` + `enqueueBest` as 5a — picking the best of duplicate releases. A small additive change to `internal/core/provider` + `internal/indexer` captures the `tmdbid`/`imdbid`/`tvdbid` feed attrs that make id-matching possible.

**Tech Stack:** Go (pure, `CGO_ENABLED=0`), `chi` v5 router, SQLite via `modernc.org/sqlite`, existing `internal/core/*` (store, provider, events, command, scheduler, api), `internal/parsing`, `internal/quality`, `internal/importing`.

## Global Constraints

- Go toolchain is NOT on the session PATH: prefix every Go command with `export PATH="/c/Program Files/Go/bin:$PATH"`.
- `go test -race` is unavailable (no CGO/C compiler) — verify with `-count=N` if needed. 5b adds no new concurrency (RSSSync runs on the existing single-threaded command worker).
- Module path root: `github.com/hellboundg/nexus`.
- Dependency boundary (DIRECT imports only): `automation` may import `internal/core/*`, `internal/parsing`, `internal/quality`, `internal/importing`. It must NOT directly import `internal/indexer`, `internal/downloadclient`, `internal/media`, or `internal/naming`. The new `Release` id fields live in `internal/core/provider`; the parser change lives in `internal/indexer` — neither adds an import to `automation`.
- `command.Reporter` is `interface{ Progress(pct int, msg string) }`; there is no exported NopReporter — tests define a local `nopReporter` (already defined in `command_test.go`, reused).
- Store constructor for tests: `db, _ := database.Open(t.TempDir()+"/t.db"); database.Migrate(db); st := store.New(db)` (helper `newStore(t)` already exists in `config_test.go`).
- 5b reuses these 5a symbols verbatim (same package): `Decide`, `Candidate`, `activeQueue`, `profileFor`, `enqueueBest`, `tvRequest`, `episodeIDs`, `containsInt`, `SearchCompleted`, `searchCommand`, and test helpers `newStore`, `hdProfile`, `seedersPtr`, `seedMovie`, `seedSeries`, `fakeSearcher`, `fakeEnqueuer`, `nopReporter`.
- All new automation files start with `package automation`.
- Commit after every task with the exact message shown.

## File Structure

- `internal/core/provider/provider.go` — MODIFY: add `Release.TMDBID`/`.IMDbID`/`.TVDBID` (Task 1).
- `internal/indexer/parse.go` — MODIFY: capture `tmdbid`/`imdbid`/`tvdbid` attrs (Task 1).
- `internal/automation/config.go` — MODIFY: add RSS fields to `Config`/`DefaultConfig`/clamping (Task 2).
- `internal/automation/rss.go` — CREATE: library index + reverse matcher (Task 3) then `RSSSync` pipeline + `RSSCompleted` event (Task 4).
- `internal/automation/command.go` — MODIFY: add `NewRSSSyncCommand` (Task 5).
- `cmd/nexus/main.go` — MODIFY: schedule RSS (gated on enabled) + WS forward (Task 5).
- Tests: `internal/indexer/parse_test.go` (Task 1), `internal/automation/config_test.go` (Task 2), `internal/automation/rss_test.go` (Tasks 3 & 4), `internal/automation/command_test.go` (Task 5).

---

### Task 1: Release identity capture (provider + Newznab parser)

**Files:**
- Modify: `internal/core/provider/provider.go` (the `Release` struct, ~line 49-61)
- Modify: `internal/indexer/parse.go` (the attr switch, ~line 65-86)
- Test: `internal/indexer/parse_test.go` (add one test, inline XML)

**Interfaces:**
- Consumes: existing `parseReleases(data []byte, indexerID string, proto provider.Protocol) ([]provider.Release, error)`; `xmlItem.Attrs` (each `{Name, Value string}`).
- Produces: `provider.Release` gains `TMDBID int`, `IMDbID string`, `TVDBID int`, populated from the `tmdbid`/`imdbid`/`tvdbid` feed attrs (imdb value stored with any leading `tt` stripped). Task 3's matcher reads these three fields.

- [ ] **Step 1: Write the failing test**

Add to `internal/indexer/parse_test.go`:

```go
func TestParseCapturesIDAttrs(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
  <channel>
    <item>
      <title>The.Film.2020.1080p.BluRay.x264-GRP</title>
      <guid>https://idx.test/details/xyz</guid>
      <link>https://idx.test/getnzb/xyz.nzb</link>
      <enclosure url="https://idx.test/getnzb/xyz.nzb" length="100" type="application/x-nzb"/>
      <newznab:attr name="category" value="2040"/>
      <newznab:attr name="tmdbid" value="603"/>
      <newznab:attr name="imdbid" value="tt0133093"/>
      <newznab:attr name="tvdbid" value="78901"/>
    </item>
  </channel>
</rss>`
	rels, err := parseReleases([]byte(feed), "1", provider.ProtocolUsenet)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Fatalf("want 1 release, got %d", len(rels))
	}
	r := rels[0]
	if r.TMDBID != 603 {
		t.Errorf("tmdbid = %d, want 603", r.TMDBID)
	}
	if r.IMDbID != "0133093" { // "tt" stripped
		t.Errorf("imdbid = %q, want %q", r.IMDbID, "0133093")
	}
	if r.TVDBID != 78901 {
		t.Errorf("tvdbid = %d, want 78901", r.TVDBID)
	}
}

func TestParseMissingIDAttrsAreZero(t *testing.T) {
	data, _ := os.ReadFile("testdata/newznab_search.xml") // no id attrs
	rels, _ := parseReleases(data, "1", provider.ProtocolUsenet)
	if len(rels) != 1 {
		t.Fatalf("want 1 release, got %d", len(rels))
	}
	r := rels[0]
	if r.TMDBID != 0 || r.IMDbID != "" || r.TVDBID != 0 {
		t.Errorf("absent id attrs should be zero, got tmdb=%d imdb=%q tvdb=%d", r.TMDBID, r.IMDbID, r.TVDBID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/indexer/ -run 'TestParseCapturesIDAttrs|TestParseMissingIDAttrsAreZero' -v`
Expected: FAIL — `r.TMDBID undefined` (field does not exist yet).

- [ ] **Step 3: Add the fields to `provider.Release`**

In `internal/core/provider/provider.go`, the `Release` struct — add three fields after `Leechers`:

```go
// Release is a single indexer result. Seeders/Leechers are set only for torrents.
type Release struct {
	Title       string
	DownloadURL string
	InfoURL     string
	Size        int64
	IndexerID   string
	Categories  []int
	PublishDate time.Time
	GUID        string
	Protocol    Protocol
	Seeders     *int
	Leechers    *int
	// Identity ids from newznab/torznab attrs (0/"" when absent). IMDbID is stored
	// without the "tt" prefix. Used by automation's RSS reverse-matcher.
	TMDBID int
	IMDbID string
	TVDBID int
}
```

- [ ] **Step 4: Capture the attrs in the parser**

In `internal/indexer/parse.go`, add `"strings"` to the imports, then add three cases to the `for _, a := range it.Attrs { switch a.Name {` block (alongside `category`/`size`/`seeders`/`peers`):

```go
			case "tmdbid":
				if n, err := strconv.Atoi(a.Value); err == nil {
					r.TMDBID = n
				}
			case "imdbid":
				r.IMDbID = strings.TrimPrefix(strings.ToLower(a.Value), "tt")
			case "tvdbid":
				if n, err := strconv.Atoi(a.Value); err == nil {
					r.TVDBID = n
				}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/indexer/ -run TestParse -v`
Expected: PASS (new tests + existing `TestParseNewznab`/`TestParseTorznab` still green).

- [ ] **Step 6: Confirm no regression across the module**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go build ./... && go test ./...`
Expected: build clean; all packages PASS (fields are additive; every existing consumer ignores them).

- [ ] **Step 7: Commit**

```bash
git add internal/core/provider/provider.go internal/indexer/parse.go internal/indexer/parse_test.go
git commit -m "feat(indexer): capture tmdbid/imdbid/tvdbid release attrs for RSS matching"
```

---

### Task 2: RSS config fields

**Files:**
- Modify: `internal/automation/config.go`
- Test: `internal/automation/config_test.go` (add tests)

**Interfaces:**
- Consumes: existing `Config`, `DefaultConfig`, `Service.Config`/`SetConfig`, `configSettingKey`.
- Produces: `Config` gains `RSSSyncEnabled bool` (default true) + `RSSSyncIntervalMinutes int` (default 15); a non-positive interval clamps to the default. Task 4 reads neither directly; Task 5's `main.go` reads both at startup. `Config` remains a comparable struct (`==` still works in tests).

- [ ] **Step 1: Write the failing tests**

Add to `internal/automation/config_test.go`:

```go
func TestConfigRSSDefaults(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	got, err := svc.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.RSSSyncEnabled {
		t.Fatalf("RSS should default enabled")
	}
	if got.RSSSyncIntervalMinutes != 15 {
		t.Fatalf("RSS interval default = %d, want 15", got.RSSSyncIntervalMinutes)
	}
}

func TestConfigRSSIntervalClamps(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	ctx := context.Background()
	// Persist a zero interval and disabled=false; interval must clamp, disabled respected.
	if err := svc.SetConfig(ctx, Config{
		MissingSearchIntervalHours: 6, MissingSearchBatchSize: 100,
		RSSSyncEnabled: false, RSSSyncIntervalMinutes: 0,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.RSSSyncIntervalMinutes != 15 {
		t.Fatalf("non-positive interval should clamp to 15, got %d", got.RSSSyncIntervalMinutes)
	}
	if got.RSSSyncEnabled {
		t.Fatalf("explicit disabled=false must be respected")
	}
}

func TestConfigRSSRoundTrip(t *testing.T) {
	svc := NewService(newStore(t), nil, nil, nil)
	ctx := context.Background()
	want := Config{
		MissingSearchIntervalHours: 6, MissingSearchBatchSize: 100,
		RSSSyncEnabled: true, RSSSyncIntervalMinutes: 30,
	}
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

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestConfigRSS -v`
Expected: FAIL — `unknown field RSSSyncEnabled in struct literal`.

- [ ] **Step 3: Extend `Config`, `DefaultConfig`, and clamping**

In `internal/automation/config.go`, replace the `Config` struct and `DefaultConfig`:

```go
// Config controls the scheduled missing-item sweep and RSS sync. Intervals are
// read at startup to register the scheduler; a change takes effect on next
// startup.
type Config struct {
	MissingSearchIntervalHours int  `json:"missingSearchIntervalHours"`
	MissingSearchBatchSize     int  `json:"missingSearchBatchSize"`
	RSSSyncEnabled             bool `json:"rssSyncEnabled"`
	RSSSyncIntervalMinutes     int  `json:"rssSyncIntervalMinutes"`
}

// DefaultConfig is applied when no config has been saved.
func DefaultConfig() Config {
	return Config{
		MissingSearchIntervalHours: 6,
		MissingSearchBatchSize:     100,
		RSSSyncEnabled:             true,
		RSSSyncIntervalMinutes:     15,
	}
}
```

Then in `Service.Config`, add the RSS interval clamp next to the existing clamps (leave `RSSSyncEnabled` untouched — a stored `false` must be respected):

```go
	d := DefaultConfig()
	if c.MissingSearchIntervalHours <= 0 {
		c.MissingSearchIntervalHours = d.MissingSearchIntervalHours
	}
	if c.MissingSearchBatchSize <= 0 {
		c.MissingSearchBatchSize = d.MissingSearchBatchSize
	}
	if c.RSSSyncIntervalMinutes <= 0 {
		c.RSSSyncIntervalMinutes = d.RSSSyncIntervalMinutes
	}
	return c, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestConfig -v`
Expected: PASS (RSS tests + the existing `TestConfigDefaultsWhenAbsent`/`TestConfigRoundTrip`).

> Note: `TestConfigDefaultsWhenAbsent` compares `got != DefaultConfig()`; because the absent-key path returns `DefaultConfig()` directly it still holds. `TestConfigRoundTrip` (5a) sets only the two sweep fields, leaving RSS zero-valued; on read the interval clamps to 15 and enabled stays false — so that older test's `got != want` would now differ. **Update the 5a `TestConfigRoundTrip` `want` to include `RSSSyncEnabled: false, RSSSyncIntervalMinutes: 15`** (the clamped/read-back values) so it still passes. Make that one-line edit to the existing test literal.

- [ ] **Step 5: Commit**

```bash
git add internal/automation/config.go internal/automation/config_test.go
git commit -m "feat(automation): RSS sync config (enabled + interval minutes)"
```

---

### Task 3: Library index + reverse matcher (pure)

**Files:**
- Create: `internal/automation/rss.go`
- Test: `internal/automation/rss_test.go`

**Interfaces:**
- Consumes: `store.Movie` (`ID`, `TMDBID int`, `IMDbID string`, `Title string`, `Year int`, `Monitored bool`); `store.Series` (`ID`, `TMDBID int`, `Title string`, `FirstAired string`, `Monitored bool`); `provider.Release` (`.TMDBID`, `.IMDbID`, `.TVDBID`, `.Categories`, `.Title`); `provider.MediaKind`, `provider.KindMovie`, `provider.KindTV`; `parsing.Parse`, `parsing.ParsedRelease` (`.Title`, `.Season`, `.Year`).
- Produces (all unexported, package-internal — Task 4 calls them):
  - `type libraryIndex struct { ... }`
  - `func buildLibraryIndex(movies []store.Movie, series []store.Series) *libraryIndex`
  - `func (idx *libraryIndex) matchMovie(r provider.Release, p parsing.ParsedRelease) (*store.Movie, bool)`
  - `func (idx *libraryIndex) matchSeries(r provider.Release, p parsing.ParsedRelease) (*store.Series, bool)`
  - `func routeKind(r provider.Release) (provider.MediaKind, bool)`
  - `func normTitle(s string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/automation/rss_test.go`:

```go
package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

func testIndex() *libraryIndex {
	return buildLibraryIndex(
		[]store.Movie{
			{ID: 1, TMDBID: 603, IMDbID: "tt0133093", Title: "The Matrix", Year: 1999, Monitored: true},
			{ID: 2, Title: "The Office", Year: 2005, Monitored: true},
			{ID: 3, Title: "The Office", Year: 2001, Monitored: true},
			{ID: 9, Title: "Unmonitored", Year: 2010, Monitored: false},
		},
		[]store.Series{
			{ID: 10, TMDBID: 7, Title: "The Show", FirstAired: "2018-01-01", Monitored: true},
			{ID: 11, Title: "Clone", FirstAired: "1999-01-01", Monitored: true},
			{ID: 12, Title: "Clone", FirstAired: "2015-01-01", Monitored: true},
		},
	)
}

func TestNormTitle(t *testing.T) {
	if got := normTitle("The.Matrix: Reloaded!"); got != "the matrix reloaded" {
		t.Fatalf("normTitle = %q", got)
	}
}

func TestMatchMovieByID(t *testing.T) {
	idx := testIndex()
	// tmdbid wins even if the title is wrong.
	r := provider.Release{TMDBID: 603, Title: "Totally.Different.2020.1080p.BluRay-GRP"}
	m, ok := idx.matchMovie(r, parsing.Parse(r.Title, provider.KindMovie))
	if !ok || m.ID != 1 {
		t.Fatalf("tmdbid match failed: ok=%v m=%v", ok, m)
	}
	// imdbid (release stores it tt-stripped, as Task 1 does).
	r2 := provider.Release{IMDbID: "0133093", Title: "x"}
	m2, ok2 := idx.matchMovie(r2, parsing.Parse(r2.Title, provider.KindMovie))
	if !ok2 || m2.ID != 1 {
		t.Fatalf("imdbid match failed: ok=%v m=%v", ok2, m2)
	}
}

func TestMatchMovieByTitleYear(t *testing.T) {
	idx := testIndex()
	r := provider.Release{Title: "The.Office.2005.1080p.WEB-DL.x264-GRP"}
	m, ok := idx.matchMovie(r, parsing.Parse(r.Title, provider.KindMovie))
	if !ok || m.ID != 2 {
		t.Fatalf("title+year should pick the 2005 Office, got ok=%v m=%v", ok, m)
	}
}

func TestMatchMovieAmbiguousTitleDropped(t *testing.T) {
	idx := testIndex()
	// No year in the release, two "The Office" movies → ambiguous → drop.
	r := provider.Release{Title: "The.Office.1080p.WEB-DL.x264-GRP"}
	if _, ok := idx.matchMovie(r, parsing.Parse(r.Title, provider.KindMovie)); ok {
		t.Fatalf("ambiguous same-title movies must be dropped")
	}
}

func TestMatchMovieUnmonitoredNotIndexed(t *testing.T) {
	idx := testIndex()
	r := provider.Release{Title: "Unmonitored.2010.1080p.BluRay-GRP"}
	if _, ok := idx.matchMovie(r, parsing.Parse(r.Title, provider.KindMovie)); ok {
		t.Fatalf("unmonitored movie must not be matchable")
	}
}

func TestMatchSeriesByID(t *testing.T) {
	idx := testIndex()
	r := provider.Release{TMDBID: 7, Title: "Wrong.Title.S01E01-GRP"}
	se, ok := idx.matchSeries(r, parsing.Parse(r.Title, provider.KindTV))
	if !ok || se.ID != 10 {
		t.Fatalf("tmdbid series match failed: ok=%v se=%v", ok, se)
	}
}

func TestMatchSeriesByTitle(t *testing.T) {
	idx := testIndex()
	r := provider.Release{Title: "The.Show.S02E05.1080p.WEB-DL-GRP"}
	se, ok := idx.matchSeries(r, parsing.Parse(r.Title, provider.KindTV))
	if !ok || se.ID != 10 {
		t.Fatalf("title series match failed: ok=%v se=%v", ok, se)
	}
}

func TestMatchSeriesAmbiguousDroppedWithoutYear(t *testing.T) {
	idx := testIndex()
	// Two "Clone" series, release title has no year → ambiguous → drop.
	r := provider.Release{Title: "Clone.S01E01.1080p.WEB-DL-GRP"}
	if _, ok := idx.matchSeries(r, parsing.Parse(r.Title, provider.KindTV)); ok {
		t.Fatalf("ambiguous same-title series must be dropped without a year")
	}
}

func TestMatchSeriesDisambiguatedByYear(t *testing.T) {
	idx := testIndex()
	// Release carries a year matching the 2015 "Clone" first-aired year.
	r := provider.Release{Title: "Clone.2015.S01E01.1080p.WEB-DL-GRP"}
	se, ok := idx.matchSeries(r, parsing.Parse(r.Title, provider.KindTV))
	if !ok || se.ID != 12 {
		t.Fatalf("year should disambiguate to the 2015 Clone, got ok=%v se=%v", ok, se)
	}
}

func TestRouteKind(t *testing.T) {
	cases := []struct {
		name string
		r    provider.Release
		kind provider.MediaKind
		ok   bool
	}{
		{"category movie", provider.Release{Categories: []int{2040}, Title: "x"}, provider.KindMovie, true},
		{"category tv", provider.Release{Categories: []int{5040}, Title: "x"}, provider.KindTV, true},
		{"heuristic tv", provider.Release{Title: "The.Show.S01E01.1080p.WEB-DL-GRP"}, provider.KindTV, true},
		{"heuristic movie", provider.Release{Title: "The.Film.2020.1080p.BluRay-GRP"}, provider.KindMovie, true},
		{"undecidable", provider.Release{Title: "Just Some Words"}, "", false},
	}
	for _, c := range cases {
		k, ok := routeKind(c.r)
		if ok != c.ok || k != c.kind {
			t.Errorf("%s: routeKind = (%q,%v), want (%q,%v)", c.name, k, ok, c.kind, c.ok)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run 'TestNormTitle|TestMatch|TestRouteKind' -v`
Expected: FAIL — `undefined: buildLibraryIndex` / `undefined: routeKind`.

- [ ] **Step 3: Write the matcher**

Create `internal/automation/rss.go`:

```go
package automation

import (
	"regexp"
	"strings"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

var (
	reNonAlnum  = regexp.MustCompile(`[^a-z0-9]+`)
	reTitleYear = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
)

// normTitle lowercases a title and collapses every run of non-alphanumeric
// characters to a single space, so "The.Matrix" and "The Matrix" compare equal.
func normTitle(s string) string {
	s = strings.ToLower(s)
	s = reNonAlnum.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// normIMDb strips the "tt" prefix so a release id (already tt-stripped by the
// parser) compares against a stored movie IMDbID regardless of its stored form.
func normIMDb(s string) string {
	return strings.TrimPrefix(strings.ToLower(s), "tt")
}

// titleYear returns the first 4-digit year in a raw title, or 0. Used to
// disambiguate same-titled TV series (parsing.Parse does not extract a year for
// the TV kind).
func titleYear(s string) int {
	if m := reTitleYear.FindString(s); m != "" {
		return atoiSafe(m)
	}
	return 0
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func firstAiredYear(se *store.Series) int {
	if len(se.FirstAired) >= 4 {
		return atoiSafe(se.FirstAired[:4])
	}
	return 0
}

// libraryIndex is an in-memory lookup over monitored library items, built once
// per RSS poll. Title maps hold slices because two monitored items can share a
// normalized title (disambiguated by year at match time).
type libraryIndex struct {
	movieByTMDB   map[int]*store.Movie
	movieByIMDb   map[string]*store.Movie
	movieByTitle  map[string][]*store.Movie
	seriesByTMDB  map[int]*store.Series
	seriesByTitle map[string][]*store.Series
}

func buildLibraryIndex(movies []store.Movie, series []store.Series) *libraryIndex {
	idx := &libraryIndex{
		movieByTMDB:   map[int]*store.Movie{},
		movieByIMDb:   map[string]*store.Movie{},
		movieByTitle:  map[string][]*store.Movie{},
		seriesByTMDB:  map[int]*store.Series{},
		seriesByTitle: map[string][]*store.Series{},
	}
	for i := range movies {
		m := &movies[i]
		if !m.Monitored {
			continue
		}
		if m.TMDBID != 0 {
			idx.movieByTMDB[m.TMDBID] = m
		}
		if m.IMDbID != "" {
			idx.movieByIMDb[normIMDb(m.IMDbID)] = m
		}
		key := normTitle(m.Title)
		idx.movieByTitle[key] = append(idx.movieByTitle[key], m)
	}
	for i := range series {
		se := &series[i]
		if !se.Monitored {
			continue
		}
		if se.TMDBID != 0 {
			idx.seriesByTMDB[se.TMDBID] = se
		}
		key := normTitle(se.Title)
		idx.seriesByTitle[key] = append(idx.seriesByTitle[key], se)
	}
	return idx
}

// matchMovie resolves a release to a monitored movie: tmdbid, then imdbid, then
// normalized title disambiguated by year. Returns false when nothing matches or
// the title is ambiguous.
func (idx *libraryIndex) matchMovie(r provider.Release, p parsing.ParsedRelease) (*store.Movie, bool) {
	if r.TMDBID != 0 {
		if m, ok := idx.movieByTMDB[r.TMDBID]; ok {
			return m, true
		}
	}
	if r.IMDbID != "" {
		if m, ok := idx.movieByIMDb[normIMDb(r.IMDbID)]; ok {
			return m, true
		}
	}
	cands := idx.movieByTitle[normTitle(p.Title)]
	var hits []*store.Movie
	for _, m := range cands {
		if p.Year == 0 || m.Year == 0 || m.Year == p.Year {
			hits = append(hits, m)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return nil, false
}

// matchSeries resolves a release to a monitored series: tmdbid, then normalized
// title disambiguated by the release year against the series' first-aired year.
func (idx *libraryIndex) matchSeries(r provider.Release, p parsing.ParsedRelease) (*store.Series, bool) {
	if r.TMDBID != 0 {
		if se, ok := idx.seriesByTMDB[r.TMDBID]; ok {
			return se, true
		}
	}
	cands := idx.seriesByTitle[normTitle(p.Title)]
	if len(cands) == 1 {
		return cands[0], true
	}
	year := titleYear(r.Title)
	if year == 0 {
		return nil, false // ambiguous with no year to disambiguate
	}
	var hits []*store.Series
	for _, se := range cands {
		if firstAiredYear(se) == year {
			hits = append(hits, se)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return nil, false
}

// routeKind decides movie vs TV for a release. Newznab category wins
// (2000–2999 = movie, 5000–5999 = TV); otherwise a parse heuristic: a parsable
// season => TV, else a parsable year => movie. Returns false when undecidable.
func routeKind(r provider.Release) (provider.MediaKind, bool) {
	for _, c := range r.Categories {
		if c >= 2000 && c < 3000 {
			return provider.KindMovie, true
		}
		if c >= 5000 && c < 6000 {
			return provider.KindTV, true
		}
	}
	if parsing.Parse(r.Title, provider.KindTV).Season > 0 {
		return provider.KindTV, true
	}
	if parsing.Parse(r.Title, provider.KindMovie).Year > 0 {
		return provider.KindMovie, true
	}
	return "", false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run 'TestNormTitle|TestMatch|TestRouteKind' -v`
Expected: PASS (all matcher tests).

- [ ] **Step 5: Commit**

```bash
git add internal/automation/rss.go internal/automation/rss_test.go
git commit -m "feat(automation): RSS reverse matcher (id-first, title+year fallback)"
```

---

### Task 4: RSSSync pipeline (group-by-target + Decide + enqueueBest)

**Files:**
- Modify: `internal/automation/rss.go` (append the pipeline + event)
- Test: `internal/automation/rss_test.go` (add pipeline tests)

**Interfaces:**
- Consumes: `s.search.Search(ctx, provider.Query{Type: provider.SearchGeneric, Limit})`; `s.store` (`ListMovies`, `ListSeries`, `ListEpisodes`, `GetMovie`, `GetSeries`, `MediaFileForMovie`, `MediaFileForEpisode`); `activeQueue`, `profileFor`, `enqueueBest`, `Decide`, `Candidate`, `tvRequest`, `episodeIDs`, `containsInt` (5a); `importing.EnqueueRequest`; `events.Event`; `buildLibraryIndex`/`matchMovie`/`matchSeries`/`routeKind` (Task 3).
- Produces:
  - `type RSSResult struct { Considered, Matched, Grabbed int }`
  - `func (s *Service) RSSSync(ctx context.Context) (RSSResult, error)`
  - `type RSSCompleted struct { Considered, Matched, Grabbed int }` with `Name() string == "automation.rss.completed"`
  - unexported `rssPlaceTV`. Task 5 wraps `RSSSync` in a command and forwards the event.

- [ ] **Step 1: Write the failing tests**

Add to `internal/automation/rss_test.go` (imports `context`, `store`, `provider`, `importing` — extend the import block):

```go
func TestRSSSyncMatchesMovieAndGrabs(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true) // "The Film" (2020), tmdb 42, monitored, HD profile
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u", Protocol: provider.ProtocolUsenet, Categories: []int{2040}},
		{Title: "Unrelated.Thing.2019.1080p.WEB-DL-GRP", DownloadURL: "x", Protocol: provider.ProtocolUsenet, Categories: []int{2040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Considered != 2 || res.Matched != 1 || res.Grabbed != 1 {
		t.Fatalf("counts = %+v, want considered=2 matched=1 grabbed=1", res)
	}
	if len(fe.reqs) != 1 || fe.reqs[0].MovieID != id || fe.reqs[0].DownloadURL != "u" {
		t.Fatalf("bad enqueue: %+v", fe.reqs)
	}
	if fs.lastQuery.Type != provider.SearchGeneric {
		t.Fatalf("RSS must use a generic empty-term query, got %+v", fs.lastQuery)
	}
}

func TestRSSSyncPicksBestOfDuplicates(t *testing.T) {
	st := newStore(t)
	seedSeries(t, st, true, 1) // "The Show", 1 monitored episode S01E01
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.WEB-DL.x264-GRP", DownloadURL: "web", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 {
		t.Fatalf("two dupes should yield one grab: res=%+v reqs=%d", res, len(fe.reqs))
	}
	if fe.reqs[0].DownloadURL != "blu" {
		t.Fatalf("best (Bluray) should be chosen, got %q", fe.reqs[0].DownloadURL)
	}
}

func TestRSSSyncPrefersSeasonPack(t *testing.T) {
	st := newStore(t)
	sid, _ := seedSeries(t, st, true, 3) // 3 monitored missing episodes → fully missing
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "single", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "pack" {
		t.Fatalf("fully-missing season should grab the pack once: res=%+v reqs=%+v", res, fe.reqs)
	}
	if len(fe.reqs[0].EpisodeIDs) != 3 {
		t.Fatalf("pack should carry all 3 missing episode ids, got %v", fe.reqs[0].EpisodeIDs)
	}
	_ = sid
}

func TestRSSSyncSkipsFiledEpisode(t *testing.T) {
	st := newStore(t)
	_, epIDs := seedSeries(t, st, true, 1)
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{
		MediaKind: "tv", EpisodeID: &epIDs[0], RelativePath: "e1.mkv", QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Matched (it resolves to the series) but not grabbed (episode already filed).
	if res.Grabbed != 0 || len(fe.reqs) != 0 {
		t.Fatalf("already-filed episode must not be grabbed: res=%+v reqs=%d", res, len(fe.reqs))
	}
}

func TestRSSSyncSkipsInFlightEpisode(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 1)
	// Pre-existing in-flight grab for that episode.
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		SeriesID: &sid, MediaKind: "tv", EpisodeIDs: []int64{epIDs[0]},
		SourceTitle: "prev", Protocol: "usenet", QualityID: 9, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 0 || len(fe.reqs) != 0 {
		t.Fatalf("in-flight episode must not be re-grabbed: res=%+v reqs=%d", res, len(fe.reqs))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestRSSSync -v`
Expected: FAIL — `svc.RSSSync undefined`.

- [ ] **Step 3: Write the pipeline**

Append to `internal/automation/rss.go`. Add `"context"`, `"log/slog"`, `"github.com/hellboundg/nexus/internal/core/events"`, `"github.com/hellboundg/nexus/internal/core/provider"` (already imported), `"github.com/hellboundg/nexus/internal/importing"` to the import block:

```go
// rssFeedLimit bounds each indexer's latest-feed response.
const rssFeedLimit = 100

// RSSResult summarizes one poll: releases seen, releases matched to a monitored
// item, and releases grabbed.
type RSSResult struct {
	Considered int
	Matched    int
	Grabbed    int
}

// RSSCompleted is emitted when an RSS poll finishes → WS.
type RSSCompleted struct {
	Considered int `json:"considered"`
	Matched    int `json:"matched"`
	Grabbed    int `json:"grabbed"`
}

func (RSSCompleted) Name() string { return "automation.rss.completed" }

// RSSSync polls every enabled indexer's latest feed once, reverse-matches each
// release to a monitored missing item, and grabs the best acceptable release per
// target. Release-driven, but grabs are decided per target (best-of-duplicates),
// reusing the same guards and Decide/enqueueBest as the wanted/missing search.
func (s *Service) RSSSync(ctx context.Context) (RSSResult, error) {
	releases, err := s.search.Search(ctx, provider.Query{Type: provider.SearchGeneric, Limit: rssFeedLimit})
	if err != nil {
		slog.Warn("automation: rss feed had indexer errors", "err", err)
	}
	res := RSSResult{Considered: len(releases)}

	movies, err := s.store.ListMovies(ctx)
	if err != nil {
		return res, err
	}
	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return res, err
	}
	idx := buildLibraryIndex(movies, series)

	// Bucket releases by resolved target.
	movieRels := map[int64][]provider.Release{}
	movieTargets := map[int64]*store.Movie{}
	tvRels := map[int64][]provider.Release{}
	tvTargets := map[int64]*store.Series{}
	for _, r := range releases {
		kind, ok := routeKind(r)
		if !ok {
			continue
		}
		p := parsing.Parse(r.Title, kind)
		if kind == provider.KindMovie {
			m, ok := idx.matchMovie(r, p)
			if !ok {
				continue
			}
			movieRels[m.ID] = append(movieRels[m.ID], r)
			movieTargets[m.ID] = m
			res.Matched++
		} else {
			se, ok := idx.matchSeries(r, p)
			if !ok {
				continue
			}
			tvRels[se.ID] = append(tvRels[se.ID], r)
			tvTargets[se.ID] = se
			res.Matched++
		}
	}

	activeMovies, activeEps, err := s.activeQueue(ctx)
	if err != nil {
		return res, err
	}

	// Movies: skip filed/in-flight, then Decide + enqueue best of the bucket.
	for movieID, rels := range movieRels {
		if _, active := activeMovies[movieID]; active {
			continue
		}
		if f, err := s.store.MediaFileForMovie(ctx, movieID); err != nil {
			return res, err
		} else if f != nil {
			continue
		}
		m := movieTargets[movieID]
		profile, ok, err := s.profileFor(ctx, m.QualityProfileID)
		if err != nil {
			return res, err
		}
		if !ok {
			continue
		}
		cands := Decide(rels, provider.KindMovie, profile)
		grabbed, err := s.enqueueBest(ctx, cands, func(c Candidate) importing.EnqueueRequest {
			return importing.EnqueueRequest{
				DownloadURL: c.Release.DownloadURL, Title: c.Release.Title,
				Protocol: c.Release.Protocol, IndexerID: c.Release.IndexerID,
				MediaKind: provider.KindMovie, MovieID: movieID,
			}
		})
		if err != nil {
			return res, err
		}
		if grabbed {
			res.Grabbed++
		}
	}

	// TV: per series, rank the bucket once, then place season packs / episodes.
	for seriesID, rels := range tvRels {
		se := tvTargets[seriesID]
		profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
		if err != nil {
			return res, err
		}
		if !ok {
			continue
		}
		eps, err := s.store.ListEpisodes(ctx, seriesID)
		if err != nil {
			return res, err
		}
		ranked := Decide(rels, provider.KindTV, profile)
		n, err := s.rssPlaceTV(ctx, se, eps, ranked, activeEps)
		if err != nil {
			return res, err
		}
		res.Grabbed += n
	}

	s.emit(ctx, RSSCompleted(res))
	return res, nil
}

// rssPlaceTV places ranked TV candidates against a series' monitored-missing
// episodes: a full-season pack first for any fully-missing monitored season
// (grabbed with all its missing episode ids), then per-episode for any still-
// unhandled missing episode. Mirrors the wanted/missing season strategy but over
// an already-fetched candidate pool.
func (s *Service) rssPlaceTV(ctx context.Context, se *store.Series, eps []store.Episode, ranked []Candidate, activeEps map[int64]struct{}) (int, error) {
	missingBySeason := map[int][]store.Episode{}
	monitoredBySeason := map[int]int{}
	for _, e := range eps {
		if !e.Monitored {
			continue
		}
		monitoredBySeason[e.SeasonNumber]++
		f, err := s.store.MediaFileForEpisode(ctx, e.ID)
		if err != nil {
			return 0, err
		}
		if _, active := activeEps[e.ID]; f == nil && !active {
			missingBySeason[e.SeasonNumber] = append(missingBySeason[e.SeasonNumber], e)
		}
	}

	handled := map[int64]struct{}{}
	grabbed := 0

	// Season packs first, for fully-missing monitored seasons.
	for season, missing := range missingBySeason {
		if len(missing) != monitoredBySeason[season] {
			continue
		}
		var packs []Candidate
		for _, c := range ranked {
			if c.Parsed.Season == season && len(c.Parsed.Episodes) == 0 {
				packs = append(packs, c)
			}
		}
		if len(packs) == 0 {
			continue
		}
		ids := episodeIDs(missing)
		ok, err := s.enqueueBest(ctx, packs, func(c Candidate) importing.EnqueueRequest {
			return tvRequest(se.ID, ids, c)
		})
		if err != nil {
			return grabbed, err
		}
		if ok {
			grabbed++
			for _, e := range missing {
				handled[e.ID] = struct{}{}
			}
		}
	}

	// Per-episode for anything not covered by a grabbed pack.
	for season, missing := range missingBySeason {
		for _, e := range missing {
			if _, done := handled[e.ID]; done {
				continue
			}
			var covering []Candidate
			for _, c := range ranked {
				if c.Parsed.Season == season && containsInt(c.Parsed.Episodes, e.EpisodeNumber) {
					covering = append(covering, c)
				}
			}
			if len(covering) == 0 {
				continue
			}
			ok, err := s.enqueueBest(ctx, covering, func(c Candidate) importing.EnqueueRequest {
				return tvRequest(se.ID, []int64{e.ID}, c)
			})
			if err != nil {
				return grabbed, err
			}
			if ok {
				grabbed++
				handled[e.ID] = struct{}{}
			}
		}
	}
	return grabbed, nil
}
```

> `events` is imported for the `events.Event` contract satisfied by `RSSCompleted`; `s.emit` (5a) accepts any `events.Event`. `RSSCompleted(res)` converts the identically-shaped `RSSResult` to the event.

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestRSSSync -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Run the full automation package**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -v`
Expected: PASS (all 5a + 5b tests). `go vet ./internal/automation/` clean.

- [ ] **Step 6: Commit**

```bash
git add internal/automation/rss.go internal/automation/rss_test.go
git commit -m "feat(automation): RSS sync pipeline (group-by-target, best-of-duplicates)"
```

---

### Task 5: RSS command + composition-root wiring + boundary verification

**Files:**
- Modify: `internal/automation/command.go`
- Modify: `cmd/nexus/main.go`
- Test: `internal/automation/command_test.go` (add one test)

**Interfaces:**
- Consumes: `searchCommand` (5a); `Service.RSSSync`; `automation.NewRSSSyncCommand`; `autoCfg` (`automation.Config` with `RSSSyncEnabled`, `RSSSyncIntervalMinutes`); `scheduler.Every`; `command.Command`.
- Produces: `func NewRSSSyncCommand(svc *Service) command.Command` (name `"RSSSync"`); a scheduled RSS job in `main.go` registered only when enabled, and `automation.rss.completed` forwarded to the WS.

- [ ] **Step 1: Write the failing test**

Add to `internal/automation/command_test.go`:

```go
func TestRSSSyncCommandRuns(t *testing.T) {
	st := newStore(t)
	seedMovie(t, st, true, true)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u", Protocol: provider.ProtocolUsenet, Categories: []int{2040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	cmd := NewRSSSyncCommand(svc)
	if cmd.Name() != "RSSSync" {
		t.Fatalf("bad name %q", cmd.Name())
	}
	if err := cmd.Run(context.Background(), nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if len(fe.reqs) != 1 {
		t.Fatalf("RSS command should have grabbed one, got %d", len(fe.reqs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestRSSSyncCommandRuns -v`
Expected: FAIL — `NewRSSSyncCommand undefined`.

- [ ] **Step 3: Add the command**

Append to `internal/automation/command.go`:

```go
// NewRSSSyncCommand is the scheduled RSS poll over all enabled indexers.
func NewRSSSyncCommand(svc *Service) command.Command {
	return &searchCommand{name: "RSSSync", run: func(ctx context.Context) (int, error) {
		res, err := svc.RSSSync(ctx)
		return res.Grabbed, err
	}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestRSSSyncCommandRuns -v`
Expected: PASS.

- [ ] **Step 5: Wire the scheduled RSS job in `main.go`**

In `cmd/nexus/main.go`, in the scheduler block — after the existing `sch.Every(... MissingSearchIntervalHours ...)` registration (around line 123-125) and before `sch.Start()` — add:

```go
	if autoCfg.RSSSyncEnabled {
		sch.Every(time.Duration(autoCfg.RSSSyncIntervalMinutes)*time.Minute, func() command.Command {
			return automation.NewRSSSyncCommand(autoSvc)
		})
	}
```

- [ ] **Step 6: Forward the RSS event to the WS**

In the `api.NewRouter(api.Deps{...})` call, append `"automation.rss.completed"` to the `WSForward` slice:

```go
		WSForward: []string{"indexer.status", "download.status", "media.series.updated", "media.movie.updated", "import.completed", "queue.updated", "automation.search.completed", "automation.rss.completed"},
```

- [ ] **Step 7: Build, vet, and full test suite**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...
```
Expected: build + vet clean; all packages PASS.

- [ ] **Step 8: Verify module boundary (direct imports)**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
go list -f '{{ join .Imports "\n" }}' ./internal/automation | grep -E 'internal/(indexer|downloadclient|media|naming)$' || echo "BOUNDARY OK"
```
Expected: prints `BOUNDARY OK`. If any line prints instead, a forbidden direct import slipped in — fix it before committing.

- [ ] **Step 9: Commit**

```bash
git add internal/automation/command.go internal/automation/command_test.go cmd/nexus/main.go
git commit -m "feat(automation): schedule RSS sync + forward rss.completed to WS"
```

---

## Self-Review (completed during authoring)

- **Spec coverage:** §2.1 id-first matching / §3.1 Release id capture → Task 1; §2.6 / §3.5 config (enabled + interval minutes) → Task 2; §3.2 feed fetch via generic query → Task 4 (`RSSSync` query); §3.3 reverse matcher (kind routing, movie/series id+title+year, ambiguous drop) → Task 3; §2.4 / §3.4 group-by-target + Decide + enqueueBest + season-pack precedence + best-of-duplicates → Task 4; §3.6 command + scheduler (gated on enabled) + event → Tasks 4 (event) & 5 (command/schedule/WS); §5 error handling (partial feed, ambiguous/undecidable skip, filed/in-flight skip, grab fall-through, ErrNoProfile skip) → Tasks 3/4 (`enqueueBest` fall-through is inherited from 5a); §6 testing → Tasks 1–5; §8 acceptance criteria 1–7 → Tasks 1–5. No gaps. (§2.5 no-seen-cache and §7 out-of-scope items are deliberately un-tasked.)
- **Placeholder scan:** no TBD/TODO; every code step shows complete code and exact commands.
- **Type consistency:** `Release.TMDBID/IMDbID/TVDBID` (T1) read by `matchMovie`/`matchSeries` (T3); `buildLibraryIndex`/`matchMovie`/`matchSeries`/`routeKind` (T3) called by `RSSSync` (T4); `RSSResult`/`RSSCompleted` share the same three int fields so `RSSCompleted(res)` compiles (T4); `RSSCompleted.Name()` == `"automation.rss.completed"` matches the WSForward string (T5); reused 5a symbols (`Decide`, `Candidate`, `activeQueue`→`(movies, episodes map[int64]struct{}, error)`, `profileFor`→`(profile, ok, err)`, `enqueueBest`, `tvRequest`, `episodeIDs`, `containsInt`, `searchCommand`, `nopReporter`) are used with their existing signatures; `Config` stays a comparable struct so `==` in config/command tests holds.
- **Cross-test note flagged for the implementer:** Task 2 requires a one-line edit to 5a's existing `TestConfigRoundTrip` `want` literal (add the read-back RSS fields) — called out inline in Task 2 Step 4 so it isn't missed.

## Notes for the implementer

- Do the tasks in order: Task 3 (matcher) must land before Task 4 (pipeline calls it); Task 1 (Release id fields) before Task 3 (matcher reads them).
- `parsing.Parse(title, KindTV)` does NOT extract a year — that is why `matchSeries` uses the local `titleYear(r.Title)` regex for disambiguation rather than `p.Year`. `parsing.Parse(title, KindMovie)` DOES set `Year`, so `matchMovie` uses `p.Year`.
- Season-pack parsing (`S01` → `Season=1, Episodes=[]`) already exists (merged in 5a via `reSeasonPack`); no parser change is needed in 5b.
- `store.MediaFile` has `EpisodeID *int64` / `MovieID *int64` — set exactly one in fixtures. `store.QueueItem` for the in-flight fixture needs `Status: store.QueueGrabbed` and `EpisodeIDs`/`SeriesID` set; confirm field names against `internal/core/store/import_store.go` if the compiler complains.
- Keep `slog` at `Warn` for non-fatal feed errors — a partial feed must not fail the poll.
- Map iteration order over `movieRels`/`tvRels`/`missingBySeason` is nondeterministic but each target is independent, so results are order-independent; tests assert on counts and the chosen download URL, not on call order.
