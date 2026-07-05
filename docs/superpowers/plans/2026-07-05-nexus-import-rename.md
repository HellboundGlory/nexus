# Nexus Import & Rename (Sub-project 4c) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the import pipeline — detect a completed, grab-tracked download, attribute it to the library item it was grabbed for, decide accept/reject/upgrade against the quality profile, rename via editable templates, hardlink (fallback copy) it into the root folder, track the file, and record history.

**Architecture:** New feature package `internal/importing` owns a `download_queue` (grab tracking), an `Enqueue` action, and the import pipeline. It imports only `internal/core/*` + `internal/parsing` + `internal/quality`; it reaches download clients through two consumer-defined interfaces (`Grabber`, `QueueReader`) satisfied by sub-3's `downloadclient.Service` at the composition root, and reads/writes library rows directly via `store`. A pure `naming` package renders templates. Migration `0006` adds `download_queue`, `media_files`, `history`.

**Tech Stack:** Go 1.26, `go-chi/chi/v5`, `modernc.org/sqlite` (pure Go, `CGO_ENABLED=0`), stdlib `net/http`/`httptest`/`os`/`io`/`path/filepath`/`regexp`, `log/slog`.

## Global Constraints

- Module path is `github.com/hellboundg/nexus`.
- Module boundaries (verify with `go list -deps` in Task 12): `internal/importing` imports **only** `internal/parsing`, `internal/quality`, and `internal/core/*` (incl. `internal/core/provider`); it must NOT import `internal/downloadclient`, `internal/media`, `internal/indexer`, or `internal/automation`. `internal/quality` still imports **only** `internal/parsing` + `internal/core/*`.
- Go is not on PATH: prefix every Go command with `export PATH="/c/Program Files/Go/bin:$PATH"`.
- `-race` is unavailable (no C compiler / `CGO_ENABLED=0`). Verify with `-count=N`, never `-race`.
- All tests are offline, deterministic, CGO-free: no network; `httptest`; real temp dirs via `t.TempDir()`. Test DBs use `database.Open(t.TempDir()+"/t.db")`, never `:memory:`. `database.Open` already sets `_pragma=foreign_keys(ON)`, so FK cascades/SET NULL are enforced in tests.
- Reuse existing store helpers (`boolToInt`, sentinel `store.ErrNotFound`) — do NOT redefine them.
- REST error responses go through `api.WriteError(w, status, code, msg)`; success through `api.WriteJSON(w, status, v)`. Sub-routers expose `Mount(r chi.Router)` and are mounted into the authed `/api/v1` group in `cmd/nexus/main.go` (variadic mounts on `api.NewRouter`).
- `command.Command` is `interface{ Name() string; Run(ctx context.Context, r command.Reporter) error }`; `command.Reporter` is `interface{ Progress(pct int, msg string) }` (tests define a local `nopReporter`). Scheduler: `sch.Every(d, func() command.Command { return sameInstance })` — a stateful command's factory MUST return the SAME instance each tick.
- Settings persistence: `store.GetSetting(ctx, key) (string, bool, error)` and `store.SetSetting(ctx, key, value) error`.
- No new credentials or external services. Grab reuses sub-3's server-side fetch.

---

### Task 1: `DownloadItem.OutputPath` + SAB/qBit population

**Files:**
- Modify: `internal/core/provider/provider.go`
- Modify: `internal/downloadclient/sabnzbd.go`
- Modify: `internal/downloadclient/qbittorrent.go`
- Modify: `internal/downloadclient/sabnzbd_test.go`
- Modify: `internal/downloadclient/qbittorrent_test.go`

**Interfaces:**
- Produces: `provider.DownloadItem.OutputPath string` (`json:"outputPath,omitempty"`), populated by both clients for completed items (SAB history `storage`, qBit `content_path`).
- Consumes: nothing new.

- [ ] **Step 1: Add the failing assertions**

In `internal/downloadclient/sabnzbd_test.go`, find the history-slot test fixture (the JSON with a completed history slot) and add a `"storage"` field to it, then assert the mapped item carries it. If the existing test builds the history JSON inline, add `"storage":"/downloads/complete/Show.S01E01"` to the completed slot and after mapping assert:
```go
if got := findByID(items, "<completed nzo id>"); got.OutputPath != "/downloads/complete/Show.S01E01" {
	t.Fatalf("OutputPath = %q, want the storage path", got.OutputPath)
}
```
(If no `findByID` helper exists, inline a loop to locate the completed item.) In `internal/downloadclient/qbittorrent_test.go`, add `"content_path":"/downloads/Movie.2019"` to the torrent-info fixture and assert the mapped item's `OutputPath == "/downloads/Movie.2019"`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/downloadclient/ -run 'Sab|QBit|Queue|History' -count=1`
Expected: FAIL — `OutputPath` is empty / undefined field.

- [ ] **Step 3: Add the field**

In `internal/core/provider/provider.go`, add to `DownloadItem` (after `ErrorMessage`):
```go
	OutputPath       string         `json:"outputPath,omitempty"` // final on-disk path for completed items
```

- [ ] **Step 4: Populate in SAB**

In `internal/downloadclient/sabnzbd.go`, the history slot struct: add the `storage` field to the JSON struct used for `h.History.Slots` (add `Storage string \`json:"storage"\`` to that struct definition). Then in the history-slot mapping loop, set it on completed items:
```go
		it := provider.DownloadItem{
			ID:               s.NzoID,
			Title:            s.Name,
			Status:           status,
			Size:             s.Bytes,
			Downloaded:       s.Bytes,
			DownloadClientID: c.id,
			Protocol:         provider.ProtocolUsenet,
			ErrorMessage:     s.FailMessage,
			OutputPath:       s.Storage,
		}
```

- [ ] **Step 5: Populate in qBit**

In `internal/downloadclient/qbittorrent.go`, add `ContentPath string \`json:"content_path"\`` to the torrent-info response struct (the one with `Completed int64`), and set `OutputPath: r.ContentPath` in the `provider.DownloadItem` it builds.

- [ ] **Step 6: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/downloadclient/ ./internal/core/provider/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/core/provider/provider.go internal/downloadclient/sabnzbd.go internal/downloadclient/qbittorrent.go internal/downloadclient/sabnzbd_test.go internal/downloadclient/qbittorrent_test.go
git commit -m "feat: surface completed-download OutputPath from SAB and qBit clients"
```

---

### Task 2: `quality.IsUpgrade` helper

**Files:**
- Modify: `internal/quality/decision.go`
- Create: `internal/quality/upgrade_test.go`

**Interfaces:**
- Produces: `quality.IsUpgrade(newID, existingID int, profile store.QualityProfile) bool` — true iff `newID` outranks `existingID` in the profile's item order, `profile.UpgradeAllowed` is true, and the existing quality is strictly below the profile cutoff's rank.
- Consumes: `profileRank` (already in `decision.go`), `store.QualityProfile`.

- [ ] **Step 1: Write the failing test**

Create `internal/quality/upgrade_test.go`:
```go
package quality

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/store"
)

func upProfile(upgrade bool) store.QualityProfile {
	// order: WEBDL-720p(7) < Bluray-1080p(9); cutoff = Bluray-1080p
	return store.QualityProfile{
		Name:            "HD",
		CutoffQualityID: 9,
		UpgradeAllowed:  upgrade,
		Items: []store.QualityProfileItem{
			{QualityID: 7, Allowed: true},
			{QualityID: 9, Allowed: true},
		},
	}
}

func TestIsUpgrade(t *testing.T) {
	p := upProfile(true)
	if !IsUpgrade(9, 7, p) {
		t.Fatal("Bluray-1080p over WEBDL-720p should be an upgrade")
	}
	if IsUpgrade(7, 9, p) {
		t.Fatal("lower quality is not an upgrade")
	}
	if IsUpgrade(9, 9, p) {
		t.Fatal("same quality is not an upgrade")
	}
	// existing already at cutoff rank -> no upgrade even to a higher-ranked allowed item
	if IsUpgrade(9, 9, p) {
		t.Fatal("at cutoff, no upgrade")
	}
	// upgrades disabled
	if IsUpgrade(9, 7, upProfile(false)) {
		t.Fatal("upgrades disabled -> never an upgrade")
	}
	// existing not in profile ranks below everything -> upgrade to any allowed
	if !IsUpgrade(7, 999, p) {
		t.Fatal("any allowed quality upgrades an unknown/absent existing quality")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -run TestIsUpgrade -count=1`
Expected: FAIL — `undefined: IsUpgrade`.

- [ ] **Step 3: Implement**

Append to `internal/quality/decision.go`:
```go
// IsUpgrade reports whether importing quality newID over an existing file of
// quality existingID is a profile-sanctioned upgrade: upgrades must be enabled,
// newID must rank strictly above existingID in the profile's item order, and the
// existing quality must rank strictly below the profile's cutoff (once the cutoff
// is met, no further upgrades). Qualities absent from the profile rank below all
// present ones (profileRank returns -1).
func IsUpgrade(newID, existingID int, profile store.QualityProfile) bool {
	if !profile.UpgradeAllowed {
		return false
	}
	newRank, _ := profileRank(profile, newID)
	existingRank, _ := profileRank(profile, existingID)
	cutoffRank, _ := profileRank(profile, profile.CutoffQualityID)
	if existingRank >= cutoffRank {
		return false
	}
	return newRank > existingRank
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -count=1`
Expected: PASS (all quality tests).

- [ ] **Step 5: Commit**

```bash
git add internal/quality/decision.go internal/quality/upgrade_test.go
git commit -m "feat: add quality.IsUpgrade profile-ranked upgrade check"
```

---

### Task 3: `naming` package — templates, Render, Sanitize

**Files:**
- Create: `internal/naming/naming.go`
- Create: `internal/naming/naming_test.go`

**Interfaces:**
- Produces: `naming.Tokens{SeriesTitle, EpisodeTitle, MovieTitle, Quality, ReleaseGroup string; Season, Episode, Year int}`; `naming.Config{SeriesFolder, SeasonFolder, EpisodeFile, MovieFolder, MovieFile string}`; `naming.DefaultConfig() Config`; `naming.Render(template string, t Tokens) string`; `naming.Sanitize(name string) string`.
- Consumes: nothing (pure, stdlib only). `internal/naming` imports NOTHING from the module (keeps it a leaf).

- [ ] **Step 1: Write the failing test**

Create `internal/naming/naming_test.go`:
```go
package naming

import "testing"

func TestRenderEpisodeAndMovie(t *testing.T) {
	tok := Tokens{SeriesTitle: "The Show", EpisodeTitle: "Pilot", Quality: "Bluray-1080p", Season: 2, Episode: 5}
	got := Render(DefaultConfig().EpisodeFile, tok)
	want := "The Show - S02E05 - Pilot [Bluray-1080p]"
	if got != want {
		t.Fatalf("episode = %q, want %q", got, want)
	}

	mt := Tokens{MovieTitle: "Movie Title", Year: 2019, Quality: "WEBDL-720p"}
	if got := Render(DefaultConfig().MovieFile, mt); got != "Movie Title (2019) [WEBDL-720p]" {
		t.Fatalf("movie = %q", got)
	}
	if got := Render(DefaultConfig().SeasonFolder, tok); got != "Season 02" {
		t.Fatalf("season folder = %q", got)
	}
}

func TestSanitize(t *testing.T) {
	if got := Sanitize(`A:B/C\\D?E*F "G" <H>|I`); got != "ABCDEF G HI" {
		t.Fatalf("sanitize = %q", got)
	}
	if got := Sanitize("trailing dots..."); got != "trailing dots" {
		t.Fatalf("trailing = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/naming/ -count=1`
Expected: FAIL — undefined package symbols.

- [ ] **Step 3: Implement**

Create `internal/naming/naming.go`:
```go
// Package naming renders library file/folder names from a small, editable token
// template set. Pure — no I/O, no module imports.
package naming

import (
	"fmt"
	"regexp"
	"strings"
)

// Tokens are the substitution values for a single render.
type Tokens struct {
	SeriesTitle  string
	EpisodeTitle string
	MovieTitle   string
	Quality      string
	ReleaseGroup string
	Season       int
	Episode      int
	Year         int
}

// Config holds the five editable templates.
type Config struct {
	SeriesFolder string `json:"seriesFolder"`
	SeasonFolder string `json:"seasonFolder"`
	EpisodeFile  string `json:"episodeFile"`
	MovieFolder  string `json:"movieFolder"`
	MovieFile    string `json:"movieFile"`
}

// DefaultConfig is the built-in template set.
func DefaultConfig() Config {
	return Config{
		SeriesFolder: "{Series Title}",
		SeasonFolder: "Season {season:00}",
		EpisodeFile:  "{Series Title} - S{season:00}E{episode:00} - {Episode Title} [{Quality}]",
		MovieFolder:  "{Movie Title} ({year})",
		MovieFile:    "{Movie Title} ({year}) [{Quality}]",
	}
}

// Render substitutes tokens in template. Unknown tokens are left as-is. The
// result is NOT sanitized — callers sanitize each path segment via Sanitize.
func Render(template string, t Tokens) string {
	r := strings.NewReplacer(
		"{Series Title}", t.SeriesTitle,
		"{Episode Title}", t.EpisodeTitle,
		"{Movie Title}", t.MovieTitle,
		"{Quality}", t.Quality,
		"{Release Group}", t.ReleaseGroup,
		"{season:00}", fmt.Sprintf("%02d", t.Season),
		"{episode:00}", fmt.Sprintf("%02d", t.Episode),
		"{season}", fmt.Sprintf("%d", t.Season),
		"{episode}", fmt.Sprintf("%d", t.Episode),
		"{year}", fmt.Sprintf("%d", t.Year),
	)
	return r.Replace(template)
}

var illegal = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// Sanitize strips characters illegal in file names on common filesystems,
// collapses the resulting whitespace, and trims trailing dots/spaces.
func Sanitize(name string) string {
	s := illegal.ReplaceAllString(name, "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimRight(s, " .")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/naming/ -count=1`
Expected: PASS. (Note: `Render` on `EpisodeFile` yields the exact spaces asserted; `Sanitize` removes illegal chars then collapses the double-spaces they leave behind.)

- [ ] **Step 5: Commit**

```bash
git add internal/naming/naming.go internal/naming/naming_test.go
git commit -m "feat: add naming package (token templates, Render, Sanitize)"
```

---

### Task 4: Migration 0006 + `download_queue` store

**Files:**
- Create: `internal/core/database/migrations/0006_import.sql`
- Create: `internal/core/store/import_store.go`
- Create: `internal/core/store/import_store_test.go`
- Modify: `internal/core/database/database_test.go` (migration count 5 → 6)

**Interfaces:**
- Produces: `store.QueueItem{ID int64; DownloadClientID, ClientItemID, Protocol, SourceTitle, MediaKind string; SeriesID, MovieID *int64; EpisodeIDs []int64; QualityID int; Status, Error string; CreatedAt, UpdatedAt time.Time}`; constants `store.QueueGrabbed/QueueImporting/QueueImported/QueueFailed` (string values `"grabbed"/"importing"/"imported"/"failed"`); methods `EnqueueGrab(ctx, QueueItem) (QueueItem, error)`, `ListQueue(ctx) ([]QueueItem, error)`, `QueueByStatus(ctx, status string) ([]QueueItem, error)`, `GetQueueItem(ctx, id int64) (QueueItem, error)`, `SetQueueStatus(ctx, id int64, status, errMsg string) error`, `DeleteQueueItem(ctx, id int64) error`.
- Consumes: `store.Store`, `store.ErrNotFound`, migration runner.

- [ ] **Step 1: Write the failing test**

Create `internal/core/store/import_store_test.go`:
```go
package store

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
)

func newImportTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func i64(v int64) *int64 { return &v }

func TestQueueCRUD(t *testing.T) {
	st := newImportTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S"}); err != nil {
		t.Fatal(err)
	}
	q := QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "Show.S01E01.1080p.BluRay-GRP", MediaKind: "tv",
		SeriesID: i64(1), EpisodeIDs: []int64{10, 11}, QualityID: 9, Status: QueueGrabbed,
	}
	created, err := st.EnqueueGrab(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("bad created: %+v", created)
	}
	got, err := st.GetQueueItem(ctx, created.ID)
	if err != nil || len(got.EpisodeIDs) != 2 || got.EpisodeIDs[1] != 11 || got.QualityID != 9 {
		t.Fatalf("roundtrip mismatch: %+v err=%v", got, err)
	}
	if err := st.SetQueueStatus(ctx, created.ID, QueueFailed, "boom"); err != nil {
		t.Fatal(err)
	}
	failed, _ := st.QueueByStatus(ctx, QueueFailed)
	if len(failed) != 1 || failed[0].Error != "boom" {
		t.Fatalf("QueueByStatus = %+v", failed)
	}
	all, _ := st.ListQueue(ctx)
	if len(all) != 1 {
		t.Fatalf("ListQueue len = %d", len(all))
	}
	if err := st.DeleteQueueItem(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetQueueItem(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run TestQueueCRUD -count=1`
Expected: FAIL — undefined `QueueItem` / missing table.

- [ ] **Step 3: Create the migration (all three tables)**

Create `internal/core/database/migrations/0006_import.sql`:
```sql
CREATE TABLE download_queue (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    download_client_id TEXT    NOT NULL,
    client_item_id     TEXT    NOT NULL,
    protocol           TEXT    NOT NULL,
    source_title       TEXT    NOT NULL,
    media_kind         TEXT    NOT NULL,
    series_id          INTEGER REFERENCES series(id) ON DELETE CASCADE,
    movie_id           INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    episode_ids        TEXT    NOT NULL DEFAULT '[]',
    quality_id         INTEGER NOT NULL,
    status             TEXT    NOT NULL,
    error              TEXT    NOT NULL DEFAULT '',
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(download_client_id, client_item_id)
);

CREATE TABLE media_files (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    media_kind    TEXT    NOT NULL,
    episode_id    INTEGER REFERENCES episodes(id) ON DELETE CASCADE,
    movie_id      INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    relative_path TEXT    NOT NULL,
    size          INTEGER NOT NULL,
    quality_id    INTEGER NOT NULL,
    added_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(episode_id),
    UNIQUE(movie_id)
);

CREATE TABLE history (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type   TEXT    NOT NULL,
    media_kind   TEXT    NOT NULL,
    series_id    INTEGER REFERENCES series(id)   ON DELETE SET NULL,
    episode_id   INTEGER REFERENCES episodes(id) ON DELETE SET NULL,
    movie_id     INTEGER REFERENCES movies(id)   ON DELETE SET NULL,
    source_title TEXT    NOT NULL DEFAULT '',
    quality_id   INTEGER,
    message      TEXT    NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 4: Create the `download_queue` store**

Create `internal/core/store/import_store.go`:
```go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Queue lifecycle statuses.
const (
	QueueGrabbed   = "grabbed"
	QueueImporting = "importing"
	QueueImported  = "imported"
	QueueFailed    = "failed"
)

// QueueItem is one grab-tracked download awaiting or having completed import.
type QueueItem struct {
	ID               int64     `json:"id"`
	DownloadClientID string    `json:"downloadClientId"`
	ClientItemID     string    `json:"clientItemId"`
	Protocol         string    `json:"protocol"`
	SourceTitle      string    `json:"sourceTitle"`
	MediaKind        string    `json:"mediaKind"`
	SeriesID         *int64    `json:"seriesId,omitempty"`
	MovieID          *int64    `json:"movieId,omitempty"`
	EpisodeIDs       []int64   `json:"episodeIds"`
	QualityID        int       `json:"qualityId"`
	Status           string    `json:"status"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func (s *Store) EnqueueGrab(ctx context.Context, q QueueItem) (QueueItem, error) {
	eps, err := json.Marshal(q.EpisodeIDs)
	if err != nil {
		return QueueItem{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO download_queue
		 (download_client_id, client_item_id, protocol, source_title, media_kind,
		  series_id, movie_id, episode_ids, quality_id, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.DownloadClientID, q.ClientItemID, q.Protocol, q.SourceTitle, q.MediaKind,
		q.SeriesID, q.MovieID, string(eps), q.QualityID, q.Status)
	if err != nil {
		return QueueItem{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetQueueItem(ctx, id)
}

func scanQueueItem(sc interface{ Scan(...any) error }) (QueueItem, error) {
	var (
		q   QueueItem
		eps string
	)
	if err := sc.Scan(&q.ID, &q.DownloadClientID, &q.ClientItemID, &q.Protocol, &q.SourceTitle,
		&q.MediaKind, &q.SeriesID, &q.MovieID, &eps, &q.QualityID, &q.Status, &q.Error,
		&q.CreatedAt, &q.UpdatedAt); err != nil {
		return QueueItem{}, err
	}
	if err := json.Unmarshal([]byte(eps), &q.EpisodeIDs); err != nil {
		return QueueItem{}, err
	}
	return q, nil
}

const queueCols = `id, download_client_id, client_item_id, protocol, source_title, media_kind,
	series_id, movie_id, episode_ids, quality_id, status, error, created_at, updated_at`

func (s *Store) GetQueueItem(ctx context.Context, id int64) (QueueItem, error) {
	q, err := scanQueueItem(s.db.QueryRowContext(ctx, `SELECT `+queueCols+` FROM download_queue WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return QueueItem{}, ErrNotFound
	}
	return q, err
}

func (s *Store) ListQueue(ctx context.Context) ([]QueueItem, error) {
	return s.queueQuery(ctx, `SELECT `+queueCols+` FROM download_queue ORDER BY id`)
}

func (s *Store) QueueByStatus(ctx context.Context, status string) ([]QueueItem, error) {
	return s.queueQuery(ctx, `SELECT `+queueCols+` FROM download_queue WHERE status = ? ORDER BY id`, status)
}

func (s *Store) queueQuery(ctx context.Context, query string, args ...any) ([]QueueItem, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueItem
	for rows.Next() {
		q, err := scanQueueItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) SetQueueStatus(ctx context.Context, id int64, status, errMsg string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE download_queue SET status = ?, error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, errMsg, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteQueueItem(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM download_queue WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 5: Bump the migration-count test**

In `internal/core/database/database_test.go`, change the `applied != 5` assertion (and its message) to `6`:
```go
	if applied != 6 {
		t.Fatalf("expected 6 applied migrations, got %d", applied)
	}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ ./internal/core/database/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/core/database/migrations/0006_import.sql internal/core/store/import_store.go internal/core/store/import_store_test.go internal/core/database/database_test.go
git commit -m "feat: add migration 0006 and download_queue store"
```

---

### Task 5: `media_files` store

**Files:**
- Modify: `internal/core/store/import_store.go`
- Modify: `internal/core/store/import_store_test.go`

**Interfaces:**
- Produces: `store.MediaFile{ID int64; MediaKind string; EpisodeID, MovieID *int64; RelativePath string; Size int64; QualityID int; AddedAt time.Time}`; methods `UpsertMediaFile(ctx, MediaFile) (MediaFile, error)` (replaces any existing file for the same episode/movie), `MediaFileForEpisode(ctx, episodeID int64) (*MediaFile, error)` (nil,nil when none), `MediaFileForMovie(ctx, movieID int64) (*MediaFile, error)`, `DeleteMediaFile(ctx, id int64) error`.
- Consumes: Task 4 store, `ErrNotFound`.

- [ ] **Step 1: Add the failing test**

Append to `internal/core/store/import_store_test.go`:
```go
func TestMediaFileUpsertAndLookup(t *testing.T) {
	st := newImportTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, Episode{SeriesID: 1, SeasonNumber: 1, EpisodeNumber: 1}); err != nil {
		t.Fatal(err)
	}
	eps, _ := st.ListEpisodes(ctx, 1)
	epID := eps[0].ID

	mf, err := st.UpsertMediaFile(ctx, MediaFile{MediaKind: "tv", EpisodeID: &epID, RelativePath: "S/Season 01/e1.mkv", Size: 100, QualityID: 7})
	if err != nil || mf.ID == 0 {
		t.Fatalf("upsert: %+v err=%v", mf, err)
	}
	got, err := st.MediaFileForEpisode(ctx, epID)
	if err != nil || got == nil || got.QualityID != 7 {
		t.Fatalf("lookup: %+v err=%v", got, err)
	}
	// upsert again for the same episode replaces (UNIQUE(episode_id))
	mf2, err := st.UpsertMediaFile(ctx, MediaFile{MediaKind: "tv", EpisodeID: &epID, RelativePath: "S/Season 01/e1.better.mkv", Size: 200, QualityID: 9})
	if err != nil {
		t.Fatal(err)
	}
	got2, _ := st.MediaFileForEpisode(ctx, epID)
	if got2.ID != mf2.ID || got2.QualityID != 9 {
		t.Fatalf("replace failed: %+v", got2)
	}
	// only one row total
	var n int
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM media_files`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("media_files count = %d err=%v", n, err)
	}
	// no file for a movie id
	nf, err := st.MediaFileForMovie(ctx, 999)
	if err != nil || nf != nil {
		t.Fatalf("expected nil file, got %+v err=%v", nf, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run TestMediaFileUpsertAndLookup -count=1`
Expected: FAIL — undefined `MediaFile`/methods.

- [ ] **Step 3: Implement**

Append to `internal/core/store/import_store.go` (add nothing to the import block — `json`, `sql`, `errors`, `time` are already imported):
```go
// MediaFile is one imported file on disk, linked to an episode or a movie.
type MediaFile struct {
	ID           int64     `json:"id"`
	MediaKind    string    `json:"mediaKind"`
	EpisodeID    *int64    `json:"episodeId,omitempty"`
	MovieID      *int64    `json:"movieId,omitempty"`
	RelativePath string    `json:"relativePath"`
	Size         int64     `json:"size"`
	QualityID    int       `json:"qualityId"`
	AddedAt      time.Time `json:"addedAt"`
}

// UpsertMediaFile inserts a file row, replacing any existing file for the same
// episode or movie (one current file per item, enforced by UNIQUE constraints).
func (s *Store) UpsertMediaFile(ctx context.Context, f MediaFile) (MediaFile, error) {
	if f.EpisodeID != nil {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM media_files WHERE episode_id = ?`, *f.EpisodeID); err != nil {
			return MediaFile{}, err
		}
	}
	if f.MovieID != nil {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM media_files WHERE movie_id = ?`, *f.MovieID); err != nil {
			return MediaFile{}, err
		}
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO media_files (media_kind, episode_id, movie_id, relative_path, size, quality_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		f.MediaKind, f.EpisodeID, f.MovieID, f.RelativePath, f.Size, f.QualityID)
	if err != nil {
		return MediaFile{}, err
	}
	id, _ := res.LastInsertId()
	return s.getMediaFile(ctx, id)
}

func (s *Store) getMediaFile(ctx context.Context, id int64) (MediaFile, error) {
	f, err := scanMediaFile(s.db.QueryRowContext(ctx,
		`SELECT id, media_kind, episode_id, movie_id, relative_path, size, quality_id, added_at FROM media_files WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return MediaFile{}, ErrNotFound
	}
	return f, err
}

func scanMediaFile(sc interface{ Scan(...any) error }) (MediaFile, error) {
	var f MediaFile
	err := sc.Scan(&f.ID, &f.MediaKind, &f.EpisodeID, &f.MovieID, &f.RelativePath, &f.Size, &f.QualityID, &f.AddedAt)
	return f, err
}

func (s *Store) MediaFileForEpisode(ctx context.Context, episodeID int64) (*MediaFile, error) {
	return s.mediaFileWhere(ctx, "episode_id", episodeID)
}

func (s *Store) MediaFileForMovie(ctx context.Context, movieID int64) (*MediaFile, error) {
	return s.mediaFileWhere(ctx, "movie_id", movieID)
}

func (s *Store) mediaFileWhere(ctx context.Context, col string, id int64) (*MediaFile, error) {
	f, err := scanMediaFile(s.db.QueryRowContext(ctx,
		`SELECT id, media_kind, episode_id, movie_id, relative_path, size, quality_id, added_at FROM media_files WHERE `+col+` = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) DeleteMediaFile(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run 'TestMediaFile|TestQueue' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/store/import_store.go internal/core/store/import_store_test.go
git commit -m "feat: add media_files store with one-file-per-item upsert"
```

---

### Task 6: `history` store

**Files:**
- Modify: `internal/core/store/import_store.go`
- Modify: `internal/core/store/import_store_test.go`

**Interfaces:**
- Produces: `store.HistoryEvent{ID int64; EventType, MediaKind string; SeriesID, EpisodeID, MovieID *int64; SourceTitle string; QualityID *int; Message string; CreatedAt time.Time}`; methods `AddHistory(ctx, HistoryEvent) error`, `ListHistory(ctx, limit int) ([]HistoryEvent, error)` (newest first; limit ≤ 0 → default 100).
- Consumes: Task 4 store.

- [ ] **Step 1: Add the failing test**

Append to `internal/core/store/import_store_test.go`:
```go
func TestHistoryAppendAndList(t *testing.T) {
	st := newImportTestStore(t)
	ctx := context.Background()
	if err := st.AddHistory(ctx, HistoryEvent{EventType: "grabbed", MediaKind: "tv", SourceTitle: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddHistory(ctx, HistoryEvent{EventType: "imported", MediaKind: "tv", SourceTitle: "B"}); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListHistory(ctx, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("history len = %d err=%v", len(list), err)
	}
	if list[0].SourceTitle != "B" { // newest first
		t.Fatalf("expected newest first, got %q", list[0].SourceTitle)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run TestHistoryAppendAndList -count=1`
Expected: FAIL — undefined `HistoryEvent`/methods.

- [ ] **Step 3: Implement**

Append to `internal/core/store/import_store.go`:
```go
// HistoryEvent is one append-only library event.
type HistoryEvent struct {
	ID          int64     `json:"id"`
	EventType   string    `json:"eventType"`
	MediaKind   string    `json:"mediaKind"`
	SeriesID    *int64    `json:"seriesId,omitempty"`
	EpisodeID   *int64    `json:"episodeId,omitempty"`
	MovieID     *int64    `json:"movieId,omitempty"`
	SourceTitle string    `json:"sourceTitle,omitempty"`
	QualityID   *int      `json:"qualityId,omitempty"`
	Message     string    `json:"message,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Store) AddHistory(ctx context.Context, h HistoryEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO history (event_type, media_kind, series_id, episode_id, movie_id, source_title, quality_id, message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		h.EventType, h.MediaKind, h.SeriesID, h.EpisodeID, h.MovieID, h.SourceTitle, h.QualityID, h.Message)
	return err
}

func (s *Store) ListHistory(ctx context.Context, limit int) ([]HistoryEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, media_kind, series_id, episode_id, movie_id, source_title, quality_id, message, created_at
		 FROM history ORDER BY id DESC LIMIT ?`, limit)
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

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -count=1`
Expected: PASS (all store tests).

- [ ] **Step 5: Commit**

```bash
git add internal/core/store/import_store.go internal/core/store/import_store_test.go
git commit -m "feat: add history store (append + newest-first list)"
```

---

### Task 7: `importing` Service + naming config + Enqueue

**Files:**
- Create: `internal/importing/importing.go`
- Create: `internal/importing/enqueue.go`
- Create: `internal/importing/enqueue_test.go`

**Interfaces:**
- Produces:
  - `importing.Grabber interface { Grab(ctx, provider.DownloadRequest, clientID string) (string, error) }`
  - `importing.QueueReader interface { Queue(ctx) []provider.DownloadItem; Remove(ctx, clientID, itemID string, deleteData bool) error }`
  - `importing.Service`; `importing.NewService(st *store.Store, grab Grabber, q QueueReader, bus *events.Bus) *Service`
  - `importing.EnqueueRequest{ DownloadURL, Title string; Protocol provider.Protocol; IndexerID, ClientID string; MediaKind provider.MediaKind; SeriesID int64; EpisodeIDs []int64; MovieID int64 }`
  - `(*Service).Enqueue(ctx, EnqueueRequest) (store.QueueItem, error)`; sentinel `importing.ErrRejected`, `importing.ErrNoProfile`
  - `(*Service).NamingConfig(ctx) (naming.Config, error)`, `(*Service).SetNamingConfig(ctx, naming.Config) error` (persist to settings key `naming.config`)
- Consumes: `store` (Task 4-6), `naming` (Task 3), `parsing`, `quality` (Decide/Resolve), `provider`, `events`.

- [ ] **Step 1: Write the failing test**

Create `internal/importing/enqueue_test.go`:
```go
package importing

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type fakeGrabber struct {
	lastReq  provider.DownloadRequest
	returnID string
	err      error
}

func (f *fakeGrabber) Grab(_ context.Context, req provider.DownloadRequest, _ string) (string, error) {
	f.lastReq = req
	return f.returnID, f.err
}

type fakeQueue struct {
	items []provider.DownloadItem
}

func (f *fakeQueue) Queue(context.Context) []provider.DownloadItem { return f.items }
func (f *fakeQueue) Remove(context.Context, string, string, bool) error { return nil }

func newSvc(t *testing.T) (*Service, *store.Store) {
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
	return NewService(st, &fakeGrabber{returnID: "h1"}, &fakeQueue{}, nil), st
}

// seed a series with a quality profile that allows Bluray-1080p(9).
func seedSeriesWithProfile(t *testing.T, st *store.Store) (seriesID int64, epID int64) {
	t.Helper()
	ctx := context.Background()
	prof, err := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 7, Allowed: true}, {QualityID: 9, Allowed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesQualityProfileID(ctx, sid, &prof.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"}); err != nil {
		t.Fatal(err)
	}
	eps, _ := st.ListEpisodes(ctx, sid)
	return sid, eps[0].ID
}

func TestEnqueueAcceptsAndTracks(t *testing.T) {
	svc, st := newSvc(t)
	ctx := context.Background()
	sid, epID := seedSeriesWithProfile(t, st)

	q, err := svc.Enqueue(ctx, EnqueueRequest{
		DownloadURL: "http://x/nzb", Title: "The.Show.S01E01.1080p.BluRay.x264-GRP",
		Protocol: provider.ProtocolUsenet, MediaKind: provider.KindTV,
		SeriesID: sid, EpisodeIDs: []int64{epID},
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if q.Status != store.QueueGrabbed || q.ClientItemID != "h1" || q.QualityID != 9 {
		t.Fatalf("bad queue row: %+v", q)
	}
	hist, _ := st.ListHistory(ctx, 10)
	if len(hist) != 1 || hist[0].EventType != "grabbed" {
		t.Fatalf("expected grabbed history, got %+v", hist)
	}
}

func TestEnqueueRejectsDisallowedQuality(t *testing.T) {
	svc, st := newSvc(t)
	ctx := context.Background()
	sid, epID := seedSeriesWithProfile(t, st)
	// 2160p resolves to Bluray-2160p(12), not in the profile -> rejected.
	_, err := svc.Enqueue(ctx, EnqueueRequest{
		DownloadURL: "http://x/nzb", Title: "The.Show.S01E01.2160p.BluRay.x265-GRP",
		Protocol: provider.ProtocolUsenet, MediaKind: provider.KindTV,
		SeriesID: sid, EpisodeIDs: []int64{epID},
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if all, _ := st.ListQueue(ctx); len(all) != 0 {
		t.Fatalf("no row should be written on reject, got %d", len(all))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -count=1`
Expected: FAIL — undefined package symbols.

- [ ] **Step 3: Create the Service scaffold + naming config**

Create `internal/importing/importing.go`:
```go
// Package importing owns the grab-tracking download queue and the import
// pipeline: it attributes completed downloads to library items, decides
// accept/reject/upgrade, renames via templates, hardlinks files into root
// folders, tracks files, and records history. It imports only internal/core/*,
// internal/parsing, and internal/quality; download clients are reached via the
// Grabber/QueueReader interfaces wired at the composition root.
package importing

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/naming"
)

// Grabber fetches a release and adds it to a download client, returning the
// client's item id. Satisfied by downloadclient.Service.Grab.
type Grabber interface {
	Grab(ctx context.Context, req provider.DownloadRequest, clientID string) (string, error)
}

// QueueReader reads the aggregated download-client queue and removes items.
// Satisfied by a thin adapter over downloadclient.Service at the composition root.
type QueueReader interface {
	Queue(ctx context.Context) []provider.DownloadItem
	Remove(ctx context.Context, clientID, itemID string, deleteData bool) error
}

const namingSettingKey = "naming.config"

// Service owns enqueue + import.
type Service struct {
	store *store.Store
	grab  Grabber
	queue QueueReader
	bus   *events.Bus
}

func NewService(st *store.Store, grab Grabber, q QueueReader, bus *events.Bus) *Service {
	return &Service{store: st, grab: grab, queue: q, bus: bus}
}

// NamingConfig returns the persisted config, or the defaults if none saved.
func (s *Service) NamingConfig(ctx context.Context) (naming.Config, error) {
	raw, ok, err := s.store.GetSetting(ctx, namingSettingKey)
	if err != nil {
		return naming.Config{}, err
	}
	if !ok {
		return naming.DefaultConfig(), nil
	}
	var c naming.Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return naming.Config{}, err
	}
	return c, nil
}

// SetNamingConfig persists the naming config.
func (s *Service) SetNamingConfig(ctx context.Context, c naming.Config) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, namingSettingKey, string(b))
}

func (s *Service) emit(ctx context.Context, ev events.Event) {
	if s.bus != nil {
		s.bus.PublishAsync(ctx, ev)
	}
}

// QueueUpdated is emitted when a queue row changes.
type QueueUpdated struct {
	ID int64 `json:"id"`
}

func (QueueUpdated) Name() string { return "queue.updated" }

// ImportCompletedEvent is emitted after an import attempt on a row.
type ImportCompletedEvent struct {
	QueueID int64  `json:"queueId"`
	Status  string `json:"status"`
}

func (ImportCompletedEvent) Name() string { return "import.completed" }

var (
	// ErrRejected means the release's quality is not allowed by the item's profile.
	ErrRejected = errors.New("importing: release rejected by quality profile")
	// ErrNoProfile means the target media item has no quality profile assigned.
	ErrNoProfile = errors.New("importing: media item has no quality profile")
)
```

Note: `events.Event` is the bus event interface (`interface{ Name() string }`) already used by `SeriesUpdated`/`DownloadStatusChanged`; confirm its exact name in `internal/core/events` and match it (if the method-only interface is unnamed in signatures, `PublishAsync(ctx, ev)` takes whatever `SeriesUpdated` implements — mirror that call site).

- [ ] **Step 4: Implement Enqueue**

Create `internal/importing/enqueue.go`:
```go
package importing

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/quality"
)

// EnqueueRequest grabs one release for one library item (single episode, a set
// of episodes for a pack, or a movie) and records the tracking row.
type EnqueueRequest struct {
	DownloadURL string
	Title       string
	Protocol    provider.Protocol
	IndexerID   string
	ClientID    string // optional client override
	MediaKind   provider.MediaKind
	SeriesID    int64
	EpisodeIDs  []int64
	MovieID     int64
}

// Enqueue decides the release against the item's profile, grabs it, and writes a
// grabbed queue row + history. Returns ErrRejected/ErrNoProfile without grabbing.
func (s *Service) Enqueue(ctx context.Context, req EnqueueRequest) (store.QueueItem, error) {
	profile, err := s.profileFor(ctx, req.MediaKind, req.SeriesID, req.MovieID)
	if err != nil {
		return store.QueueItem{}, err
	}
	parsed := parsing.Parse(req.Title, req.MediaKind)
	decision := quality.Decide(parsed, profile)
	if !decision.Accepted {
		return store.QueueItem{}, ErrRejected
	}
	itemID, err := s.grab.Grab(ctx, provider.DownloadRequest{
		URL: req.DownloadURL, Title: req.Title, Protocol: req.Protocol, IndexerID: req.IndexerID,
	}, req.ClientID)
	if err != nil {
		return store.QueueItem{}, err
	}
	row := store.QueueItem{
		DownloadClientID: req.ClientID, ClientItemID: itemID, Protocol: string(req.Protocol),
		SourceTitle: req.Title, MediaKind: string(req.MediaKind), EpisodeIDs: req.EpisodeIDs,
		QualityID: decision.Quality.ID, Status: store.QueueGrabbed,
	}
	if req.MediaKind == provider.KindTV {
		row.SeriesID = &req.SeriesID
	} else {
		row.MovieID = &req.MovieID
	}
	created, err := s.store.EnqueueGrab(ctx, row)
	if err != nil {
		return store.QueueItem{}, err
	}
	qid := decision.Quality.ID
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: "grabbed", MediaKind: string(req.MediaKind), SeriesID: created.SeriesID,
		MovieID: created.MovieID, SourceTitle: req.Title, QualityID: &qid,
	})
	s.emit(ctx, QueueUpdated{ID: created.ID})
	return created, nil
}

// profileFor loads the quality profile assigned to the target media item.
func (s *Service) profileFor(ctx context.Context, kind provider.MediaKind, seriesID, movieID int64) (store.QualityProfile, error) {
	var profileID *int64
	if kind == provider.KindTV {
		se, err := s.store.GetSeries(ctx, seriesID)
		if err != nil {
			return store.QualityProfile{}, err
		}
		profileID = se.QualityProfileID
	} else {
		m, err := s.store.GetMovie(ctx, movieID)
		if err != nil {
			return store.QualityProfile{}, err
		}
		profileID = m.QualityProfileID
	}
	if profileID == nil {
		return store.QualityProfile{}, ErrNoProfile
	}
	return s.store.GetQualityProfile(ctx, *profileID)
}
```

Note: confirm `store.GetSeries`/`store.GetMovie` return a pointer (`*store.Series`) — from 4a they do; adjust `se.QualityProfileID`/`m.QualityProfileID` field access accordingly (both are `*int64`).

- [ ] **Step 5: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/importing/importing.go internal/importing/enqueue.go internal/importing/enqueue_test.go
git commit -m "feat: add importing Service, naming config, and Enqueue"
```

---

### Task 8: Import pipeline — single-file import

**Files:**
- Create: `internal/importing/fileops.go`
- Create: `internal/importing/importer.go`
- Create: `internal/importing/importer_test.go`

**Interfaces:**
- Produces: `(*Service).ImportItem(ctx, queueID int64) error` — imports the completed download for one queue row (this task: movie or single-episode, first-time import, no existing file). Helpers `isVideoFile(name string) bool`, `isSample(name string, size int64) bool`, `placeFile(src, dst string) error` (hardlink → copy fallback, creates parent dirs). Sets the row to `imported`/`failed` and writes `media_files` + `history`.
- Consumes: Task 4-7; `naming`, `quality.Resolve`, `parsing.Parse`, `QueueReader`.

- [ ] **Step 1: Write the failing test**

Create `internal/importing/importer_test.go`:
```go
package importing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportSingleEpisode(t *testing.T) {
	ctx := context.Background()
	db := t.TempDir()
	_ = db
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)

	// root folder + series + profile + episode
	root := t.TempDir()
	rf, err := st.CreateRootFolder(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show", RootFolderID: &rf.ID, QualityProfileID: &prof.ID})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"})
	eps, _ := st.ListEpisodes(ctx, sid)
	epID := eps[0].ID

	// a completed download folder with one video + a sample + junk
	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.1080p.BluRay.x264-GRP.mkv"), 60*1024*1024)
	writeFile(t, filepath.Join(dl, "sample.mkv"), 5*1024*1024)
	writeFile(t, filepath.Join(dl, "readme.nfo"), 10)

	// queue row + a matching completed client item exposing the output path
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01E01.1080p.BluRay.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{epID}, QualityID: 9, Status: store.QueueGrabbed,
	})
	fq.items = []provider.DownloadItem{{
		ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl,
	}}

	if err := svc.ImportItem(ctx, q.ID); err != nil {
		t.Fatalf("import: %v", err)
	}

	// media_file recorded, file placed at rendered path, junk skipped
	mf, _ := st.MediaFileForEpisode(ctx, epID)
	if mf == nil {
		t.Fatal("no media file recorded")
	}
	want := filepath.Join(root, "The Show", "Season 01", "The Show - S01E01 - Pilot [Bluray-1080p].mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected imported file at %s: %v", want, err)
	}
	updated, _ := st.GetQueueItem(ctx, q.ID)
	if updated.Status != store.QueueImported {
		t.Fatalf("status = %q want imported", updated.Status)
	}
	hist, _ := st.ListHistory(ctx, 10)
	if len(hist) == 0 || hist[0].EventType != "imported" {
		t.Fatalf("expected imported history, got %+v", hist)
	}
}
```

Add this helper to `enqueue_test.go` (so both test files share it):
```go
func newSvcWithQueue(t *testing.T, q QueueReader) (*Service, *store.Store) {
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
	return NewService(st, &fakeGrabber{returnID: "h1"}, q, nil), st
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -run TestImportSingleEpisode -count=1`
Expected: FAIL — `undefined: ImportItem`.

- [ ] **Step 3: Implement fileops**

Create `internal/importing/fileops.go`:
```go
package importing

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

const minVideoSize = 50 * 1024 * 1024 // 50 MB — below this is treated as a sample/extra

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".m4v": true, ".ts": true, ".wmv": true, ".mov": true,
}

func isVideoFile(name string) bool { return videoExts[strings.ToLower(filepath.Ext(name))] }

func isSample(name string, size int64) bool {
	if strings.Contains(strings.ToLower(name), "sample") {
		return true
	}
	return size < minVideoSize
}

// placeFile hardlinks src to dst, falling back to a copy on cross-device/link
// failure. It creates parent directories. If dst already exists it is left as-is
// (idempotent retry). The original src is never removed (seeding-safe).
func placeFile(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil // already placed (retry)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// videoFilesIn walks root and returns non-sample video files (absolute paths).
func videoFilesIn(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() { // single-file download
		if isVideoFile(root) && !isSample(filepath.Base(root), info.Size()) {
			return []string{root}, nil
		}
		return nil, nil
	}
	var out []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !isVideoFile(d.Name()) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if isSample(d.Name(), fi.Size()) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out, err
}
```

- [ ] **Step 4: Implement ImportItem (single-target, first import)**

Create `internal/importing/importer.go`:
```go
package importing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/naming"
	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/quality"
)

// ImportItem imports the completed download tracked by the given queue row.
func (s *Service) ImportItem(ctx context.Context, queueID int64) error {
	row, err := s.store.GetQueueItem(ctx, queueID)
	if err != nil {
		return err
	}
	outputPath, ok := s.completedOutputPath(ctx, row)
	if !ok {
		return s.fail(ctx, row, "download not completed or not found")
	}
	_ = s.store.SetQueueStatus(ctx, row.ID, store.QueueImporting, "")

	cfg, err := s.NamingConfig(ctx)
	if err != nil {
		return s.fail(ctx, row, "load naming config: "+err.Error())
	}
	files, err := videoFilesIn(outputPath)
	if err != nil {
		return s.fail(ctx, row, "scan output: "+err.Error())
	}
	if len(files) == 0 {
		return s.fail(ctx, row, "no video files found")
	}

	kind := provider.MediaKind(row.MediaKind)
	imported := 0
	for _, f := range files {
		if err := s.importFile(ctx, row, kind, cfg, f); err != nil {
			return s.fail(ctx, row, err.Error())
		}
		imported++
	}
	if imported == 0 {
		return s.fail(ctx, row, "nothing imported")
	}
	_ = s.store.SetQueueStatus(ctx, row.ID, store.QueueImported, "")
	s.emit(ctx, ImportCompletedEvent{QueueID: row.ID, Status: store.QueueImported})
	s.emit(ctx, QueueUpdated{ID: row.ID})
	return nil
}

// completedOutputPath finds the client item for this row and returns its output
// path if it is completed.
func (s *Service) completedOutputPath(ctx context.Context, row store.QueueItem) (string, bool) {
	for _, it := range s.queue.Queue(ctx) {
		if it.DownloadClientID == row.DownloadClientID && it.ID == row.ClientItemID {
			if it.Status == provider.StatusCompleted && it.OutputPath != "" {
				return it.OutputPath, true
			}
			return "", false
		}
	}
	return "", false
}

// importFile places one video file for a movie or single episode (first import).
func (s *Service) importFile(ctx context.Context, row store.QueueItem, kind provider.MediaKind, cfg naming.Config, srcFile string) error {
	parsed := parsing.Parse(filepath.Base(srcFile), kind)
	q := quality.Resolve(parsed)
	if q.ID == 0 {
		if d, ok := quality.DefinitionByID(row.QualityID); ok {
			q = d
		}
	}
	ext := filepath.Ext(srcFile)

	if kind == provider.KindMovie {
		m, err := s.store.GetMovie(ctx, *row.MovieID)
		if err != nil {
			return err
		}
		root, err := s.rootPath(ctx, m.RootFolderID)
		if err != nil {
			return err
		}
		tok := naming.Tokens{MovieTitle: m.Title, Year: m.Year, Quality: q.Name, ReleaseGroup: parsed.ReleaseGroup}
		dst := filepath.Join(root, naming.Sanitize(naming.Render(cfg.MovieFolder, tok)), naming.Sanitize(naming.Render(cfg.MovieFile, tok))+ext)
		if err := s.placeAndRecord(ctx, row, dst, root, srcFile, q, store.MediaFile{MediaKind: "movie", MovieID: row.MovieID}); err != nil {
			return err
		}
		return nil
	}

	// TV: single episode = the one recorded episode id.
	if len(row.EpisodeIDs) != 1 {
		return fmt.Errorf("season-pack import not handled in this task")
	}
	epID := row.EpisodeIDs[0]
	ep, err := s.store.GetEpisode(ctx, epID)
	if err != nil {
		return err
	}
	se, err := s.store.GetSeries(ctx, *row.SeriesID)
	if err != nil {
		return err
	}
	root, err := s.rootPath(ctx, se.RootFolderID)
	if err != nil {
		return err
	}
	tok := naming.Tokens{
		SeriesTitle: se.Title, EpisodeTitle: ep.Title, Quality: q.Name,
		ReleaseGroup: parsed.ReleaseGroup, Season: ep.SeasonNumber, Episode: ep.EpisodeNumber,
	}
	dst := filepath.Join(root,
		naming.Sanitize(naming.Render(cfg.SeriesFolder, tok)),
		naming.Sanitize(naming.Render(cfg.SeasonFolder, tok)),
		naming.Sanitize(naming.Render(cfg.EpisodeFile, tok))+ext)
	return s.placeAndRecord(ctx, row, dst, root, srcFile, q, store.MediaFile{MediaKind: "tv", EpisodeID: &epID})
}

// placeAndRecord hardlinks the file, records the media_files row (relative to the
// root folder), and writes an imported history entry.
func (s *Service) placeAndRecord(ctx context.Context, row store.QueueItem, dst, root, srcFile string, q quality.QualityDefinition, mf store.MediaFile) error {
	if err := placeFile(srcFile, dst); err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dst)
	if err != nil {
		rel = dst
	}
	fi, _ := os.Stat(dst)
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	mf.RelativePath = filepath.ToSlash(rel)
	mf.Size = size
	mf.QualityID = q.ID
	if _, err := s.store.UpsertMediaFile(ctx, mf); err != nil {
		return err
	}
	qid := q.ID
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: "imported", MediaKind: row.MediaKind, SeriesID: row.SeriesID, MovieID: row.MovieID,
		EpisodeID: mf.EpisodeID, SourceTitle: row.SourceTitle, QualityID: &qid,
	})
	return nil
}

func (s *Service) rootPath(ctx context.Context, rootFolderID *int64) (string, error) {
	if rootFolderID == nil {
		return "", fmt.Errorf("item has no root folder")
	}
	rf, err := s.store.GetRootFolder(ctx, *rootFolderID)
	if err != nil {
		return "", err
	}
	return rf.Path, nil
}

// fail marks the row failed, records history, and emits.
func (s *Service) fail(ctx context.Context, row store.QueueItem, msg string) error {
	_ = s.store.SetQueueStatus(ctx, row.ID, store.QueueFailed, msg)
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: "import_failed", MediaKind: row.MediaKind, SeriesID: row.SeriesID,
		MovieID: row.MovieID, SourceTitle: row.SourceTitle, Message: msg,
	})
	s.emit(ctx, ImportCompletedEvent{QueueID: row.ID, Status: store.QueueFailed})
	s.emit(ctx, QueueUpdated{ID: row.ID})
	return nil
}
```

Note: confirm `store.GetRootFolder(ctx, id)` returns `(store.RootFolder, error)` or a pointer (from 4a) and that `RootFolder.Path` exists; adjust `rf.Path` access accordingly. Confirm `store.Episode` has `SeasonNumber`/`EpisodeNumber`/`Title` and `store.Movie` has `Year`/`RootFolderID` (all from 4a). `ImportItem` returns `nil` even on a handled import failure (the row carries the failure) — it returns a non-nil error only for unexpected store/IO faults the caller should surface.

- [ ] **Step 5: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/importing/fileops.go internal/importing/importer.go internal/importing/importer_test.go internal/importing/enqueue_test.go
git commit -m "feat: import pipeline for single-file movie/episode downloads"
```

---

### Task 9: Upgrades + season packs

**Files:**
- Modify: `internal/importing/importer.go`
- Modify: `internal/importing/importer_test.go`

**Interfaces:**
- Produces: `importFile` now (a) for TV with >1 recorded episode, parses each video file and matches it to the recorded episode by season+episode; (b) when the target already has a `media_files` row, imports only if `quality.IsUpgrade` — replacing and deleting the old file (history `upgraded`) — else records an `import_failed` "not an upgrade" and leaves the existing file. Adds `matchEpisode` + refactors the per-file body into an episode/movie target resolver.
- Consumes: `quality.IsUpgrade` (Task 2), Task 8 code.

- [ ] **Step 1: Add the failing tests**

Append to `internal/importing/importer_test.go`:
```go
func TestImportUpgradeReplacesLowerQuality(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rf, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 6, Allowed: true}, {QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show", RootFolderID: &rf.ID, QualityProfileID: &prof.ID})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"})
	eps, _ := st.ListEpisodes(ctx, sid)
	epID := eps[0].ID

	// existing WEBDL-720p(6) file on disk + tracked
	oldPath := filepath.Join(root, "The Show", "Season 01", "old.mkv")
	writeFile(t, oldPath, 60*1024*1024)
	_, _ = st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "tv", EpisodeID: &epID, RelativePath: "The Show/Season 01/old.mkv", Size: 1, QualityID: 6})

	// new Bluray-1080p(9) download
	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.1080p.BluRay.x264-GRP.mkv"), 70*1024*1024)
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01E01.1080p.BluRay.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{epID}, QualityID: 9, Status: store.QueueGrabbed,
	})
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl}}

	if err := svc.ImportItem(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	mf, _ := st.MediaFileForEpisode(ctx, epID)
	if mf == nil || mf.QualityID != 9 {
		t.Fatalf("expected upgraded to 9, got %+v", mf)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file should be deleted, stat err = %v", err)
	}
	hist, _ := st.ListHistory(ctx, 10)
	if hist[0].EventType != "upgraded" {
		t.Fatalf("expected upgraded history, got %q", hist[0].EventType)
	}
}

func TestImportRejectsNonUpgrade(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rf, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 6, Allowed: true}, {QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show", RootFolderID: &rf.ID, QualityProfileID: &prof.ID})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"})
	eps, _ := st.ListEpisodes(ctx, sid)
	epID := eps[0].ID
	// existing Bluray-1080p(9), at cutoff
	_, _ = st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "tv", EpisodeID: &epID, RelativePath: "keep.mkv", Size: 1, QualityID: 9})

	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.720p.WEB-DL.x264-GRP.mkv"), 60*1024*1024)
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01E01.720p.WEB-DL.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{epID}, QualityID: 6, Status: store.QueueGrabbed,
	})
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl}}

	_ = svc.ImportItem(ctx, q.ID)
	mf, _ := st.MediaFileForEpisode(ctx, epID)
	if mf.QualityID != 9 || mf.RelativePath != "keep.mkv" {
		t.Fatalf("existing file should be kept, got %+v", mf)
	}
	updated, _ := st.GetQueueItem(ctx, q.ID)
	if updated.Status != store.QueueFailed {
		t.Fatalf("status = %q want failed (not an upgrade)", updated.Status)
	}
}

func TestImportSeasonPack(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rf, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show", RootFolderID: &rf.ID, QualityProfileID: &prof.ID})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "One"})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 2, Title: "Two"})
	eps, _ := st.ListEpisodes(ctx, sid)
	var ep1, ep2 int64
	for _, e := range eps {
		if e.EpisodeNumber == 1 {
			ep1 = e.ID
		} else {
			ep2 = e.ID
		}
	}
	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.1080p.BluRay.x264-GRP.mkv"), 60*1024*1024)
	writeFile(t, filepath.Join(dl, "The.Show.S01E02.1080p.BluRay.x264-GRP.mkv"), 60*1024*1024)
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01.1080p.BluRay.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{ep1, ep2}, QualityID: 9, Status: store.QueueGrabbed,
	})
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl}}

	if err := svc.ImportItem(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	if mf, _ := st.MediaFileForEpisode(ctx, ep1); mf == nil {
		t.Fatal("episode 1 not imported")
	}
	if mf, _ := st.MediaFileForEpisode(ctx, ep2); mf == nil {
		t.Fatal("episode 2 not imported")
	}
	if updated, _ := st.GetQueueItem(ctx, q.ID); updated.Status != store.QueueImported {
		t.Fatalf("status = %q want imported", updated.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -run 'Upgrade|NonUpgrade|SeasonPack' -count=1`
Expected: FAIL — season-pack error / upgrade not handled.

- [ ] **Step 3: Rework `importFile` for targets, upgrades, and packs**

Replace the `importFile` function in `internal/importing/importer.go` with a version that resolves a target per file, handles existing files via `IsUpgrade`, and tracks per-episode success. Replace the whole `func (s *Service) importFile(...)` block from Task 8 with:
```go
// importTarget is the resolved destination for one video file.
type importTarget struct {
	episodeID *int64
	movieID   *int64
	dst       string
}

// importFile resolves the target for one video file and imports it, honoring
// upgrades. Returns (imported, error): imported is false when the file was a
// deliberate skip (no target match, or not an upgrade) that should not fail the
// whole row on its own.
func (s *Service) importFile(ctx context.Context, row store.QueueItem, kind provider.MediaKind, cfg naming.Config, srcFile string) (bool, error) {
	parsed := parsing.Parse(filepath.Base(srcFile), kind)
	q := quality.Resolve(parsed)
	if q.ID == 0 {
		if d, ok := quality.DefinitionByID(row.QualityID); ok {
			q = d
		}
	}
	ext := filepath.Ext(srcFile)

	target, profile, mf, err := s.resolveTarget(ctx, row, kind, cfg, parsed, q, ext)
	if err != nil {
		return false, err
	}
	if target == nil {
		return false, nil // no matching library item for this file — skip
	}

	// existing-file upgrade check
	var existing *store.MediaFile
	if target.episodeID != nil {
		existing, _ = s.store.MediaFileForEpisode(ctx, *target.episodeID)
	} else {
		existing, _ = s.store.MediaFileForMovie(ctx, *target.movieID)
	}
	if existing != nil {
		if !quality.IsUpgrade(q.ID, existing.QualityID, profile) {
			qid := q.ID
			_ = s.store.AddHistory(ctx, store.HistoryEvent{
				EventType: "import_failed", MediaKind: row.MediaKind, SeriesID: row.SeriesID,
				MovieID: row.MovieID, EpisodeID: target.episodeID, SourceTitle: row.SourceTitle,
				QualityID: &qid, Message: "not an upgrade",
			})
			return false, nil
		}
	}

	if err := placeFile(srcFile, target.dst); err != nil {
		return false, err
	}
	root := s.mustRoot(ctx, row, kind) // resolved again cheaply; see helper
	rel, err := filepath.Rel(root, target.dst)
	if err != nil {
		rel = target.dst
	}
	fi, _ := os.Stat(target.dst)
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	mf.RelativePath = filepath.ToSlash(rel)
	mf.Size = size
	mf.QualityID = q.ID
	if _, err := s.store.UpsertMediaFile(ctx, mf); err != nil {
		return false, err
	}
	if existing != nil {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(existing.RelativePath)))
	}
	evt := "imported"
	if existing != nil {
		evt = "upgraded"
	}
	qid := q.ID
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: evt, MediaKind: row.MediaKind, SeriesID: row.SeriesID, MovieID: row.MovieID,
		EpisodeID: target.episodeID, SourceTitle: row.SourceTitle, QualityID: &qid,
	})
	return true, nil
}

// resolveTarget builds the destination path + media_files template for one file.
func (s *Service) resolveTarget(ctx context.Context, row store.QueueItem, kind provider.MediaKind, cfg naming.Config, parsed parsing.ParsedRelease, q quality.QualityDefinition, ext string) (*importTarget, store.QualityProfile, store.MediaFile, error) {
	if kind == provider.KindMovie {
		m, err := s.store.GetMovie(ctx, *row.MovieID)
		if err != nil {
			return nil, store.QualityProfile{}, store.MediaFile{}, err
		}
		profile, _ := s.profileFor(ctx, kind, 0, *row.MovieID)
		root, err := s.rootPath(ctx, m.RootFolderID)
		if err != nil {
			return nil, store.QualityProfile{}, store.MediaFile{}, err
		}
		tok := naming.Tokens{MovieTitle: m.Title, Year: m.Year, Quality: q.Name, ReleaseGroup: parsed.ReleaseGroup}
		dst := filepath.Join(root, naming.Sanitize(naming.Render(cfg.MovieFolder, tok)), naming.Sanitize(naming.Render(cfg.MovieFile, tok))+ext)
		return &importTarget{movieID: row.MovieID, dst: dst}, profile, store.MediaFile{MediaKind: "movie", MovieID: row.MovieID}, nil
	}

	// TV: match the parsed S/E to one of the row's recorded episodes.
	se, err := s.store.GetSeries(ctx, *row.SeriesID)
	if err != nil {
		return nil, store.QualityProfile{}, store.MediaFile{}, err
	}
	profile, _ := s.profileFor(ctx, kind, *row.SeriesID, 0)
	root, err := s.rootPath(ctx, se.RootFolderID)
	if err != nil {
		return nil, store.QualityProfile{}, store.MediaFile{}, err
	}
	ep := s.matchEpisode(ctx, row.EpisodeIDs, parsed)
	if ep == nil {
		return nil, profile, store.MediaFile{}, nil
	}
	tok := naming.Tokens{
		SeriesTitle: se.Title, EpisodeTitle: ep.Title, Quality: q.Name,
		ReleaseGroup: parsed.ReleaseGroup, Season: ep.SeasonNumber, Episode: ep.EpisodeNumber,
	}
	dst := filepath.Join(root,
		naming.Sanitize(naming.Render(cfg.SeriesFolder, tok)),
		naming.Sanitize(naming.Render(cfg.SeasonFolder, tok)),
		naming.Sanitize(naming.Render(cfg.EpisodeFile, tok))+ext)
	epID := ep.ID
	return &importTarget{episodeID: &epID, dst: dst}, profile, store.MediaFile{MediaKind: "tv", EpisodeID: &epID}, nil
}

// matchEpisode returns the recorded episode whose season+number matches the
// parse. For a single-file download with one recorded episode and no S/E in the
// parse, it returns that episode.
func (s *Service) matchEpisode(ctx context.Context, episodeIDs []int64, parsed parsing.ParsedRelease) *store.Episode {
	if len(episodeIDs) == 1 && len(parsed.Episodes) == 0 {
		ep, err := s.store.GetEpisode(ctx, episodeIDs[0])
		if err != nil {
			return nil
		}
		return ep
	}
	for _, id := range episodeIDs {
		ep, err := s.store.GetEpisode(ctx, id)
		if err != nil {
			continue
		}
		for _, n := range parsed.Episodes {
			if ep.SeasonNumber == parsed.Season && ep.EpisodeNumber == n {
				return ep
			}
		}
	}
	return nil
}

func (s *Service) mustRoot(ctx context.Context, row store.QueueItem, kind provider.MediaKind) string {
	if kind == provider.KindMovie {
		if m, err := s.store.GetMovie(ctx, *row.MovieID); err == nil {
			if p, err := s.rootPath(ctx, m.RootFolderID); err == nil {
				return p
			}
		}
		return ""
	}
	if se, err := s.store.GetSeries(ctx, *row.SeriesID); err == nil {
		if p, err := s.rootPath(ctx, se.RootFolderID); err == nil {
			return p
		}
	}
	return ""
}
```
Also delete the now-unused `placeAndRecord` function from Task 8 (its logic moved inline into `importFile`).

- [ ] **Step 4: Update `ImportItem` to the new `importFile` signature + per-episode completeness**

In `ImportItem`, replace the import loop and final status decision with the rule
**"a row where nothing new was placed is a failed/rejected import; otherwise it
succeeds only if every target now has a file"**:
```go
	kind := provider.MediaKind(row.MediaKind)
	placed := 0
	for _, f := range files {
		ok, err := s.importFile(ctx, row, kind, cfg, f)
		if err != nil {
			return s.fail(ctx, row, err.Error())
		}
		if ok {
			placed++
		}
	}
	if placed == 0 {
		return s.fail(ctx, row, "no files imported (rejected as non-upgrade or unmatched)")
	}
	if !s.allTargetsHaveFiles(ctx, row, kind) {
		return s.fail(ctx, row, fmt.Sprintf("incomplete import (%d file(s) placed)", placed))
	}
	_ = s.store.SetQueueStatus(ctx, row.ID, store.QueueImported, "")
	if row.DownloadClientID != "" {
		_ = s.queue.Remove(ctx, row.DownloadClientID, row.ClientItemID, false)
	}
	s.emit(ctx, ImportCompletedEvent{QueueID: row.ID, Status: store.QueueImported})
	s.emit(ctx, QueueUpdated{ID: row.ID})
	return nil
```
So a single-episode non-upgrade → `placed==0` → row `failed` "not an upgrade"
(the existing file is untouched); a season pack where one episode upgrades and
another is a non-upgrade → `placed>=1` and all targets still have files → row
`imported`.
And add the completeness helper to `importer.go`:
```go
// allTargetsHaveFiles reports whether every targeted episode (or the movie) now
// has a media_files row.
func (s *Service) allTargetsHaveFiles(ctx context.Context, row store.QueueItem, kind provider.MediaKind) bool {
	if kind == provider.KindMovie {
		mf, _ := s.store.MediaFileForMovie(ctx, *row.MovieID)
		return mf != nil
	}
	for _, id := range row.EpisodeIDs {
		mf, _ := s.store.MediaFileForEpisode(ctx, id)
		if mf == nil {
			return false
		}
	}
	return true
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -count=1`
Expected: PASS. `TestImportSingleEpisode` (Task 8) still passes; `TestImportUpgradeReplacesLowerQuality`, `TestImportRejectsNonUpgrade` (expects `store.QueueFailed` — nothing new placed, old file retained), and `TestImportSeasonPack` pass.

- [ ] **Step 6: Commit**

```bash
git add internal/importing/importer.go internal/importing/importer_test.go
git commit -m "feat: handle upgrades (replace/reject) and season-pack imports"
```

---

### Task 10: `ImportCompleted` scheduled command

**Files:**
- Create: `internal/importing/command.go`
- Create: `internal/importing/command_test.go`

**Interfaces:**
- Produces: `(*Service).ImportCompleted(ctx) error` — for each `grabbed` queue row whose client item is `Completed`, run `ImportItem`; `importing.NewImportCommand(svc *Service) *ImportCommand` implementing `command.Command` (`Name()`="ImportCompletedDownloads", `Run` calls `ImportCompleted`).
- Consumes: Task 8-9, `command.Reporter`.

- [ ] **Step 1: Write the failing test**

Create `internal/importing/command_test.go`:
```go
package importing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestImportCompletedScansGrabbedRows(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rf, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show", RootFolderID: &rf.ID, QualityProfileID: &prof.ID})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"})
	eps, _ := st.ListEpisodes(ctx, sid)
	epID := eps[0].ID

	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.1080p.BluRay.x264-GRP.mkv"), 60*1024*1024)
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01E01.1080p.BluRay.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{epID}, QualityID: 9, Status: store.QueueGrabbed,
	})

	// not completed yet -> nothing imported
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusDownloading, OutputPath: dl}}
	if err := svc.ImportCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.GetQueueItem(ctx, q.ID); r.Status != store.QueueGrabbed {
		t.Fatalf("should still be grabbed, got %q", r.Status)
	}

	// now completed -> imported
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl}}
	if err := (NewImportCommand(svc)).Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.GetQueueItem(ctx, q.ID); r.Status != store.QueueImported {
		t.Fatalf("expected imported, got %q", r.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "The Show", "Season 01", "The Show - S01E01 - Pilot [Bluray-1080p].mkv")); err != nil {
		t.Fatalf("file not imported: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -run TestImportCompletedScansGrabbedRows -count=1`
Expected: FAIL — undefined `ImportCompleted`/`NewImportCommand`.

- [ ] **Step 3: Implement**

Create `internal/importing/command.go`:
```go
package importing

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// ImportCompleted imports every grabbed queue row whose client item is completed.
func (s *Service) ImportCompleted(ctx context.Context) error {
	rows, err := s.store.QueueByStatus(ctx, store.QueueGrabbed)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	completed := map[string]bool{}
	for _, it := range s.queue.Queue(ctx) {
		if it.Status == provider.StatusCompleted {
			completed[it.DownloadClientID+"|"+it.ID] = true
		}
	}
	for _, row := range rows {
		if !completed[row.DownloadClientID+"|"+row.ClientItemID] {
			continue
		}
		if err := s.ImportItem(ctx, row.ID); err != nil {
			return err
		}
	}
	return nil
}

// ImportCommand adapts ImportCompleted to the scheduler's command.Command.
type ImportCommand struct{ svc *Service }

func NewImportCommand(svc *Service) *ImportCommand { return &ImportCommand{svc: svc} }

func (c *ImportCommand) Name() string { return "ImportCompletedDownloads" }

func (c *ImportCommand) Run(ctx context.Context, _ command.Reporter) error {
	return c.svc.ImportCompleted(ctx)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/importing/command.go internal/importing/command_test.go
git commit -m "feat: add scheduled ImportCompleted command"
```

---

### Task 11: REST API

**Files:**
- Create: `internal/importing/api.go`
- Create: `internal/importing/api_test.go`

**Interfaces:**
- Produces: `importing.API`; `importing.NewAPI(svc *Service) *API`; `(*API).Mount(r chi.Router)` registering `GET /queue`, `POST /queue` (enqueue), `DELETE /queue/{id}`, `POST /queue/{id}/import`, `GET /history`, `GET /config/naming`, `PUT /config/naming`.
- Consumes: `Service`, `store.QueueItem`/`HistoryEvent`, `naming.Config`, `api.WriteJSON`/`WriteError`, `ErrRejected`/`ErrNoProfile`/`store.ErrNotFound`.

- [ ] **Step 1: Write the failing test**

Create `internal/importing/api_test.go`:
```go
package importing

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestAPI(t *testing.T) (http.Handler, *store.Store) {
	svc, st := newSvcWithQueue(t, &fakeQueue{})
	r := chi.NewRouter()
	NewAPI(svc).Mount(r)
	return r, st
}

func TestAPIQueueListAndHistory(t *testing.T) {
	r, _ := newTestAPI(t)
	for _, path := range []string{"/queue", "/history"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(w.Body.String()), "[") {
			t.Fatalf("GET %s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestAPIEnqueueRejectMaps400(t *testing.T) {
	r, st := newTestAPI(t)
	// series with profile that disallows 2160p
	sid, epID := seedSeriesWithProfile(t, st)
	body := `{"downloadUrl":"http://x","title":"The.Show.S01E01.2160p.BluRay.x265-GRP","protocol":"usenet","mediaKind":"tv","seriesId":` +
		itoa(sid) + `,"episodeIds":[` + itoa(epID) + `]}`
	req := httptest.NewRequest(http.MethodPost, "/queue", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("reject status=%d want 400 body=%s", w.Code, w.Body.String())
	}
}

func TestAPINamingConfigRoundTrip(t *testing.T) {
	r, _ := newTestAPI(t)
	put := `{"seriesFolder":"{Series Title}","seasonFolder":"S{season:00}","episodeFile":"{Series Title} S{season:00}E{episode:00}","movieFolder":"{Movie Title}","movieFile":"{Movie Title} ({year})"}`
	req := httptest.NewRequest(http.MethodPut, "/config/naming", strings.NewReader(put))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put naming status=%d", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/config/naming", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "S{season:00}") {
		t.Fatalf("naming not persisted: %s", w.Body.String())
	}
}
```
Add to `enqueue_test.go` a tiny helper (used by the API test):
```go
import "strconv"

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
```
(If `strconv` is already imported in that file, just add the `itoa` func.)

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -run TestAPI -count=1`
Expected: FAIL — `undefined: NewAPI`.

- [ ] **Step 3: Implement**

Create `internal/importing/api.go`:
```go
package importing

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/naming"
)

type API struct{ svc *Service }

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Mount(r chi.Router) {
	r.Route("/queue", func(r chi.Router) {
		r.Get("/", a.listQueue)
		r.Post("/", a.enqueue)
		r.Delete("/{id}", a.deleteQueue)
		r.Post("/{id}/import", a.importItem)
	})
	r.Get("/history", a.history)
	r.Route("/config/naming", func(r chi.Router) {
		r.Get("/", a.getNaming)
		r.Put("/", a.putNaming)
	})
}

func idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func (a *API) listQueue(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.store.ListQueue(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list queue")
		return
	}
	if rows == nil {
		rows = []store.QueueItem{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

type enqueueBody struct {
	DownloadURL string            `json:"downloadUrl"`
	Title       string            `json:"title"`
	Protocol    provider.Protocol `json:"protocol"`
	IndexerID   string            `json:"indexerId"`
	ClientID    string            `json:"clientId"`
	MediaKind   provider.MediaKind `json:"mediaKind"`
	SeriesID    int64             `json:"seriesId"`
	EpisodeIDs  []int64           `json:"episodeIds"`
	MovieID     int64             `json:"movieId"`
}

func (a *API) enqueue(w http.ResponseWriter, r *http.Request) {
	var b enqueueBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if b.Title == "" || b.DownloadURL == "" {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "downloadUrl and title are required")
		return
	}
	q, err := a.svc.Enqueue(r.Context(), EnqueueRequest{
		DownloadURL: b.DownloadURL, Title: b.Title, Protocol: b.Protocol, IndexerID: b.IndexerID,
		ClientID: b.ClientID, MediaKind: b.MediaKind, SeriesID: b.SeriesID, EpisodeIDs: b.EpisodeIDs, MovieID: b.MovieID,
	})
	if err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, q)
}

func (a *API) deleteQueue(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := a.svc.store.DeleteQueueItem(r.Context(), id); err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) importItem(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := a.svc.ImportItem(r.Context(), id); err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := a.svc.store.ListHistory(r.Context(), limit)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list history")
		return
	}
	if rows == nil {
		rows = []store.HistoryEvent{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) getNaming(w http.ResponseWriter, r *http.Request) {
	c, err := a.svc.NamingConfig(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load naming config")
		return
	}
	api.WriteJSON(w, http.StatusOK, c)
}

func (a *API) putNaming(w http.ResponseWriter, r *http.Request) {
	var c naming.Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.svc.SetNamingConfig(r.Context(), c); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to save naming config")
		return
	}
	api.WriteJSON(w, http.StatusOK, c)
}

func (a *API) writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrRejected):
		api.WriteError(w, http.StatusBadRequest, "rejected", err.Error())
	case errors.Is(err, ErrNoProfile):
		api.WriteError(w, http.StatusBadRequest, "no_profile", err.Error())
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
```

Note: `api.go` accesses `a.svc.store` (unexported) — legal because `API` is in the same `importing` package as `Service`. The `store` field already exists on `Service`.

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/importing/ -count=1`
Expected: PASS (all importing tests).

- [ ] **Step 5: Commit**

```bash
git add internal/importing/api.go internal/importing/api_test.go internal/importing/enqueue_test.go
git commit -m "feat: add importing REST API (queue, import, history, naming config)"
```

---

### Task 12: Composition wiring + full sweep + boundaries

**Files:**
- Modify: `cmd/nexus/main.go`
- Modify: `cmd/nexus/main_test.go`

**Interfaces:**
- Consumes: `importing.NewService`, `importing.NewAPI`, `importing.NewImportCommand`; the `downloadclient.Service` (as `Grabber` + a `QueueReader` adapter); `scheduler`, `api.NewRouter` variadic mounts + `WSForward`.
- Produces: a server that mounts `/api/v1/queue`, `/api/v1/history`, `/api/v1/config/naming` and runs the scheduled import.

- [ ] **Step 1: Extend the run test**

Add to `cmd/nexus/main_test.go`:
```go
func TestRunMountsQueueRoutes(t *testing.T) {
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
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9597/api/v1/queue", nil)
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
		t.Fatalf("GET /api/v1/queue status = %d want 200", status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./cmd/nexus/ -run TestRunMountsQueueRoutes -count=1`
Expected: FAIL — 404.

- [ ] **Step 3: Wire it in `main.go`**

Add the import `"github.com/hellboundg/nexus/internal/importing"` (alphabetical: after `.../importing`? place it before `internal/indexer`). After the quality construction block (`qualityAPI := quality.NewAPI(qualitySvc)`), add the adapter + service:
```go
	// Adapter: importing reaches download clients via consumer-defined interfaces.
	importDeps := dlQueueAdapter{svc: dlSvc}
	importSvc := importing.NewService(st, dlSvc, importDeps, bus)
	importAPI := importing.NewAPI(importSvc)
	importCmd := importing.NewImportCommand(importSvc)
```
Register the scheduled import next to the others:
```go
	sch.Every(1*time.Minute, func() command.Command { return importCmd })
```
Append the mount + WSForward topics:
```go
	}, web.Handler(), idxAPI.Mount, dlAPI.Mount, mediaAPI.Mount, qualityAPI.Mount, importAPI.Mount)
```
and add `"import.completed"`, `"queue.updated"` to the `WSForward` slice.
Add the adapter type at the bottom of `main.go`:
```go
// dlQueueAdapter exposes downloadclient.Service.Queue()'s items (dropping the
// aggregate error wrapper) so importing's QueueReader interface is satisfied
// without importing the downloadclient package into internal/importing.
type dlQueueAdapter struct{ svc *downloadclient.Service }

func (a dlQueueAdapter) Queue(ctx context.Context) []provider.DownloadItem {
	return a.svc.Queue(ctx).Items
}

func (a dlQueueAdapter) Remove(ctx context.Context, clientID, itemID string, deleteData bool) error {
	return a.svc.Remove(ctx, clientID, itemID, deleteData)
}
```
Confirm `main.go` already imports `"context"`, `"github.com/hellboundg/nexus/internal/core/provider"`, and `"github.com/hellboundg/nexus/internal/downloadclient"` (it does — `dlSvc` is constructed there). `dlSvc` satisfies `importing.Grabber` directly via its `Grab` method.

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./cmd/nexus/ -run TestRunMountsQueueRoutes -count=1`
Expected: PASS.

- [ ] **Step 5: Full build + vet + test sweep**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build ./cmd/nexus
go vet ./...
go test ./... -count=1
```
Expected: build succeeds; vet clean; all packages PASS. Remove any built binary (`rm -f nexus nexus.exe`). If `gofmt -l cmd/nexus/main.go internal/importing internal/naming` lists a file, run `gofmt -w` on it and re-run the sweep.

- [ ] **Step 6: Verify module boundaries**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
go list -deps ./internal/importing | grep hellboundg
go list -deps ./internal/naming | grep hellboundg
go list -deps ./internal/quality | grep hellboundg
```
Expected: `internal/importing` → only `internal/core/*` + `internal/parsing` + `internal/quality` + `internal/naming` (NO `internal/downloadclient`, `internal/media`, `internal/indexer`, `internal/automation`); `internal/naming` → nothing from the module (leaf); `internal/quality` → only `internal/parsing` + `internal/core/*`. Any violation fails the task.

- [ ] **Step 7: Commit**

```bash
git add cmd/nexus/main.go cmd/nexus/main_test.go
git commit -m "feat: wire importing engine into composition root"
```

---

## Notes for the executor

- **Model:** use `sonnet` (not `haiku`) for every implementer and reviewer in this repo.
- **Go env:** prefix `export PATH="/c/Program Files/Go/bin:$PATH"`; `-race` unavailable (no CGO) — use `-count=N`.
- **`internal/naming` is a leaf** — it must import nothing from the module. `internal/importing` is the only new feature package and must not import `internal/downloadclient`/`internal/media`/`internal/indexer`/`internal/automation`.
- **Confirm 4a store shapes before editing importer code** (Task 8/9): `GetSeries`/`GetMovie`/`GetEpisode` return pointers; `RootFolder.Path`; `Series.RootFolderID`/`QualityProfileID` and `Movie.RootFolderID`/`Year`/`QualityProfileID` are `*int64`/`int`. Adjust field access to the actual types — do not invent fields.
- **`events` bus call site:** mirror how `internal/media` publishes (`s.bus.PublishAsync(ctx, SeriesUpdated{...})`) for the `QueueUpdated`/`ImportCompletedEvent` types; match the exact `events` interface/method names in `internal/core/events`.
- **Non-upgrade semantics (Task 9):** the existing file is always left untouched on a non-upgrade. Row status reflects whether anything new was imported: if NO new file was placed (single-episode/movie non-upgrade, or no file matched a target), the row ends `failed` (rejected) with the reason in history; if at least one new file was placed and every target now has a file, the row ends `imported` (e.g. a season pack that upgrades one episode and keeps another).
