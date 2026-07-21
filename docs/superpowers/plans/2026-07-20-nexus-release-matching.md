# Release Matching (SP-1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop automation grabbing releases that belong to a different TV show, by matching releases against series aliases and rejecting releases whose episode title contradicts the stored one.

**Architecture:** Two independent checks replace the current exact-primary-title `releaseIsForSeries`. Aliases come from TMDB `alternative_titles`, stored in a new `series_aliases` table, populated on series add and on the existing 12-hourly refresh. Episode-title contradiction is a new parser field compared against the stored episode title, vetoing only on positive evidence. Both apply at the three TV grab paths; the RSS path gains aliases in its existing title index.

**Tech Stack:** Go 1.x, SQLite (modernc driver), chi router. Backend only — no frontend, no `web/dist` rebuild.

**Spec:** `docs/superpowers/specs/2026-07-20-nexus-release-matching-design.md`

## Global Constraints

- **Extraction must fail toward silence.** A false rejection means nothing downloads — the failure the user has already suffered twice. A missed signal only falls back to the other checks. When in doubt, produce no signal.
- **Reject only on positive contradiction.** An absent or unrecognisable episode title is never grounds for rejection.
- **Three TV grab paths, all must be gated:** `searchEpisode` (`search.go`), the `searchSeason` pack branch (`search.go`), `upgradeEpisode` (`upgrade.go`). A passing test on one proves nothing about the others — three previous fixes on this project each missed a site.
- **Alias fetch failure must never fail a series add or refresh.** Log and continue.
- **A series with no aliases yet falls back to primary-title matching** — not open (accept everything), not closed (accept nothing).
- **`normTitle` (`internal/automation/rss.go`) folds diacritics and is load-bearing.** `"Pokémon"` would otherwise normalise to `"pok mon"` and never equal a release's `"pokemon"`.
- Movies are out of scope. `store.ListQueue`/`QueueByStatus` stay unpaged.
- Every gate test must be mutation-verified: neuter the check, confirm the test fails, revert. Report any mutation that comes back green rather than papering over it.

## File Structure

| File | Responsibility |
|---|---|
| `internal/core/database/migrations/0009_series_aliases.sql` | new `series_aliases` table |
| `internal/core/store/media_store.go` | alias read/write |
| `internal/core/provider/metadata.go` | `SeriesAlias` type, `SeriesMetadata.Aliases` |
| `internal/media/tmdb.go` | fetch `/tv/{id}/alternative_titles` |
| `internal/media/media.go` | persist aliases on add + refresh |
| `internal/parsing/parser.go` | `EpisodeTitle`, `PDTV`/`SDTV` sources |
| `internal/automation/match.go` *(new)* | `titleIndex`, `releaseIsForSeries`, `episodeTitleContradicts` |
| `internal/automation/search.go`, `upgrade.go`, `rss.go` | apply the checks |

---

### Task 1: Migration and alias storage

**Files:**
- Create: `internal/core/database/migrations/0009_series_aliases.sql`
- Modify: `internal/core/store/media_store.go`
- Modify: `internal/core/database/database_test.go:32`
- Test: `internal/core/store/media_store_test.go`

**Interfaces:**
- Produces:
  - `type SeriesAlias struct { ID, SeriesID int64; Title, Country, Type string }`
  - `func (s *Store) ReplaceSeriesAliases(ctx context.Context, seriesID int64, aliases []SeriesAlias) error`
  - `func (s *Store) SeriesAliasesFor(ctx context.Context, seriesID int64) ([]SeriesAlias, error)`
  - `func (s *Store) AllSeriesAliases(ctx context.Context) ([]SeriesAlias, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/core/store/media_store_test.go`. Tests are inside package `store`, so types are unqualified.

```go
func TestSeriesAliasesReplaceAndRead(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sid, err := s.CreateSeries(ctx, Series{TMDBID: 60572, Title: "Pokémon", SortTitle: "pokemon"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateSeries(ctx, Series{TMDBID: 220150, Title: "Pokémon Horizons", SortTitle: "pokemon horizons"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceSeriesAliases(ctx, sid, []SeriesAlias{
		{Title: "Pokémon: Indigo League", Country: "US", Type: "season 1"},
		{Title: "Pocket Monsters", Country: "JP"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceSeriesAliases(ctx, other, []SeriesAlias{{Title: "Pokemon Horizons", Country: "US"}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.SeriesAliasesFor(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 aliases for the series, got %d: %+v", len(got), got)
	}
	for _, a := range got {
		if a.SeriesID != sid {
			t.Fatalf("alias scoped to the wrong series: %+v", a)
		}
	}

	// Replace is a full replacement, scoped to one series: the other series' row survives.
	if err := s.ReplaceSeriesAliases(ctx, sid, []SeriesAlias{{Title: "Pokemon", Country: "US", Type: "alternative spelling"}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.SeriesAliasesFor(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Pokemon" {
		t.Fatalf("replace should leave exactly the new alias, got %+v", got)
	}
	all, err := s.AllSeriesAliases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("replace must not touch other series: want 2 rows library-wide, got %d: %+v", len(all), all)
	}

	// Deleting the series cascades its aliases away.
	if err := s.DeleteSeries(ctx, sid); err != nil {
		t.Fatal(err)
	}
	all, err = s.AllSeriesAliases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].SeriesID != other {
		t.Fatalf("aliases must cascade on series delete, got %+v", all)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/store/ -run TestSeriesAliasesReplaceAndRead`
Expected: FAIL to COMPILE — `undefined: SeriesAlias`.

- [ ] **Step 3: Create the migration**

Create `internal/core/database/migrations/0009_series_aliases.sql`:

```sql
CREATE TABLE series_aliases (
  id         INTEGER PRIMARY KEY,
  series_id  INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  country    TEXT NOT NULL DEFAULT '',
  type       TEXT NOT NULL DEFAULT '',
  UNIQUE(series_id, title)
);
CREATE INDEX idx_series_aliases_series ON series_aliases(series_id);
```

- [ ] **Step 4: Implement the store methods**

Add to `internal/core/store/media_store.go`:

```go
// SeriesAlias is an alternative title for a series, used to match releases whose
// scene name differs from the primary title (e.g. "Pokémon: Indigo League").
// Country and Type are TMDB metadata, stored but not interpreted: Type is free
// text ("season 1", "23th season in Catalan") and is deliberately not parsed.
type SeriesAlias struct {
	ID       int64  `json:"id"`
	SeriesID int64  `json:"seriesId"`
	Title    string `json:"title"`
	Country  string `json:"country"`
	Type     string `json:"type"`
}

// ReplaceSeriesAliases swaps the whole alias set for one series in a transaction,
// so a refresh that drops an alias upstream drops it here too. Scoped to the one
// series: other series' aliases are untouched.
func (s *Store) ReplaceSeriesAliases(ctx context.Context, seriesID int64, aliases []SeriesAlias) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM series_aliases WHERE series_id=?`, seriesID); err != nil {
		return err
	}
	for _, a := range aliases {
		if a.Title == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO series_aliases (series_id, title, country, type) VALUES (?, ?, ?, ?)
			 ON CONFLICT(series_id, title) DO NOTHING`,
			seriesID, a.Title, a.Country, a.Type); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SeriesAliasesFor(ctx context.Context, seriesID int64) ([]SeriesAlias, error) {
	return s.scanAliases(ctx, `SELECT id, series_id, title, country, type FROM series_aliases WHERE series_id=? ORDER BY id`, seriesID)
}

// AllSeriesAliases returns every alias in the library, for building the
// release-to-series title index in one query rather than one per series.
func (s *Store) AllSeriesAliases(ctx context.Context) ([]SeriesAlias, error) {
	return s.scanAliases(ctx, `SELECT id, series_id, title, country, type FROM series_aliases ORDER BY series_id, id`)
}

func (s *Store) scanAliases(ctx context.Context, q string, args ...any) ([]SeriesAlias, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeriesAlias
	for rows.Next() {
		var a SeriesAlias
		if err := rows.Scan(&a.ID, &a.SeriesID, &a.Title, &a.Country, &a.Type); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Fix the migration-count assertion**

`internal/core/database/database_test.go:32` hardcodes the applied-migration count. A previous wave broke this exact assertion. Change `8` to `9` in both the comparison and the message.

- [ ] **Step 6: Run the tests**

Run: `go test -count=1 ./internal/core/store/ ./internal/core/database/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/core/database internal/core/store
git commit -m "feat(store): series_aliases table and read/write helpers"
```

---

### Task 2: Fetch alternative titles from TMDB

**Files:**
- Modify: `internal/core/provider/metadata.go`
- Modify: `internal/media/tmdb.go`
- Test: `internal/media/tmdb_test.go`

**Interfaces:**
- Produces: `provider.SeriesAlias{Title, Country, Type string}`; `SeriesMetadata.Aliases []SeriesAlias`.

- [ ] **Step 1: Write the failing test**

Read `internal/media/tmdb_test.go` first for its existing `httptest` pattern and reuse it; `newTMDB(apiKey, baseURL, httpClient)` (`tmdb.go:33`) is the seam for pointing the client at a test server. Add a test that serves both `/tv/60572` and `/tv/60572/alternative_titles` and asserts the aliases land on the metadata:

```go
func TestTVDetailsFetchesAlternativeTitles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tv/60572":
			w.Write([]byte(`{"id":60572,"name":"Pokémon","seasons":[]}`))
		case "/tv/60572/alternative_titles":
			w.Write([]byte(`{"id":60572,"results":[
				{"iso_3166_1":"US","title":"Pokémon: Indigo League","type":"season 1"},
				{"iso_3166_1":"JP","title":"Pocket Monsters","type":""}]}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	md, err := newTMDB("k", srv.URL, srv.Client()).TVDetails(context.Background(), 60572)
	if err != nil {
		t.Fatal(err)
	}
	if len(md.Aliases) != 2 {
		t.Fatalf("want 2 aliases, got %+v", md.Aliases)
	}
	if md.Aliases[0].Title != "Pokémon: Indigo League" || md.Aliases[0].Country != "US" || md.Aliases[0].Type != "season 1" {
		t.Fatalf("alias fields not mapped: %+v", md.Aliases[0])
	}
}

// An alias-endpoint failure must not fail the whole detail fetch: the series is
// still usable, it just has no aliases until the next refresh.
func TestTVDetailsSurvivesAlternativeTitlesFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tv/60572/alternative_titles" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"id":60572,"name":"Pokémon","seasons":[]}`))
	}))
	defer srv.Close()

	md, err := newTMDB("k", srv.URL, srv.Client()).TVDetails(context.Background(), 60572)
	if err != nil {
		t.Fatalf("alias failure must not fail TVDetails: %v", err)
	}
	if md.Title != "Pokémon" {
		t.Fatalf("series metadata should still be populated, got %+v", md)
	}
	if len(md.Aliases) != 0 {
		t.Fatalf("want no aliases on failure, got %+v", md.Aliases)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/media/ -run TestTVDetails`
Expected: FAIL to COMPILE — `md.Aliases` undefined.

- [ ] **Step 3: Add the provider type**

In `internal/core/provider/metadata.go`, add the type and the field on `SeriesMetadata` (after `Seasons`):

```go
// SeriesAlias is an alternative title for a series. Type is free-form provider
// metadata ("season 1", "alternative spelling") and is not interpreted.
type SeriesAlias struct {
	Title   string
	Country string
	Type    string
}
```

```go
	Seasons    []SeasonMetadata
	Aliases    []SeriesAlias
```

- [ ] **Step 4: Fetch the aliases**

In `internal/media/tmdb.go`, add the response type beside `tmdbTVDetails`:

```go
type tmdbAltTitles struct {
	Results []struct {
		Country string `json:"iso_3166_1"`
		Title   string `json:"title"`
		Type    string `json:"type"`
	} `json:"results"`
}
```

In `TVDetails`, after the `provider.SeriesMetadata` value `s` is built and before the season loop, add:

```go
	// Aliases are best-effort: a failure here must not fail the series add or
	// refresh that called us. The series simply has no aliases until next time.
	var alt tmdbAltTitles
	if err := c.get(ctx, "/tv/"+strconv.Itoa(tmdbID)+"/alternative_titles", nil, &alt); err != nil {
		slog.Warn("tmdb: alternative titles lookup failed", "tmdbId", tmdbID, "err", err)
	} else {
		for _, a := range alt.Results {
			s.Aliases = append(s.Aliases, provider.SeriesAlias{Title: a.Title, Country: a.Country, Type: a.Type})
		}
	}
```

Add `"log/slog"` to the imports if not already present.

- [ ] **Step 5: Run them to verify they pass**

Run: `go test -count=1 ./internal/media/ -run TestTVDetails`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/provider internal/media
git commit -m "feat(media): fetch series alternative titles from TMDB"
```

---

### Task 3: Persist aliases on add and refresh

**Files:**
- Modify: `internal/media/media.go` (`AddSeries` ~:160, `RefreshSeries` ~:245)
- Test: `internal/media/service_test.go`

**Interfaces:**
- Consumes: Task 1's `store.ReplaceSeriesAliases`; Task 2's `provider.SeriesMetadata.Aliases`.

- [ ] **Step 1: Write the failing test**

`fakeProvider` (`service_test.go:27`) returns `f.series` from `TVDetails`, so set `Aliases` on that fixture. `newTestService(t, fp)` returns `(*Service, *store.Store)`.

```go
func TestAddSeriesPersistsAliases(t *testing.T) {
	fp := &fakeProvider{series: provider.SeriesMetadata{
		TMDBID: 100, Title: "Pokémon",
		Aliases: []provider.SeriesAlias{
			{Title: "Pokémon: Indigo League", Country: "US", Type: "season 1"},
			{Title: "Pocket Monsters", Country: "JP"},
		},
	}}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	rf := mustRootFolder(t, st)
	prof := mustQualityProfile(t, st)

	se, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, RootFolderID: &rf, QualityProfileID: &prof, MonitorOption: MonitorAll})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.SeriesAliasesFor(ctx, se.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 aliases persisted on add, got %+v", got)
	}
}

func TestRefreshSeriesReplacesAliases(t *testing.T) {
	fp := &fakeProvider{series: provider.SeriesMetadata{
		TMDBID: 100, Title: "Pokémon",
		Aliases: []provider.SeriesAlias{{Title: "Old Alias", Country: "US"}},
	}}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	rf := mustRootFolder(t, st)
	prof := mustQualityProfile(t, st)
	se, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, RootFolderID: &rf, QualityProfileID: &prof, MonitorOption: MonitorAll})
	if err != nil {
		t.Fatal(err)
	}

	// Upstream drops the old alias and adds a new one.
	fp.series.Aliases = []provider.SeriesAlias{{Title: "Pocket Monsters", Country: "JP"}}
	if err := svc.RefreshSeries(ctx, se.ID); err != nil {
		t.Fatal(err)
	}
	got, err := st.SeriesAliasesFor(ctx, se.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Pocket Monsters" {
		t.Fatalf("refresh must replace the alias set, got %+v", got)
	}
}
```

**Verify the helper names before writing:** `mustRootFolder`/`mustQualityProfile` are used in this package's existing tests — confirm their exact names and signatures in `service_test.go` and use whatever is actually there. If they do not exist, create the root folder and quality profile inline with `st.CreateRootFolder` and `st.CreateQualityProfile`, matching a neighbouring test.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/media/ -run 'TestAddSeriesPersistsAliases|TestRefreshSeriesReplacesAliases'`
Expected: FAIL — 0 aliases persisted.

- [ ] **Step 3: Persist on add**

In `AddSeries`, immediately after the `CreateSeries` error handling and before the `for _, sn := range md.Seasons` loop:

```go
	if err := s.store.ReplaceSeriesAliases(ctx, id, toStoreAliases(id, md.Aliases)); err != nil {
		slog.Warn("media: storing series aliases failed", "seriesId", id, "err", err)
	}
```

In `RefreshSeries`, immediately after the `UpdateSeries` error handling:

```go
	if err := s.store.ReplaceSeriesAliases(ctx, id, toStoreAliases(id, md.Aliases)); err != nil {
		slog.Warn("media: refreshing series aliases failed", "seriesId", id, "err", err)
	}
```

Add the converter near the bottom of `media.go`:

```go
// toStoreAliases maps provider aliases onto store rows. Alias persistence is
// best-effort at both call sites: a failure is logged, never fatal, because a
// series that exists without aliases still works via primary-title matching.
func toStoreAliases(seriesID int64, in []provider.SeriesAlias) []store.SeriesAlias {
	out := make([]store.SeriesAlias, 0, len(in))
	for _, a := range in {
		out = append(out, store.SeriesAlias{SeriesID: seriesID, Title: a.Title, Country: a.Country, Type: a.Type})
	}
	return out
}
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test -count=1 ./internal/media/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/media
git commit -m "feat(media): persist series aliases on add and refresh"
```

---

### Task 4: Alias-aware series matching

**Files:**
- Create: `internal/automation/match.go`
- Create: `internal/automation/match_test.go`
- Modify: `internal/automation/search.go` (remove the old `releaseIsForSeries`, thread the index)
- Modify: `internal/automation/upgrade.go`
- Test: `internal/automation/search_test.go`, `internal/automation/upgrade_test.go`

**Interfaces:**
- Consumes: Task 1's `store.AllSeriesAliases`.
- Produces:
  - `type titleIndex map[string][]int64`
  - `func (s *Service) buildTitleIndex(ctx context.Context) (titleIndex, error)`
  - `func releaseIsForSeries(se *store.Series, p parsing.ParsedRelease, ti titleIndex) bool`

  Task 7 calls `releaseIsForSeries` with the same signature. The three grab paths thread `ti` exactly as they already thread `bud *budget`.

- [ ] **Step 1: Write the failing unit test**

Create `internal/automation/match_test.go`:

```go
package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

func parsedTV(title string) parsing.ParsedRelease { return parsing.Parse(title, provider.KindTV) }

func TestReleaseIsForSeries(t *testing.T) {
	se := &store.Series{ID: 1, Title: "Pokémon"}
	// Series 1 owns two aliases; series 2 is a different show in the library.
	ti := titleIndex{
		"pokemon":               {1},
		"pokemon indigo league": {1},
		"pocket monsters":       {1},
		"pokemon horizons":      {2},
		"shared name":           {1, 2},
	}
	cases := []struct {
		name  string
		title string
		want  bool
	}{
		{"primary title", "Pokemon.S01E01.1080p.WEB-DL.x264-GRP", true},
		{"accented primary vs ascii release", "Pokemon.S01E01.720p.HDTV.x264-GRP", true},
		{"alias", "Pokmon.Indigo.League.s01e01", true},
		{"different show", "Pokemon.Trainer.Tour.S01E01.1080p.WEB-DL.x264-GRP", false},
		{"another library series", "Pokemon.Horizons.S01E01.1080p.WEB-DL.x264-GRP", false},
		{"ambiguous across two series", "Shared.Name.S01E01.1080p.WEB-DL.x264-GRP", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := releaseIsForSeries(se, parsedTV(tc.title), ti); got != tc.want {
				t.Fatalf("releaseIsForSeries(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

// A series whose aliases were never fetched must still match its own primary
// title, rather than failing open (accept everything) or closed (accept nothing).
func TestReleaseIsForSeriesFallsBackWithoutAliases(t *testing.T) {
	se := &store.Series{ID: 1, Title: "Pokémon"}
	empty := titleIndex{}
	if !releaseIsForSeries(se, parsedTV("Pokemon.S01E01.1080p.WEB-DL.x264-GRP"), empty) {
		t.Fatal("must fall back to primary-title matching when the index has no entry")
	}
	if releaseIsForSeries(se, parsedTV("Pokemon.Trainer.Tour.S01E01.1080p.WEB-DL.x264-GRP"), empty) {
		t.Fatal("fallback must still reject a different show")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/automation/ -run TestReleaseIsForSeries`
Expected: FAIL to COMPILE — `undefined: titleIndex`.

- [ ] **Step 3: Implement the matcher**

Create `internal/automation/match.go`:

```go
package automation

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

// titleIndex maps a normalized title to every series it could refer to. It is
// built from primary titles AND aliases, so a release named for an alternate
// scene title ("Pokemon.Indigo.League") still resolves to its series. A key with
// more than one series id is ambiguous.
type titleIndex map[string][]int64

// buildTitleIndex loads the whole library's titles in two queries. Callers build
// it once per search entry point and thread it down, exactly as they thread the
// per-series budget.
func (s *Service) buildTitleIndex(ctx context.Context) (titleIndex, error) {
	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return nil, err
	}
	ti := titleIndex{}
	add := func(title string, id int64) {
		k := normTitle(title)
		if k == "" {
			return
		}
		for _, existing := range ti[k] {
			if existing == id {
				return
			}
		}
		ti[k] = append(ti[k], id)
	}
	for _, se := range series {
		add(se.Title, se.ID)
	}
	aliases, err := s.store.AllSeriesAliases(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range aliases {
		add(a.Title, a.SeriesID)
	}
	return ti, nil
}

// releaseIsForSeries reports whether a parsed release belongs to se.
//
// Search results cannot be trusted to be on-target: newznab matches its `q` term
// loosely, and Nexus cannot scope a TV search server-side (it sends a tmdbid, but
// newznab TV is keyed on tvdbid, which Nexus does not store — and a probe showed
// that even a tvdbid-scoped query still returns sibling shows). Without this
// check the best-SCORING release wins regardless of which show it belongs to.
//
// Matching is exact on the normalized title, never a prefix: a prefix test would
// re-accept "Pokemon Trainer Tour" for a series called "Pokemon".
func releaseIsForSeries(se *store.Series, p parsing.ParsedRelease, ti titleIndex) bool {
	key := normTitle(p.Title)
	if key == "" {
		return false
	}
	ids, known := ti[key]
	if !known {
		// No index entry: the library has never seen this title. Fall back to the
		// series' own primary title so a series whose aliases were never fetched
		// still matches its own releases.
		return key == normTitle(se.Title)
	}
	if len(ids) > 1 {
		return false // ambiguous across the library — refuse rather than guess
	}
	return ids[0] == se.ID
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/automation/ -run TestReleaseIsForSeries`
Expected: PASS (8 subtests across the two functions).

- [ ] **Step 5: Delete the old matcher and thread the index**

Delete the existing `releaseIsForSeries` from `internal/automation/search.go` (added in `481dc7e`) — the new one in `match.go` replaces it. Then thread `ti titleIndex` through the same call chain that already carries `bud *budget`:

- `searchSeries`, `searchSeasonEntry`, `searchEpisodeEntry`: build it once with
  `ti, err := s.buildTitleIndex(ctx)` (returning the error) alongside the existing `s.Config(ctx)` call, and pass it down.
- `searchSeason(ctx, se, seasonNumber, eps, profile, activeEps, bud, ti)` and
  `searchEpisode(ctx, se, e, profile, activeEps, bud, ti)` gain a trailing `ti titleIndex` parameter.
- `upgradeSweep` builds it once before the series loop and `upgradeEpisode` gains the same trailing parameter.

Every existing call to `releaseIsForSeries(se, c.Parsed)` becomes `releaseIsForSeries(se, c.Parsed, ti)`. There are three: `searchSeason`'s pack branch, `searchEpisode`, and `upgradeEpisode`.

- [ ] **Step 6: Add path-level tests**

The unit test above proves the matcher; these prove it is actually wired in at each path. Add to `internal/automation/search_test.go`:

```go
// seedAliasedSeries gives the series an alias so a release named for the alias
// resolves to it, and a same-numbered release of another show does not.
func seedAliasedSeries(t *testing.T, st *store.Store) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 3)
	if err := st.ReplaceSeriesAliases(ctx, sid, []store.SeriesAlias{{Title: "The Show Alternate", Country: "US"}}); err != nil {
		t.Fatal(err)
	}
	return sid, epIDs
}

func TestSearchEpisodeAcceptsAliasNamedRelease(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	_, epIDs := seedAliasedSeries(t, st)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.Alternate.S01E01.1080p.WEB-DL.x264-GRP", DownloadURL: "alias", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchEpisode(ctx, epIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "alias" {
		t.Fatalf("an alias-named release must be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
}

func TestSearchSeasonPackAcceptsAliasNamedRelease(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedAliasedSeries(t, st)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.Alternate.S01.1080p.BluRay.x264-GRP", DownloadURL: "aliaspack", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(ctx, sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "aliaspack" {
		t.Fatalf("an alias-named pack must be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
}
```

Add to `internal/automation/upgrade_test.go`:

```go
func TestUpgradeEpisodeAcceptsAliasNamedRelease(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedUpgradableSeries(t, st, 1)
	if err := st.ReplaceSeriesAliases(ctx, sid, []store.SeriesAlias{{Title: "The Show Alternate", Country: "US"}}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.Alternate.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "alias", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("an alias-named upgrade must be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
}
```

- [ ] **Step 7: Mutation-verify**

Apply each mutation, run the named test, confirm it FAILS, then revert. Report any that come back green.

1. In `buildTitleIndex`, skip the alias loop entirely → all three alias tests must fail.
2. In `releaseIsForSeries`, drop the `len(ids) > 1` ambiguity guard → `TestReleaseIsForSeries/ambiguous_across_two_series` must fail.
3. Change the no-entry fallback to `return true` → `TestReleaseIsForSeriesFallsBackWithoutAliases` must fail on its second assertion.

- [ ] **Step 8: Run the package suite**

Run: `go build ./... && go test -count=1 ./internal/automation/`
Expected: PASS. The pre-existing tests from `481dc7e`
(`TestSearchEpisodeRejectsReleaseFromADifferentShow`, `TestSearchEpisodePrefersTheRightShowOverABetterWrongOne`,
`TestSearchSeasonRejectsPackFromADifferentShow`, `TestUpgradeEpisodeRejectsReleaseFromADifferentShow`,
`TestSearchEpisodeMatchesAccentedSeriesTitleAgainstASCIIRelease`) must all still pass unchanged — they are the regression guard for the behaviour being replaced. If any needs modifying, STOP and report why.

- [ ] **Step 9: Commit**

```bash
git add internal/automation
git commit -m "feat(automation): match releases against series aliases"
```

---

### Task 5: Aliases in the RSS title index

**Files:**
- Modify: `internal/automation/rss.go` (`buildLibraryIndex` ~:74, `RSSSync` ~:239)
- Test: `internal/automation/rss_test.go`

**Interfaces:**
- Consumes: Task 1's `store.AllSeriesAliases`.
- Produces: `buildLibraryIndex(movies []store.Movie, series []store.Series, aliases []store.SeriesAlias) *libraryIndex`.

- [ ] **Step 1: Write the failing test**

```go
func TestRSSSyncMatchesAliasNamedRelease(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 3)
	if err := st.ReplaceSeriesAliases(ctx, sid, []store.SeriesAlias{{Title: "The Show Alternate", Country: "US"}}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.Alternate.S01E01.1080p.WEB-DL.x264-GRP", DownloadURL: "alias", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "alias" {
		t.Fatalf("RSS must resolve an alias-named release to its series: grabbed=%d reqs=%+v", res.Grabbed, fe.reqs)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/automation/ -run TestRSSSyncMatchesAliasNamedRelease`
Expected: FAIL — 0 grabbed, because `seriesByTitle` is keyed on primary titles only.

- [ ] **Step 3: Add aliases to the index**

Change the signature and populate the alias keys:

```go
func buildLibraryIndex(movies []store.Movie, series []store.Series, aliases []store.SeriesAlias) *libraryIndex {
```

At the end of the series loop in that function, after the primary-title key is added, add:

```go
	byID := map[int64]*store.Series{}
	for i := range series {
		byID[series[i].ID] = &series[i]
	}
	for _, a := range aliases {
		se, ok := byID[a.SeriesID]
		if !ok {
			continue
		}
		key := normTitle(a.Title)
		if key == "" {
			continue
		}
		idx.seriesByTitle[key] = append(idx.seriesByTitle[key], se)
	}
```

`matchSeries` already refuses when a title key yields more than one candidate and no year disambiguates, so the ambiguity behaviour needs no change.

In `RSSSync`, load the aliases beside the existing series load and pass them:

```go
	aliases, err := s.store.AllSeriesAliases(ctx)
	if err != nil {
		return res, err
	}
	idx := buildLibraryIndex(movies, eligible, aliases)
```

Update the existing `testIndex()` helper in `rss_test.go:14` to pass a third argument — `nil` preserves its current behaviour.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test -count=1 ./internal/automation/`
Expected: PASS.

- [ ] **Step 5: Mutation-verify**

Remove the alias loop from `buildLibraryIndex` → `TestRSSSyncMatchesAliasNamedRelease` must fail. Revert.

- [ ] **Step 6: Commit**

```bash
git add internal/automation
git commit -m "feat(automation): resolve RSS releases through series aliases"
```

---

### Task 6: Parse the episode title

**Files:**
- Modify: `internal/parsing/parsing.go` (add the field), `internal/parsing/parser.go`
- Test: `internal/parsing/parser_test.go`

**Interfaces:**
- Produces: `parsing.ParsedRelease.EpisodeTitle string`.

- [ ] **Step 1: Write the failing test**

```go
func TestParseEpisodeTitle(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "stops at resolution",
			title: "Pokemon.S01E01.The.Pendant.That.Starts.It.All.Part.1.1080p.WEBRip.10bit.EAC3.2.0.x265-iVy",
			want:  "The Pendant That Starts It All Part 1",
		},
		{"stops at source", "Pokemon.S01E01.DVDRip.x264-QCF", ""},
		// PDTV must be a recognised source, else extraction cuts at the codec and
		// yields "PDTV HebDub", contradicting the stored title and rejecting a
		// possibly-correct release.
		{"pdtv is a source, not an episode title", "Pokemon.S01E01.PDTV.HebDub.XviD-Sweet-Star", ""},
		{"no episode title at all", "Pokmon.Indigo.League.s01e01", ""},
		{"no season/episode marker", "Pokemon.Concierge.1080p.WEB-DL.x264-GRP", ""},
		// Single-token candidates carry no signal: a stray technical token that
		// leaks past the stop list must not become a false contradiction.
		{"single token is not a signal", "The.Show.S01E01.Untokenized", ""},
		{"no terminating token keeps the remainder", "The.Show.S01E01.A.Real Episode Name", "A Real Episode Name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.title, provider.KindTV).EpisodeTitle
			if got != tc.want {
				t.Fatalf("EpisodeTitle = %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/parsing/ -run TestParseEpisodeTitle`
Expected: FAIL to COMPILE — `EpisodeTitle` undefined.

- [ ] **Step 3: Add the field**

In `internal/parsing/parsing.go`, add to `ParsedRelease` after `Episodes`:

```go
	EpisodeTitle string     `json:"episodeTitle"`
```

- [ ] **Step 4: Add PDTV and SDTV as sources**

In `internal/parsing/parser.go`, add to `sourcePatterns` immediately after the `hdtv` entry:

```go
		{regexp.MustCompile(`(?i)\b(pdtv|sdtv)\b`), SourceHDTV},
```

`SourceHDTV` is reused deliberately: PDTV/SDTV are broadcast captures and Nexus has no finer bucket. The purpose here is that they *terminate* episode-title extraction.

- [ ] **Step 5: Extract the episode title**

In `parser.go`, inside the `if kind == provider.KindTV` block, in the branch where `reSeasonEp` matched (after the episode numbers are collected), add:

```go
			p.EpisodeTitle = episodeTitleFrom(title[reSeasonEp.FindStringIndex(title)[1]:])
```

Add the helper below `cleanTitle`:

```go
// episodeTitleFrom extracts an episode title from the part of a release name
// that follows the S##E## marker, cutting at the first technical token
// (resolution, source or codec). It is deliberately conservative: it returns ""
// whenever the result is not clearly a title, because a wrong episode title
// causes a false rejection and a missing one merely costs a signal.
func episodeTitleFrom(rest string) string {
	cut := len(rest)
	if m := reResolution.FindStringIndex(rest); m != nil && m[0] < cut {
		cut = m[0]
	}
	if m := reCodec.FindStringIndex(rest); m != nil && m[0] < cut {
		cut = m[0]
	}
	for _, sp := range sourcePatterns {
		if m := sp.re.FindStringIndex(rest); m != nil && m[0] < cut {
			cut = m[0]
		}
	}
	words := strings.FieldsFunc(rest[:cut], func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == ' '
	})
	// A single token is not a title: it is far more likely to be a technical
	// token that leaked past the cuts above.
	alpha := 0
	for _, w := range words {
		if len(w) >= 3 && strings.IndexFunc(w, unicode.IsLetter) >= 0 {
			alpha++
		}
	}
	if alpha < 2 {
		return ""
	}
	return strings.Join(words, " ")
}
```

Add `"unicode"` to the imports.

- [ ] **Step 6: Run it to verify it passes**

Run: `go test -count=1 ./internal/parsing/`
Expected: PASS, including the pre-existing parser tests.

- [ ] **Step 7: Commit**

```bash
git add internal/parsing
git commit -m "feat(parsing): extract episode titles and recognise PDTV/SDTV"
```

---

### Task 7: Episode-title contradiction, wired in

**Files:**
- Modify: `internal/automation/match.go`
- Modify: `internal/automation/search.go`, `internal/automation/upgrade.go`
- Test: `internal/automation/match_test.go`, `search_test.go`, `upgrade_test.go`

**Interfaces:**
- Consumes: Task 6's `parsing.ParsedRelease.EpisodeTitle`; Task 4's `releaseIsForSeries` and threaded `ti`.
- Produces: `func episodeTitleContradicts(stored string, p parsing.ParsedRelease) bool`.

- [ ] **Step 1: Write the failing unit test**

Add to `internal/automation/match_test.go`:

```go
func TestEpisodeTitleContradicts(t *testing.T) {
	const stored = "Pokémon - I Choose You!"
	cases := []struct {
		name  string
		title string
		want  bool
	}{
		{"different show's episode title", "Pokemon.S01E01.The.Pendant.That.Starts.It.All.Part.1.1080p.WEBRip.x265-iVy", true},
		{"matching episode title", "Pokemon.S01E01.I.Choose.You.1080p.WEB-DL.x264-GRP", false},
		{"no episode title in the release", "Pokemon.S01E01.DVDRip.x264-QCF", false},
		{"technical tokens only", "Pokemon.S01E01.PDTV.HebDub.XviD-Sweet-Star", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := episodeTitleContradicts(stored, parsedTV(tc.title)); got != tc.want {
				t.Fatalf("episodeTitleContradicts(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

// With no stored episode title there is nothing to contradict.
func TestEpisodeTitleContradictsNeedsBothSides(t *testing.T) {
	if episodeTitleContradicts("", parsedTV("Pokemon.S01E01.The.Pendant.That.Starts.It.All.1080p.x265-iVy")) {
		t.Fatal("an empty stored title must never contradict")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/automation/ -run TestEpisodeTitleContradicts`
Expected: FAIL to COMPILE — `undefined: episodeTitleContradicts`.

- [ ] **Step 3: Implement it**

Add to `internal/automation/match.go`:

```go
// episodeTitleContradicts reports whether a release's episode title positively
// contradicts the stored one.
//
// This is the only automatic signal that separates two shows sharing a scene
// name: "Pokemon.S01E01.The.Pendant.That.Starts.It.All" is Pokémon Horizons,
// while the monitored series' S01E01 is "Pokémon - I Choose You!". No
// series-title comparison can tell them apart.
//
// It vetoes ONLY on positive evidence. An absent or unrecognisable episode title
// on either side yields false, because a false rejection means nothing downloads
// while a missed signal merely falls through to the other checks.
func episodeTitleContradicts(stored string, p parsing.ParsedRelease) bool {
	got := normTitle(p.EpisodeTitle)
	want := normTitle(stored)
	if got == "" || want == "" {
		return false
	}
	// Either containing the other counts as agreement: release names abbreviate
	// ("I Choose You" vs "Pokémon - I Choose You!") and may carry a trailing
	// release-group suffix.
	if strings.Contains(want, got) || strings.Contains(got, want) {
		return false
	}
	return true
}
```

Add `"strings"` to `match.go`'s imports.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/automation/ -run TestEpisodeTitleContradicts`
Expected: PASS.

- [ ] **Step 5: Wire it into the three paths**

In `searchEpisode` (`search.go`), inside the candidate loop, immediately after the existing `releaseIsForSeries` guard:

```go
		if episodeTitleContradicts(e.Title, c.Parsed) {
			continue
		}
```

In `upgradeEpisode` (`upgrade.go`), the same line in its candidate loop, also keyed on `e.Title`.

The `searchSeason` **pack branch takes no episode-title check**: a season pack covers many episodes and carries no single episode title, so there is nothing to compare against. Do not add one there.

- [ ] **Step 6: Add the saga regression test**

This is the guard for the whole incident. Add to `internal/automation/search_test.go`:

```go
// Seeded from the real production incident: a series monitoring Pokémon (1997)
// grabbed and imported episodes of Pokémon Horizons and Pokémon Trainer Tour.
// Every candidate below was returned by the user's real indexer for this search.
func TestSagaPokemonGrabsOnlyTheRightShow(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	prof, err := st.CreateQualityProfile(ctx, hdProfile())
	if err != nil {
		t.Fatal(err)
	}
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 60572, Title: "Pokémon", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesQualityProfileID(ctx, sid, &prof.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSeriesAliases(ctx, sid, []store.SeriesAlias{
		{Title: "Pokémon: Indigo League", Country: "US", Type: "season 1"},
		{Title: "Pokemon", Country: "US", Type: "alternative spelling"},
		{Title: "Pocket Monsters", Country: "JP"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSeason(ctx, store.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{
		SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true,
		Title: "Pokémon - I Choose You!", AirDate: "1997-04-01",
	}); err != nil {
		t.Fatal(err)
	}
	eps, err := st.ListEpisodes(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}

	fs := &fakeSearcher{releases: []provider.Release{
		// Wrong show, and the highest quality — this is what actually got grabbed.
		{Title: "Pokemon.S01E01.The.Pendant.That.Starts.It.All.Part.1.1080p.WEBRip.10bit.EAC3.2.0.x265-iVy", DownloadURL: "horizons", Protocol: provider.ProtocolUsenet},
		// Wrong show, different name.
		{Title: "Pokemon.Trainer.Tour.S01E01.Creative.Evolution.1080p.AMZN.WEB-DL.DDP5.1.H.264-BurCyg", DownloadURL: "trainertour", Protocol: provider.ProtocolUsenet},
		// Right show, named by alias, no episode title to check.
		{Title: "Pokmon.Indigo.League.s01e01.1080p.WEB-DL.x264-GRP", DownloadURL: "indigo", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchEpisode(ctx, eps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("want exactly 1 grab, got n=%d reqs=%+v", n, fe.reqs)
	}
	if fe.reqs[0].DownloadURL != "indigo" {
		t.Fatalf("must grab the right show's release, got %q", fe.reqs[0].DownloadURL)
	}
}
```

- [ ] **Step 7: Mutation-verify**

Apply each, run the named test, confirm FAIL, revert. Report any that come back green.

1. Make `episodeTitleContradicts` always return false → `TestSagaPokemonGrabsOnlyTheRightShow` must fail (it grabs `horizons`), as must `TestEpisodeTitleContradicts/different_show's_episode_title`.
2. Remove the `strings.Contains` agreement check (always contradict when both present) → `TestEpisodeTitleContradicts/matching_episode_title` must fail.
3. Remove the wiring line from `searchEpisode` only → the saga test must fail while the unit test still passes. This is the check that the guard is actually installed, not merely written.

- [ ] **Step 8: Full verification**

Run: `go build ./... && go vet ./... && go test -count=1 ./...`
Expected: PASS across all packages.

- [ ] **Step 9: Commit**

```bash
git add internal/automation
git commit -m "feat(automation): reject releases whose episode title contradicts the stored one"
```

---

## Verification Checklist

After Task 7, confirm each spec requirement:

- [ ] Aliases fetched from TMDB `/tv/{id}/alternative_titles` and stored (spec §3.1, §3.2)
- [ ] Alias fetch failure does not fail a series add or refresh (spec §3.3)
- [ ] Existing series backfill on refresh — verified by `TestRefreshSeriesReplacesAliases` (spec §3.3)
- [ ] Release matches on primary title OR alias (spec §3.4)
- [ ] Ambiguous match across two series is rejected (spec §3.4)
- [ ] A series with no aliases falls back to primary-title matching (spec §3.4)
- [ ] `EpisodeTitle` extraction matches the spec's four worked examples (spec §4.1)
- [ ] PDTV/SDTV recognised as sources; single-token candidates yield no signal (spec §4.1.1)
- [ ] Contradiction vetoes only on positive evidence (spec §4.2)
- [ ] Both checks applied at `searchEpisode`, `searchSeason` pack branch, `upgradeEpisode` (spec §5)
- [ ] RSS resolves alias-named releases (spec §5)
- [ ] The saga regression test passes (spec §6)
- [ ] Migration count assertion in `database_test.go` updated 8 → 9
- [ ] `go build ./... && go vet ./... && go test -count=1 ./...` all pass
- [ ] No `web/dist` change — this sub-project is backend only
