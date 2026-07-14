# Failed-Download Handling + Blocklist (Wave C1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a download fails, Nexus records it, blocklists that release (scoped to the movie/series), and auto-searches the next best release skipping blocklisted ones; the Queue becomes transient (Sonarr parity) and Activity gains a Blocklist tab.

**Architecture:** A new `blocklist` store table (migration 0007). The existing once-a-minute importing reconcile is extended: a `grabbed` row whose live download item is `StatusFailed` is recorded in history, added to the blocklist, removed from the client, its queue row deleted, and a re-search triggered via a `Researcher` interface that `automation` implements (wired in `main.go`, no import cycle). Automation's search filters out blocklisted candidates. Imported rows are now deleted from the queue. A new Activity "Blocklist" page lists blocked releases with a remove action.

**Tech Stack:** Go 1.26 (chi router, modernc SQLite), React 19 + TypeScript + Tailwind v4 + TanStack Query v5 (served from committed `web/dist`).

## Global Constraints

- Go binary path: `C:\Program Files\Go\bin` is NOT on the session PATH — prefix Go commands with `export PATH="/c/Program Files/Go/bin:$PATH"`. Module path is `github.com/hellboundg/nexus`.
- `go test -race` is unavailable on this box (no C compiler); verify concurrency with `-count=N` if needed.
- Blocklist scope: **per media item** — `movie_id` for movies, `series_id` for TV (a TV block is keyed by `series_id + normalized release title`).
- Failure trigger: **`provider.StatusFailed` only** (no stalled-torrent detection).
- Queue is **transient**: imported and handled-failure rows are **deleted** from `download_queue`.
- Blocklist match uses `store.NormReleaseTitle` on BOTH sides (stored `norm_title` and candidate titles) — identical normalization is mandatory.
- Migrations are embedded SQL run in filename order; the next number is **0007**. The migration-count test asserts the total — bump it.
- Frontend: existing CSS-var tokens only (`var(--color-*)`), no raw hex. `npx tsc -b` must pass; Vitest is the runner (`cd web && npx vitest run <path>`).
- `web/dist` drift guard (`git diff --exit-code web/dist`) must be clean at the end.
- **ASK before pushing `master`.** Work stays on branch `feat/failed-download-blocklist`.
- Commit after each task; end every commit message with the `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` trailer.

---

### Task 1: Blocklist store + migration 0007

**Files:**
- Create: `internal/core/database/migrations/0007_blocklist.sql`
- Create: `internal/core/store/blocklist_store.go`
- Create: `internal/core/store/blocklist_store_test.go`
- Modify: `internal/core/database/database_test.go:31` (migration count 6 → 7)

**Interfaces:**
- Produces:
  - `store.Blocklist{ ID int64; MediaKind string; MovieID *int64; SeriesID *int64; SourceTitle string; NormTitle string; Protocol string; QualityID int; Reason string; CreatedAt time.Time }`
  - `store.NormReleaseTitle(s string) string` — lowercase; runs of non-alphanumeric → single space; trimmed.
  - `(*Store) AddBlocklist(ctx, Blocklist) (int64, error)` — sets `norm_title = NormReleaseTitle(SourceTitle)`.
  - `(*Store) ListBlocklist(ctx) ([]Blocklist, error)` — newest first.
  - `(*Store) RemoveBlocklist(ctx, id int64) error` — `ErrNotFound` on 0 rows.
  - `(*Store) BlocklistedTitles(ctx, movieID *int64, seriesID *int64) (map[string]bool, error)` — normalized titles blocked for whichever id is non-nil (empty map if both nil).

- [ ] **Step 1: Write the migration**

```sql
-- internal/core/database/migrations/0007_blocklist.sql
CREATE TABLE blocklist (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    media_kind   TEXT    NOT NULL,
    movie_id     INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    series_id    INTEGER REFERENCES series(id) ON DELETE CASCADE,
    source_title TEXT    NOT NULL,
    norm_title   TEXT    NOT NULL,
    protocol     TEXT    NOT NULL DEFAULT '',
    quality_id   INTEGER NOT NULL DEFAULT 0,
    reason       TEXT    NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_blocklist_movie  ON blocklist(movie_id);
CREATE INDEX idx_blocklist_series ON blocklist(series_id);

-- Queue is now transient: clear pre-existing terminal rows so the Queue view
-- shows only active downloads immediately after upgrade.
DELETE FROM download_queue WHERE status IN ('imported','failed');
```

- [ ] **Step 2: Write the failing store test**

```go
// internal/core/store/blocklist_store_test.go
package store

import (
	"context"
	"testing"
)

func TestNormReleaseTitle(t *testing.T) {
	cases := map[string]string{
		"Show.S01E01.1080p-GRP": "show s01e01 1080p grp",
		"  Movie (2021) [x265] ": "movie 2021 x265",
		"A__B--C":                "a b c",
	}
	for in, want := range cases {
		if got := NormReleaseTitle(in); got != want {
			t.Fatalf("NormReleaseTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlocklistCRUDAndScope(t *testing.T) {
	st := newTestStore(t) // helper used across store tests
	ctx := context.Background()
	mid := int64(7)
	sid := int64(9)

	id, err := st.AddBlocklist(ctx, Blocklist{MediaKind: "movie", MovieID: &mid, SourceTitle: "Dune.2021.1080p-GRP", Protocol: "usenet", QualityID: 3, Reason: "missing articles"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBlocklist(ctx, Blocklist{MediaKind: "tv", SeriesID: &sid, SourceTitle: "Show.S01E01.1080p-GRP", QualityID: 3}); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListBlocklist(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListBlocklist len=%d err=%v", len(list), err)
	}

	byMovie, err := st.BlocklistedTitles(ctx, &mid, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !byMovie[NormReleaseTitle("Dune.2021.1080p-GRP")] {
		t.Fatalf("movie block not found in %v", byMovie)
	}
	if byMovie[NormReleaseTitle("Show.S01E01.1080p-GRP")] {
		t.Fatalf("series block leaked into movie scope")
	}

	if err := st.RemoveBlocklist(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveBlocklist(ctx, id); err != ErrNotFound {
		t.Fatalf("remove missing: want ErrNotFound, got %v", err)
	}
}
```

Note: `newTestStore(t)` is the existing store-test helper (see other `*_store_test.go` files for its exact name; reuse the same one). If the helper differs, match the pattern used by `import_store_test.go`.

- [ ] **Step 3: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/core/store/ -run 'TestNormReleaseTitle|TestBlocklistCRUDAndScope'`
Expected: FAIL — `NormReleaseTitle`/`AddBlocklist` undefined.

- [ ] **Step 4: Implement `blocklist_store.go`**

```go
// internal/core/store/blocklist_store.go
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Blocklist struct {
	ID          int64     `json:"id"`
	MediaKind   string    `json:"mediaKind"`
	MovieID     *int64    `json:"movieId,omitempty"`
	SeriesID    *int64    `json:"seriesId,omitempty"`
	SourceTitle string    `json:"sourceTitle"`
	NormTitle   string    `json:"-"`
	Protocol    string    `json:"protocol"`
	QualityID   int       `json:"qualityId"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"createdAt"`
}

// NormReleaseTitle lowercases and collapses runs of non-alphanumeric characters
// to single spaces, so blocklist matching is robust to punctuation differences.
func NormReleaseTitle(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func (s *Store) AddBlocklist(ctx context.Context, bl Blocklist) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO blocklist (media_kind, movie_id, series_id, source_title, norm_title, protocol, quality_id, reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		bl.MediaKind, bl.MovieID, bl.SeriesID, bl.SourceTitle, NormReleaseTitle(bl.SourceTitle),
		bl.Protocol, bl.QualityID, bl.Reason)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const blocklistCols = `id, media_kind, movie_id, series_id, source_title, norm_title, protocol, quality_id, reason, created_at`

func scanBlocklistRow(row rowScanner) (Blocklist, error) {
	var bl Blocklist
	var movieID, seriesID sql.NullInt64
	if err := row.Scan(&bl.ID, &bl.MediaKind, &movieID, &seriesID, &bl.SourceTitle,
		&bl.NormTitle, &bl.Protocol, &bl.QualityID, &bl.Reason, &bl.CreatedAt); err != nil {
		return Blocklist{}, err
	}
	if movieID.Valid {
		bl.MovieID = &movieID.Int64
	}
	if seriesID.Valid {
		bl.SeriesID = &seriesID.Int64
	}
	return bl, nil
}

func (s *Store) ListBlocklist(ctx context.Context) ([]Blocklist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+blocklistCols+` FROM blocklist ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Blocklist
	for rows.Next() {
		bl, err := scanBlocklistRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, bl)
	}
	return out, rows.Err()
}

func (s *Store) RemoveBlocklist(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM blocklist WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) BlocklistedTitles(ctx context.Context, movieID, seriesID *int64) (map[string]bool, error) {
	out := map[string]bool{}
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case movieID != nil:
		rows, err = s.db.QueryContext(ctx, `SELECT norm_title FROM blocklist WHERE movie_id = ?`, *movieID)
	case seriesID != nil:
		rows, err = s.db.QueryContext(ctx, `SELECT norm_title FROM blocklist WHERE series_id = ?`, *seriesID)
	default:
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out[t] = true
	}
	return out, rows.Err()
}

var _ = errors.Is // keep errors import if unused after edits
```

(If `errors` ends up unused, drop the import and the `var _` line.)

- [ ] **Step 5: Bump the migration-count assertion**

In `internal/core/database/database_test.go:31`, change `if applied != 6 {` to `if applied != 7 {` and the message `"expected 6 applied migrations"` to `"expected 7 applied migrations"`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/core/store/ ./internal/core/database/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/core/database/migrations/0007_blocklist.sql internal/core/store/blocklist_store.go internal/core/store/blocklist_store_test.go internal/core/database/database_test.go
git commit -m "feat(store): blocklist table + CRUD + title normalization (migration 0007)"
```

---

### Task 2: Importing — failure handling, Researcher seam, transient queue

**Files:**
- Modify: `internal/importing/importing.go` (add `Researcher` interface, `researcher` field, `SetResearcher`, `DownloadFailedEvent`)
- Modify: `internal/importing/importer.go` (delete queue row on import success)
- Modify: `internal/importing/command.go` (reconcile handles `StatusFailed`)
- Test: `internal/importing/command_test.go` (extend)

**Interfaces:**
- Consumes: `store.AddBlocklist`, `store.DeleteQueueItem`, `store.AddHistory` (Task 1 + existing); `provider.StatusCompleted`/`StatusFailed`; `QueueReader.Remove` (existing field `s.queue`).
- Produces:
  - `importing.Researcher` interface: `ResearchMovie(ctx, movieID int64) error`; `ResearchEpisode(ctx, episodeID int64) error`.
  - `(*Service) SetResearcher(r Researcher)`.
  - `importing.DownloadFailedEvent{ QueueID int64; MediaKind string; MovieID *int64; SeriesID *int64; EpisodeIDs []int64 }` with `Name() string { return "download.failed" }`.

- [ ] **Step 1: Write the failing test**

Extend `internal/importing/command_test.go`. Use the existing test scaffolding in that file (fake queue + store). Add a fake researcher and a failed-item case:

```go
type fakeResearcher struct{ movies, episodes []int64 }

func (f *fakeResearcher) ResearchMovie(_ context.Context, id int64) error   { f.movies = append(f.movies, id); return nil }
func (f *fakeResearcher) ResearchEpisode(_ context.Context, id int64) error { f.episodes = append(f.episodes, id); return nil }

func TestReconcileFailedDownloadBlocklistsAndRetries(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	mid := int64(5)
	// a grabbed movie queue row
	row, err := st.CreateQueueItem(ctx, store.QueueItem{
		DownloadClientID: "sab1", ClientItemID: "nzo_1", Protocol: "usenet",
		SourceTitle: "Dune.2021.1080p-GRP", MediaKind: "movie", MovieID: &mid,
		QualityID: 3, Status: store.QueueGrabbed,
	})
	if err != nil {
		t.Fatal(err)
	}
	// live client reports it FAILED
	q := &fakeQueue{items: []provider.DownloadItem{{
		ID: "nzo_1", DownloadClientID: "sab1", Status: provider.StatusFailed, ErrorMessage: "missing articles",
	}}}
	res := &fakeResearcher{}
	svc := NewService(st, &fakeGrabber{}, q, nil)
	svc.SetResearcher(res)

	if err := svc.ImportCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	// queue row deleted
	if _, err := st.GetQueueItem(ctx, row.ID); err != store.ErrNotFound {
		t.Fatalf("queue row should be deleted, got %v", err)
	}
	// release blocklisted for the movie
	bl, _ := st.BlocklistedTitles(ctx, &mid, nil)
	if !bl[store.NormReleaseTitle("Dune.2021.1080p-GRP")] {
		t.Fatalf("release not blocklisted: %v", bl)
	}
	// download_failed history recorded
	hist, _ := st.ListHistory(ctx, 10)
	if len(hist) == 0 || hist[0].EventType != "download_failed" {
		t.Fatalf("expected download_failed history, got %+v", hist)
	}
	// re-search triggered for the movie
	if len(res.movies) != 1 || res.movies[0] != mid {
		t.Fatalf("expected ResearchMovie(%d), got %v", mid, res.movies)
	}
	// dead client item removed
	if !q.removed["nzo_1"] {
		t.Fatalf("client item should be removed")
	}
}
```

Match the actual helper/fake names already in `command_test.go` (`fakeQueue`, `fakeGrabber`, `newTestStore`, `CreateQueueItem`); adjust the fake `Remove` to record into a `removed map[string]bool` if it doesn't already.

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/importing/ -run TestReconcileFailedDownload`
Expected: FAIL — `SetResearcher` undefined / failed items ignored.

- [ ] **Step 3: Add the interface, field, setter, and event in `importing.go`**

Add to the `Service` struct a field `researcher Researcher`. Add after `NewService`:

```go
// Researcher re-searches a target after its download failed. Implemented by the
// automation service and wired in main.go (importing defines the interface to
// avoid an import cycle: automation depends on importing, not the reverse).
type Researcher interface {
	ResearchMovie(ctx context.Context, movieID int64) error
	ResearchEpisode(ctx context.Context, episodeID int64) error
}

func (s *Service) SetResearcher(r Researcher) { s.researcher = r }
```

Add next to `ImportCompletedEvent` (around importing.go:86):

```go
// DownloadFailedEvent is published when a grabbed download failed and was
// blocklisted + removed. Forwarded to the UI so it refreshes queue/history/blocklist.
type DownloadFailedEvent struct {
	QueueID    int64   `json:"queueId"`
	MediaKind  string  `json:"mediaKind"`
	MovieID    *int64  `json:"movieId,omitempty"`
	SeriesID   *int64  `json:"seriesId,omitempty"`
	EpisodeIDs []int64 `json:"episodeIds"`
}

func (DownloadFailedEvent) Name() string { return "download.failed" }
```

- [ ] **Step 4: Delete the queue row on import success in `importer.go`**

In `ImportItem` (importer.go), replace the success-path line:

```go
	_ = s.store.SetQueueStatus(ctx, row.ID, store.QueueImported, "")
```

with:

```go
	// Queue is transient: the imported row is fully captured by history, so drop
	// it from the queue (Sonarr parity — completed items live only in History).
	_ = s.store.DeleteQueueItem(ctx, row.ID)
```

(Leave the earlier `SetQueueStatus(ctx, row.ID, store.QueueImporting, "")` at the start of `ImportItem` unchanged.)

- [ ] **Step 5: Handle failed items in the reconcile (`command.go`)**

Replace the loop body of `ImportCompleted` so it switches on the live item status, and add `handleFailed` + `researchAfterFailure`:

```go
	items := s.queue.Queue(ctx)
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
		case provider.StatusFailed:
			if err := s.handleFailed(ctx, row, it); err != nil {
				return err
			}
		}
	}
	return nil
}

// handleFailed records the failure, blocklists the release (scoped to the media
// item), removes the dead client item, deletes the queue row, and re-searches.
func (s *Service) handleFailed(ctx context.Context, row store.QueueItem, it provider.DownloadItem) error {
	reason := it.ErrorMessage
	qid := row.QualityID
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: "download_failed", MediaKind: row.MediaKind, SeriesID: row.SeriesID,
		MovieID: row.MovieID, SourceTitle: row.SourceTitle, QualityID: &qid, Message: reason,
	})
	if _, err := s.store.AddBlocklist(ctx, store.Blocklist{
		MediaKind: row.MediaKind, MovieID: row.MovieID, SeriesID: row.SeriesID,
		SourceTitle: row.SourceTitle, Protocol: row.Protocol, QualityID: row.QualityID, Reason: reason,
	}); err != nil {
		return err
	}
	if it.DownloadClientID != "" && it.ID != "" {
		_ = s.queue.Remove(ctx, it.DownloadClientID, it.ID, true)
	}
	if err := s.store.DeleteQueueItem(ctx, row.ID); err != nil {
		return err
	}
	s.emit(ctx, DownloadFailedEvent{
		QueueID: row.ID, MediaKind: row.MediaKind, MovieID: row.MovieID,
		SeriesID: row.SeriesID, EpisodeIDs: row.EpisodeIDs,
	})
	s.researchAfterFailure(ctx, row)
	return nil
}

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
	for _, epID := range row.EpisodeIDs {
		if err := s.researcher.ResearchEpisode(ctx, epID); err != nil {
			slog.Warn("importing: re-search after failure failed", "episodeId", epID, "err", err)
		}
	}
}
```

Add `"log/slog"` and (if not present) `"github.com/hellboundg/nexus/internal/core/provider"` to `command.go` imports.

- [ ] **Step 6: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/importing/`
Expected: PASS (new failure test + existing import tests, which now assert the row is deleted — update any existing test that asserted `status=imported` to assert the row is gone instead).

- [ ] **Step 7: Commit**

```bash
git add internal/importing/importing.go internal/importing/importer.go internal/importing/command.go internal/importing/command_test.go
git commit -m "feat(importing): handle failed downloads (blocklist + retry), transient queue"
```

---

### Task 3: Automation — blocklist filter + Researcher implementation

**Files:**
- Create: `internal/automation/blocklist_filter.go`
- Modify: `internal/automation/search.go` (filter candidates in searchMovie/searchSeason/searchEpisode; add ResearchMovie/ResearchEpisode)
- Test: `internal/automation/blocklist_filter_test.go`

**Interfaces:**
- Consumes: `store.BlocklistedTitles`, `store.NormReleaseTitle` (Task 1); `Candidate` (existing).
- Produces:
  - `filterBlocklisted(cands []Candidate, blocked map[string]bool) []Candidate` — keeps candidates whose `NormReleaseTitle(Release.Title)` is not in `blocked`.
  - `(*Service) ResearchMovie(ctx, movieID int64) error` and `ResearchEpisode(ctx, episodeID int64) error` — satisfy `importing.Researcher` (thin wrappers over `SearchMovie`/`SearchEpisode`, discarding the count).

- [ ] **Step 1: Write the failing test**

```go
// internal/automation/blocklist_filter_test.go
package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

func TestFilterBlocklisted(t *testing.T) {
	cands := []Candidate{
		{Release: provider.Release{Title: "Dune.2021.1080p-GRP"}},
		{Release: provider.Release{Title: "Dune.2021.2160p-GRP"}},
	}
	blocked := map[string]bool{store.NormReleaseTitle("Dune.2021.1080p-GRP"): true}
	got := filterBlocklisted(cands, blocked)
	if len(got) != 1 || got[0].Release.Title != "Dune.2021.2160p-GRP" {
		t.Fatalf("expected only the 2160p release, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/ -run TestFilterBlocklisted`
Expected: FAIL — `filterBlocklisted` undefined.

- [ ] **Step 3: Implement the filter and Researcher wrappers**

```go
// internal/automation/blocklist_filter.go
package automation

import "github.com/hellboundg/nexus/internal/core/store"

// filterBlocklisted drops candidates whose normalized release title is blocked.
func filterBlocklisted(cands []Candidate, blocked map[string]bool) []Candidate {
	if len(blocked) == 0 {
		return cands
	}
	out := cands[:0:0]
	for _, c := range cands {
		if !blocked[store.NormReleaseTitle(c.Release.Title)] {
			out = append(out, c)
		}
	}
	return out
}

// ResearchMovie / ResearchEpisode satisfy importing.Researcher (re-search after
// a failed download). They reuse the existing search paths.
func (s *Service) ResearchMovie(ctx context.Context, movieID int64) error {
	_, err := s.SearchMovie(ctx, movieID)
	return err
}

func (s *Service) ResearchEpisode(ctx context.Context, episodeID int64) error {
	_, err := s.SearchEpisode(ctx, episodeID)
	return err
}
```

- [ ] **Step 4: Apply the filter in the three search paths (`search.go`)**

In `searchMovie`, after `cands := Decide(releases, provider.KindMovie, profile)` add:

```go
	blocked, _ := s.store.BlocklistedTitles(ctx, &m.ID, nil)
	cands = filterBlocklisted(cands, blocked)
```

In `searchEpisode` (episode path), after its `Decide(...)` line add (using the series id in scope — the episode's `SeriesID`):

```go
	blocked, _ := s.store.BlocklistedTitles(ctx, nil, &ep.SeriesID)
	cands = filterBlocklisted(cands, blocked)
```

In `searchSeason`, after its `Decide(...)` line add (using the series id, `se.ID`):

```go
	blocked, _ := s.store.BlocklistedTitles(ctx, nil, &se.ID)
	cands = filterBlocklisted(cands, blocked)
```

(Read each function first to use the correct in-scope variable name for the movie/series/episode; the id must be addressable — assign to a local if needed, e.g. `mid := m.ID; ... &mid`.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/automation/`
Expected: PASS (new filter test + existing automation tests unchanged — blocklist is empty in those, so `filterBlocklisted` is a no-op).

- [ ] **Step 6: Commit**

```bash
git add internal/automation/blocklist_filter.go internal/automation/blocklist_filter_test.go internal/automation/search.go
git commit -m "feat(automation): skip blocklisted releases in search + Researcher impl"
```

---

### Task 4: Blocklist API + event forwarding + main.go wiring

**Files:**
- Modify: `internal/importing/api.go` (add `/blocklist` GET + DELETE with title enrichment)
- Modify: `cmd/nexus/main.go` (wire `importSvc.SetResearcher(autoSvc)`; add `"download.failed"` to `WSForward`)
- Test: `internal/importing/api_test.go` (extend)

**Interfaces:**
- Consumes: `store.ListBlocklist`, `store.RemoveBlocklist` (Task 1); `importing.Service.SetResearcher`, `automation.Service.ResearchMovie/ResearchEpisode` (Tasks 2–3).
- Produces: `GET /api/v1/blocklist` → `[]blocklistDTO`; `DELETE /api/v1/blocklist/{id}` → 204 / 404.

- [ ] **Step 1: Write the failing API test**

Extend `internal/importing/api_test.go` (reuse its router/store harness):

```go
func TestBlocklistListAndDelete(t *testing.T) {
	h, st := newTestAPI(t) // existing helper returning (http.Handler, *store.Store)
	ctx := context.Background()
	mid := int64(3)
	id, _ := st.AddBlocklist(ctx, store.Blocklist{MediaKind: "movie", MovieID: &mid, SourceTitle: "Dune.2021-GRP", Reason: "boom"})

	// GET
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/blocklist", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Dune.2021-GRP") {
		t.Fatalf("GET /blocklist = %d %s", rr.Code, rr.Body.String())
	}
	// DELETE
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/blocklist/"+strconv.FormatInt(id, 10), nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rr.Code)
	}
	// DELETE missing -> 404
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/blocklist/9999", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing = %d", rr.Code)
	}
}
```

Add any missing imports (`net/http/httptest`, `strconv`, `strings`).

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/importing/ -run TestBlocklistListAndDelete`
Expected: FAIL — routes 404.

- [ ] **Step 3: Add the routes + handlers in `api.go`**

In `Mount`, add after `r.Get("/history", a.history)`:

```go
	r.Route("/blocklist", func(r chi.Router) {
		r.Get("/", a.listBlocklist)
		r.Delete("/{id}", a.removeBlocklist)
	})
```

Add the handlers + DTO (reuse the movie/series title-map approach already used by the history handler — read `a.history` for the exact helper names; if it builds maps inline, do the same):

```go
type blocklistDTO struct {
	store.Blocklist
	Title string `json:"title"` // movie/series display title, "" if deleted
}

func (a *API) listBlocklist(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.store.ListBlocklist(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list blocklist")
		return
	}
	out := make([]blocklistDTO, 0, len(rows))
	for _, bl := range rows {
		title := ""
		if bl.MovieID != nil {
			if m, err := a.svc.store.GetMovie(r.Context(), *bl.MovieID); err == nil {
				title = m.Title
			}
		} else if bl.SeriesID != nil {
			if se, err := a.svc.store.GetSeries(r.Context(), *bl.SeriesID); err == nil {
				title = se.Title
			}
		}
		out = append(out, blocklistDTO{Blocklist: bl, Title: title})
	}
	api.WriteJSON(w, http.StatusOK, out)
}

func (a *API) removeBlocklist(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if err := a.svc.store.RemoveBlocklist(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "blocklist entry not found")
			return
		}
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to remove blocklist entry")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Ensure imports include `strconv`, `errors`, `github.com/go-chi/chi/v5`, `store`, and the api helper package already used in this file.

- [ ] **Step 4: Wire the researcher + event in `main.go`**

After `autoSvc := automation.NewService(...)` (main.go:143) add:

```go
	importSvc.SetResearcher(autoSvc)
```

In the `api.NewRouter(api.Deps{... WSForward: []string{...}}` list (main.go:175), add `"download.failed"` to the slice.

- [ ] **Step 5: Run tests + full build**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go build ./... && go test ./internal/importing/ ./internal/automation/ ./internal/core/...`
Expected: build OK; tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/importing/api.go internal/importing/api_test.go cmd/nexus/main.go
git commit -m "feat(api): blocklist endpoints + wire failed-download re-search + WS event"
```

---

### Task 5: Frontend — blocklist types + api hooks

**Files:**
- Modify: `web/src/features/activity/types.ts` (add `BlocklistEntry`)
- Modify: `web/src/features/activity/api.ts` (add `useBlocklist`, `useRemoveBlocklist`; invalidate blocklist)
- Modify: `web/src/features/activity/resolve.ts` (add `download.failed` to `REFRESH_EVENTS`)
- Test: `web/src/features/activity/api.test.tsx` (or a new small test if none exists for api)

**Interfaces:**
- Produces: `BlocklistEntry` type; `useBlocklist()`, `useRemoveBlocklist()`; `activityKeys.blocklist`.

- [ ] **Step 1: Add the type**

In `web/src/features/activity/types.ts` add (numeric wire shape mirroring `blocklistDTO`):

```ts
export type BlocklistEntry = {
  id: number
  mediaKind: string
  movieId?: number
  seriesId?: number
  sourceTitle: string
  protocol: string
  qualityId: number
  reason: string
  createdAt: string
  title: string
}
```

- [ ] **Step 2: Add hooks + invalidation in `api.ts`**

Add `blocklist: ["blocklist"] as const` to `activityKeys`. Add:

```ts
import type { QueueItem, HistoryEvent, BlocklistEntry } from "./types"

export function useBlocklist() {
  return useQuery({ queryKey: activityKeys.blocklist, queryFn: () => apiGet<BlocklistEntry[]>("/blocklist") })
}

export function useRemoveBlocklist() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<void>(`/blocklist/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.blocklist }),
  })
}
```

In `useActivityInvalidation`, add a blocklist invalidation alongside the existing two:

```ts
      qc.invalidateQueries({ queryKey: activityKeys.queue })
      qc.invalidateQueries({ queryKey: activityKeys.history })
      qc.invalidateQueries({ queryKey: activityKeys.blocklist })
```

- [ ] **Step 3: Add the event to `REFRESH_EVENTS` in `resolve.ts`**

Change:

```ts
const REFRESH_EVENTS = new Set(["queue.updated", "import.completed", "download.status"])
```

to include the failure event:

```ts
const REFRESH_EVENTS = new Set(["queue.updated", "import.completed", "download.status", "download.failed"])
```

- [ ] **Step 4: Write/extend a test**

Add a `shouldRefresh` case in the activity resolve test (find the existing `resolve.test.ts`; if none, create `web/src/features/activity/resolve.test.ts`):

```ts
import { describe, it, expect } from "vitest"
import { shouldRefresh } from "./resolve"

describe("shouldRefresh", () => {
  it("refreshes on download.failed", () => {
    expect(shouldRefresh("download.failed")).toBe(true)
    expect(shouldRefresh("nope")).toBe(false)
  })
})
```

- [ ] **Step 5: Run test + type check**

Run: `cd web && npx vitest run src/features/activity/resolve.test.ts && npx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/activity/types.ts web/src/features/activity/api.ts web/src/features/activity/resolve.ts web/src/features/activity/resolve.test.ts
git commit -m "feat(webui): blocklist query/mutation hooks + failure event invalidation"
```

---

### Task 6: Frontend — Blocklist page + Activity third tab

**Files:**
- Create: `web/src/features/activity/BlocklistSection.tsx`
- Create: `web/src/features/activity/BlocklistSection.test.tsx`
- Modify: `web/src/features/activity/ActivityLayout.tsx` (third tab)
- Modify: `web/src/app/routes.tsx` (blocklist route)

**Interfaces:**
- Consumes: `useBlocklist`, `useRemoveBlocklist` (Task 5).

- [ ] **Step 1: Write the failing component test**

```tsx
// web/src/features/activity/BlocklistSection.test.tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { BlocklistSection } from "@/features/activity/BlocklistSection"
import * as api from "@/features/activity/api"

vi.mock("@/features/activity/api", async (orig) => {
  const actual = await orig<typeof import("@/features/activity/api")>()
  return { ...actual, useBlocklist: vi.fn(), useRemoveBlocklist: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

describe("BlocklistSection", () => {
  it("lists entries and removes one", async () => {
    const remove = vi.fn()
    vi.mocked(api.useBlocklist).mockReturnValue({ data: [{ id: 1, mediaKind: "movie", sourceTitle: "Dune.2021-GRP", protocol: "usenet", qualityId: 3, reason: "missing articles", createdAt: "", title: "Dune" }], isLoading: false, isError: false } as unknown as ReturnType<typeof api.useBlocklist>)
    vi.mocked(api.useRemoveBlocklist).mockReturnValue({ mutate: remove, isPending: false } as unknown as ReturnType<typeof api.useRemoveBlocklist>)
    render(<ToastProvider><BlocklistSection /></ToastProvider>)
    expect(screen.getByText("Dune.2021-GRP")).toBeInTheDocument()
    expect(screen.getByText(/missing articles/i)).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: /remove/i }))
    expect(remove).toHaveBeenCalledWith(1)
  })

  it("shows an empty state", () => {
    vi.mocked(api.useBlocklist).mockReturnValue({ data: [], isLoading: false, isError: false } as unknown as ReturnType<typeof api.useBlocklist>)
    vi.mocked(api.useRemoveBlocklist).mockReturnValue({ mutate: vi.fn(), isPending: false } as unknown as ReturnType<typeof api.useRemoveBlocklist>)
    render(<ToastProvider><BlocklistSection /></ToastProvider>)
    expect(screen.getByText(/no blocklisted releases/i)).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/activity/BlocklistSection.test.tsx`
Expected: FAIL — module missing.

- [ ] **Step 3: Implement `BlocklistSection.tsx`**

```tsx
import { useBlocklist, useRemoveBlocklist } from "./api"
import { useToast } from "@/lib/toast"

export function BlocklistSection() {
  const q = useBlocklist()
  const remove = useRemoveBlocklist()
  const { toast } = useToast()

  if (q.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading…</div>
  if (q.isError) return <div className="p-6 text-sm text-[var(--color-warn)]">Failed to load blocklist.</div>
  const rows = q.data ?? []
  if (rows.length === 0) return <div className="p-10 text-center text-sm text-[var(--color-muted)]">No blocklisted releases.</div>

  return (
    <div className="p-6">
      <table className="w-full text-left text-sm">
        <thead className="text-xs text-[var(--color-muted)]">
          <tr className="border-b border-[var(--color-border)]">
            <th className="py-2 pr-3 font-medium">Release</th>
            <th className="py-2 pr-3 font-medium">For</th>
            <th className="py-2 pr-3 font-medium">Reason</th>
            <th className="py-2 pr-3" />
          </tr>
        </thead>
        <tbody>
          {rows.map((b) => (
            <tr key={b.id} className="border-b border-[var(--color-border)]">
              <td className="py-2 pr-3 font-medium">{b.sourceTitle}</td>
              <td className="py-2 pr-3 text-[var(--color-muted)]">{b.title || "—"}</td>
              <td className="py-2 pr-3 text-[var(--color-muted)]">{b.reason || "—"}</td>
              <td className="py-2 pr-3 text-right">
                <button
                  onClick={() =>
                    remove.mutate(b.id, {
                      onSuccess: () => toast("Removed from blocklist", { variant: "ok" }),
                      onError: (e) => toast(e instanceof Error ? e.message : "Failed to remove", { variant: "error" }),
                    })
                  }
                  className="rounded-md border border-[var(--color-border)] px-3 py-1 text-xs hover:border-[var(--color-brand)]"
                >
                  Remove
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
```

- [ ] **Step 4: Add the tab + route**

In `ActivityLayout.tsx` extend `TABS`:

```tsx
const TABS: { to: string; label: string }[] = [
  { to: "/activity/queue", label: "Queue" },
  { to: "/activity/history", label: "History" },
  { to: "/activity/blocklist", label: "Blocklist" },
]
```

In `web/src/app/routes.tsx`, import `BlocklistSection` and add the child route after the history route:

```tsx
import { BlocklistSection } from "@/features/activity/BlocklistSection"
// ...
          { path: "blocklist", element: <BlocklistSection /> },
```

- [ ] **Step 5: Run tests + type check**

Run: `cd web && npx vitest run src/features/activity && npx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/activity/BlocklistSection.tsx web/src/features/activity/BlocklistSection.test.tsx web/src/features/activity/ActivityLayout.tsx web/src/app/routes.tsx
git commit -m "feat(webui): Activity Blocklist page + third tab"
```

---

### Task 7: Rebuild dist + full verification + manual AC

**Files:**
- Modify: `web/dist/**` (committed build output)

- [ ] **Step 1: Full FE suite + type check**

Run: `cd web && npm test && npx tsc -b`
Expected: all Vitest files PASS; tsc exit 0.

- [ ] **Step 2: Rebuild the embedded bundle**

Run: `cd web && npm run build`
Expected: `tsc -b && vite build` succeed; `web/dist` regenerated.

- [ ] **Step 3: Full Go build/vet/test**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`
Expected: all green (incl. `web/spa_test.go` embed test).

- [ ] **Step 4: Manual browser acceptance (seeded instance)**

Start a throwaway instance on a fresh data dir; add a movie with a quality profile. Simulate a failed download (e.g. a fake download client returning `StatusFailed`, or force a queue row + failed live item via a throwaway seeder) and confirm:
- (a) the failed release appears on **Activity → Blocklist** with its reason;
- (b) it is gone from **Queue** and a `download_failed` row shows in **History**;
- (c) an imported item leaves the Queue and shows only in History;
- (d) **Remove** on the Blocklist page un-blocks it (row disappears);
- (e) zero console errors.

(If simulating a real indexer/download failure is impractical locally, drive the store directly with a throwaway seeder to create a `grabbed` row + a fake failed live item, run one reconcile tick, and verify the blocklist/history/queue state via the UI and API.)

- [ ] **Step 5: Confirm drift guard + commit dist**

Run: `git add web/dist && git diff --cached --stat web/dist`

```bash
git commit -m "build(webui): rebuild dist for blocklist + failed-download UI"
```

Then verify: `git diff --exit-code web/dist` exits 0.

---

## Self-Review

**Spec coverage:**
- §2 blocklist table + store + `NormReleaseTitle` + `BlocklistedTitles` → Task 1. ✅
- §2 migration clears terminal queue rows → Task 1 SQL. ✅
- §3 failure flow (history + blocklist + client remove + delete row + event + re-search) → Task 2. ✅
- §3 `Researcher` seam (interface in importing, impl in automation, wired in main) → Tasks 2/3/4. ✅
- §3 blocklist filter in search → Task 3. ✅
- §4 delete queue row on import success → Task 2 Step 4. ✅
- §5 blocklist API (GET/DELETE + title enrichment) + `download.failed` in WSForward → Task 4. ✅
- §6 Activity third tab + BlocklistSection + hooks + invalidation → Tasks 5/6. ✅
- §7 tests at each layer → Tasks 1–6; §8 dist + verify + AC → Task 7. ✅

**Placeholder scan:** No TBD/TODO; every code step shows complete code. Two steps say "read the function first to use the correct in-scope variable" (Task 3 Step 4, Task 4 Step 3 title-map) — these are precise instructions to match existing local names, not missing code. ✅

**Type consistency:** `Blocklist`/`NormReleaseTitle`/`BlocklistedTitles`/`AddBlocklist`/`RemoveBlocklist`/`ListBlocklist` identical across Tasks 1/2/3/4. `Researcher.ResearchMovie/ResearchEpisode` identical in importing (Task 2 def) and automation (Task 3 impl) and main wiring (Task 4). `DownloadFailedEvent.Name()="download.failed"` matches the `REFRESH_EVENTS`/`WSForward` string (Tasks 4/5). `BlocklistEntry` (TS) mirrors `blocklistDTO` (Go) field-for-field. ✅
