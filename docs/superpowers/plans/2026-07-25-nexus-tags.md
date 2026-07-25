# Nexus Tags Subsystem (SP-2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a tag subsystem — create/rename/delete tags, apply them to series and movies — so SP-3 release profiles have something to scope to.

**Architecture:** A `tags` table plus `series_tags` / `movie_tags` junction tables. Store methods for tag CRUD, per-entity replace-set, and a whole-library batch read. A new `internal/tag` API package for CRUD, with assignment endpoints added to the existing media routes. A hand-rolled `TagInput` component drives a Tags row on both detail pages and a Settings → Tags page.

**Tech Stack:** Go 1.22+, chi v5, modernc.org/sqlite, React 18 + TypeScript, TanStack Query v5, Tailwind (CSS custom properties only), vitest + @testing-library/react.

**Spec:** `docs/superpowers/specs/2026-07-25-nexus-tags-design.md` (commit `1c241e9`).

**Branch:** `feat/tags`, off master `9de14af`.

## Global Constraints

- **Nothing in SP-2 changes automation behaviour.** After this ships, tags exist and are read by no automation code. SP-3 is the first consumer. Do not wire tags into `internal/automation`.
- **`store.Series` and `store.Movie` must NOT gain a `Tags` field.** Tags are read through the sibling endpoints in Task 4. Adding the field means every read path must populate it or silently returns `[]` — the defect class that produced SP-1's missed fourth grab path.
- **Fixture rule:** any test covering both series and movies must use **different tag ids and different media ids** on the two sides. If both use id 1, a `series_tags`/`movie_tags` mixup passes by construction.
  **The trap that already caught this plan once:** `series` and `movies` are separate tables with **independent rowid sequences**, so "create two series, then a movie" yields series 1, 2 and movie **1** — a collision, not the separation it looks like. Burn throwaway movie rows until the tagged movie's id is clear of every tagged series id, and assert the ids actually differ rather than trusting the seeding order.
- **Frontend typecheck is `cd web && npx tsc -p tsconfig.app.json --noEmit`.** A bare `npx tsc --noEmit` in `web/` typechecks nothing and always exits 0.
- **`gofmt -l` is useless in this repo** (line-ending noise lists nearly every file). Trust `go build ./...` and `go vet ./...`.
- **Styling uses CSS custom properties only** — `var(--color-brand)`, `var(--color-border)`, `var(--color-panel)`, `var(--color-muted)`, `var(--color-warn)`, `var(--color-fg)`. No raw hex or `rgba()` literals.
- **Every task must mutation-verify its own tests before reporting done.** Each task lists named mutations. If a mutation comes back GREEN, report it — do not paper over it by adding an unrelated assertion.
- Source files must stay ASCII in Go comments. Editing a UTF-8 Go file with Python `open()` on this machine defaults to cp1252 and mangles non-ASCII into build errors. Use the Edit tool.

---

## File Structure

**Create:**
- `internal/core/database/migrations/0010_tags.sql` — the three tables.
- `internal/core/store/tag_store.go` — `Tag`, the sentinel errors, tag CRUD, association read/write, batch readers.
- `internal/core/store/tag_store_test.go` — store tests.
- `internal/tag/api.go` — tag CRUD HTTP handlers.
- `internal/tag/api_test.go` — tag API tests.
- `web/src/components/ui/tag-input.tsx` — the reusable control.
- `web/src/components/ui/tag-input.test.tsx`
- `web/src/features/settings/tagApi.ts` — query hooks for the tag CRUD endpoints.
- `web/src/features/settings/tagTypes.ts` — the `Tag` type.
- `web/src/features/settings/TagsSection.tsx` + `.test.tsx` — Settings → Tags.

**Modify:**
- `internal/media/api.go` — four tag assignment routes + handlers.
- `internal/media/api_test.go` — tests for them.
- `cmd/nexus/main.go:136-186` — construct and mount `tagAPI`.
- `cmd/nexus/main_test.go` — a `TestRunMountsTagRoutes`.
- `web/src/features/library/api.ts` — tag read/write hooks for detail pages.
- `web/src/features/library/SeriesDetail.tsx:77-87` — Tags row after the quality `Select`.
- `web/src/features/library/MovieDetail.tsx:97-107` — same.
- `web/src/features/settings/SettingsLayout.tsx:4-12` — a Tags tab.
- `web/src/app/routes.tsx:59` — a `tags` route.

---

### Task 1: Migration and tag CRUD in the store

**Files:**
- Create: `internal/core/database/migrations/0010_tags.sql`
- Create: `internal/core/store/tag_store.go`
- Create: `internal/core/store/tag_store_test.go`

**Interfaces:**
- Consumes: `database.Open`, `database.Migrate`, `store.New`, `store.ErrNotFound` (all existing).
- Produces: `store.Tag{ID int64, Label string, SeriesCount int, MovieCount int}`; `store.ErrTagExists`, `store.ErrInvalidTag`, `store.ErrTagNotFound`, `store.ErrTagInUse`, `store.TagInUseError{SeriesCount, MovieCount int}`; `(*Store).CreateTag(ctx, label string) (Tag, error)`, `ListTags(ctx) ([]Tag, error)`, `RenameTag(ctx, id int64, label string) error`, `DeleteTag(ctx, id int64) error`.

- [ ] **Step 1: Write the migration**

Create `internal/core/database/migrations/0010_tags.sql`:

```sql
CREATE TABLE tags (
  id    INTEGER PRIMARY KEY,
  label TEXT NOT NULL,
  UNIQUE(label COLLATE NOCASE)
);

CREATE TABLE series_tags (
  series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  tag_id    INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
  PRIMARY KEY(series_id, tag_id)
);

CREATE TABLE movie_tags (
  movie_id INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
  tag_id   INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
  PRIMARY KEY(movie_id, tag_id)
);

CREATE INDEX idx_series_tags_tag ON series_tags(tag_id);
CREATE INDEX idx_movie_tags_tag  ON movie_tags(tag_id);
```

All three tables land in this one migration even though associations aren't written until Task 2 — `ListTags` and `DeleteTag` below query the junction tables, so they must exist now.

- [ ] **Step 2: Write the failing tests**

Create `internal/core/store/tag_store_test.go`. Note `newTagTestStore` mirrors `newQualityTestStore` in `quality_store_test.go` — a separate helper because Go test helpers in this package are per-domain.

```go
package store

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
)

func newTagTestStore(t *testing.T) *Store {
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

func TestTagCRUD(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()

	created, err := st.CreateTag(ctx, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Label != "anime" {
		t.Fatalf("bad created: %+v", created)
	}
	if created.SeriesCount != 0 || created.MovieCount != 0 {
		t.Fatalf("new tag must have zero counts: %+v", created)
	}

	if err := st.RenameTag(ctx, created.ID, "anime-dubbed"); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Label != "anime-dubbed" {
		t.Fatalf("list = %+v", list)
	}

	if err := st.DeleteTag(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListTags(ctx)
	if len(list) != 0 {
		t.Fatalf("expected empty after delete, got %+v", list)
	}
	if list == nil {
		t.Fatal("ListTags must return an empty slice, never nil")
	}
}

func TestTagLabelsAreCaseInsensitivelyUnique(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()

	if _, err := st.CreateTag(ctx, "HD"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTag(ctx, "hd"); !errors.Is(err, ErrTagExists) {
		t.Fatalf("create hd: expected ErrTagExists, got %v", err)
	}

	other, err := st.CreateTag(ctx, "uhd")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RenameTag(ctx, other.ID, "Hd"); !errors.Is(err, ErrTagExists) {
		t.Fatalf("rename onto existing: expected ErrTagExists, got %v", err)
	}
}

func TestTagLabelsAreTrimmedAndNonEmpty(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()

	created, err := st.CreateTag(ctx, "  spaced  ")
	if err != nil {
		t.Fatal(err)
	}
	if created.Label != "spaced" {
		t.Fatalf("label not trimmed: %q", created.Label)
	}
	// The trimmed form must collide with an untrimmed duplicate.
	if _, err := st.CreateTag(ctx, "spaced"); !errors.Is(err, ErrTagExists) {
		t.Fatalf("expected ErrTagExists for the trimmed duplicate, got %v", err)
	}
	if _, err := st.CreateTag(ctx, "   "); !errors.Is(err, ErrInvalidTag) {
		t.Fatalf("expected ErrInvalidTag for a blank label, got %v", err)
	}
	if err := st.RenameTag(ctx, created.ID, ""); !errors.Is(err, ErrInvalidTag) {
		t.Fatalf("expected ErrInvalidTag renaming to blank, got %v", err)
	}
}

func TestTagMissingIDs(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()

	if err := st.RenameTag(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename missing: expected ErrNotFound, got %v", err)
	}
	if err := st.DeleteTag(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: expected ErrNotFound, got %v", err)
	}
}

// Counts and the in-use refusal are seeded with raw SQL because the association
// API does not exist until Task 2. Different tag ids AND different media ids on
// the series and movie sides, so a series_tags/movie_tags mixup cannot pass.
func TestListTagsCountsAndDeleteInUse(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()

	seriesTag, err := st.CreateTag(ctx, "series-only")
	if err != nil {
		t.Fatal(err)
	}
	movieTag, err := st.CreateTag(ctx, "movie-only")
	if err != nil {
		t.Fatal(err)
	}
	unusedTag, err := st.CreateTag(ctx, "unused")
	if err != nil {
		t.Fatal(err)
	}

	s1, err := st.CreateSeries(ctx, Series{TMDBID: 11, Title: "S1"})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := st.CreateSeries(ctx, Series{TMDBID: 12, Title: "S2"})
	if err != nil {
		t.Fatal(err)
	}
	// series and movies have INDEPENDENT rowid sequences, so the first movie
	// would also be id 1 and collide with s1. Burn two movie ids first, so the
	// tagged movie lands at 3 and no id is shared across the two junction
	// tables. Without this the fixture cannot distinguish a series_tags /
	// movie_tags mixup that keys on the raw entity id.
	for i := 0; i < 2; i++ {
		if _, err := st.CreateMovie(ctx, Movie{TMDBID: 90 + i, Title: "filler"}); err != nil {
			t.Fatal(err)
		}
	}
	m1, err := st.CreateMovie(ctx, Movie{TMDBID: 21, Title: "M1"})
	if err != nil {
		t.Fatal(err)
	}
	if m1 == s1 || m1 == s2 {
		t.Fatalf("fixture is degenerate: movie id %d collides with a series id (%d, %d)", m1, s1, s2)
	}
	for _, id := range []int64{s1, s2} {
		if _, err := st.db.ExecContext(ctx,
			`INSERT INTO series_tags (series_id, tag_id) VALUES (?, ?)`, id, seriesTag.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.ExecContext(ctx,
		`INSERT INTO movie_tags (movie_id, tag_id) VALUES (?, ?)`, m1, movieTag.ID); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byLabel := map[string]Tag{}
	for _, tg := range list {
		byLabel[tg.Label] = tg
	}
	if got := byLabel["series-only"]; got.SeriesCount != 2 || got.MovieCount != 0 {
		t.Fatalf("series-only counts = %+v, want 2 series / 0 movies", got)
	}
	if got := byLabel["movie-only"]; got.SeriesCount != 0 || got.MovieCount != 1 {
		t.Fatalf("movie-only counts = %+v, want 0 series / 1 movie", got)
	}
	if got := byLabel["unused"]; got.SeriesCount != 0 || got.MovieCount != 0 {
		t.Fatalf("unused counts = %+v, want zeroes", got)
	}

	// Delete is refused for a series-only association and for a movie-only one.
	var inUse *TagInUseError
	err = st.DeleteTag(ctx, seriesTag.ID)
	if !errors.As(err, &inUse) {
		t.Fatalf("delete series-tagged: expected *TagInUseError, got %v", err)
	}
	if inUse.SeriesCount != 2 || inUse.MovieCount != 0 {
		t.Fatalf("error counts = %+v, want 2 series / 0 movies", inUse)
	}
	if !errors.Is(err, ErrTagInUse) {
		t.Fatal("TagInUseError must satisfy errors.Is(err, ErrTagInUse)")
	}
	if err := st.DeleteTag(ctx, movieTag.ID); !errors.Is(err, ErrTagInUse) {
		t.Fatalf("delete movie-tagged: expected ErrTagInUse, got %v", err)
	}
	// The unused one still deletes.
	if err := st.DeleteTag(ctx, unusedTag.ID); err != nil {
		t.Fatalf("delete unused: %v", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/core/store/ -run 'TestTag|TestListTags' -v`
Expected: FAIL to compile — `undefined: Tag`, `undefined: ErrTagExists`, `st.CreateTag undefined`.

- [ ] **Step 4: Write the implementation**

Create `internal/core/store/tag_store.go`:

```go
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrTagExists is returned when a label collides with an existing tag,
// case-insensitively.
var ErrTagExists = errors.New("store: tag label already exists")

// ErrInvalidTag is returned for a label that is empty after trimming.
var ErrInvalidTag = errors.New("store: invalid tag label")

// ErrTagNotFound is returned when an association write names a tag id that does
// not exist. Distinct from ErrNotFound, which means the series/movie/tag being
// addressed is missing.
var ErrTagNotFound = errors.New("store: tag not found")

// ErrTagInUse is the sentinel for a refused delete. The concrete error returned
// is *TagInUseError, which carries the counts; errors.Is matches this sentinel.
var ErrTagInUse = errors.New("store: tag in use")

// TagInUseError reports how many series and movies still reference a tag, so
// the API can name them in the 409 without a second query.
type TagInUseError struct {
	SeriesCount int
	MovieCount  int
}

func (e *TagInUseError) Error() string {
	return fmt.Sprintf("store: tag in use by %d series and %d movies", e.SeriesCount, e.MovieCount)
}

func (e *TagInUseError) Is(target error) bool { return target == ErrTagInUse }

// Tag is a user-defined label. SeriesCount and MovieCount are populated by
// ListTags only; CreateTag returns zeroes because a new tag has no associations.
type Tag struct {
	ID          int64  `json:"id"`
	Label       string `json:"label"`
	SeriesCount int    `json:"seriesCount"`
	MovieCount  int    `json:"movieCount"`
}

func normalizeLabel(label string) (string, error) {
	l := strings.TrimSpace(label)
	if l == "" {
		return "", ErrInvalidTag
	}
	return l, nil
}

// isUniqueViolation reports whether err is a UNIQUE constraint failure. The
// modernc driver does not export a typed error for this, so the message is the
// only signal available.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE CONSTRAINT FAILED")
}

func (s *Store) CreateTag(ctx context.Context, label string) (Tag, error) {
	l, err := normalizeLabel(label)
	if err != nil {
		return Tag{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO tags (label) VALUES (?)`, l)
	if isUniqueViolation(err) {
		return Tag{}, ErrTagExists
	}
	if err != nil {
		return Tag{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Tag{}, err
	}
	return Tag{ID: id, Label: l}, nil
}

func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.label,
		        (SELECT COUNT(*) FROM series_tags st WHERE st.tag_id = t.id),
		        (SELECT COUNT(*) FROM movie_tags  mt WHERE mt.tag_id = t.id)
		 FROM tags t ORDER BY t.label COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var tg Tag
		if err := rows.Scan(&tg.ID, &tg.Label, &tg.SeriesCount, &tg.MovieCount); err != nil {
			return nil, err
		}
		out = append(out, tg)
	}
	return out, rows.Err()
}

func (s *Store) RenameTag(ctx context.Context, id int64, label string) error {
	l, err := normalizeLabel(label)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE tags SET label = ? WHERE id = ?`, l, id)
	if isUniqueViolation(err) {
		return ErrTagExists
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	var seriesCount, movieCount int
	err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM series_tags WHERE tag_id = ?),
		        (SELECT COUNT(*) FROM movie_tags  WHERE tag_id = ?)`, id, id).
		Scan(&seriesCount, &movieCount)
	if err != nil {
		return err
	}
	if seriesCount > 0 || movieCount > 0 {
		return &TagInUseError{SeriesCount: seriesCount, MovieCount: movieCount}
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

Task 1 does not import `database/sql`; Task 2 adds no need for it either, since all its queries go through `*sql.DB`/`*sql.Tx` methods on the existing `s.db`. Do not add an unused import — `go build` fails on it.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core/store/ -run 'TestTag|TestListTags' -v`
Expected: PASS, all five tests.

Then `go build ./... && go vet ./...` — both clean.

- [ ] **Step 6: Mutation-verify**

Run each, confirm the named test goes RED, then revert:

1. Drop `COLLATE NOCASE` from the `UNIQUE(label ...)` in the migration → `TestTagLabelsAreCaseInsensitivelyUnique` fails. (Delete the test DB by re-running; each test gets a fresh `t.TempDir()`.)
2. Remove the `strings.TrimSpace` from `normalizeLabel` (return `label` as-is, still erroring on `""`) → `TestTagLabelsAreTrimmedAndNonEmpty` fails.
3. In `DeleteTag`, change the movie subquery to count `series_tags` instead of `movie_tags` → `TestListTagsCountsAndDeleteInUse` fails on the movie-only delete.
4. In `ListTags`, swap the two count subqueries → `TestListTagsCountsAndDeleteInUse` fails on the counts.
5. In `ListTags`, change `out := []Tag{}` to `var out []Tag` → `TestTagCRUD`'s nil check fails.

Report any mutation that stays GREEN rather than adding an assertion to cover it.

- [ ] **Step 7: Commit**

```bash
git add internal/core/database/migrations/0010_tags.sql internal/core/store/tag_store.go internal/core/store/tag_store_test.go
git commit -m "feat(store): tags table and tag CRUD" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: Tag associations and batch readers

**Files:**
- Modify: `internal/core/store/tag_store.go`
- Modify: `internal/core/store/tag_store_test.go`

**Interfaces:**
- Consumes: Task 1's `Tag`, `ErrTagNotFound`, `ErrNotFound`.
- Produces: `(*Store).TagsForSeries(ctx, seriesID int64) ([]int64, error)`, `SetSeriesTags(ctx, seriesID int64, tagIDs []int64) error`, `SeriesTagIDs(ctx) (map[int64][]int64, error)`, and the identical `TagsForMovie` / `SetMovieTags` / `MovieTagIDs`.

**Note on `SeriesTagIDs` / `MovieTagIDs`:** these have no caller in SP-2. They exist because SP-3 resolves release profiles inside `rssPlaceTV`, which builds its library index over the whole library up front; a per-id lookup there is N queries in the RSS hot path. Do not delete them as dead code.

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/store/tag_store_test.go`:

```go
func TestSetSeriesTagsReplacesTheSet(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()

	sid, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, label := range []string{"a", "b", "c"} {
		tg, err := st.CreateTag(ctx, label)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tg.ID)
	}

	if err := st.SetSeriesTags(ctx, sid, []int64{ids[0], ids[1]}); err != nil {
		t.Fatal(err)
	}
	got, err := st.TagsForSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != ids[0] || got[1] != ids[1] {
		t.Fatalf("after first set: %v want %v", got, ids[:2])
	}

	// Replace, not merge: {a,b} then {b,c} must leave exactly {b,c}.
	if err := st.SetSeriesTags(ctx, sid, []int64{ids[1], ids[2]}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.TagsForSeries(ctx, sid)
	if len(got) != 2 || got[0] != ids[1] || got[1] != ids[2] {
		t.Fatalf("after replace: %v want %v", got, ids[1:])
	}

	// Duplicates in the input are deduplicated, not an error.
	if err := st.SetSeriesTags(ctx, sid, []int64{ids[0], ids[0]}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.TagsForSeries(ctx, sid)
	if len(got) != 1 || got[0] != ids[0] {
		t.Fatalf("after duplicate input: %v want [%d]", got, ids[0])
	}

	// nil clears.
	if err := st.SetSeriesTags(ctx, sid, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = st.TagsForSeries(ctx, sid)
	if len(got) != 0 {
		t.Fatalf("after nil: %v want empty", got)
	}
	if got == nil {
		t.Fatal("TagsForSeries must return an empty slice, never nil")
	}
}

func TestSetSeriesTagsRejectsUnknownTagAndRollsBack(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()

	sid, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	good, err := st.CreateTag(ctx, "good")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesTags(ctx, sid, []int64{good.ID}); err != nil {
		t.Fatal(err)
	}

	if err := st.SetSeriesTags(ctx, sid, []int64{good.ID, 999}); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("expected ErrTagNotFound, got %v", err)
	}
	// The prior set must be intact — no partial write.
	got, _ := st.TagsForSeries(ctx, sid)
	if len(got) != 1 || got[0] != good.ID {
		t.Fatalf("prior set not preserved after rollback: %v", got)
	}
}

func TestSetTagsOnMissingEntity(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()
	tg, err := st.CreateTag(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesTags(ctx, 999, []int64{tg.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("series: expected ErrNotFound, got %v", err)
	}
	if err := st.SetMovieTags(ctx, 999, []int64{tg.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("movie: expected ErrNotFound, got %v", err)
	}
}

// Series and movies are tagged independently. DIFFERENT tag ids and DIFFERENT
// media ids on the two sides, so a series_tags/movie_tags mixup cannot pass.
func TestSeriesAndMovieTagsAreIndependent(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()

	// Three tags so the series and movie sides never share an id. Errors are
	// checked: a silently failed create yields id 0 and turns every later
	// assertion into a confusing ErrTagNotFound.
	mustTag := func(label string) Tag {
		t.Helper()
		tg, err := st.CreateTag(ctx, label)
		if err != nil {
			t.Fatal(err)
		}
		return tg
	}
	tagA, tagB, tagC := mustTag("a"), mustTag("b"), mustTag("c")

	s1, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S1"})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := st.CreateSeries(ctx, Series{TMDBID: 2, Title: "S2"})
	if err != nil {
		t.Fatal(err)
	}
	// series and movies have INDEPENDENT rowid sequences, so the first movie
	// would also be id 1 and collide with s1. Burn two movie ids so the tagged
	// movie lands at 3 and no id is shared across the two junction tables.
	for i := 0; i < 2; i++ {
		if _, err := st.CreateMovie(ctx, Movie{TMDBID: 90 + i, Title: "filler"}); err != nil {
			t.Fatal(err)
		}
	}
	m1, err := st.CreateMovie(ctx, Movie{TMDBID: 3, Title: "M1"})
	if err != nil {
		t.Fatal(err)
	}
	if m1 == s1 || m1 == s2 {
		t.Fatalf("fixture is degenerate: movie id %d collides with a series id (%d, %d)", m1, s1, s2)
	}

	if err := st.SetSeriesTags(ctx, s1, []int64{tagA.ID, tagB.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesTags(ctx, s2, []int64{tagB.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMovieTags(ctx, m1, []int64{tagC.ID}); err != nil {
		t.Fatal(err)
	}

	if got, _ := st.TagsForMovie(ctx, m1); len(got) != 1 || got[0] != tagC.ID {
		t.Fatalf("movie tags = %v want [%d]", got, tagC.ID)
	}
	if got, _ := st.TagsForSeries(ctx, s1); len(got) != 2 {
		t.Fatalf("series 1 tags = %v want 2", got)
	}

	seriesMap, err := st.SeriesTagIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesMap) != 2 {
		t.Fatalf("series map has %d entries, want 2: %v", len(seriesMap), seriesMap)
	}
	if len(seriesMap[s1]) != 2 || seriesMap[s1][0] != tagA.ID || seriesMap[s1][1] != tagB.ID {
		t.Fatalf("series map[%d] = %v want [%d %d]", s1, seriesMap[s1], tagA.ID, tagB.ID)
	}
	if len(seriesMap[s2]) != 1 || seriesMap[s2][0] != tagB.ID {
		t.Fatalf("series map[%d] = %v want [%d]", s2, seriesMap[s2], tagB.ID)
	}

	movieMap, err := st.MovieTagIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(movieMap) != 1 || len(movieMap[m1]) != 1 || movieMap[m1][0] != tagC.ID {
		t.Fatalf("movie map = %v want {%d: [%d]}", movieMap, m1, tagC.ID)
	}
}

func TestBatchTagIDsEmptyLibrary(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()
	m, err := st.SeriesTagIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || len(m) != 0 {
		t.Fatalf("expected an empty non-nil map, got %v", m)
	}
}

func TestDeletingSeriesCascadesItsTagRows(t *testing.T) {
	st := newTagTestStore(t)
	ctx := context.Background()

	tg, err := st.CreateTag(ctx, "keepme")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesTags(ctx, sid, []int64{tg.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSeries(ctx, sid); err != nil {
		t.Fatal(err)
	}
	// The junction row is gone, so the tag is no longer in use and deletes.
	list, _ := st.ListTags(ctx)
	if len(list) != 1 || list[0].SeriesCount != 0 {
		t.Fatalf("expected the association to cascade away, got %+v", list)
	}
	if err := st.DeleteTag(ctx, tg.ID); err != nil {
		t.Fatalf("tag should be deletable after its series went away: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/store/ -run 'TestSetSeriesTags|TestSetTags|TestSeriesAndMovie|TestBatchTag|TestDeletingSeries' -v`
Expected: FAIL to compile — `st.SetSeriesTags undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/core/store/tag_store.go` (and make sure `database/sql` is imported and the Task 1 placeholder line is gone):

```go
// entityExists reports whether a row with the given id exists in table, which
// must be a literal from this file — never interpolate caller input.
func (s *Store) entityExists(ctx context.Context, table string, id int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id = ?`, table), id).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// dedupeIDs preserves first-seen order.
func dedupeIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// setTags replaces an entity's tag set inside one transaction. The tag ids are
// checked explicitly rather than relying on the foreign-key violation, because
// the driver's FK error is indistinguishable from any other error at the API
// layer and could not be mapped to a 400 without matching on message text.
func (s *Store) setTags(ctx context.Context, table, entityTable, idCol string, entityID int64, tagIDs []int64) error {
	ok, err := s.entityExists(ctx, entityTable, entityID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	ids := dedupeIDs(tagIDs)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, tagID := range ids {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE id = ?`, tagID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrTagNotFound
		}
	}
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, table, idCol), entityID); err != nil {
		return err
	}
	for _, tagID := range ids {
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (%s, tag_id) VALUES (?, ?)`, table, idCol), entityID, tagID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) tagsFor(ctx context.Context, table, idCol string, entityID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT tag_id FROM %s WHERE %s = ? ORDER BY tag_id`, table, idCol), entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) tagIDsByEntity(ctx context.Context, table, idCol string) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s, tag_id FROM %s ORDER BY %s, tag_id`, idCol, table, idCol))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var entityID, tagID int64
		if err := rows.Scan(&entityID, &tagID); err != nil {
			return nil, err
		}
		out[entityID] = append(out[entityID], tagID)
	}
	return out, rows.Err()
}

func (s *Store) TagsForSeries(ctx context.Context, seriesID int64) ([]int64, error) {
	return s.tagsFor(ctx, "series_tags", "series_id", seriesID)
}

func (s *Store) SetSeriesTags(ctx context.Context, seriesID int64, tagIDs []int64) error {
	return s.setTags(ctx, "series_tags", "series", "series_id", seriesID, tagIDs)
}

// SeriesTagIDs returns every series' tag ids in one query. Series with no tags
// are omitted. SP-3 consumes this; SP-2 does not.
func (s *Store) SeriesTagIDs(ctx context.Context) (map[int64][]int64, error) {
	return s.tagIDsByEntity(ctx, "series_tags", "series_id")
}

func (s *Store) TagsForMovie(ctx context.Context, movieID int64) ([]int64, error) {
	return s.tagsFor(ctx, "movie_tags", "movie_id", movieID)
}

func (s *Store) SetMovieTags(ctx context.Context, movieID int64, tagIDs []int64) error {
	return s.setTags(ctx, "movie_tags", "movies", "movie_id", movieID, tagIDs)
}

// MovieTagIDs returns every movie's tag ids in one query. Movies with no tags
// are omitted. SP-3 consumes this; SP-2 does not.
func (s *Store) MovieTagIDs(ctx context.Context) (map[int64][]int64, error) {
	return s.tagIDsByEntity(ctx, "movie_tags", "movie_id")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core/store/ -count=1 -v`
Expected: PASS, including the Task 1 tests.

Then `go build ./... && go vet ./...`.

- [ ] **Step 5: Mutation-verify**

1. In `setTags`, delete the `DELETE FROM ...` statement (making it a merge, not a replace) → `TestSetSeriesTagsReplacesTheSet` fails.
2. In `setTags`, move the tag-existence check to after the delete-and-insert loop → `TestSetSeriesTagsRejectsUnknownTagAndRollsBack` fails on the preserved-set assertion.
3. In `SetMovieTags`, pass `"series_tags", "movies", "series_id"` → `TestSeriesAndMovieTagsAreIndependent` fails. **This is the mutation the fixture rule exists for — confirm it goes red.**
4. In `setTags`, drop the `dedupeIDs` call → `TestSetSeriesTagsReplacesTheSet` fails on the duplicate-input assertion (primary key violation).
5. In `setTags`, remove the `entityExists` guard → `TestSetTagsOnMissingEntity` fails.
6. Remove `_pragma=foreign_keys(ON)` from `database.go:17` → `TestDeletingSeriesCascadesItsTagRows` fails. **Revert this one immediately; it is global.**

- [ ] **Step 6: Commit**

```bash
git add internal/core/store/tag_store.go internal/core/store/tag_store_test.go
git commit -m "feat(store): series and movie tag associations" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: Tag CRUD API package

**Files:**
- Create: `internal/tag/api.go`
- Create: `internal/tag/api_test.go`
- Modify: `cmd/nexus/main.go` (around `:136` and `:186`)
- Modify: `cmd/nexus/main_test.go`

**Interfaces:**
- Consumes: Task 1's store methods and errors; `api.WriteJSON(w, status, v)`, `api.WriteError(w, status, code, message)` from `internal/core/api`.
- Produces: `tag.NewAPI(st *store.Store) *tag.API` with `(*API).Mount(r chi.Router)` registering `/tag` routes.

There is no service layer — the handlers call the store directly. `quality` has a `Service` because it holds decision logic; tags have none, and `media.API` already takes a bare `*store.Store` for its simpler handlers.

- [ ] **Step 1: Write the failing tests**

Create `internal/tag/api_test.go`:

```go
package tag

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestRouter(t *testing.T) (http.Handler, *store.Store) {
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
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { NewAPI(st).Mount(r) })
	return r, st
}

func do(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body == nil {
		rdr = bytes.NewReader(nil)
	} else {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTagAPICRUD(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := do(t, r, http.MethodGet, "/api/v1/tag", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", rec.Code, rec.Body.String())
	}
	// api.WriteJSON uses json.NewEncoder().Encode, which appends a newline.
	if got := rec.Body.String(); got != "[]\n" {
		t.Fatalf("empty list must serialise as [], got %q", got)
	}

	rec = do(t, r, http.MethodPost, "/api/v1/tag", map[string]string{"label": "anime"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created store.Tag
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Label != "anime" {
		t.Fatalf("created = %+v", created)
	}

	rec = do(t, r, http.MethodPut, "/api/v1/tag/"+itoa(created.ID), map[string]string{"label": "anime-dub"})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodDelete, "/api/v1/tag/"+itoa(created.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTagAPIConflictsAndErrors(t *testing.T) {
	r, st := newTestRouter(t)
	ctx := context.Background()

	if rec := do(t, r, http.MethodPost, "/api/v1/tag", map[string]string{"label": "hd"}); rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rec.Code)
	}
	// Case-insensitive duplicate create -> 409.
	rec := do(t, r, http.MethodPost, "/api/v1/tag", map[string]string{"label": "HD"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create: %d body=%s", rec.Code, rec.Body.String())
	}
	// Blank label -> 400.
	rec = do(t, r, http.MethodPost, "/api/v1/tag", map[string]string{"label": "  "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank label: %d body=%s", rec.Code, rec.Body.String())
	}
	// Unknown id -> 404 on both rename and delete.
	if rec = do(t, r, http.MethodPut, "/api/v1/tag/999", map[string]string{"label": "x"}); rec.Code != http.StatusNotFound {
		t.Fatalf("rename missing: %d", rec.Code)
	}
	if rec = do(t, r, http.MethodDelete, "/api/v1/tag/999", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", rec.Code)
	}
	// Non-numeric id -> 400.
	if rec = do(t, r, http.MethodDelete, "/api/v1/tag/abc", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rec.Code)
	}

	// In-use delete -> 409, and the message names the counts.
	tg, err := st.CreateTag(ctx, "used")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesTags(ctx, sid, []int64{tg.ID}); err != nil {
		t.Fatal(err)
	}
	rec = do(t, r, http.MethodDelete, "/api/v1/tag/"+itoa(tg.ID), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("in-use delete: %d body=%s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Error.Code != "tag_in_use" {
		t.Fatalf("code = %q want tag_in_use", errBody.Error.Code)
	}
	if !bytes.Contains([]byte(errBody.Error.Message), []byte("1 series")) {
		t.Fatalf("message must name the counts, got %q", errBody.Error.Message)
	}
}
```

Add this helper at the bottom of the test file (avoids importing `strconv` in every assertion):

```go
func itoa(id int64) string { return strconv.FormatInt(id, 10) }
```

and add `"strconv"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tag/ -v`
Expected: FAIL — the package does not exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/tag/api.go`:

```go
package tag

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/store"
)

type API struct {
	store *store.Store
}

func NewAPI(st *store.Store) *API { return &API{store: st} }

// Mount registers routes on an already-authenticated router (the /api/v1 group).
func (a *API) Mount(r chi.Router) {
	r.Route("/tag", func(r chi.Router) {
		r.Get("/", a.list)
		r.Post("/", a.create)
		r.Put("/{id}", a.rename)
		r.Delete("/{id}", a.delete)
	})
}

type labelBody struct {
	Label string `json:"label"`
}

func tagID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func writeTagError(w http.ResponseWriter, err error) {
	var inUse *store.TagInUseError
	switch {
	case errors.As(err, &inUse):
		api.WriteError(w, http.StatusConflict, "tag_in_use",
			fmt.Sprintf("tag is in use by %d series and %d movies", inUse.SeriesCount, inUse.MovieCount))
	case errors.Is(err, store.ErrTagExists):
		api.WriteError(w, http.StatusConflict, "tag_exists", "a tag with that label already exists")
	case errors.Is(err, store.ErrInvalidTag):
		api.WriteError(w, http.StatusBadRequest, "bad_request", "label must not be empty")
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "tag not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListTags(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list tags")
		return
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var b labelBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	created, err := a.store.CreateTag(r.Context(), b.Label)
	if err != nil {
		writeTagError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, created)
}

func (a *API) rename(w http.ResponseWriter, r *http.Request) {
	id, ok := tagID(w, r)
	if !ok {
		return
	}
	var b labelBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.store.RenameTag(r.Context(), id, b.Label); err != nil {
		writeTagError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := tagID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteTag(r.Context(), id); err != nil {
		writeTagError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

**Two notes on `writeTagError`:**

- The `errors.As` for `*TagInUseError` comes first. `TagInUseError`'s `Is` method matches `ErrTagInUse` only, so ordering is not strictly load-bearing today — mutation 2 below confirms that — but the most specific match belongs first so it stays safe if a future error embeds another.
- The `fmt.Sprintf` here **deliberately duplicates** the wording in `TagInUseError.Error()`. Reusing `inUse.Error()` would leak the `store: ` prefix into an HTTP response body. The store error is for logs; this string is the user-facing one. Keep them separate.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tag/ -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Mount it in main.go**

In `cmd/nexus/main.go`, after the `qualityAPI` line (around `:137`):

```go
	tagAPI := tag.NewAPI(st)
```

Add `"github.com/hellboundg/nexus/internal/tag"` to the imports. Then extend the mounts varargs at `:186`:

```go
	}, web.Handler(), idxAPI.Mount, dlAPI.Mount, mediaAPI.Mount, qualityAPI.Mount, importAPI.Mount, autoAPI.Mount, tagAPI.Mount)
```

- [ ] **Step 6: Add the mount test**

Add to `cmd/nexus/main_test.go`. This mirrors the existing `TestRunMountsQualityRoutes` (`main_test.go:144`) exactly. **Port 9595** — the file already uses 9596, 9597, 9598 and 9599, so this is the next free one. Authentication is by the `X-Api-Key` header, matching the sibling tests.

```go
func TestRunMountsTagRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9595")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9595/api/v1/tag", nil)
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
		t.Fatalf("GET /api/v1/tag status = %d want 200", status)
	}
}
```

No new imports — `context`, `net/http`, `testing` and `time` are already imported by this file.

- [ ] **Step 7: Run the full Go suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: all packages ok.

- [ ] **Step 8: Mutation-verify**

1. Remove `tagAPI.Mount` from the varargs in `main.go` → `TestRunMountsTagRoutes` fails.
2. In `writeTagError`, move the `errors.Is(err, store.ErrTagExists)` case above the `errors.As` for `*TagInUseError` → confirm it stays GREEN and **report that**: it documents that the two errors are genuinely disjoint. Then revert.
3. In `writeTagError`, map `ErrTagExists` to 400 instead of 409 → `TestTagAPIConflictsAndErrors` fails.
4. In `writeTagError`, replace the in-use message with a constant string containing no counts → `TestTagAPIConflictsAndErrors` fails on the `1 series` assertion.
5. In `list`, return `nil` when `rows` is empty → the `[]` assertion in `TestTagAPICRUD` fails. (Task 1's `ListTags` already returns `[]Tag{}`; this mutation confirms the API test is actually pinning it.)

- [ ] **Step 9: Commit**

```bash
git add internal/tag cmd/nexus/main.go cmd/nexus/main_test.go
git commit -m "feat(tag): tag CRUD API" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Tag assignment endpoints on the media routes

**Files:**
- Modify: `internal/media/api.go` (routes at `:33-54`, handlers appended after `assignMovieProfile` at `:638`)
- Modify: `internal/media/api_test.go`

**Interfaces:**
- Consumes: Task 2's `TagsForSeries` / `SetSeriesTags` / `TagsForMovie` / `SetMovieTags`; `store.ErrTagNotFound`, `store.ErrNotFound`.
- Produces: `GET|PUT /api/v1/series/{id}/tags` and `GET|PUT /api/v1/movies/{id}/tags`, both using the JSON shape `{"tagIds":[…]}`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/media/api_test.go`. **Harness facts, verified — do not re-derive:** the file's helper is `newTestAPI(t *testing.T, fp *fakeProvider) (http.Handler, *store.Store)` at `api_test.go:19`, and it mounts with a bare `a.Mount(r)`, **so request paths have NO `/api/v1` prefix** — they are `/series/{id}/tags`. There is no `do` helper in this package; requests are built with `httptest.NewRequest` + `strings.NewReader` directly. `context`, `encoding/json`, `net/http`, `net/http/httptest`, `strconv`, `strings`, `testing` and `store` are already imported.

```go
// tagReq issues a request against the media router and returns the recorder.
func tagReq(t *testing.T, r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func seriesTagPath(id int64) string { return "/series/" + strconv.FormatInt(id, 10) + "/tags" }
func movieTagPath(id int64) string  { return "/movies/" + strconv.FormatInt(id, 10) + "/tags" }

func decodeTagIDs(t *testing.T, w *httptest.ResponseRecorder) []int64 {
	t.Helper()
	var got struct {
		TagIDs []int64 `json:"tagIds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return got.TagIDs
}

func TestSeriesAndMovieTagAssignment(t *testing.T) {
	r, st := newTestAPI(t, &fakeProvider{})
	ctx := context.Background()

	tagA, err := st.CreateTag(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	tagB, err := st.CreateTag(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	// Two series, so the series id (2) and the movie id (1) differ and a
	// series/movie mixup cannot pass by coincidence.
	if _, err := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "S1"}); err != nil {
		t.Fatal(err)
	}
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 2, Title: "S2"})
	if err != nil {
		t.Fatal(err)
	}
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 3, Title: "M1"})
	if err != nil {
		t.Fatal(err)
	}
	// series ids are 1,2 and movie ids start again at 1 — the tagged series is
	// 2 and the tagged movie is 1, so they differ. Asserted rather than assumed,
	// because the two tables have independent rowid sequences.
	if sid == mid {
		t.Fatalf("fixture is degenerate: series id == movie id == %d", sid)
	}

	w := tagReq(t, r, http.MethodPut, seriesTagPath(sid), `{"tagIds":[`+strconv.FormatInt(tagA.ID, 10)+`]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put series tags: %d body=%s", w.Code, w.Body.String())
	}
	if got := decodeTagIDs(t, tagReq(t, r, http.MethodGet, seriesTagPath(sid), "")); len(got) != 1 || got[0] != tagA.ID {
		t.Fatalf("series tags = %v want [%d]", got, tagA.ID)
	}

	w = tagReq(t, r, http.MethodPut, movieTagPath(mid), `{"tagIds":[`+strconv.FormatInt(tagB.ID, 10)+`]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put movie tags: %d body=%s", w.Code, w.Body.String())
	}
	if got := decodeTagIDs(t, tagReq(t, r, http.MethodGet, movieTagPath(mid), "")); len(got) != 1 || got[0] != tagB.ID {
		t.Fatalf("movie tags = %v want [%d]", got, tagB.ID)
	}
	// The series must be untouched by the movie write.
	if got := decodeTagIDs(t, tagReq(t, r, http.MethodGet, seriesTagPath(sid), "")); len(got) != 1 || got[0] != tagA.ID {
		t.Fatalf("series tags changed after a movie write: %v", got)
	}
}

func TestTagAssignmentErrors(t *testing.T) {
	r, st := newTestAPI(t, &fakeProvider{})
	ctx := context.Background()
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}

	// Unknown tag id -> 400, not a silent partial write.
	if w := tagReq(t, r, http.MethodPut, seriesTagPath(sid), `{"tagIds":[999]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown tag: %d body=%s", w.Code, w.Body.String())
	}
	// Unknown series -> 404.
	if w := tagReq(t, r, http.MethodPut, "/series/999/tags", `{"tagIds":[]}`); w.Code != http.StatusNotFound {
		t.Fatalf("unknown series: %d", w.Code)
	}
	// Malformed JSON -> 400.
	if w := tagReq(t, r, http.MethodPut, seriesTagPath(sid), `{`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", w.Code)
	}
	// Empty set is valid and clears.
	if w := tagReq(t, r, http.MethodPut, seriesTagPath(sid), `{"tagIds":[]}`); w.Code != http.StatusOK {
		t.Fatalf("clear: %d body=%s", w.Code, w.Body.String())
	}
	// GET on a tagless series returns [] not null.
	if body := tagReq(t, r, http.MethodGet, seriesTagPath(sid), "").Body.String(); !strings.Contains(body, `"tagIds":[]`) {
		t.Fatalf("expected an empty array, got %s", body)
	}
}

// GET is deliberately lenient: reading the tags of a series that does not exist
// returns 200 with an empty list rather than 404. TagsForSeries does no entity
// lookup (only the write path does), and the detail page's own /series/{id}
// request is what surfaces a missing series. Pinned so the leniency is a
// decision rather than an accident.
func TestGetTagsForMissingEntityIsLenient(t *testing.T) {
	r, _ := newTestAPI(t, &fakeProvider{})
	w := tagReq(t, r, http.MethodGet, "/series/999/tags", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d want 200", w.Code)
	}
	if got := decodeTagIDs(t, w); len(got) != 0 {
		t.Fatalf("tags = %v want empty", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/media/ -run 'TagAssignment|TagsForMissing' -v`
Expected: FAIL — 404 from the unregistered routes.

- [ ] **Step 3: Register the routes**

In `internal/media/api.go`, inside the `/series` route block after `r.Put("/{id}/qualityprofile", a.assignSeriesProfile)`:

```go
		r.Get("/{id}/tags", a.getSeriesTags)
		r.Put("/{id}/tags", a.setSeriesTags)
```

and inside the `/movies` block after `r.Put("/{id}/qualityprofile", a.assignMovieProfile)`:

```go
		r.Get("/{id}/tags", a.getMovieTags)
		r.Put("/{id}/tags", a.setMovieTags)
```

- [ ] **Step 4: Write the handlers**

Append to `internal/media/api.go`, after `assignMovieProfile`:

```go
type tagsBody struct {
	TagIDs []int64 `json:"tagIds"`
}

func writeTagAssignError(w http.ResponseWriter, err error, entity string) {
	switch {
	case errors.Is(err, store.ErrTagNotFound):
		api.WriteError(w, http.StatusBadRequest, "bad_request", "unknown tag id")
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", entity+" not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (a *API) getSeriesTags(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	ids, err := a.store.TagsForSeries(r.Context(), id)
	if err != nil {
		writeTagAssignError(w, err, "series")
		return
	}
	api.WriteJSON(w, http.StatusOK, tagsBody{TagIDs: ids})
}

func (a *API) setSeriesTags(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	var b tagsBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.store.SetSeriesTags(r.Context(), id, b.TagIDs); err != nil {
		writeTagAssignError(w, err, "series")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) getMovieTags(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	ids, err := a.store.TagsForMovie(r.Context(), id)
	if err != nil {
		writeTagAssignError(w, err, "movie")
		return
	}
	api.WriteJSON(w, http.StatusOK, tagsBody{TagIDs: ids})
}

func (a *API) setMovieTags(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	var b tagsBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.store.SetMovieTags(r.Context(), id, b.TagIDs); err != nil {
		writeTagAssignError(w, err, "movie")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/media/ -count=1 -v`
Expected: PASS, no pre-existing test broken.

Then `go build ./... && go vet ./... && go test ./... -count=1`.

- [ ] **Step 6: Mutation-verify**

1. In `setMovieTags`, call `a.store.SetSeriesTags` → `TestSeriesAndMovieTagAssignment` fails. **The fixture rule's target mutation — confirm red.**
2. In `writeTagAssignError`, map `ErrTagNotFound` to 500 → `TestTagAssignmentErrors` fails.
3. Swap the order of the `ErrTagNotFound` and `ErrNotFound` cases → confirm it stays GREEN and report it (they are disjoint sentinels); revert.
4. In `getSeriesTags`, return the raw `ids` slice instead of the `tagsBody` wrapper → `TestSeriesAndMovieTagAssignment` fails to decode.

- [ ] **Step 7: Commit**

```bash
git add internal/media/api.go internal/media/api_test.go
git commit -m "feat(media): tag assignment endpoints for series and movies" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: The `TagInput` component

**Files:**
- Create: `web/src/components/ui/tag-input.tsx`
- Create: `web/src/components/ui/tag-input.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks (pure component).
- Produces: `TagInput` with props `{ value: number[]; options: TagOption[]; onChange: (ids: number[]) => void; onCreate: (label: string) => Promise<number>; disabled?: boolean; "aria-label"?: string }`, and `export type TagOption = { id: number; label: string }`.

**Behaviour contract:**
- Selected ids render as chips with a remove button labelled `Remove <label>`.
- The text input filters `options` (case-insensitive substring), excluding already-selected ids. Suggestions are buttons; clicking one selects it and clears the input.
- **Enter acts on the typed text, not the suggestion list.** Trimmed text that case-insensitively equals an existing option's label selects that option; anything else calls `onCreate`. Enter deliberately does not pick the first suggestion — typing `an` with `anime` present would otherwise silently select `anime`.
- Empty/whitespace input on Enter does nothing.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/ui/tag-input.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { TagInput, type TagOption } from "./tag-input"

const options: TagOption[] = [
  { id: 1, label: "anime" },
  { id: 2, label: "uk-tv" },
]

describe("TagInput", () => {
  it("renders selected tags as chips and removes them", async () => {
    const onChange = vi.fn()
    render(<TagInput value={[1]} options={options} onChange={onChange} onCreate={vi.fn()} />)
    expect(screen.getByText("anime")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Remove anime" }))
    expect(onChange).toHaveBeenCalledWith([])
  })

  it("filters suggestions and selects one on click", async () => {
    const onChange = vi.fn()
    render(<TagInput value={[]} options={options} onChange={onChange} onCreate={vi.fn()} />)
    await userEvent.type(screen.getByRole("textbox"), "uk")
    expect(screen.queryByRole("button", { name: "anime" })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "uk-tv" }))
    expect(onChange).toHaveBeenCalledWith([2])
  })

  it("hides already-selected tags from the suggestions", async () => {
    render(<TagInput value={[1]} options={options} onChange={vi.fn()} onCreate={vi.fn()} />)
    await userEvent.type(screen.getByRole("textbox"), "anim")
    expect(screen.queryByRole("button", { name: "anime" })).not.toBeInTheDocument()
  })

  it("creates a new tag on Enter when the text matches nothing", async () => {
    const onCreate = vi.fn().mockResolvedValue(9)
    const onChange = vi.fn()
    render(<TagInput value={[]} options={options} onChange={onChange} onCreate={onCreate} />)
    await userEvent.type(screen.getByRole("textbox"), "documentary{Enter}")
    await waitFor(() => expect(onCreate).toHaveBeenCalledWith("documentary"))
    await waitFor(() => expect(onChange).toHaveBeenCalledWith([9]))
  })

  it("selects the existing tag instead of creating when the label differs only by case", async () => {
    const onCreate = vi.fn()
    const onChange = vi.fn()
    render(<TagInput value={[]} options={options} onChange={onChange} onCreate={onCreate} />)
    await userEvent.type(screen.getByRole("textbox"), "ANIME{Enter}")
    await waitFor(() => expect(onChange).toHaveBeenCalledWith([1]))
    expect(onCreate).not.toHaveBeenCalled()
  })

  it("does not pick the first suggestion on Enter", async () => {
    const onCreate = vi.fn().mockResolvedValue(9)
    const onChange = vi.fn()
    render(<TagInput value={[]} options={options} onChange={onChange} onCreate={onCreate} />)
    // "an" is a prefix of "anime" but not equal to it: this must CREATE "an".
    await userEvent.type(screen.getByRole("textbox"), "an{Enter}")
    await waitFor(() => expect(onCreate).toHaveBeenCalledWith("an"))
    expect(onChange).not.toHaveBeenCalledWith([1])
  })

  it("ignores Enter on blank input", async () => {
    const onCreate = vi.fn()
    render(<TagInput value={[]} options={options} onChange={vi.fn()} onCreate={onCreate} />)
    await userEvent.type(screen.getByRole("textbox"), "   {Enter}")
    expect(onCreate).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/components/ui/tag-input.test.tsx`
Expected: FAIL — cannot resolve `./tag-input`.

- [ ] **Step 3: Write the component**

Create `web/src/components/ui/tag-input.tsx`:

```tsx
import { useState } from "react"

export type TagOption = { id: number; label: string }

export function TagInput({
  value, options, onChange, onCreate, disabled, "aria-label": ariaLabel = "Tags",
}: {
  value: number[]
  options: TagOption[]
  onChange: (ids: number[]) => void
  onCreate: (label: string) => Promise<number>
  disabled?: boolean
  "aria-label"?: string
}) {
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)

  const selected = value
    .map((id) => options.find((o) => o.id === id))
    .filter((o): o is TagOption => o != null)

  const typed = text.trim()
  const needle = typed.toLowerCase()
  const suggestions =
    needle === ""
      ? []
      : options.filter((o) => !value.includes(o.id) && o.label.toLowerCase().includes(needle))

  const select = (id: number) => {
    if (!value.includes(id)) onChange([...value, id])
    setText("")
  }

  const commit = async () => {
    if (typed === "" || busy) return
    // Enter acts on the typed text: an exact (case-insensitive) label selects
    // that tag, anything else creates. Deliberately NOT the first suggestion —
    // typing "an" with "anime" present must create "an".
    const exact = options.find((o) => o.label.toLowerCase() === needle)
    if (exact) {
      select(exact.id)
      return
    }
    setBusy(true)
    try {
      const id = await onCreate(typed)
      onChange([...value, id])
      setText("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-1.5">
        {selected.map((o) => (
          <span
            key={o.id}
            className="inline-flex items-center gap-1 rounded-full border border-[var(--color-border)] bg-[var(--color-panel-2)] px-2 py-0.5 text-xs"
          >
            {o.label}
            <button
              type="button"
              aria-label={`Remove ${o.label}`}
              disabled={disabled}
              onClick={() => onChange(value.filter((id) => id !== o.id))}
              className="text-[var(--color-muted)] hover:text-[var(--color-fg)]"
            >
              x
            </button>
          </span>
        ))}
      </div>
      <input
        type="text"
        aria-label={ariaLabel}
        value={text}
        disabled={disabled || busy}
        placeholder="Add a tag…"
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault()
            void commit()
          }
        }}
        className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-3 py-1.5 text-sm disabled:opacity-50"
      />
      {suggestions.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {suggestions.map((o) => (
            <li key={o.id}>
              <button
                type="button"
                onClick={() => select(o.id)}
                className="rounded-full border border-[var(--color-border)] px-2 py-0.5 text-xs text-[var(--color-muted)] hover:text-[var(--color-fg)]"
              >
                {o.label}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/ui/tag-input.test.tsx`
Expected: PASS, 7 tests.

Then: `cd web && npx tsc -p tsconfig.app.json --noEmit` — 0 errors.

- [ ] **Step 5: Mutation-verify**

1. Change `commit`'s exact-match lookup to `options.find((o) => o.label.toLowerCase().startsWith(needle))` → "does not pick the first suggestion on Enter" fails.
2. Change the exact-match comparison to be case-sensitive (`o.label === typed`) → "selects the existing tag instead of creating when the label differs only by case" fails.
3. Remove the `!value.includes(o.id)` filter from `suggestions` → "hides already-selected tags from the suggestions" fails.
4. Remove the `typed === ""` guard in `commit` → "ignores Enter on blank input" fails.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/ui/tag-input.tsx web/src/components/ui/tag-input.test.tsx
git commit -m "feat(web): TagInput component" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: Settings → Tags page

**Files:**
- Create: `web/src/features/settings/tagTypes.ts`
- Create: `web/src/features/settings/tagApi.ts`
- Create: `web/src/features/settings/TagsSection.tsx`
- Create: `web/src/features/settings/TagsSection.test.tsx`
- Modify: `web/src/features/settings/SettingsLayout.tsx:4-12`
- Modify: `web/src/app/routes.tsx` (import + a child route after `qualityprofiles`)

**Interfaces:**
- Consumes: Task 3's `/tag` endpoints; `apiGet/apiPost/apiPut/apiDelete` and `ApiError` from `@/lib/api`; `useToast` from `@/lib/toast`.
- Produces: `tagKeys.all`, `useTags()`, `useCreateTag()`, `useRenameTag()`, `useDeleteTag()`, and `type Tag`.

- [ ] **Step 1: Write the types and hooks**

Create `web/src/features/settings/tagTypes.ts`:

```ts
export type Tag = {
  id: number
  label: string
  seriesCount: number
  movieCount: number
}
```

Create `web/src/features/settings/tagApi.ts`:

```ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type { Tag } from "./tagTypes"

export const tagKeys = {
  all: ["settings", "tags"] as const,
}

export function useTags() {
  return useQuery({ queryKey: tagKeys.all, queryFn: () => apiGet<Tag[]>("/tag") })
}

export function useCreateTag() {
  const qc = useQueryClient()
  return useMutation<Tag, Error, string>({
    mutationFn: (label) => apiPost<Tag>("/tag", { label }),
    onSuccess: () => qc.invalidateQueries({ queryKey: tagKeys.all }),
  })
}

export function useRenameTag() {
  const qc = useQueryClient()
  return useMutation<{ ok: boolean }, Error, { id: number; label: string }>({
    mutationFn: ({ id, label }) => apiPut<{ ok: boolean }>(`/tag/${id}`, { label }),
    onSuccess: () => qc.invalidateQueries({ queryKey: tagKeys.all }),
  })
}

export function useDeleteTag() {
  const qc = useQueryClient()
  return useMutation<{ ok: boolean }, Error, number>({
    mutationFn: (id) => apiDelete<{ ok: boolean }>(`/tag/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: tagKeys.all }),
  })
}
```

- [ ] **Step 2: Write the failing section test**

**Harness facts, verified — do not invent a different style.** `QualityProfilesSection.test.tsx` mocks the hook module with `vi.mock("./qualityApi", async (orig) => ({ ...actual, useX: vi.fn() }))` and drives it with `vi.mocked(...)`. It does **not** mock `@/lib/toast` — it wraps the component in the real `<ToastProvider>` and asserts the toast text with `screen.findByText`. Match that.

Create `web/src/features/settings/TagsSection.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { TagsSection } from "./TagsSection"
import * as api from "./tagApi"

vi.mock("./tagApi", async (orig) => {
  const actual = await orig<typeof import("./tagApi")>()
  return {
    ...actual,
    useTags: vi.fn(), useCreateTag: vi.fn(), useRenameTag: vi.fn(), useDeleteTag: vi.fn(),
  }
})
beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

const tags = [
  { id: 1, label: "anime", seriesCount: 3, movieCount: 0 },
  { id: 2, label: "classics", seriesCount: 0, movieCount: 2 },
]

function setup(rows: typeof tags, over: { del?: object; create?: object } = {}) {
  vi.mocked(api.useTags).mockReturnValue({ data: rows, isLoading: false, isError: false } as never)
  vi.mocked(api.useCreateTag).mockReturnValue(mut(over.create))
  vi.mocked(api.useRenameTag).mockReturnValue(mut())
  vi.mocked(api.useDeleteTag).mockReturnValue(mut(over.del))
  render(<ToastProvider><TagsSection /></ToastProvider>)
}

describe("TagsSection", () => {
  it("shows each tag with its in-use counts", () => {
    setup(tags)
    expect(screen.getByText("anime")).toBeInTheDocument()
    expect(screen.getByText(/3 series, 0 movies/)).toBeInTheDocument()
    expect(screen.getByText(/0 series, 2 movies/)).toBeInTheDocument()
  })

  it("creates a tag from the inline input", async () => {
    const create = vi.fn()
    setup(tags, { create: { mutate: create } })
    await userEvent.type(screen.getByLabelText("New tag label"), "documentary")
    await userEvent.click(screen.getByRole("button", { name: "Add" }))
    expect(create).toHaveBeenCalledWith("documentary", expect.anything())
  })

  it("shows the server's in-use message verbatim on a 409 delete", async () => {
    const del = vi.fn((_id, opts) =>
      opts.onError(new ApiError(409, "tag_in_use", "tag is in use by 3 series and 0 movies")),
    )
    setup(tags, { del: { mutate: del } })
    await userEvent.click(screen.getAllByRole("button", { name: "Delete" })[0])
    // Asserting the exact server text, not a client-side constant: this is what
    // makes the refusal actionable.
    expect(await screen.findByText("tag is in use by 3 series and 0 movies")).toBeInTheDocument()
  })

  it("shows an empty state when there are no tags", () => {
    setup([])
    expect(screen.getByText(/No tags yet/)).toBeInTheDocument()
  })
})
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd web && npx vitest run src/features/settings/TagsSection.test.tsx`
Expected: FAIL — cannot resolve `./TagsSection`.

- [ ] **Step 4: Write the section**

Create `web/src/features/settings/TagsSection.tsx`:

```tsx
import { useState } from "react"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { useTags, useCreateTag, useRenameTag, useDeleteTag } from "./tagApi"
import type { Tag } from "./tagTypes"

// Plain formatter, not a hook — it is called inside .map(), so a `use` prefix
// would trip the rules-of-hooks lint rule.
function countLabel(tag: Tag): string {
  return `${tag.seriesCount} series, ${tag.movieCount} ${tag.movieCount === 1 ? "movie" : "movies"}`
}

export function TagsSection() {
  const { toast } = useToast()
  const q = useTags()
  const create = useCreateTag()
  const rename = useRenameTag()
  const del = useDeleteTag()
  const [label, setLabel] = useState("")
  const [editing, setEditing] = useState<{ id: number; label: string } | null>(null)
  const rows = (q.data ?? []) as Tag[]

  const onAdd = () => {
    const l = label.trim()
    if (l === "") return
    create.mutate(l, {
      onSuccess: () => { setLabel(""); toast("Tag created") },
      onError: (e) =>
        toast(
          e instanceof ApiError && e.status === 409 ? "A tag with that label already exists" : "Create failed",
          { variant: "error" },
        ),
    })
  }

  const onRename = () => {
    if (!editing) return
    const l = editing.label.trim()
    if (l === "") return
    rename.mutate({ id: editing.id, label: l }, {
      onSuccess: () => { setEditing(null); toast("Tag renamed") },
      onError: (e) =>
        toast(
          e instanceof ApiError && e.status === 409 ? "A tag with that label already exists" : "Rename failed",
          { variant: "error" },
        ),
    })
  }

  const onDelete = (t: Tag) => {
    del.mutate(t.id, {
      onSuccess: () => toast("Deleted"),
      // The server's 409 message already names the counts, so show it verbatim.
      onError: (e) =>
        toast(e instanceof ApiError && e.status === 409 ? e.message : "Delete failed", { variant: "error" }),
    })
  }

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">Tags</h2>

      <div className="mb-4 flex items-center gap-2">
        <input
          type="text"
          aria-label="New tag label"
          value={label}
          placeholder="New tag…"
          onChange={(e) => setLabel(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); onAdd() } }}
          className="w-64 rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-3 py-1.5 text-sm"
        />
        <button
          onClick={onAdd}
          className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white"
        >
          Add
        </button>
      </div>

      {q.isLoading ? (
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      ) : q.isError ? (
        <p className="text-sm text-[var(--color-warn)]">Failed to load.</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-[var(--color-muted)]">No tags yet — add one above.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((t) => (
            <li
              key={t.id}
              className="flex items-center gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] px-4 py-3"
            >
              <div className="min-w-0 flex-1">
                {editing?.id === t.id ? (
                  <input
                    type="text"
                    aria-label={`Rename ${t.label}`}
                    value={editing.label}
                    autoFocus
                    onChange={(e) => setEditing({ id: t.id, label: e.target.value })}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") { e.preventDefault(); onRename() }
                      if (e.key === "Escape") setEditing(null)
                    }}
                    onBlur={onRename}
                    className="w-64 rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-2 py-1 text-sm"
                  />
                ) : (
                  <div className="font-medium">{t.label}</div>
                )}
                <div className="text-xs text-[var(--color-muted)]">{countLabel(t)}</div>
              </div>
              <button
                onClick={() => setEditing({ id: t.id, label: t.label })}
                className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
              >
                Rename
              </button>
              <button
                onClick={() => onDelete(t)}
                className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
              >
                Delete
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
```

- [ ] **Step 5: Wire the tab and route**

In `web/src/features/settings/SettingsLayout.tsx`, add to `TABS` after the Quality Profiles entry:

```tsx
  { to: "/settings/tags", label: "Tags" },
```

In `web/src/app/routes.tsx`, add the import next to the other settings imports:

```tsx
import { TagsSection } from "@/features/settings/TagsSection"
```

and the child route after the `qualityprofiles` entry:

```tsx
          { path: "tags", element: <TagsSection /> },
```

- [ ] **Step 6: Run the tests**

Run: `cd web && npx vitest run src/features/settings/ && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS, 0 type errors. `SettingsLayout.test.tsx` may assert on the tab list — if it does, update it to include Tags.

- [ ] **Step 7: Mutation-verify**

1. In `onDelete`'s `onError`, replace `e.message` with a constant `"Tag is in use"` → the 409 toast test fails (it asserts the server's message verbatim).
2. Remove the `t.seriesCount`/`t.movieCount` from `countLabel` → the counts test fails.
3. Remove the `{ path: "tags", ... }` route → if `SettingsLayout.test.tsx` covers navigation it fails; if nothing fails, **report it** — it means the route is unpinned and a nav assertion should be added.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/settings/tagTypes.ts web/src/features/settings/tagApi.ts web/src/features/settings/TagsSection.tsx web/src/features/settings/TagsSection.test.tsx web/src/features/settings/SettingsLayout.tsx web/src/app/routes.tsx
git commit -m "feat(web): Settings > Tags page" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: Tags on the Series and Movie detail pages

**Files:**
- Modify: `web/src/features/library/api.ts`
- Modify: `web/src/features/library/SeriesDetail.tsx` (after the quality `Select` block at `:77-87`)
- Modify: `web/src/features/library/MovieDetail.tsx` (after the quality `Select` block at `:97-107`)
- Modify: `web/src/features/library/SeriesDetail.test.tsx`, `MovieDetail.test.tsx`

**Interfaces:**
- Consumes: Task 4's `/series/{id}/tags` and `/movies/{id}/tags`; Task 5's `TagInput`; Task 6's `useTags` and `useCreateTag`.
- Produces: `useMediaTags(kind, id)` and `useSetMediaTags(kind, id)` in `features/library/api.ts`.

- [ ] **Step 1: Add the hooks**

In `web/src/features/library/api.ts`, add to `libraryKeys`:

```ts
  tags: (kind: "series" | "movie", id: number) => ["library", "tags", kind, id] as const,
```

and add the hooks (place them with the other reads/mutations, matching the file's existing section comments):

```ts
export function useMediaTags(kind: "series" | "movie", id: number) {
  const path = kind === "series" ? `/series/${id}/tags` : `/movies/${id}/tags`
  return useQuery({
    queryKey: libraryKeys.tags(kind, id),
    queryFn: async () => (await apiGet<{ tagIds: number[] }>(path)).tagIds,
  })
}

export function useSetMediaTags(kind: "series" | "movie", id: number) {
  const qc = useQueryClient()
  const path = kind === "series" ? `/series/${id}/tags` : `/movies/${id}/tags`
  return useMutation<{ ok: boolean }, Error, number[]>({
    mutationFn: (tagIds) => apiPut<{ ok: boolean }>(path, { tagIds }),
    onSuccess: () => qc.invalidateQueries({ queryKey: libraryKeys.tags(kind, id) }),
  })
}
```

`apiPut` and `useMutation`/`useQueryClient` are already imported in this file; confirm before adding duplicates.

- [ ] **Step 2: Write the failing detail test**

**Harness facts, verified.** Both detail test files use `vi.mock("@/features/library/api", async (orig) => ({ ...actual, <named hooks>: vi.fn() }))`, a local `mut()` helper, and render inside `QueryClientProvider` → `MemoryRouter` → `ToastProvider`. Because the mock **spreads `actual`**, any hook not named in that object runs for real — so `useMediaTags` and `useSetMediaTags` **must be added to the mock list** or the tests will attempt a real fetch. `SeriesDetail.test.tsx` inlines its render in each test; `MovieDetail.test.tsx` has a `renderMovie(id, movie, …)` helper at `:26`.

**In `SeriesDetail.test.tsx`:** extend the existing `vi.mock` object at `:14-15` with `useMediaTags: vi.fn(), useSetMediaTags: vi.fn(),`, add the tagApi mock and the imports, then add the tests.

```tsx
// --- add near the top, after the existing vi.mock("@/features/library/api", …) ---
import * as tagApi from "@/features/settings/tagApi"

vi.mock("@/features/settings/tagApi", async (orig) => {
  const actual = await orig<typeof import("@/features/settings/tagApi")>()
  return { ...actual, useTags: vi.fn(), useCreateTag: vi.fn() }
})

// --- and these two tests inside describe("SeriesDetail", …) ---
function renderSeriesWithTags(setTags: ReturnType<typeof vi.fn>, createTag: ReturnType<typeof vi.fn>) {
  vi.mocked(lib.useSeriesDetail).mockReturnValue({
    data: {
      id: 3, title: "The Bear", firstAired: "2022-06-23", overview: "", monitored: true,
      qualityProfileId: 1, posterUrl: "", fanartUrl: "", episodeCount: 0, episodeFileCount: 0,
      seasons: [], episodes: [],
    },
    isLoading: false, isError: false, refetch: vi.fn(),
  } as unknown as ReturnType<typeof lib.useSeriesDetail>)
  vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
  vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
  vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
  vi.mocked(lib.useRefresh).mockReturnValue(mut())
  vi.mocked(lib.useDelete).mockReturnValue(mut())
  vi.mocked(lib.useSearch).mockReturnValue(mut())
  vi.mocked(lib.useMediaTags).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useMediaTags>)
  vi.mocked(lib.useSetMediaTags).mockReturnValue(mut({ mutate: setTags }))
  vi.mocked(tagApi.useTags).mockReturnValue({
    data: [{ id: 7, label: "anime", seriesCount: 0, movieCount: 0 }],
    isLoading: false, isError: false,
  } as never)
  vi.mocked(tagApi.useCreateTag).mockReturnValue(mut({ mutateAsync: createTag }))

  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ToastProvider>
          <SeriesDetail id={3} />
        </ToastProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

it("assigns an existing tag to the series", async () => {
  const setTags = vi.fn()
  renderSeriesWithTags(setTags, vi.fn())
  await userEvent.type(screen.getByLabelText("Tags"), "anime{Enter}")
  await waitFor(() => expect(setTags).toHaveBeenCalledWith([7]))
})

// Pins the seam between TagInput's onCreate contract and useCreateTag: a novel
// label must create the tag and then assign the id the server returned.
it("creates a new tag from the series detail page and assigns it", async () => {
  const setTags = vi.fn()
  const createTag = vi.fn().mockResolvedValue({ id: 42, label: "documentary", seriesCount: 0, movieCount: 0 })
  renderSeriesWithTags(setTags, createTag)
  await userEvent.type(screen.getByLabelText("Tags"), "documentary{Enter}")
  await waitFor(() => expect(createTag).toHaveBeenCalledWith("documentary"))
  await waitFor(() => expect(setTags).toHaveBeenCalledWith([42]))
})
```

`waitFor` must be added to the `@testing-library/react` import in this file.

**In `MovieDetail.test.tsx`:** extend the `vi.mock` object at `:14-16` the same way, add the identical `tagApi` mock, extend the existing `renderMovie` helper with the four new `vi.mocked(...)` lines (accepting `setTags` and `createTag` as extra parameters defaulting to `vi.fn()`), and add:

```tsx
it("assigns an existing tag to the movie", async () => {
  const setTags = vi.fn()
  // DIFFERENT tag id (8) and DIFFERENT media id (5) than the series tests,
  // so a series/movie kind mix-up in useSetMediaTags cannot pass.
  vi.mocked(tagApi.useTags).mockReturnValue({
    data: [{ id: 8, label: "classics", seriesCount: 0, movieCount: 0 }],
    isLoading: false, isError: false,
  } as never)
  renderMovie(5, { id: 5, title: "Dune", year: 2021, overview: "x", monitored: true, hasFile: false, qualityProfileId: 1, posterUrl: "", fanartUrl: "" }, vi.fn(), vi.fn(), vi.fn(), setTags)
  await userEvent.type(screen.getByLabelText("Tags"), "classics{Enter}")
  await waitFor(() => expect(setTags).toHaveBeenCalledWith([8]))
})
```

Note the `vi.mocked(tagApi.useTags)` call must come **before** `renderMovie` so the helper's own default does not overwrite it — or, simpler, give `renderMovie` a `tags` parameter. Either is fine; pick one and be consistent.

- [ ] **Step 3: Run to verify it fails**

Run: `cd web && npx vitest run src/features/library/SeriesDetail.test.tsx`
Expected: FAIL — no element labelled `Tags`.

- [ ] **Step 4: Add the Tags row to `SeriesDetail.tsx`**

Add imports:

```tsx
import { TagInput } from "@/components/ui/tag-input"
import { useTags, useCreateTag } from "@/features/settings/tagApi"
```

Extend the existing `./api` import with `useMediaTags, useSetMediaTags`.

Inside the component, next to the other hooks:

```tsx
  const allTags = useTags()
  const createTag = useCreateTag()
  const mediaTags = useMediaTags("series", id)
  const setTags = useSetMediaTags("series", id)
```

Then, immediately after the closing `</div>` of the quality-profile `<div className="w-48">` block and before the closing `</div>` of the button row:

```tsx
          <div className="w-72">
            <TagInput
              aria-label="Tags"
              value={mediaTags.data ?? []}
              options={(allTags.data ?? []).map((t) => ({ id: t.id, label: t.label }))}
              onChange={(ids) => setTags.mutate(ids)}
              onCreate={async (label) => (await createTag.mutateAsync(label)).id}
            />
          </div>
```

- [ ] **Step 5: Add the same to `MovieDetail.tsx`**

Identical, with `"movie"` in place of `"series"` in both `useMediaTags` and `useSetMediaTags`. Everything else is the same.

- [ ] **Step 6: Run the tests**

Run: `cd web && npx vitest run && npx tsc -p tsconfig.app.json --noEmit`
Expected: all suites pass, 0 type errors.

- [ ] **Step 7: Mutation-verify**

1. In `MovieDetail.tsx`, change `useSetMediaTags("movie", id)` to `useSetMediaTags("series", id)` → the movie detail test fails. **This is why the two tests must use different ids — confirm red.**
2. In `useMediaTags`, return the raw response instead of `.tagIds` → the detail tests fail.
3. In `useSetMediaTags`, send `tagIds` as a bare array instead of `{ tagIds }` → the payload assertion fails.
4. In both detail pages, change `onCreate` to `async (label) => (await createTag.mutateAsync(label)) as unknown as number` (returning the whole tag instead of `.id`) → "creates a new tag from the series detail page and assigns it" fails on the `[42]` assertion. **This is the Task 5 ↔ Task 6 seam; confirm red.**

- [ ] **Step 8: Commit**

```bash
git add web/src/features/library/api.ts web/src/features/library/SeriesDetail.tsx web/src/features/library/MovieDetail.tsx web/src/features/library/SeriesDetail.test.tsx web/src/features/library/MovieDetail.test.tsx
git commit -m "feat(web): tag assignment on series and movie detail pages" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: Rebuild `web/dist` and verify the whole branch

**Files:**
- Modify: `web/dist/**` (build output)

**Interfaces:**
- Consumes: everything above.
- Produces: a committed bundle containing the tags UI.

- [ ] **Step 1: Full Go verification**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: every package `ok`. Record the package count.

- [ ] **Step 2: Full frontend verification**

Run: `cd web && npx tsc -p tsconfig.app.json --noEmit && npx vitest run`
Expected: 0 type errors; all test files pass. Record the file/test counts and compare to the pre-branch baseline (55 files / 268 tests) — the number must have gone **up**, not down.

- [ ] **Step 3: Rebuild the bundle**

Run: `cd web && npm run build`

- [ ] **Step 4: Verify the feature is actually in the bundle**

Run: `grep -r "New tag label" web/dist/assets/ | head -1`
Expected: a match. If there is no match, the build did not pick up `TagsSection` and the bundle is stale — stop and investigate rather than committing.

- [ ] **Step 5: Verify the build is reproducible**

Run `cd web && npm run build` a second time, then `git status --short web/dist`.
Expected: no further changes beyond what the first build produced. If the two builds differ, report it — a non-reproducible bundle means every future task produces spurious diffs.

- [ ] **Step 6: Commit**

```bash
git add web/dist
git commit -m "build(web): rebuild dist with the tags UI" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

- [ ] **Step 7: Report for whole-branch review**

Report: the commit range (`9de14af..HEAD`), the Go package count, the FE file/test counts, and every mutation that came back GREEN across all eight tasks. Do not merge — a whole-branch opus review runs first, then `superpowers:finishing-a-development-branch`.

---

## Self-Review Notes

**Spec coverage:** §2 → T1. §3 → T1 (CRUD) + T2 (associations, batch). §3.1 set semantics → T2. §3.2 batch reader → T2. §4.1 tag API → T3. §4.2 assignment → T4. §4.3 in-use 409 → T1 (store) + T3 (API) + T6 (toast). §4.4 client-side type-to-create → T5 (`onCreate`) + T7 (wiring). §4.5 no `Tags` struct field → Global Constraints. §5.1 `TagInput` → T5. §5.2 detail pages → T7. §5.3 Settings → T6. §6 testing → per-task. §7 build → T8.

**Known deviations from the spec, deliberate:**
- The spec's `Tag` struct is reused verbatim as the API response type; there is no separate DTO. Consistent with `quality`, which serialises `store.QualityProfile` directly.
- `internal/tag` has no `Service` layer. `quality` has one because it holds decision logic; tags have none.
- **`GET /series/{id}/tags` on a missing series returns 200 with an empty list, not 404.** The spec's §4.2 table only specifies the 400 case. `TagsForSeries` does no entity lookup, and the detail page's own `/series/{id}` request is what surfaces a missing series. Pinned by `TestGetTagsForMissingEntityIsLenient` so it is a decision rather than an accident.

**Harness facts verified against the real files before writing the snippets** (this repo has produced a fix wave every time a plan guessed): `newTestAPI(t, fp)` in `internal/media/api_test.go:19` mounts at the router root, so media test paths carry **no `/api/v1` prefix`**; `api.WriteJSON` uses `json.NewEncoder().Encode`, so an empty list is exactly `"[]\n"`; `cmd/nexus/main_test.go` already uses ports 9596-9599, so the tag test takes 9595 and authenticates with `X-Api-Key`; settings section tests wrap in the **real** `ToastProvider` and assert toast text with `findByText` rather than mocking `useToast`; both detail test files' `vi.mock` **spreads `actual`**, so `useMediaTags`/`useSetMediaTags` must be named in the mock object or the real hooks fire.

**Type consistency:** `Tag` (Go) ↔ `Tag` (TS) both carry `id`/`label`/`seriesCount`/`movieCount`. `tagIds` is the JSON key on every assignment endpoint (T4) and in both hooks (T7). `TagOption` (T5) is structurally a subset of `Tag` and is mapped explicitly at the call site in T7, not aliased.
