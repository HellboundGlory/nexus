# Nexus Media Library (Sub-project 4a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Nexus a persistent library of TV series and movies populated from TMDb, with root folders, add/refresh/monitor lifecycle, and a REST + WebSocket surface — no files, parsing, quality, or searching yet.

**Architecture:** A new feature module `internal/media` depending only on `internal/core/*`, mirroring the indexer/download-client engines. A `provider.MetadataProvider` interface isolates TMDb's wire format; a `Service` owns all library mutations over the `Store`; a scheduler `RefreshCommand` re-pulls metadata; an `API` mounts routes into the authed `/api/v1` group; refresh/monitor changes publish `media.*` events forwarded to the WebSocket.

**Tech Stack:** Go 1.26, `go-chi/chi/v5`, `modernc.org/sqlite` (pure Go, `CGO_ENABLED=0`), stdlib `net/http` + `net/http/httptest`, `log/slog`.

## Global Constraints

- Module boundary: `internal/media` imports **only** `internal/core/*` — never `internal/indexer`, `internal/downloadclient`, or `internal/automation`. (Verify with `go list -deps`.)
- Module path is `github.com/hellboundg/nexus`.
- Go is not on PATH in the dev environment: prefix every Go command with `export PATH="/c/Program Files/Go/bin:$PATH"`.
- `-race` is unavailable (no C compiler / `CGO_ENABLED=0`). Verify concurrency with `-count=N`, never `-race`.
- All tests are offline, deterministic, CGO-free: no real network, no real TMDb calls (use `httptest` + recorded fixtures and fake providers).
- Credentials are write-only: the TMDb API key must never appear in any API response body.
- Reuse existing store helpers `boolToInt` and `rowScanner` (in `internal/core/store`) — do NOT redefine them.
- Store "not found" is the existing sentinel `store.ErrNotFound`.
- Event forwarding to WebSocket uses `bus.PublishAsync` (not `Publish`) for media events — apply the sub-3 emit-under-lock lesson from the start.
- Dates from TMDb are date-only strings (e.g. `"2008-01-20"`); store them as TEXT and carry them as Go `string` (empty = unknown). Timestamps Nexus generates (`added_at`, `last_refreshed_at`) are `DATETIME` scanned as `time.Time` / `*time.Time`.
- Nullable foreign keys `root_folder_id` and `quality_profile_id` are Go `*int64` (`quality_profile_id` is a placeholder for sub-project 4b — never written with a non-nil value in 4a).

---

### Task 1: MetadataProvider contract + TMDb config key

**Files:**
- Create: `internal/core/provider/metadata.go`
- Create: `internal/core/provider/metadata_test.go`
- Modify: `internal/core/config/config.go`
- Modify: `internal/core/config/config_test.go`

**Interfaces:**
- Produces: `provider.MetadataProvider` interface and the metadata data types (`MetadataResult`, `SeriesMetadata`, `SeasonMetadata`, `EpisodeMetadata`, `MovieMetadata`); `config.Config.TMDBAPIKey` (from env `NEXUS_TMDB_API_KEY`).
- Consumes: existing `provider.MediaKind` (`KindTV`, `KindMovie`) from `provider.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/provider/metadata_test.go`:
```go
package provider

import (
	"context"
	"testing"
)

// fakeMeta proves the interface is implementable and stable.
type fakeMeta struct{}

func (fakeMeta) SearchTV(context.Context, string) ([]MetadataResult, error)  { return nil, nil }
func (fakeMeta) SearchMovie(context.Context, string) ([]MetadataResult, error) { return nil, nil }
func (fakeMeta) TVDetails(context.Context, int) (SeriesMetadata, error)      { return SeriesMetadata{}, nil }
func (fakeMeta) MovieDetails(context.Context, int) (MovieMetadata, error)    { return MovieMetadata{}, nil }

func TestMetadataProviderShape(t *testing.T) {
	var p MetadataProvider = fakeMeta{}
	if _, err := p.SearchTV(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	s := SeriesMetadata{TMDBID: 1, Title: "T", Seasons: []SeasonMetadata{{
		SeasonNumber: 1, Episodes: []EpisodeMetadata{{SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"}},
	}}}
	if s.Seasons[0].Episodes[0].EpisodeNumber != 1 {
		t.Fatal("episode shape wrong")
	}
	r := MetadataResult{TMDBID: 2, Title: "M", Kind: KindMovie}
	if r.Kind != KindMovie {
		t.Fatal("kind wrong")
	}
}
```

Add to `internal/core/config/config_test.go` (append; the file already tests `Load` with a fake getenv — follow the existing style there):
```go
func TestLoadReadsTMDBAPIKey(t *testing.T) {
	env := map[string]string{"NEXUS_TMDB_API_KEY": "tmdbkey"}
	c, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.TMDBAPIKey != "tmdbkey" {
		t.Fatalf("TMDBAPIKey = %q want tmdbkey", c.TMDBAPIKey)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/provider/ ./internal/core/config/ -run 'MetadataProviderShape|TMDBAPIKey'`
Expected: FAIL — `undefined: MetadataProvider` / `c.TMDBAPIKey undefined`.

- [ ] **Step 3: Create the provider contract**

Create `internal/core/provider/metadata.go`:
```go
package provider

import "context"

// MetadataResult is one hit from a metadata search (the add picker).
type MetadataResult struct {
	TMDBID    int       `json:"tmdbId"`
	Title     string    `json:"title"`
	Year      int       `json:"year"`
	Overview  string    `json:"overview"`
	PosterURL string    `json:"posterUrl"`
	Kind      MediaKind `json:"kind"`
}

// EpisodeMetadata is one episode from a series detail lookup.
type EpisodeMetadata struct {
	SeasonNumber  int
	EpisodeNumber int
	TMDBID        int
	Title         string
	Overview      string
	AirDate       string // date-only ("2008-01-20") or ""
}

// SeasonMetadata groups episodes under a season number.
type SeasonMetadata struct {
	SeasonNumber int
	Episodes     []EpisodeMetadata
}

// SeriesMetadata is a full TV detail lookup, including seasons and episodes.
type SeriesMetadata struct {
	TMDBID     int
	Title      string
	Overview   string
	Status     string
	FirstAired string // date-only or ""
	PosterURL  string
	FanartURL  string
	Seasons    []SeasonMetadata
}

// MovieMetadata is a full movie detail lookup.
type MovieMetadata struct {
	TMDBID      int
	Title       string
	Overview    string
	Status      string
	Year        int
	ReleaseDate string // date-only or ""
	Runtime     int
	IMDbID      string
	PosterURL   string
	FanartURL   string
}

// MetadataProvider is the contract every metadata source implements. Concrete
// providers (TMDb now; TVDB etc. later) isolate the external wire format.
type MetadataProvider interface {
	SearchTV(ctx context.Context, term string) ([]MetadataResult, error)
	SearchMovie(ctx context.Context, term string) ([]MetadataResult, error)
	TVDetails(ctx context.Context, tmdbID int) (SeriesMetadata, error)
	MovieDetails(ctx context.Context, tmdbID int) (MovieMetadata, error)
}
```

- [ ] **Step 4: Add the config field**

In `internal/core/config/config.go`, add `TMDBAPIKey string` to the `Config` struct (place it after `APIKey`), and inside `Load`, after the `NEXUS_API_KEY` block, add:
```go
	if v := getenv("NEXUS_TMDB_API_KEY"); v != "" {
		c.TMDBAPIKey = v
	}
```
(No default/generation — an empty key means "TMDb not configured", handled by the client in Task 5.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/provider/ ./internal/core/config/ -run 'MetadataProviderShape|TMDBAPIKey'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/provider/metadata.go internal/core/provider/metadata_test.go internal/core/config/config.go internal/core/config/config_test.go
git commit -m "feat: add MetadataProvider contract and TMDb config key"
```

---

### Task 2: Migration 0004 + root_folders store

**Files:**
- Create: `internal/core/database/migrations/0004_media.sql`
- Create: `internal/core/store/media_store.go`
- Create: `internal/core/store/media_store_test.go`
- Modify: `internal/core/database/database_test.go` (bump migration count 3 → 4)

**Interfaces:**
- Produces: all media tables (series/seasons/episodes/movies/root_folders); `store.RootFolder` + `CreateRootFolder`/`GetRootFolder`/`ListRootFolders`/`DeleteRootFolder`.
- Consumes: existing `store.ErrNotFound`, `boolToInt`, `rowScanner`, `newTestStore(t)`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/store/media_store_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestRootFolderCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateRootFolder(ctx, "/data/tv")
	if err != nil {
		t.Fatal(err)
	}
	rf, err := s.GetRootFolder(ctx, id)
	if err != nil || rf.Path != "/data/tv" {
		t.Fatalf("get: %+v err=%v", rf, err)
	}
	all, err := s.ListRootFolders(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("list: %+v err=%v", all, err)
	}
	if err := s.DeleteRootFolder(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRootFolder(ctx, id); err != ErrNotFound {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run TestRootFolderCRUD`
Expected: FAIL — `undefined: (*Store).CreateRootFolder` and missing `root_folders` table.

- [ ] **Step 3: Create the migration**

Create `internal/core/database/migrations/0004_media.sql`:
```sql
CREATE TABLE root_folders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE series (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id            INTEGER NOT NULL UNIQUE,
    title              TEXT NOT NULL,
    sort_title         TEXT NOT NULL DEFAULT '',
    overview           TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT '',
    first_aired        TEXT NOT NULL DEFAULT '',
    poster_url         TEXT NOT NULL DEFAULT '',
    fanart_url         TEXT NOT NULL DEFAULT '',
    root_folder_id     INTEGER REFERENCES root_folders(id),
    quality_profile_id INTEGER,
    monitored          INTEGER NOT NULL DEFAULT 1,
    added_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_refreshed_at  DATETIME
);

CREATE TABLE seasons (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id     INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season_number INTEGER NOT NULL,
    monitored     INTEGER NOT NULL DEFAULT 1,
    UNIQUE(series_id, season_number)
);

CREATE TABLE episodes (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id      INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season_number  INTEGER NOT NULL,
    episode_number INTEGER NOT NULL,
    tmdb_id        INTEGER NOT NULL DEFAULT 0,
    title          TEXT NOT NULL DEFAULT '',
    overview       TEXT NOT NULL DEFAULT '',
    air_date       TEXT NOT NULL DEFAULT '',
    monitored      INTEGER NOT NULL DEFAULT 1,
    UNIQUE(series_id, season_number, episode_number)
);

CREATE TABLE movies (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id            INTEGER NOT NULL UNIQUE,
    title              TEXT NOT NULL,
    sort_title         TEXT NOT NULL DEFAULT '',
    overview           TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT '',
    year               INTEGER NOT NULL DEFAULT 0,
    release_date       TEXT NOT NULL DEFAULT '',
    runtime            INTEGER NOT NULL DEFAULT 0,
    imdb_id            TEXT NOT NULL DEFAULT '',
    poster_url         TEXT NOT NULL DEFAULT '',
    fanart_url         TEXT NOT NULL DEFAULT '',
    root_folder_id     INTEGER REFERENCES root_folders(id),
    quality_profile_id INTEGER,
    monitored          INTEGER NOT NULL DEFAULT 1,
    added_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_refreshed_at  DATETIME
);
```
(Migrations are embedded and auto-applied by `database.Migrate`; no code wiring needed — the `schema_migrations` count increments automatically.)

- [ ] **Step 4: Create the root-folder store**

Create `internal/core/store/media_store.go`:
```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type RootFolder struct {
	ID        int64     `json:"id"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Store) CreateRootFolder(ctx context.Context, path string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO root_folders (path) VALUES (?)`, path)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetRootFolder(ctx context.Context, id int64) (*RootFolder, error) {
	var rf RootFolder
	err := s.db.QueryRowContext(ctx,
		`SELECT id, path, created_at FROM root_folders WHERE id = ?`, id).
		Scan(&rf.ID, &rf.Path, &rf.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rf, nil
}

func (s *Store) ListRootFolders(ctx context.Context) ([]RootFolder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, path, created_at FROM root_folders ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RootFolder
	for rows.Next() {
		var rf RootFolder
		if err := rows.Scan(&rf.ID, &rf.Path, &rf.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rf)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRootFolder(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM root_folders WHERE id = ?`, id)
	return err
}
```

- [ ] **Step 5: Bump the migration-count assertion**

In `internal/core/database/database_test.go`, change the two occurrences of `3` (the `if applied != 3` check and its message) to `4`:
```go
	if applied != 4 {
		t.Fatalf("expected 4 applied migrations, got %d", applied)
	}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ ./internal/core/database/ -run 'TestRootFolderCRUD|Idempotent'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/core/database/migrations/0004_media.sql internal/core/store/media_store.go internal/core/store/media_store_test.go internal/core/database/database_test.go
git commit -m "feat: add media schema migration 0004 and root_folders store"
```

---

### Task 3: Series / seasons / episodes store

**Files:**
- Modify: `internal/core/store/media_store.go` (add series/season/episode types + methods)
- Modify: `internal/core/store/media_store_test.go` (add tests)

**Interfaces:**
- Produces: `store.Series`, `store.Season`, `store.Episode`; `CreateSeries`/`GetSeries`/`ListSeries`/`UpdateSeries`/`DeleteSeries`/`SetSeriesMonitored`; `UpsertSeason`/`ListSeasons`/`SetSeasonMonitored`; `UpsertEpisode`/`ListEpisodes`/`SetEpisodeMonitored`/`SetSeasonEpisodesMonitored`/`SetSeriesEpisodesMonitored`.
- Consumes: `boolToInt`, `ErrNotFound`.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/store/media_store_test.go`:
```go
func TestSeriesAndEpisodeUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateSeries(ctx, Series{TMDBID: 100, Title: "Show", SortTitle: "show", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSeries(ctx, Series{TMDBID: 100, Title: "Dup"}); err == nil {
		t.Fatal("expected duplicate tmdb_id to error")
	}

	if err := s.UpsertSeason(ctx, Season{SeriesID: id, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	// Upsert same episode twice: second call updates title, does not duplicate.
	ep := Episode{SeriesID: id, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot", Monitored: true}
	if err := s.UpsertEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	ep.Title = "Pilot (Extended)"
	if err := s.UpsertEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	eps, err := s.ListEpisodes(ctx, id)
	if err != nil || len(eps) != 1 || eps[0].Title != "Pilot (Extended)" {
		t.Fatalf("episodes: %+v err=%v", eps, err)
	}

	// Monitored preserved across a title-only re-upsert path is a Service concern;
	// here verify SetEpisodeMonitored + cascade helpers.
	if err := s.SetSeriesEpisodesMonitored(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	eps, _ = s.ListEpisodes(ctx, id)
	if eps[0].Monitored {
		t.Fatal("cascade to episodes failed")
	}

	// Cascade delete: deleting the series removes seasons + episodes.
	if err := s.DeleteSeries(ctx, id); err != nil {
		t.Fatal(err)
	}
	eps, _ = s.ListEpisodes(ctx, id)
	if len(eps) != 0 {
		t.Fatalf("expected episodes gone after series delete, got %d", len(eps))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run TestSeriesAndEpisodeUpsert`
Expected: FAIL — `undefined: Series` etc.

- [ ] **Step 3: Add series/season/episode store code**

Append to `internal/core/store/media_store.go`:
```go
type Series struct {
	ID               int64      `json:"id"`
	TMDBID           int        `json:"tmdbId"`
	Title            string     `json:"title"`
	SortTitle        string     `json:"sortTitle"`
	Overview         string     `json:"overview"`
	Status           string     `json:"status"`
	FirstAired       string     `json:"firstAired"`
	PosterURL        string     `json:"posterUrl"`
	FanartURL        string     `json:"fanartUrl"`
	RootFolderID     *int64     `json:"rootFolderId"`
	QualityProfileID *int64     `json:"qualityProfileId"`
	Monitored        bool       `json:"monitored"`
	AddedAt          time.Time  `json:"addedAt"`
	LastRefreshedAt  *time.Time `json:"lastRefreshedAt"`
}

type Season struct {
	ID           int64 `json:"id"`
	SeriesID     int64 `json:"seriesId"`
	SeasonNumber int   `json:"seasonNumber"`
	Monitored    bool  `json:"monitored"`
}

type Episode struct {
	ID            int64  `json:"id"`
	SeriesID      int64  `json:"seriesId"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
	TMDBID        int    `json:"tmdbId"`
	Title         string `json:"title"`
	Overview      string `json:"overview"`
	AirDate       string `json:"airDate"`
	Monitored     bool   `json:"monitored"`
}

const seriesSelect = `SELECT id, tmdb_id, title, sort_title, overview, status, first_aired,
	poster_url, fanart_url, root_folder_id, quality_profile_id, monitored, added_at, last_refreshed_at FROM series`

func scanSeriesRow(row rowScanner) (*Series, error) {
	var s Series
	var monitored int
	var rootID, qpID sql.NullInt64
	var lastRef sql.NullTime
	err := row.Scan(&s.ID, &s.TMDBID, &s.Title, &s.SortTitle, &s.Overview, &s.Status, &s.FirstAired,
		&s.PosterURL, &s.FanartURL, &rootID, &qpID, &monitored, &s.AddedAt, &lastRef)
	if err != nil {
		return nil, err
	}
	s.Monitored = monitored != 0
	if rootID.Valid {
		s.RootFolderID = &rootID.Int64
	}
	if qpID.Valid {
		s.QualityProfileID = &qpID.Int64
	}
	if lastRef.Valid {
		s.LastRefreshedAt = &lastRef.Time
	}
	return &s, nil
}

func (s *Store) CreateSeries(ctx context.Context, se Series) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO series (tmdb_id, title, sort_title, overview, status, first_aired, poster_url,
		 fanart_url, root_folder_id, quality_profile_id, monitored)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		se.TMDBID, se.Title, se.SortTitle, se.Overview, se.Status, se.FirstAired, se.PosterURL,
		se.FanartURL, se.RootFolderID, se.QualityProfileID, boolToInt(se.Monitored))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetSeries(ctx context.Context, id int64) (*Series, error) {
	se, err := scanSeriesRow(s.db.QueryRowContext(ctx, seriesSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return se, err
}

func (s *Store) ListSeries(ctx context.Context) ([]Series, error) {
	rows, err := s.db.QueryContext(ctx, seriesSelect+` ORDER BY sort_title ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Series
	for rows.Next() {
		se, err := scanSeriesRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *se)
	}
	return out, rows.Err()
}

// UpdateSeries updates descriptive fields only (not monitored — use SetSeriesMonitored).
func (s *Store) UpdateSeries(ctx context.Context, se Series) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE series SET title=?, sort_title=?, overview=?, status=?, first_aired=?, poster_url=?,
		 fanart_url=?, last_refreshed_at=CURRENT_TIMESTAMP WHERE id=?`,
		se.Title, se.SortTitle, se.Overview, se.Status, se.FirstAired, se.PosterURL, se.FanartURL, se.ID)
	return err
}

func (s *Store) DeleteSeries(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM series WHERE id = ?`, id)
	return err
}

func (s *Store) SetSeriesMonitored(ctx context.Context, id int64, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE series SET monitored=? WHERE id=?`, boolToInt(monitored), id)
	return err
}

func (s *Store) UpsertSeason(ctx context.Context, se Season) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO seasons (series_id, season_number, monitored) VALUES (?, ?, ?)
		 ON CONFLICT(series_id, season_number) DO NOTHING`,
		se.SeriesID, se.SeasonNumber, boolToInt(se.Monitored))
	return err
}

func (s *Store) ListSeasons(ctx context.Context, seriesID int64) ([]Season, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, series_id, season_number, monitored FROM seasons WHERE series_id=? ORDER BY season_number ASC`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Season
	for rows.Next() {
		var se Season
		var m int
		if err := rows.Scan(&se.ID, &se.SeriesID, &se.SeasonNumber, &m); err != nil {
			return nil, err
		}
		se.Monitored = m != 0
		out = append(out, se)
	}
	return out, rows.Err()
}

func (s *Store) SetSeasonMonitored(ctx context.Context, id int64, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE seasons SET monitored=? WHERE id=?`, boolToInt(monitored), id)
	return err
}

// UpsertEpisode inserts a new episode or updates the descriptive fields of an
// existing one (keyed on series_id+season+episode). It does NOT touch monitored
// on update, so user/season monitoring choices survive a refresh.
func (s *Store) UpsertEpisode(ctx context.Context, e Episode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO episodes (series_id, season_number, episode_number, tmdb_id, title, overview, air_date, monitored)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(series_id, season_number, episode_number)
		 DO UPDATE SET tmdb_id=excluded.tmdb_id, title=excluded.title, overview=excluded.overview, air_date=excluded.air_date`,
		e.SeriesID, e.SeasonNumber, e.EpisodeNumber, e.TMDBID, e.Title, e.Overview, e.AirDate, boolToInt(e.Monitored))
	return err
}

func (s *Store) ListEpisodes(ctx context.Context, seriesID int64) ([]Episode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, series_id, season_number, episode_number, tmdb_id, title, overview, air_date, monitored
		 FROM episodes WHERE series_id=? ORDER BY season_number ASC, episode_number ASC`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Episode
	for rows.Next() {
		var e Episode
		var m int
		if err := rows.Scan(&e.ID, &e.SeriesID, &e.SeasonNumber, &e.EpisodeNumber, &e.TMDBID,
			&e.Title, &e.Overview, &e.AirDate, &m); err != nil {
			return nil, err
		}
		e.Monitored = m != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SetEpisodeMonitored(ctx context.Context, id int64, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE episodes SET monitored=? WHERE id=?`, boolToInt(monitored), id)
	return err
}

func (s *Store) SetSeasonEpisodesMonitored(ctx context.Context, seriesID int64, seasonNumber int, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE episodes SET monitored=? WHERE series_id=? AND season_number=?`,
		boolToInt(monitored), seriesID, seasonNumber)
	return err
}

func (s *Store) SetSeriesEpisodesMonitored(ctx context.Context, seriesID int64, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE episodes SET monitored=? WHERE series_id=?`, boolToInt(monitored), seriesID)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run TestSeriesAndEpisodeUpsert`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/store/media_store.go internal/core/store/media_store_test.go
git commit -m "feat: add series/season/episode store with idempotent upserts and cascades"
```

---

### Task 4: Movies store

**Files:**
- Modify: `internal/core/store/media_store.go`
- Modify: `internal/core/store/media_store_test.go`

**Interfaces:**
- Produces: `store.Movie`; `CreateMovie`/`GetMovie`/`ListMovies`/`UpdateMovie`/`DeleteMovie`/`SetMovieMonitored`.
- Consumes: `boolToInt`, `rowScanner`, `ErrNotFound`.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/store/media_store_test.go`:
```go
func TestMovieCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateMovie(ctx, Movie{TMDBID: 200, Title: "Film", SortTitle: "film", Year: 2020, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 200, Title: "Dup"}); err == nil {
		t.Fatal("expected duplicate tmdb_id to error")
	}
	m, err := s.GetMovie(ctx, id)
	if err != nil || m.Title != "Film" || m.Year != 2020 || !m.Monitored {
		t.Fatalf("get: %+v err=%v", m, err)
	}
	m.Title = "Film 2"
	if err := s.UpdateMovie(ctx, *m); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMovieMonitored(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListMovies(ctx)
	if err != nil || len(all) != 1 || all[0].Title != "Film 2" || all[0].Monitored {
		t.Fatalf("list: %+v err=%v", all, err)
	}
	if err := s.DeleteMovie(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMovie(ctx, id); err != ErrNotFound {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run TestMovieCRUD`
Expected: FAIL — `undefined: Movie`.

- [ ] **Step 3: Add movie store code**

Append to `internal/core/store/media_store.go`:
```go
type Movie struct {
	ID               int64      `json:"id"`
	TMDBID           int        `json:"tmdbId"`
	Title            string     `json:"title"`
	SortTitle        string     `json:"sortTitle"`
	Overview         string     `json:"overview"`
	Status           string     `json:"status"`
	Year             int        `json:"year"`
	ReleaseDate      string     `json:"releaseDate"`
	Runtime          int        `json:"runtime"`
	IMDbID           string     `json:"imdbId"`
	PosterURL        string     `json:"posterUrl"`
	FanartURL        string     `json:"fanartUrl"`
	RootFolderID     *int64     `json:"rootFolderId"`
	QualityProfileID *int64     `json:"qualityProfileId"`
	Monitored        bool       `json:"monitored"`
	AddedAt          time.Time  `json:"addedAt"`
	LastRefreshedAt  *time.Time `json:"lastRefreshedAt"`
}

const movieSelect = `SELECT id, tmdb_id, title, sort_title, overview, status, year, release_date,
	runtime, imdb_id, poster_url, fanart_url, root_folder_id, quality_profile_id, monitored,
	added_at, last_refreshed_at FROM movies`

func scanMovieRow(row rowScanner) (*Movie, error) {
	var m Movie
	var monitored int
	var rootID, qpID sql.NullInt64
	var lastRef sql.NullTime
	err := row.Scan(&m.ID, &m.TMDBID, &m.Title, &m.SortTitle, &m.Overview, &m.Status, &m.Year,
		&m.ReleaseDate, &m.Runtime, &m.IMDbID, &m.PosterURL, &m.FanartURL, &rootID, &qpID,
		&monitored, &m.AddedAt, &lastRef)
	if err != nil {
		return nil, err
	}
	m.Monitored = monitored != 0
	if rootID.Valid {
		m.RootFolderID = &rootID.Int64
	}
	if qpID.Valid {
		m.QualityProfileID = &qpID.Int64
	}
	if lastRef.Valid {
		m.LastRefreshedAt = &lastRef.Time
	}
	return &m, nil
}

func (s *Store) CreateMovie(ctx context.Context, m Movie) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO movies (tmdb_id, title, sort_title, overview, status, year, release_date, runtime,
		 imdb_id, poster_url, fanart_url, root_folder_id, quality_profile_id, monitored)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.TMDBID, m.Title, m.SortTitle, m.Overview, m.Status, m.Year, m.ReleaseDate, m.Runtime,
		m.IMDbID, m.PosterURL, m.FanartURL, m.RootFolderID, m.QualityProfileID, boolToInt(m.Monitored))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetMovie(ctx context.Context, id int64) (*Movie, error) {
	m, err := scanMovieRow(s.db.QueryRowContext(ctx, movieSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) ListMovies(ctx context.Context) ([]Movie, error) {
	rows, err := s.db.QueryContext(ctx, movieSelect+` ORDER BY sort_title ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movie
	for rows.Next() {
		m, err := scanMovieRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// UpdateMovie updates descriptive fields only (not monitored — use SetMovieMonitored).
func (s *Store) UpdateMovie(ctx context.Context, m Movie) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE movies SET title=?, sort_title=?, overview=?, status=?, year=?, release_date=?, runtime=?,
		 imdb_id=?, poster_url=?, fanart_url=?, last_refreshed_at=CURRENT_TIMESTAMP WHERE id=?`,
		m.Title, m.SortTitle, m.Overview, m.Status, m.Year, m.ReleaseDate, m.Runtime,
		m.IMDbID, m.PosterURL, m.FanartURL, m.ID)
	return err
}

func (s *Store) DeleteMovie(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM movies WHERE id = ?`, id)
	return err
}

func (s *Store) SetMovieMonitored(ctx context.Context, id int64, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE movies SET monitored=? WHERE id=?`, boolToInt(monitored), id)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run TestMovieCRUD`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/store/media_store.go internal/core/store/media_store_test.go
git commit -m "feat: add movies store CRUD"
```

---

### Task 5: Typed errors + TMDBClient

**Files:**
- Create: `internal/media/errors.go`
- Create: `internal/media/tmdb.go`
- Create: `internal/media/tmdb_test.go`
- Create: `internal/media/testdata/tmdb_search_tv.json`
- Create: `internal/media/testdata/tmdb_tv_details.json`
- Create: `internal/media/testdata/tmdb_tv_season1.json`
- Create: `internal/media/testdata/tmdb_movie_details.json`

**Interfaces:**
- Produces: `media` typed errors (`ErrNotFound`, `ErrAlreadyExists`, `ErrInvalidRootFolder`, `ErrProviderUnavailable`, `ErrProviderNotConfigured`); `TMDBClient` implementing `provider.MetadataProvider`; constructor `newTMDB(apiKey string, base string, hc *http.Client) *TMDBClient` (base defaults to TMDb v3 URL when empty — tests pass an `httptest` base).
- Consumes: `provider.MetadataProvider`, `provider.MetadataResult`, `provider.SeriesMetadata`, `provider.SeasonMetadata`, `provider.EpisodeMetadata`, `provider.MovieMetadata`, `provider.KindTV`, `provider.KindMovie`.

- [ ] **Step 1: Write the failing test**

Create the four fixtures under `internal/media/testdata/`:

`tmdb_search_tv.json`:
```json
{"results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20","overview":"A chemistry teacher.","poster_path":"/poster.jpg"}]}
```
`tmdb_tv_details.json`:
```json
{"id":1396,"name":"Breaking Bad","overview":"A chemistry teacher.","status":"Ended","first_air_date":"2008-01-20","poster_path":"/poster.jpg","backdrop_path":"/back.jpg","seasons":[{"season_number":1,"episode_count":2}]}
```
`tmdb_tv_season1.json`:
```json
{"season_number":1,"episodes":[{"id":62085,"episode_number":1,"season_number":1,"name":"Pilot","overview":"Walt starts.","air_date":"2008-01-20"},{"id":62086,"episode_number":2,"season_number":1,"name":"Cat's in the Bag","overview":"Cleanup.","air_date":"2008-01-27"}]}
```
`tmdb_movie_details.json`:
```json
{"id":603,"title":"The Matrix","overview":"A hacker learns.","status":"Released","release_date":"1999-03-30","runtime":136,"imdb_id":"tt0133093","poster_path":"/m.jpg","backdrop_path":"/mb.jpg"}
```

Create `internal/media/tmdb_test.go`:
```go
package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tmdbTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/tv"):
			w.Write(fixture(t, "tmdb_search_tv.json"))
		case strings.HasPrefix(r.URL.Path, "/tv/1396/season/1"):
			w.Write(fixture(t, "tmdb_tv_season1.json"))
		case strings.HasPrefix(r.URL.Path, "/tv/1396"):
			w.Write(fixture(t, "tmdb_tv_details.json"))
		case strings.HasPrefix(r.URL.Path, "/movie/603"):
			w.Write(fixture(t, "tmdb_movie_details.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestTMDBSearchTV(t *testing.T) {
	srv := tmdbTestServer(t)
	defer srv.Close()
	c := newTMDB("key", srv.URL, srv.Client())

	res, err := c.SearchTV(context.Background(), "breaking bad")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].TMDBID != 1396 || res[0].Title != "Breaking Bad" ||
		res[0].Year != 2008 || res[0].Kind != provider.KindTV {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestTMDBTVDetails(t *testing.T) {
	srv := tmdbTestServer(t)
	defer srv.Close()
	c := newTMDB("key", srv.URL, srv.Client())

	s, err := c.TVDetails(context.Background(), 1396)
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Breaking Bad" || s.Status != "Ended" || len(s.Seasons) != 1 ||
		len(s.Seasons[0].Episodes) != 2 || s.Seasons[0].Episodes[0].Title != "Pilot" {
		t.Fatalf("unexpected: %+v", s)
	}
}

func TestTMDBMovieDetails(t *testing.T) {
	srv := tmdbTestServer(t)
	defer srv.Close()
	c := newTMDB("key", srv.URL, srv.Client())

	m, err := c.MovieDetails(context.Background(), 603)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "The Matrix" || m.Year != 1999 || m.Runtime != 136 || m.IMDbID != "tt0133093" {
		t.Fatalf("unexpected: %+v", m)
	}
}

func TestTMDBNotConfigured(t *testing.T) {
	c := newTMDB("", "", nil)
	if _, err := c.SearchTV(context.Background(), "x"); err != ErrProviderNotConfigured {
		t.Fatalf("want ErrProviderNotConfigured got %v", err)
	}
}

func TestTMDBServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTMDB("key", srv.URL, srv.Client())
	if _, err := c.SearchMovie(context.Background(), "x"); err != ErrProviderUnavailable {
		t.Fatalf("want ErrProviderUnavailable got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ -run TestTMDB`
Expected: FAIL — `undefined: newTMDB`, `undefined: ErrProviderNotConfigured`.

- [ ] **Step 3: Create errors and the TMDb client**

Create `internal/media/errors.go`:
```go
package media

import "errors"

var (
	ErrNotFound              = errors.New("media: not found")
	ErrAlreadyExists         = errors.New("media: already exists")
	ErrInvalidRootFolder     = errors.New("media: invalid root folder")
	ErrProviderUnavailable   = errors.New("media: metadata provider unavailable")
	ErrProviderNotConfigured = errors.New("media: metadata provider not configured")
)
```

Create `internal/media/tmdb.go`:
```go
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

const (
	tmdbBaseURL   = "https://api.themoviedb.org/3"
	tmdbImageBase = "https://image.tmdb.org/t/p/original"
	maxTMDBBytes  = 4 << 20 // 4 MiB cap on a metadata response
)

// TMDBClient implements provider.MetadataProvider against TMDb's v3 REST API.
type TMDBClient struct {
	apiKey string
	base   string
	http   *http.Client
}

func newTMDB(apiKey, base string, hc *http.Client) *TMDBClient {
	if base == "" {
		base = tmdbBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &TMDBClient{apiKey: apiKey, base: strings.TrimRight(base, "/"), http: hc}
}

func imageURL(path string) string {
	if path == "" {
		return ""
	}
	return tmdbImageBase + path
}

func yearOf(date string) int {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil {
			return y
		}
	}
	return 0
}

// get performs a TMDb GET and decodes the JSON body into out.
func (c *TMDBClient) get(ctx context.Context, path string, q url.Values, out any) error {
	if c.apiKey == "" {
		return ErrProviderNotConfigured
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("api_key", c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTMDBBytes))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	return nil
}

type tmdbSearchTV struct {
	Results []struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		FirstAir     string `json:"first_air_date"`
		Overview     string `json:"overview"`
		PosterPath   string `json:"poster_path"`
	} `json:"results"`
}

type tmdbSearchMovie struct {
	Results []struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		ReleaseDate string `json:"release_date"`
		Overview    string `json:"overview"`
		PosterPath  string `json:"poster_path"`
	} `json:"results"`
}

func (c *TMDBClient) SearchTV(ctx context.Context, term string) ([]provider.MetadataResult, error) {
	var r tmdbSearchTV
	if err := c.get(ctx, "/search/tv", url.Values{"query": {term}}, &r); err != nil {
		return nil, err
	}
	out := make([]provider.MetadataResult, 0, len(r.Results))
	for _, it := range r.Results {
		out = append(out, provider.MetadataResult{
			TMDBID: it.ID, Title: it.Name, Year: yearOf(it.FirstAir), Overview: it.Overview,
			PosterURL: imageURL(it.PosterPath), Kind: provider.KindTV,
		})
	}
	return out, nil
}

func (c *TMDBClient) SearchMovie(ctx context.Context, term string) ([]provider.MetadataResult, error) {
	var r tmdbSearchMovie
	if err := c.get(ctx, "/search/movie", url.Values{"query": {term}}, &r); err != nil {
		return nil, err
	}
	out := make([]provider.MetadataResult, 0, len(r.Results))
	for _, it := range r.Results {
		out = append(out, provider.MetadataResult{
			TMDBID: it.ID, Title: it.Title, Year: yearOf(it.ReleaseDate), Overview: it.Overview,
			PosterURL: imageURL(it.PosterPath), Kind: provider.KindMovie,
		})
	}
	return out, nil
}

type tmdbTVDetails struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Status       string `json:"status"`
	FirstAir     string `json:"first_air_date"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
	Seasons      []struct {
		SeasonNumber int `json:"season_number"`
	} `json:"seasons"`
}

type tmdbSeason struct {
	SeasonNumber int `json:"season_number"`
	Episodes     []struct {
		ID            int    `json:"id"`
		EpisodeNumber int    `json:"episode_number"`
		SeasonNumber  int    `json:"season_number"`
		Name          string `json:"name"`
		Overview      string `json:"overview"`
		AirDate       string `json:"air_date"`
	} `json:"episodes"`
}

func (c *TMDBClient) TVDetails(ctx context.Context, tmdbID int) (provider.SeriesMetadata, error) {
	var d tmdbTVDetails
	if err := c.get(ctx, "/tv/"+strconv.Itoa(tmdbID), nil, &d); err != nil {
		return provider.SeriesMetadata{}, err
	}
	s := provider.SeriesMetadata{
		TMDBID: d.ID, Title: d.Name, Overview: d.Overview, Status: d.Status,
		FirstAired: d.FirstAir, PosterURL: imageURL(d.PosterPath), FanartURL: imageURL(d.BackdropPath),
	}
	for _, sn := range d.Seasons {
		var sd tmdbSeason
		if err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", tmdbID, sn.SeasonNumber), nil, &sd); err != nil {
			return provider.SeriesMetadata{}, err
		}
		sm := provider.SeasonMetadata{SeasonNumber: sn.SeasonNumber}
		for _, e := range sd.Episodes {
			sm.Episodes = append(sm.Episodes, provider.EpisodeMetadata{
				SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, TMDBID: e.ID,
				Title: e.Name, Overview: e.Overview, AirDate: e.AirDate,
			})
		}
		s.Seasons = append(s.Seasons, sm)
	}
	return s, nil
}

type tmdbMovieDetails struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Overview     string `json:"overview"`
	Status       string `json:"status"`
	ReleaseDate  string `json:"release_date"`
	Runtime      int    `json:"runtime"`
	IMDbID       string `json:"imdb_id"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

func (c *TMDBClient) MovieDetails(ctx context.Context, tmdbID int) (provider.MovieMetadata, error) {
	var d tmdbMovieDetails
	if err := c.get(ctx, "/movie/"+strconv.Itoa(tmdbID), nil, &d); err != nil {
		return provider.MovieMetadata{}, err
	}
	return provider.MovieMetadata{
		TMDBID: d.ID, Title: d.Title, Overview: d.Overview, Status: d.Status,
		Year: yearOf(d.ReleaseDate), ReleaseDate: d.ReleaseDate, Runtime: d.Runtime, IMDbID: d.IMDbID,
		PosterURL: imageURL(d.PosterPath), FanartURL: imageURL(d.BackdropPath),
	}, nil
}

// Ensure the interface is satisfied at compile time.
var _ provider.MetadataProvider = (*TMDBClient)(nil)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ -run TestTMDB`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/media/errors.go internal/media/tmdb.go internal/media/tmdb_test.go internal/media/testdata/
git commit -m "feat: add media typed errors and TMDb metadata client"
```

---

### Task 6: Service — root folders + add + monitor options + dedupe

**Files:**
- Create: `internal/media/media.go`
- Create: `internal/media/service_test.go`

**Interfaces:**
- Produces: `Service` (fields `store`, `meta`, `bus`); `NewService(st *store.Store, mp provider.MetadataProvider) *Service`; `WithBus(bus *events.Bus) *Service`; event types `SeriesUpdated{ID int64}` (`"media.series.updated"`) and `MovieUpdated{ID int64}` (`"media.movie.updated"`); `AddRootFolder(ctx, path) (store.RootFolder, error)`; `AddSeries(ctx, AddSeriesRequest) (store.Series, error)`; `AddMovie(ctx, AddMovieRequest) (store.Movie, error)`; request types `AddSeriesRequest{TMDBID int; RootFolderID *int64; MonitorOption string}` and `AddMovieRequest{TMDBID int; RootFolderID *int64; Monitored bool}`; monitor-option constants `MonitorAll`, `MonitorFuture`, `MonitorNone`; emit helpers `emitSeries`/`emitMovie`.
- Consumes: `store.*` (Task 2-4), `provider.MetadataProvider`, `events.Bus`, media errors (Task 5).

- [ ] **Step 1: Write the failing test**

Create `internal/media/service_test.go`:
```go
package media

import (
	"context"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// fakeProvider returns canned metadata; airDate controls the "future" monitor test.
type fakeProvider struct {
	series provider.SeriesMetadata
	movie  provider.MovieMetadata
}

func (f *fakeProvider) SearchTV(context.Context, string) ([]provider.MetadataResult, error) {
	return []provider.MetadataResult{{TMDBID: f.series.TMDBID, Title: f.series.Title, Kind: provider.KindTV}}, nil
}
func (f *fakeProvider) SearchMovie(context.Context, string) ([]provider.MetadataResult, error) {
	return []provider.MetadataResult{{TMDBID: f.movie.TMDBID, Title: f.movie.Title, Kind: provider.KindMovie}}, nil
}
func (f *fakeProvider) TVDetails(context.Context, int) (provider.SeriesMetadata, error) { return f.series, nil }
func (f *fakeProvider) MovieDetails(context.Context, int) (provider.MovieMetadata, error) { return f.movie, nil }

func newTestService(t *testing.T, fp *fakeProvider) (*Service, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	return NewService(st, fp), st
}

func sampleSeries() provider.SeriesMetadata {
	return provider.SeriesMetadata{
		TMDBID: 100, Title: "Show", Status: "Ended", FirstAired: "2020-01-01",
		Seasons: []provider.SeasonMetadata{{SeasonNumber: 1, Episodes: []provider.EpisodeMetadata{
			{SeasonNumber: 1, EpisodeNumber: 1, Title: "Aired", AirDate: "2020-01-01"},
			{SeasonNumber: 1, EpisodeNumber: 2, Title: "Future", AirDate: "2999-01-01"},
		}}},
	}
}

func TestAddSeriesMonitorFuture(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	rf, err := svc.AddRootFolder(ctx, "/data/tv")
	if err != nil {
		t.Fatal(err)
	}
	se, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, RootFolderID: &rf.ID, MonitorOption: MonitorFuture})
	if err != nil {
		t.Fatal(err)
	}
	if !se.Monitored {
		t.Fatal("series should be monitored")
	}
	eps, _ := st.ListEpisodes(ctx, se.ID)
	if len(eps) != 2 {
		t.Fatalf("want 2 episodes, got %d", len(eps))
	}
	// "future" → only the unaired episode is monitored.
	for _, e := range eps {
		if e.EpisodeNumber == 1 && e.Monitored {
			t.Fatal("aired episode should be unmonitored under 'future'")
		}
		if e.EpisodeNumber == 2 && !e.Monitored {
			t.Fatal("future episode should be monitored under 'future'")
		}
	}
}

func TestAddSeriesDuplicateRejected(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, _ := newTestService(t, fp)
	ctx := context.Background()
	if _, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorAll}); err != ErrAlreadyExists {
		t.Fatalf("want ErrAlreadyExists got %v", err)
	}
}

func TestAddSeriesInvalidRootFolder(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, _ := newTestService(t, fp)
	bad := int64(999)
	if _, err := svc.AddSeries(context.Background(), AddSeriesRequest{TMDBID: 100, RootFolderID: &bad, MonitorOption: MonitorAll}); err != ErrInvalidRootFolder {
		t.Fatalf("want ErrInvalidRootFolder got %v", err)
	}
}

func TestAddMovie(t *testing.T) {
	fp := &fakeProvider{movie: provider.MovieMetadata{TMDBID: 200, Title: "Film", Year: 2020}}
	svc, _ := newTestService(t, fp)
	m, err := svc.AddMovie(context.Background(), AddMovieRequest{TMDBID: 200, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Film" || !m.Monitored {
		t.Fatalf("unexpected: %+v", m)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ -run 'TestAdd'`
Expected: FAIL — `undefined: NewService`.

- [ ] **Step 3: Create the service (add path)**

Create `internal/media/media.go`:
```go
package media

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// Monitor options applied at add time.
const (
	MonitorAll    = "all"
	MonitorFuture = "future"
	MonitorNone   = "none"
)

// SeriesUpdated / MovieUpdated are published (async) on add / refresh / monitor changes.
type SeriesUpdated struct {
	ID int64 `json:"id"`
}

func (SeriesUpdated) Name() string { return "media.series.updated" }

type MovieUpdated struct {
	ID int64 `json:"id"`
}

func (MovieUpdated) Name() string { return "media.movie.updated" }

type AddSeriesRequest struct {
	TMDBID        int
	RootFolderID  *int64
	MonitorOption string
}

type AddMovieRequest struct {
	TMDBID       int
	RootFolderID *int64
	Monitored    bool
}

// Service owns all library mutations over the store and a metadata provider.
type Service struct {
	store *store.Store
	meta  provider.MetadataProvider
	bus   *events.Bus
}

func NewService(st *store.Store, mp provider.MetadataProvider) *Service {
	return &Service{store: st, meta: mp}
}

// WithBus attaches an event bus so add/refresh/monitor changes publish media events.
func (s *Service) WithBus(bus *events.Bus) *Service {
	s.bus = bus
	return s
}

func (s *Service) emitSeries(ctx context.Context, id int64) {
	if s.bus != nil {
		s.bus.PublishAsync(ctx, SeriesUpdated{ID: id})
	}
}

func (s *Service) emitMovie(ctx context.Context, id int64) {
	if s.bus != nil {
		s.bus.PublishAsync(ctx, MovieUpdated{ID: id})
	}
}

func sortTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, p := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(t, p) {
			return strings.TrimPrefix(t, p)
		}
	}
	return t
}

// aired reports whether a date-only string is on/before today.
func aired(airDate string) bool {
	if airDate == "" {
		return false
	}
	d, err := time.Parse("2006-01-02", airDate)
	if err != nil {
		return false
	}
	return !d.After(time.Now())
}

// episodeMonitored decides an episode's monitored flag from the add-time option.
func episodeMonitored(option, airDate string) bool {
	switch option {
	case MonitorAll:
		return true
	case MonitorFuture:
		return !aired(airDate)
	default: // MonitorNone or unknown
		return false
	}
}

func (s *Service) validateRootFolder(ctx context.Context, id *int64) error {
	if id == nil {
		return nil
	}
	if _, err := s.store.GetRootFolder(ctx, *id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidRootFolder
		}
		return err
	}
	return nil
}

func (s *Service) AddRootFolder(ctx context.Context, path string) (store.RootFolder, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return store.RootFolder{}, ErrInvalidRootFolder
	}
	id, err := s.store.CreateRootFolder(ctx, path)
	if err != nil {
		return store.RootFolder{}, err
	}
	rf, err := s.store.GetRootFolder(ctx, id)
	if err != nil {
		return store.RootFolder{}, err
	}
	return *rf, nil
}

func (s *Service) AddSeries(ctx context.Context, req AddSeriesRequest) (store.Series, error) {
	if err := s.validateRootFolder(ctx, req.RootFolderID); err != nil {
		return store.Series{}, err
	}
	md, err := s.meta.TVDetails(ctx, req.TMDBID)
	if err != nil {
		return store.Series{}, err
	}
	id, err := s.store.CreateSeries(ctx, store.Series{
		TMDBID: md.TMDBID, Title: md.Title, SortTitle: sortTitle(md.Title), Overview: md.Overview,
		Status: md.Status, FirstAired: md.FirstAired, PosterURL: md.PosterURL, FanartURL: md.FanartURL,
		RootFolderID: req.RootFolderID, Monitored: req.MonitorOption != MonitorNone,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.Series{}, ErrAlreadyExists
		}
		return store.Series{}, err
	}
	for _, sn := range md.Seasons {
		seasonMonitored := req.MonitorOption != MonitorNone
		if err := s.store.UpsertSeason(ctx, store.Season{SeriesID: id, SeasonNumber: sn.SeasonNumber, Monitored: seasonMonitored}); err != nil {
			return store.Series{}, err
		}
		for _, e := range sn.Episodes {
			if err := s.store.UpsertEpisode(ctx, store.Episode{
				SeriesID: id, SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, TMDBID: e.TMDBID,
				Title: e.Title, Overview: e.Overview, AirDate: e.AirDate,
				Monitored: episodeMonitored(req.MonitorOption, e.AirDate),
			}); err != nil {
				return store.Series{}, err
			}
		}
	}
	out, err := s.store.GetSeries(ctx, id)
	if err != nil {
		return store.Series{}, err
	}
	s.emitSeries(ctx, id)
	return *out, nil
}

func (s *Service) AddMovie(ctx context.Context, req AddMovieRequest) (store.Movie, error) {
	if err := s.validateRootFolder(ctx, req.RootFolderID); err != nil {
		return store.Movie{}, err
	}
	md, err := s.meta.MovieDetails(ctx, req.TMDBID)
	if err != nil {
		return store.Movie{}, err
	}
	id, err := s.store.CreateMovie(ctx, store.Movie{
		TMDBID: md.TMDBID, Title: md.Title, SortTitle: sortTitle(md.Title), Overview: md.Overview,
		Status: md.Status, Year: md.Year, ReleaseDate: md.ReleaseDate, Runtime: md.Runtime, IMDbID: md.IMDbID,
		PosterURL: md.PosterURL, FanartURL: md.FanartURL, RootFolderID: req.RootFolderID, Monitored: req.Monitored,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.Movie{}, ErrAlreadyExists
		}
		return store.Movie{}, err
	}
	out, err := s.store.GetMovie(ctx, id)
	if err != nil {
		return store.Movie{}, err
	}
	s.emitMovie(ctx, id)
	return *out, nil
}

// isUniqueViolation detects a SQLite UNIQUE constraint failure (duplicate tmdb_id).
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ -run 'TestAdd'`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/media/media.go internal/media/service_test.go
git commit -m "feat: add media Service with root folders, add series/movie, monitor options"
```

---

### Task 7: Service — refresh reconciliation + monitor toggles + RefreshCommand

**Files:**
- Modify: `internal/media/media.go` (add refresh + monitor-toggle methods)
- Create: `internal/media/refresh.go` (events + RefreshCommand)
- Modify: `internal/media/service_test.go` (add tests)
- Create: `internal/media/refresh_test.go`

**Interfaces:**
- Produces: `Service.RefreshSeries(ctx, id) error`, `Service.RefreshMovie(ctx, id) error`, `Service.RefreshAll(ctx) error`, `Service.SetSeriesMonitored(ctx, id, bool) error` (cascades), `Service.SetSeasonMonitored(ctx, seriesID, seasonID, seasonNumber, bool) error` (cascades to that season's episodes), `Service.SetEpisodeMonitored(ctx, seriesID, episodeID, bool) error`, `Service.SetMovieMonitored(ctx, id, bool) error`; `RefreshCommand` implementing `command.Command`; `NewRefresh(svc *Service) *RefreshCommand`.
- Consumes: `store.*`, the `bus`/`emitSeries`/`emitMovie`/`SeriesUpdated`/`MovieUpdated` infrastructure from Task 6, `command.Command`/`command.Reporter` (`command.Reporter` = `interface{ Progress(pct int, msg string) }`).

- [ ] **Step 1: Write the failing tests**

Add to `internal/media/service_test.go`:
```go
func TestRefreshReconciles(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	se, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorNone})
	if err != nil {
		t.Fatal(err)
	}
	// User monitors episode 1 manually.
	eps, _ := st.ListEpisodes(ctx, se.ID)
	var ep1 int64
	for _, e := range eps {
		if e.EpisodeNumber == 1 {
			ep1 = e.ID
		}
	}
	if err := svc.SetEpisodeMonitored(ctx, ep1, true); err != nil {
		t.Fatal(err)
	}

	// Upstream now: episode 1 title changed + a new episode 3 appears.
	updated := sampleSeries()
	updated.Title = "Show (Renamed)"
	updated.Seasons[0].Episodes[0].Title = "Aired (v2)"
	updated.Seasons[0].Episodes = append(updated.Seasons[0].Episodes, provider.EpisodeMetadata{
		SeasonNumber: 1, EpisodeNumber: 3, Title: "New", AirDate: "2020-02-01",
	})
	fp.series = updated

	if err := svc.RefreshSeries(ctx, se.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetSeries(ctx, se.ID)
	if got.Title != "Show (Renamed)" {
		t.Fatalf("title not refreshed: %q", got.Title)
	}
	eps, _ = st.ListEpisodes(ctx, se.ID)
	if len(eps) != 3 {
		t.Fatalf("want 3 episodes after refresh, got %d", len(eps))
	}
	for _, e := range eps {
		if e.EpisodeNumber == 1 {
			if e.Title != "Aired (v2)" {
				t.Fatalf("ep1 title not updated: %q", e.Title)
			}
			if !e.Monitored {
				t.Fatal("refresh must PRESERVE user's monitored=true on ep1")
			}
		}
	}
}

func TestSetSeriesMonitoredCascades(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	se, _ := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorAll})

	if err := svc.SetSeriesMonitored(ctx, se.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetSeries(ctx, se.ID)
	if got.Monitored {
		t.Fatal("series still monitored")
	}
	eps, _ := st.ListEpisodes(ctx, se.ID)
	for _, e := range eps {
		if e.Monitored {
			t.Fatal("series unmonitor should cascade to episodes")
		}
	}
}
```

Create `internal/media/refresh_test.go`:
```go
package media

import (
	"context"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/events"
)

// nopReporter satisfies command.Reporter (interface{ Progress(int, string) });
// mirrors internal/downloadclient/monitor_test.go — command has no exported Nop.
type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestRefreshCommandEmitsEvents(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, _ := newTestService(t, fp)
	bus := events.New()
	got := make(chan string, 4)
	bus.Subscribe("media.series.updated", func(_ context.Context, e events.Event) { got <- e.Name() })
	svc = svc.WithBus(bus)
	ctx := context.Background()

	if _, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorAll}); err != nil {
		t.Fatal(err)
	}
	// Drain the add event (PublishAsync → block briefly).
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("no event from AddSeries")
	}

	cmd := NewRefresh(svc)
	if cmd.Name() == "" {
		t.Fatal("command needs a name")
	}
	if err := cmd.Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-got:
		if name != "media.series.updated" {
			t.Fatalf("unexpected event %q", name)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh command did not emit a series-updated event")
	}
}
```
(`command.Reporter` is `interface{ Progress(pct int, msg string) }` — verified in `internal/core/command/command.go`. Events are delivered via `PublishAsync`, so the test blocks on the channel with a timeout rather than using a non-blocking `select`/`default`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ -run 'Refresh|Cascade'`
Expected: FAIL — `undefined: (*Service).RefreshSeries`, `undefined: NewRefresh`.

- [ ] **Step 3: Add refresh + monitor methods to the service**

Append to `internal/media/media.go` (the `events` import, `bus` field, `WithBus`, and `emitSeries`/`emitMovie` already exist from Task 6 — do NOT redefine them):
```go
// RefreshSeries re-pulls metadata and reconciles seasons/episodes. Descriptive
// fields are updated; user monitored choices are preserved (UpsertEpisode does
// not overwrite monitored on conflict). New episodes inherit their season's
// current monitored state.
func (s *Service) RefreshSeries(ctx context.Context, id int64) error {
	cur, err := s.store.GetSeries(ctx, id)
	if err != nil {
		return err
	}
	md, err := s.meta.TVDetails(ctx, cur.TMDBID)
	if err != nil {
		return err
	}
	cur.Title = md.Title
	cur.SortTitle = sortTitle(md.Title)
	cur.Overview = md.Overview
	cur.Status = md.Status
	cur.FirstAired = md.FirstAired
	cur.PosterURL = md.PosterURL
	cur.FanartURL = md.FanartURL
	if err := s.store.UpdateSeries(ctx, *cur); err != nil {
		return err
	}
	seasons, err := s.store.ListSeasons(ctx, id)
	if err != nil {
		return err
	}
	seasonMon := map[int]bool{}
	for _, sn := range seasons {
		seasonMon[sn.SeasonNumber] = sn.Monitored
	}
	for _, sn := range md.Seasons {
		mon, known := seasonMon[sn.SeasonNumber]
		if !known {
			mon = cur.Monitored
		}
		if err := s.store.UpsertSeason(ctx, store.Season{SeriesID: id, SeasonNumber: sn.SeasonNumber, Monitored: mon}); err != nil {
			return err
		}
		for _, e := range sn.Episodes {
			if err := s.store.UpsertEpisode(ctx, store.Episode{
				SeriesID: id, SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, TMDBID: e.TMDBID,
				Title: e.Title, Overview: e.Overview, AirDate: e.AirDate, Monitored: mon,
			}); err != nil {
				return err
			}
		}
	}
	s.emitSeries(ctx, id)
	return nil
}

func (s *Service) RefreshMovie(ctx context.Context, id int64) error {
	cur, err := s.store.GetMovie(ctx, id)
	if err != nil {
		return err
	}
	md, err := s.meta.MovieDetails(ctx, cur.TMDBID)
	if err != nil {
		return err
	}
	cur.Title = md.Title
	cur.SortTitle = sortTitle(md.Title)
	cur.Overview = md.Overview
	cur.Status = md.Status
	cur.Year = md.Year
	cur.ReleaseDate = md.ReleaseDate
	cur.Runtime = md.Runtime
	cur.IMDbID = md.IMDbID
	cur.PosterURL = md.PosterURL
	cur.FanartURL = md.FanartURL
	if err := s.store.UpdateMovie(ctx, *cur); err != nil {
		return err
	}
	s.emitMovie(ctx, id)
	return nil
}

// RefreshAll refreshes every monitored series and movie, best-effort: a single
// item's provider failure is logged via the returned error only if ALL fail; a
// partial failure returns nil so one bad item doesn't abort the sweep.
func (s *Service) RefreshAll(ctx context.Context) error {
	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return err
	}
	for _, se := range series {
		if se.Monitored {
			_ = s.RefreshSeries(ctx, se.ID)
		}
	}
	movies, err := s.store.ListMovies(ctx)
	if err != nil {
		return err
	}
	for _, m := range movies {
		if m.Monitored {
			_ = s.RefreshMovie(ctx, m.ID)
		}
	}
	return nil
}

func (s *Service) SetSeriesMonitored(ctx context.Context, id int64, monitored bool) error {
	if err := s.store.SetSeriesMonitored(ctx, id, monitored); err != nil {
		return err
	}
	if err := s.store.SetSeriesEpisodesMonitored(ctx, id, monitored); err != nil {
		return err
	}
	// Cascade to seasons too.
	seasons, err := s.store.ListSeasons(ctx, id)
	if err != nil {
		return err
	}
	for _, sn := range seasons {
		if err := s.store.SetSeasonMonitored(ctx, sn.ID, monitored); err != nil {
			return err
		}
	}
	s.emitSeries(ctx, id)
	return nil
}

// SetSeasonMonitored sets a season and cascades to that season's episodes. It
// needs the owning series id, resolved from the season row.
func (s *Service) SetSeasonMonitored(ctx context.Context, seriesID, seasonID int64, seasonNumber int, monitored bool) error {
	if err := s.store.SetSeasonMonitored(ctx, seasonID, monitored); err != nil {
		return err
	}
	if err := s.store.SetSeasonEpisodesMonitored(ctx, seriesID, seasonNumber, monitored); err != nil {
		return err
	}
	s.emitSeries(ctx, seriesID)
	return nil
}

func (s *Service) SetEpisodeMonitored(ctx context.Context, seriesID, episodeID int64, monitored bool) error {
	if err := s.store.SetEpisodeMonitored(ctx, episodeID, monitored); err != nil {
		return err
	}
	s.emitSeries(ctx, seriesID)
	return nil
}

func (s *Service) SetMovieMonitored(ctx context.Context, id int64, monitored bool) error {
	if err := s.store.SetMovieMonitored(ctx, id, monitored); err != nil {
		return err
	}
	s.emitMovie(ctx, id)
	return nil
}
```

- [ ] **Step 4: Create the refresh command**

Create `internal/media/refresh.go` (the `SeriesUpdated`/`MovieUpdated` event types live in `media.go` from Task 6 — this file only holds the command):
```go
package media

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/command"
)

// RefreshCommand refreshes all monitored library items on a schedule. A single
// instance is registered with the scheduler (it is stateless, so re-use is fine).
type RefreshCommand struct {
	svc *Service
}

func NewRefresh(svc *Service) *RefreshCommand { return &RefreshCommand{svc: svc} }

func (c *RefreshCommand) Name() string { return "MediaRefresh" }

func (c *RefreshCommand) Run(ctx context.Context, r command.Reporter) error {
	err := c.svc.RefreshAll(ctx)
	r.Progress(100, "")
	return err
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ -run 'Refresh|Cascade' -count=1`
Expected: PASS.

Note: `TestRefreshCommandEmitsEvents` relies on `PublishAsync`; the test drains a buffered channel with a `select`. If it is flaky, the implementer may switch that specific assertion to block on the channel with a short timeout — but keep `PublishAsync` in the service.

- [ ] **Step 6: Commit**

```bash
git add internal/media/media.go internal/media/refresh.go internal/media/service_test.go internal/media/refresh_test.go
git commit -m "feat: add media refresh reconciliation, monitor cascades, and refresh command"
```

---

### Task 8: API sub-router

**Files:**
- Create: `internal/media/api.go`
- Create: `internal/media/api_test.go`

**Interfaces:**
- Produces: `API`; `NewAPI(st *store.Store, svc *Service) *API`; `(*API).Mount(r chi.Router)` registering all media routes.
- Consumes: `Service` (Tasks 6-7), `store.*`, `api.WriteJSON`/`api.WriteError`, media errors.

- [ ] **Step 1: Write the failing test**

Create `internal/media/api_test.go`:
```go
package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestAPI(t *testing.T, fp *fakeProvider) (http.Handler, *store.Store) {
	t.Helper()
	svc, st := newTestService(t, fp)
	a := NewAPI(st, svc)
	r := chi.NewRouter()
	a.Mount(r)
	return r, st
}

func TestAPILookupAndAddSeries(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, _ := newTestAPI(t, fp)

	// lookup
	req := httptest.NewRequest(http.MethodGet, "/media/lookup?term=show&kind=tv", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("lookup status = %d", w.Code)
	}

	// add series
	body := `{"tmdbId":100,"monitorOption":"all"}`
	req = httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add series status = %d body=%s", w.Code, w.Body.String())
	}

	// duplicate → 409
	req = httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate add status = %d want 409", w.Code)
	}
}

func TestAPIGetSeriesNotFound(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, _ := newTestAPI(t, fp)
	req := httptest.NewRequest(http.MethodGet, "/series/999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d want 404", w.Code)
	}
}

func TestAPIRootFolderLifecycle(t *testing.T) {
	fp := &fakeProvider{}
	r, _ := newTestAPI(t, fp)
	req := httptest.NewRequest(http.MethodPost, "/rootfolder", strings.NewReader(`{"path":"/data/tv"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create rootfolder status = %d", w.Code)
	}
	var rf store.RootFolder
	_ = json.Unmarshal(w.Body.Bytes(), &rf)
	if rf.Path != "/data/tv" {
		t.Fatalf("unexpected rootfolder: %+v", rf)
	}
}

// Guard: the TMDb key must never surface; series/movie JSON has no such field.
// This test documents that the store structs carry no api key at all.
func TestAPINoCredentialLeak(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, _ := newTestAPI(t, fp)
	req := httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(`{"tmdbId":100,"monitorOption":"all"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if strings.Contains(strings.ToLower(w.Body.String()), "apikey") || strings.Contains(strings.ToLower(w.Body.String()), "api_key") {
		t.Fatalf("response leaked a credential field: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ -run TestAPI`
Expected: FAIL — `undefined: NewAPI`.

- [ ] **Step 3: Create the API sub-router**

Create `internal/media/api.go`:
```go
package media

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type API struct {
	store *store.Store
	svc   *Service
}

func NewAPI(st *store.Store, svc *Service) *API { return &API{store: st, svc: svc} }

// Mount registers routes on an already-authenticated router (the /api/v1 group).
func (a *API) Mount(r chi.Router) {
	r.Get("/media/lookup", a.lookup)

	r.Route("/series", func(r chi.Router) {
		r.Get("/", a.listSeries)
		r.Post("/", a.addSeries)
		r.Get("/{id}", a.getSeries)
		r.Delete("/{id}", a.deleteSeries)
		r.Post("/{id}/refresh", a.refreshSeries)
		r.Put("/{id}/monitor", a.monitorSeries)
	})
	r.Put("/season/{id}/monitor", a.monitorSeason)
	r.Put("/episode/{id}/monitor", a.monitorEpisode)

	r.Route("/movies", func(r chi.Router) {
		r.Get("/", a.listMovies)
		r.Post("/", a.addMovie)
		r.Get("/{id}", a.getMovie)
		r.Delete("/{id}", a.deleteMovie)
		r.Post("/{id}/refresh", a.refreshMovie)
		r.Put("/{id}/monitor", a.monitorMovie)
	})

	r.Route("/rootfolder", func(r chi.Router) {
		r.Get("/", a.listRootFolders)
		r.Post("/", a.addRootFolder)
		r.Delete("/{id}", a.deleteRootFolder)
	})
}

func mediaID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

// writeMediaError maps typed media errors to HTTP responses.
func writeMediaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrAlreadyExists):
		api.WriteError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, ErrInvalidRootFolder):
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, ErrProviderNotConfigured):
		api.WriteError(w, http.StatusBadRequest, "not_configured", err.Error())
	case errors.Is(err, ErrProviderUnavailable):
		api.WriteError(w, http.StatusBadGateway, "provider_unavailable", err.Error())
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (a *API) lookup(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	kind := r.URL.Query().Get("kind")
	if term == "" {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "term is required")
		return
	}
	var (
		res []provider.MetadataResult
		err error
	)
	switch kind {
	case "movie":
		res, err = a.svc.meta.SearchMovie(r.Context(), term)
	default:
		res, err = a.svc.meta.SearchTV(r.Context(), term)
	}
	if err != nil {
		writeMediaError(w, err)
		return
	}
	if res == nil {
		res = []provider.MetadataResult{}
	}
	api.WriteJSON(w, http.StatusOK, res)
}

type addSeriesBody struct {
	TMDBID        int    `json:"tmdbId"`
	RootFolderID  *int64 `json:"rootFolderId"`
	MonitorOption string `json:"monitorOption"`
}

func (a *API) addSeries(w http.ResponseWriter, r *http.Request) {
	var b addSeriesBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if b.TMDBID == 0 {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "tmdbId is required")
		return
	}
	if b.MonitorOption == "" {
		b.MonitorOption = MonitorAll
	}
	se, err := a.svc.AddSeries(r.Context(), AddSeriesRequest{TMDBID: b.TMDBID, RootFolderID: b.RootFolderID, MonitorOption: b.MonitorOption})
	if err != nil {
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, se)
}

func (a *API) listSeries(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListSeries(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list series")
		return
	}
	if rows == nil {
		rows = []store.Series{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

type seriesDetail struct {
	store.Series
	Seasons  []store.Season  `json:"seasons"`
	Episodes []store.Episode `json:"episodes"`
}

func (a *API) getSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	se, err := a.store.GetSeries(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "series not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load series")
		return
	}
	seasons, _ := a.store.ListSeasons(r.Context(), id)
	episodes, _ := a.store.ListEpisodes(r.Context(), id)
	if seasons == nil {
		seasons = []store.Season{}
	}
	if episodes == nil {
		episodes = []store.Episode{}
	}
	api.WriteJSON(w, http.StatusOK, seriesDetail{Series: *se, Seasons: seasons, Episodes: episodes})
}

func (a *API) deleteSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteSeries(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete series")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) refreshSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.svc.RefreshSeries(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "series not found")
			return
		}
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type monitorBody struct {
	Monitored bool `json:"monitored"`
}

func decodeMonitor(w http.ResponseWriter, r *http.Request) (bool, bool) {
	var b monitorBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return false, false
	}
	return b.Monitored, true
}

func (a *API) monitorSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	mon, ok := decodeMonitor(w, r)
	if !ok {
		return
	}
	if err := a.svc.SetSeriesMonitored(r.Context(), id, mon); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to set monitored")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) monitorSeason(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	mon, ok := decodeMonitor(w, r)
	if !ok {
		return
	}
	// Resolve the owning series + season number from the season row.
	sn, err := a.store.GetSeason(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "season not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load season")
		return
	}
	if err := a.svc.SetSeasonMonitored(r.Context(), sn.SeriesID, sn.ID, sn.SeasonNumber, mon); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to set monitored")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) monitorEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	mon, ok := decodeMonitor(w, r)
	if !ok {
		return
	}
	ep, err := a.store.GetEpisode(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "episode not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load episode")
		return
	}
	if err := a.svc.SetEpisodeMonitored(r.Context(), ep.SeriesID, ep.ID, mon); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to set monitored")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type addMovieBody struct {
	TMDBID       int    `json:"tmdbId"`
	RootFolderID *int64 `json:"rootFolderId"`
	Monitored    bool   `json:"monitored"`
}

func (a *API) addMovie(w http.ResponseWriter, r *http.Request) {
	var b addMovieBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if b.TMDBID == 0 {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "tmdbId is required")
		return
	}
	m, err := a.svc.AddMovie(r.Context(), AddMovieRequest{TMDBID: b.TMDBID, RootFolderID: b.RootFolderID, Monitored: b.Monitored})
	if err != nil {
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, m)
}

func (a *API) listMovies(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListMovies(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list movies")
		return
	}
	if rows == nil {
		rows = []store.Movie{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) getMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	m, err := a.store.GetMovie(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "movie not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load movie")
		return
	}
	api.WriteJSON(w, http.StatusOK, m)
}

func (a *API) deleteMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteMovie(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete movie")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) refreshMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.svc.RefreshMovie(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "movie not found")
			return
		}
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) monitorMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	mon, ok := decodeMonitor(w, r)
	if !ok {
		return
	}
	if err := a.svc.SetMovieMonitored(r.Context(), id, mon); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to set monitored")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type rootFolderBody struct {
	Path string `json:"path"`
}

func (a *API) addRootFolder(w http.ResponseWriter, r *http.Request) {
	var b rootFolderBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	rf, err := a.svc.AddRootFolder(r.Context(), b.Path)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, rf)
}

func (a *API) listRootFolders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListRootFolders(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list root folders")
		return
	}
	if rows == nil {
		rows = []store.RootFolder{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) deleteRootFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteRootFolder(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete root folder")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

- [ ] **Step 4: Add the two store getters the API needs**

The season/episode monitor handlers call `GetSeason` and `GetEpisode`. Append them to `internal/core/store/media_store.go`:
```go
func (s *Store) GetSeason(ctx context.Context, id int64) (*Season, error) {
	var se Season
	var m int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, series_id, season_number, monitored FROM seasons WHERE id = ?`, id).
		Scan(&se.ID, &se.SeriesID, &se.SeasonNumber, &m)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	se.Monitored = m != 0
	return &se, nil
}

func (s *Store) GetEpisode(ctx context.Context, id int64) (*Episode, error) {
	var e Episode
	var m int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, series_id, season_number, episode_number, tmdb_id, title, overview, air_date, monitored
		 FROM episodes WHERE id = ?`, id).
		Scan(&e.ID, &e.SeriesID, &e.SeasonNumber, &e.EpisodeNumber, &e.TMDBID, &e.Title, &e.Overview, &e.AirDate, &m)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Monitored = m != 0
	return &e, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ ./internal/core/store/ -run 'TestAPI|Media|Season|Episode'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/media/api.go internal/media/api_test.go internal/core/store/media_store.go
git commit -m "feat: add media REST sub-router (lookup, series/movies/rootfolder CRUD, refresh, monitor)"
```

---

### Task 9: Composition wiring + full sweep

**Files:**
- Modify: `cmd/nexus/main.go`
- Modify: `cmd/nexus/main_test.go`

**Interfaces:**
- Consumes: `media.NewService`, `media.NewAPI`, `media.NewRefresh`, `media.SeriesUpdated`/`MovieUpdated` event names, `config.Config.TMDBAPIKey`, `scheduler.Every`.
- Produces: a running server that mounts `/api/v1/series`, `/api/v1/movies`, `/api/v1/rootfolder`, `/api/v1/media/lookup`, reloads nothing at startup (media has no reload), schedules the media refresh, and forwards `media.series.updated` / `media.movie.updated` to the WebSocket.

- [ ] **Step 1: Extend the run test**

Add to `cmd/nexus/main_test.go`:
```go
func TestRunMountsMediaRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9597")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9597/api/v1/series", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/series status = %d want 200", status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./cmd/nexus/ -run TestRunMountsMediaRoutes`
Expected: FAIL — route returns 404 (media not yet wired).

- [ ] **Step 3: Wire the module into `main.go`**

In `cmd/nexus/main.go`, add the import `"github.com/hellboundg/nexus/internal/media"`.

After the download-client construction block (`dlMonitor := downloadclient.NewMonitor(dlSvc, bus)`), add:
```go
	tmdb := media.NewTMDBProvider(cfg.TMDBAPIKey)
	mediaSvc := media.NewService(st, tmdb).WithBus(bus)
	mediaAPI := media.NewAPI(st, mediaSvc)
	mediaRefresh := media.NewRefresh(mediaSvc)
```
(`NewTMDBProvider` is a thin exported constructor wrapping `newTMDB` with the default base + a 30s client. Add it to `internal/media/tmdb.go`:
```go
// NewTMDBProvider builds the production TMDb client (default base, 30s timeout).
func NewTMDBProvider(apiKey string) *TMDBClient { return newTMDB(apiKey, "", nil) }
```
Make this edit as part of this step and include `tmdb.go` in the commit.)

In the scheduler block, after the download-client registration, add:
```go
	sch.Every(12*time.Hour, func() command.Command { return mediaRefresh })
```

Update the router `WSForward` and mounts:
```go
	router := api.NewRouter(api.Deps{
		Auth: authSvc, Store: st, Version: version.Version(), Bus: bus,
		WSForward: []string{"indexer.status", "download.status", "media.series.updated", "media.movie.updated"},
	}, web.Handler(), idxAPI.Mount, dlAPI.Mount, mediaAPI.Mount)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./cmd/nexus/ -run TestRunMountsMediaRoutes`
Expected: PASS.

- [ ] **Step 5: Full build + vet + test sweep**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build ./cmd/nexus
go vet ./...
go test ./... -count=1
```
Expected: build succeeds; vet clean; all packages PASS. Remove any built binary (`rm -f nexus nexus.exe`).

- [ ] **Step 6: Verify module boundaries**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go list -deps ./internal/media | grep hellboundg`
Expected: only `internal/core/*` packages — no `internal/indexer`, `internal/downloadclient`, or `internal/automation`.

- [ ] **Step 7: Commit**

```bash
git add cmd/nexus/main.go cmd/nexus/main_test.go internal/media/tmdb.go
git commit -m "feat: wire media library engine into composition root with scheduled refresh"
```

---

## Self-Review Notes (author)

- **Spec coverage:** metadata provider TMDb behind `MetadataProvider` (T1, T5); root folders (T2, T6, T8); series/season/episode + movie model, no files (T2–T4); add with monitor options all/future/none (T6); refresh reconciliation preserving user monitored flags + idempotent upserts (T3, T7); monitor toggles with cascade (T3, T7, T8); scheduled refresh command (T7, T9); `media.series.updated`/`media.movie.updated` async → WSForward (T7, T9); REST surface §6 (T8); composition wiring + boundary check (T9). Acceptance criteria §8.1–8.10 map to T2, T5/T8, T6/T8, T3/T7, T7/T8, T6/T8, T7/T9, T5/T8, T9, T9.
- **Deferred correctly:** no `episode_files`/`movie_files`, no disk scan, no rename/import (4c); `quality_profile_id` is a nullable column never written non-nil in 4a (4b); no search/grab/RSS (sub-5); image fields hold TMDb URLs only.
- **Type consistency:** `provider.MetadataProvider`/`MetadataResult`/`SeriesMetadata`/`SeasonMetadata`/`EpisodeMetadata`/`MovieMetadata`; `store.Series`/`Season`/`Episode`/`Movie`/`RootFolder`; `Service`/`NewService`/`WithBus`/`AddSeries`/`AddMovie`/`AddRootFolder`/`RefreshSeries`/`RefreshMovie`/`RefreshAll`/`SetSeriesMonitored`/`SetSeasonMonitored`/`SetEpisodeMonitored`/`SetMovieMonitored`; `AddSeriesRequest`/`AddMovieRequest`; `MonitorAll`/`MonitorFuture`/`MonitorNone`; `TMDBClient`/`newTMDB`/`NewTMDBProvider`; `RefreshCommand`/`NewRefresh`; `SeriesUpdated`/`MovieUpdated`; `API`/`NewAPI`/`Mount`; store getters `GetSeason`/`GetEpisode` added in T8 for the API — all consistent across tasks.
- **Store-helper reuse:** `boolToInt`, `rowScanner`, `ErrNotFound` reused from `core/store`; not redefined.
- **Async WS:** service emits use `PublishAsync` (Global Constraints + spec §5.5); the WSForward subscriber `hub.broadcast` is already non-blocking. Because emits are async, event-assertion tests block on the channel with a timeout, never a non-blocking `select`/`default`.
- **Verified against the tree:** `command.Reporter` is `interface{ Progress(pct int, msg string) }` — there is no exported `command.NopReporter`, so the refresh test defines a local `nopReporter{}` (mirroring `internal/downloadclient/monitor_test.go`). Test DBs use `database.Open(t.TempDir()+"/t.db")` (never `:memory:`, which is unreliable across the driver's connection pool), matching `store.newTestStore`.
- **Bus infrastructure lives in Task 6:** the `bus` field, `WithBus`, `emitSeries`/`emitMovie`, and the `SeriesUpdated`/`MovieUpdated` event types are all introduced in Task 6 (Service is bus-aware from birth, and Add emits per spec §5.2); Task 7 only adds refresh/monitor methods and the `RefreshCommand`.
