# Nexus Release Profiles (SP-3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add release profiles — reusable named rules scoped to media by tag that filter and score releases by substring terms on the raw release title — and wire them into every automation grab path.

**Architecture:** A `release_profiles` table plus a `release_profile_tags` junction table. Store CRUD for release profiles and whole-library batch readers. A matching engine in `internal/quality`. A new `internal/releaseprofile` API package. Wiring into the automation `Decide`/`compare` path so every grab path (search, RSS, upgrade; TV and movie) filters by applicable release profiles. A Settings → Release Profiles page.

**Tech Stack:** Go 1.22+, chi v5, modernc.org/sqlite, React 18 + TypeScript, TanStack Query v5, Tailwind (CSS custom properties only), vitest + @testing-library/react.

**Spec:** `docs/superpowers/specs/2026-08-05-nexus-release-profiles-design.md` (commit `f012042`).

**Branch:** `feat/release-profiles`, off master `413bb3f`.

## Global Constraints

- **Release profiles apply at EVERY grab path** — search, RSS, upgrade; TV and movie. This project has been bitten three times by fixing one site and missing the others. The four TV grab paths from the release-matching spec §5 plus the movie paths are enumerated in the spec §6. A passing test on one path proves nothing about the others.
- **`required_mode` fixture trap** (release-matching spec §9): `any` and `all` are only distinguishable with **two terms and a candidate matching exactly one of them**. A single term, or a candidate matching both, makes the test pass against either mode. Every `required_mode` test must use two terms and a candidate matching exactly one.
- **Fixture rule:** series and movies have independent rowid sequences (tags spec §6.1). Any test covering both series and movies must use **different tag ids and different media ids** on the two sides. Burn throwaway movie rows until the tagged movie's id is clear of every tagged series id, and assert the ids actually differ.
- **`DeleteTag` in-use check must count `release_profile_tags`.** A tag scoping a release profile is "in use" and its delete must be refused (spec §2.3). The existing `DeleteTag` in `internal/core/store/tag_store.go` counts `series_tags` and `movie_tags`; it must also count `release_profile_tags`.
- **The database migration count assertion** in `internal/core/database/database_test.go` is `applied != 10`. Adding migration 0011 **must bump it to 11**, or the database suite fails.
- **Frontend typecheck is `cd web && npx tsc -p tsconfig.app.json --noEmit`.** A bare `npx tsc --noEmit` in `web/` typechecks nothing and always exits 0.
- **`gofmt -l` is useless in this repo** (line-ending noise lists nearly every file). Trust `go build ./...` and `go vet ./...`.
- **Styling uses CSS custom properties only** — `var(--color-brand)`, `var(--color-border)`, `var(--color-panel)`, `var(--color-muted)`, `var(--color-warn)`, `var(--color-fg)`. No raw hex or `rgba()` literals.
- **Every task must mutation-verify its own tests before reporting done.** Each task lists named mutations. If a mutation comes back GREEN, report it — do not paper over it by adding an unrelated assertion.
- Source files must stay ASCII in Go comments. Editing a UTF-8 Go file with Python `open()` on this machine defaults to cp1252 and mangles non-ASCII into build errors. Use the Edit tool.
- **`web/dist` must be rebuilt, committed, and verified reproducible** when the frontend changes. `make verify-web` (`git diff --exit-code web/dist`) must stay green.

---

## File Structure

**Create:**
- `internal/core/database/migrations/0011_release_profiles.sql` — the two tables.
- `internal/core/store/release_profile_store.go` — `ReleaseProfile`, CRUD, batch readers.
- `internal/core/store/release_profile_store_test.go` — store tests.
- `internal/quality/release_profile.go` — the matching engine.
- `internal/quality/release_profile_test.go` — matching engine tests.
- `internal/releaseprofile/api.go` — release profile CRUD HTTP handlers.
- `internal/releaseprofile/api_test.go` — release profile API tests.
- `web/src/features/settings/releaseProfileTypes.ts` — the `ReleaseProfile` type.
- `web/src/features/settings/releaseProfileApi.ts` — query/mutation hooks.
- `web/src/features/settings/ReleaseProfilesSection.tsx` + `.test.tsx` — Settings → Release Profiles.
- `web/src/features/settings/ReleaseProfileDialog.tsx` + `.test.tsx` — the add/edit dialog.

**Modify:**
- `internal/core/store/tag_store.go` — `DeleteTag` in-use check counts `release_profile_tags`.
- `internal/core/store/tag_store_test.go` — test for the extended in-use check.
- `internal/core/database/database_test.go` — bump `applied != 10` to `applied != 11`.
- `internal/automation/decide.go` — `Decide` gains a `rps []store.ReleaseProfile` param; `Candidate` gains `ReleaseProfileScore`.
- `internal/automation/search.go` — resolve applicable profiles per item; pass into `Decide` at the movie, season-pack, and episode paths.
- `internal/automation/upgrade.go` — same for the upgrade paths.
- `internal/automation/rss.go` — build batch profile maps once per sweep; resolve per item; pass into `Decide`.
- `internal/automation/decide_test.go` — tests for the new `Decide` param and score tiebreaker.
- `internal/automation/search_test.go`, `upgrade_test.go`, `rss_test.go` — per-path release-profile gate tests.
- `cmd/nexus/main.go` — construct and mount `releaseProfileAPI`.
- `cmd/nexus/main_test.go` — a `TestRunMountsReleaseProfileRoutes`.
- `web/src/features/settings/SettingsLayout.tsx` — a Release Profiles tab.
- `web/src/app/routes.tsx` — a `releaseprofiles` route.

---

### Task 1: Migration, store CRUD, batch readers, DeleteTag extension, migration count

**Files:**
- Create: `internal/core/database/migrations/0011_release_profiles.sql`
- Create: `internal/core/store/release_profile_store.go`
- Create: `internal/core/store/release_profile_store_test.go`
- Modify: `internal/core/store/tag_store.go` (DeleteTag in-use check)
- Modify: `internal/core/store/tag_store_test.go`
- Modify: `internal/core/database/database_test.go` (bump 10 → 11)

**Interfaces:**
- Consumes: `database.Open`, `database.Migrate`, `store.New`, `store.ErrNotFound`, `store.ErrTagNotFound`, `store.TagInUseError` (all existing).
- Produces: `store.ReleaseProfile{ID int64, Name string, RequiredMode string, RequiredAny []string, RequiredAll []string, Ignored []string, Preferred []string, TagIDs []int64, CreatedAt time.Time}`; `(*Store).CreateReleaseProfile(ctx, p ReleaseProfile) (ReleaseProfile, error)`, `ListReleaseProfiles(ctx) ([]ReleaseProfile, error)`, `GetReleaseProfile(ctx, id int64) (ReleaseProfile, error)`, `UpdateReleaseProfile(ctx, p ReleaseProfile) error`, `DeleteReleaseProfile(ctx, id int64) error`, `SeriesReleaseProfileIDs(ctx) (map[int64][]int64, error)`, `MovieReleaseProfileIDs(ctx) (map[int64][]int64, error)`.

- [ ] **Step 1: Write the migration**

Create `internal/core/database/migrations/0011_release_profiles.sql`:

```sql
CREATE TABLE release_profiles (
  id            INTEGER PRIMARY KEY,
  name          TEXT NOT NULL,
  required_mode TEXT NOT NULL DEFAULT 'any',
  required_any  TEXT NOT NULL DEFAULT '[]',
  required_all  TEXT NOT NULL DEFAULT '[]',
  ignored       TEXT NOT NULL DEFAULT '[]',
  preferred     TEXT NOT NULL DEFAULT '[]',
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE release_profile_tags (
  release_profile_id INTEGER NOT NULL REFERENCES release_profiles(id) ON DELETE CASCADE,
  tag_id             INTEGER NOT NULL REFERENCES tags(id)             ON DELETE CASCADE,
  PRIMARY KEY(release_profile_id, tag_id)
);

CREATE INDEX idx_release_profile_tags_tag ON release_profile_tags(tag_id);
```

- [ ] **Step 2: Bump the migration count**

In `internal/core/database/database_test.go`, change `if applied != 10 {` to `if applied != 11 {` and the message `"expected 10 applied migrations"` to `"expected 11 applied migrations"`.

- [ ] **Step 3: Write the failing store tests**

Create `internal/core/store/release_profile_store_test.go`. Note `newReleaseProfileTestStore` mirrors `newTagTestStore` — a separate helper because Go test helpers in this package are per-domain.

```go
package store

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
)

func newReleaseProfileTestStore(t *testing.T) *Store {
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

func mustTag(t *testing.T, st *Store, label string) Tag {
	t.Helper()
	tg, err := st.CreateTag(context.Background(), label)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func TestReleaseProfileCRUD(t *testing.T) {
	st := newReleaseProfileTestStore(t)
	ctx := context.Background()
	tagA := mustTag(t, st, "a")
	tagB := mustTag(t, st, "b")

	created, err := st.CreateReleaseProfile(ctx, ReleaseProfile{
		Name: "No Dub", RequiredMode: "any",
		RequiredAny: []string{"1080p"}, Ignored: []string{"dub"},
		Preferred: []string{"bluray"}, TagIDs: []int64{tagA.ID, tagB.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Name != "No Dub" {
		t.Fatalf("bad created: %+v", created)
	}
	if len(created.TagIDs) != 2 {
		t.Fatalf("created TagIDs = %v, want 2", created.TagIDs)
	}

	got, err := st.GetReleaseProfile(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "No Dub" || got.RequiredMode != "any" {
		t.Fatalf("got = %+v", got)
	}
	if len(got.RequiredAny) != 1 || got.RequiredAny[0] != "1080p" {
		t.Fatalf("requiredAny = %v", got.RequiredAny)
	}
	if len(got.Ignored) != 1 || got.Ignored[0] != "dub" {
		t.Fatalf("ignored = %v", got.Ignored)
	}
	if len(got.Preferred) != 1 || got.Preferred[0] != "bluray" {
		t.Fatalf("preferred = %v", got.Preferred)
	}
	if len(got.TagIDs) != 2 {
		t.Fatalf("TagIDs = %v, want 2", got.TagIDs)
	}

	got.Name = "No Dub v2"
	got.RequiredMode = "all"
	got.TagIDs = []int64{tagA.ID}
	if err := st.UpdateReleaseProfile(ctx, got); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := st.GetReleaseProfile(ctx, created.ID)
	if reloaded.Name != "No Dub v2" || reloaded.RequiredMode != "all" {
		t.Fatalf("reloaded = %+v", reloaded)
	}
	if len(reloaded.TagIDs) != 1 || reloaded.TagIDs[0] != tagA.ID {
		t.Fatalf("reloaded TagIDs = %v, want [%d]", reloaded.TagIDs, tagA.ID)
	}

	list, err := st.ListReleaseProfiles(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	if len(list[0].TagIDs) != 1 {
		t.Fatalf("list TagIDs = %v, want 1", list[0].TagIDs)
	}

	if err := st.DeleteReleaseProfile(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetReleaseProfile(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestReleaseProfileUnknownTagRejectedAndRollsBack(t *testing.T) {
	st := newReleaseProfileTestStore(t)
	ctx := context.Background()
	good := mustTag(t, st, "good")

	created, err := st.CreateReleaseProfile(ctx, ReleaseProfile{
		Name: "P", RequiredAny: []string{"x"}, TagIDs: []int64{good.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Update with a mix of a good and an unknown tag id must fail and roll back.
	created.TagIDs = []int64{good.ID, 999}
	if err := st.UpdateReleaseProfile(ctx, created); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("expected ErrTagNotFound, got %v", err)
	}
	reloaded, _ := st.GetReleaseProfile(ctx, created.ID)
	if len(reloaded.TagIDs) != 1 || reloaded.TagIDs[0] != good.ID {
		t.Fatalf("prior TagIDs not preserved after rollback: %v", reloaded.TagIDs)
	}

	// Create with an unknown tag id must fail and leave nothing.
	if _, err := st.CreateReleaseProfile(ctx, ReleaseProfile{
		Name: "Bad", RequiredAny: []string{"x"}, TagIDs: []int64{999},
	}); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("expected ErrTagNotFound on create, got %v", err)
	}
	list, _ := st.ListReleaseProfiles(ctx)
	if len(list) != 1 {
		t.Fatalf("create with unknown tag must not leave a row, list = %+v", list)
	}
}

func TestReleaseProfileMissingID(t *testing.T) {
	st := newReleaseProfileTestStore(t)
	ctx := context.Background()
	if err := st.DeleteReleaseProfile(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: expected ErrNotFound, got %v", err)
	}
	if _, err := st.GetReleaseProfile(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: expected ErrNotFound, got %v", err)
	}
}

// Series and movies are tagged independently. DIFFERENT tag ids and DIFFERENT
// media ids on the two sides, so a series/movie mixup cannot pass.
func TestSeriesAndMovieReleaseProfileIDs(t *testing.T) {
	st := newReleaseProfileTestStore(t)
	ctx := context.Background()

	// Three tags so the series and movie sides never share an id.
	tagA, tagB, tagC := mustTag(t, st, "a"), mustTag(t, st, "b"), mustTag(t, st, "c")

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

	// Tag the series and movie.
	if err := st.SetSeriesTags(ctx, s1, []int64{tagA.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesTags(ctx, s2, []int64{tagB.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMovieTags(ctx, m1, []int64{tagC.ID}); err != nil {
		t.Fatal(err)
	}

	// Two release profiles: one scoped to tagA (applies to s1), one scoped to
	// tagC (applies to m1). A third with no tags applies to everything.
	pA, err := st.CreateReleaseProfile(ctx, ReleaseProfile{Name: "PA", RequiredAny: []string{"x"}, TagIDs: []int64{tagA.ID}})
	if err != nil {
		t.Fatal(err)
	}
	pC, err := st.CreateReleaseProfile(ctx, ReleaseProfile{Name: "PC", RequiredAny: []string{"x"}, TagIDs: []int64{tagC.ID}})
	if err != nil {
		t.Fatal(err)
	}
	pNone, err := st.CreateReleaseProfile(ctx, ReleaseProfile{Name: "PN", RequiredAny: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}

	seriesMap, err := st.SeriesReleaseProfileIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesMap) != 2 {
		t.Fatalf("series map has %d entries, want 2: %v", len(seriesMap), seriesMap)
	}
	if len(seriesMap[s1]) != 1 || seriesMap[s1][0] != pA.ID {
		t.Fatalf("series map[%d] = %v, want [%d]", s1, seriesMap[s1], pA.ID)
	}
	if len(seriesMap[s2]) != 1 || seriesMap[s2][0] != pC.ID {
		t.Fatalf("series map[%d] = %v, want [%d] (must NOT be pA)", s2, seriesMap[s2], pC.ID)
	}

	movieMap, err := st.MovieReleaseProfileIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(movieMap) != 1 || len(movieMap[m1]) != 1 || movieMap[m1][0] != pC.ID {
		t.Fatalf("movie map = %v, want {%d: [%d]}", movieMap, m1, pC.ID)
	}
	_ = pNone // a no-tag profile is not in the per-entity maps; it applies globally
}

func TestBatchReleaseProfileIDsEmptyLibrary(t *testing.T) {
	st := newReleaseProfileTestStore(t)
	ctx := context.Background()
	m, err := st.SeriesReleaseProfileIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || len(m) != 0 {
		t.Fatalf("expected an empty non-nil map, got %v", m)
	}
}

// A tag scoping a release profile is "in use" and its delete must be refused.
func TestDeleteTagInUseByReleaseProfile(t *testing.T) {
	st := newReleaseProfileTestStore(t)
	ctx := context.Background()
	tg := mustTag(t, st, "scoped")
	if _, err := st.CreateReleaseProfile(ctx, ReleaseProfile{
		Name: "P", RequiredAny: []string{"x"}, TagIDs: []int64{tg.ID},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteTag(ctx, tg.ID); !errors.Is(err, ErrTagInUse) {
		t.Fatalf("expected ErrTagInUse, got %v", err)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/core/store/ -run 'TestReleaseProfile|TestSeriesAndMovieRelease|TestBatchRelease|TestDeleteTagInUseByRelease' -v`
Expected: FAIL to compile — `undefined: ReleaseProfile`, `st.CreateReleaseProfile undefined`.

- [ ] **Step 5: Write the implementation**

Create `internal/core/store/release_profile_store.go`:

```go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ReleaseProfile is a reusable named rule scoped to media by tag. Term lists
// are matched case-insensitively as substrings on the raw release title.
type ReleaseProfile struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	RequiredMode string    `json:"requiredMode"`
	RequiredAny  []string  `json:"requiredAny"`
	RequiredAll  []string  `json:"requiredAll"`
	Ignored      []string  `json:"ignored"`
	Preferred    []string  `json:"preferred"`
	TagIDs       []int64   `json:"tagIds"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (s *Store) CreateReleaseProfile(ctx context.Context, p ReleaseProfile) (ReleaseProfile, error) {
	anyJSON, err := json.Marshal(p.RequiredAny)
	if err != nil {
		return ReleaseProfile{}, err
	}
	allJSON, err := json.Marshal(p.RequiredAll)
	if err != nil {
		return ReleaseProfile{}, err
	}
	ignJSON, err := json.Marshal(p.Ignored)
	if err != nil {
		return ReleaseProfile{}, err
	}
	prefJSON, err := json.Marshal(p.Preferred)
	if err != nil {
		return ReleaseProfile{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReleaseProfile{}, err
	}
	defer tx.Rollback()
	if err := s.checkTagIDs(ctx, tx, p.TagIDs); err != nil {
		return ReleaseProfile{}, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO release_profiles (name, required_mode, required_any, required_all, ignored, preferred) VALUES (?, ?, ?, ?, ?, ?)`,
		p.Name, p.RequiredMode, string(anyJSON), string(allJSON), string(ignJSON), string(prefJSON))
	if err != nil {
		return ReleaseProfile{}, err
	}
	id, _ := res.LastInsertId()
	for _, tagID := range p.TagIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO release_profile_tags (release_profile_id, tag_id) VALUES (?, ?)`, id, tagID); err != nil {
			return ReleaseProfile{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ReleaseProfile{}, err
	}
	return s.GetReleaseProfile(ctx, id)
}

func (s *Store) GetReleaseProfile(ctx context.Context, id int64) (ReleaseProfile, error) {
	var (
		p        ReleaseProfile
		anyJSON  string
		allJSON  string
		ignJSON  string
		prefJSON string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, required_mode, required_any, required_all, ignored, preferred, created_at FROM release_profiles WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.RequiredMode, &anyJSON, &allJSON, &ignJSON, &prefJSON, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ReleaseProfile{}, ErrNotFound
	}
	if err != nil {
		return ReleaseProfile{}, err
	}
	if err := json.Unmarshal([]byte(anyJSON), &p.RequiredAny); err != nil {
		return ReleaseProfile{}, err
	}
	if err := json.Unmarshal([]byte(allJSON), &p.RequiredAll); err != nil {
		return ReleaseProfile{}, err
	}
	if err := json.Unmarshal([]byte(ignJSON), &p.Ignored); err != nil {
		return ReleaseProfile{}, err
	}
	if err := json.Unmarshal([]byte(prefJSON), &p.Preferred); err != nil {
		return ReleaseProfile{}, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tag_id FROM release_profile_tags WHERE release_profile_id = ? ORDER BY tag_id`, id)
	if err != nil {
		return ReleaseProfile{}, err
	}
	defer rows.Close()
	p.TagIDs = []int64{}
	for rows.Next() {
		var tagID int64
		if err := rows.Scan(&tagID); err != nil {
			return ReleaseProfile{}, err
		}
		p.TagIDs = append(p.TagIDs, tagID)
	}
	return p, rows.Err()
}

func (s *Store) ListReleaseProfiles(ctx context.Context) ([]ReleaseProfile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, required_mode, required_any, required_all, ignored, preferred, created_at FROM release_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReleaseProfile
	for rows.Next() {
		var (
			p        ReleaseProfile
			anyJSON  string
			allJSON  string
			ignJSON  string
			prefJSON string
		)
		if err := rows.Scan(&p.ID, &p.Name, &p.RequiredMode, &anyJSON, &allJSON, &ignJSON, &prefJSON, &p.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(anyJSON), &p.RequiredAny); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(allJSON), &p.RequiredAll); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(ignJSON), &p.Ignored); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(prefJSON), &p.Preferred); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Populate TagIDs for every profile in one query.
	tagRows, err := s.db.QueryContext(ctx,
		`SELECT release_profile_id, tag_id FROM release_profile_tags ORDER BY release_profile_id, tag_id`)
	if err != nil {
		return nil, err
	}
	defer tagRows.Close()
	byID := map[int64]*ReleaseProfile{}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	for tagRows.Next() {
		var profileID, tagID int64
		if err := tagRows.Scan(&profileID, &tagID); err != nil {
			return nil, err
		}
		if p, ok := byID[profileID]; ok {
			p.TagIDs = append(p.TagIDs, tagID)
		}
	}
	if err := tagRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].TagIDs == nil {
			out[i].TagIDs = []int64{}
		}
	}
	return out, nil
}

func (s *Store) UpdateReleaseProfile(ctx context.Context, p ReleaseProfile) error {
	anyJSON, err := json.Marshal(p.RequiredAny)
	if err != nil {
		return err
	}
	allJSON, err := json.Marshal(p.RequiredAll)
	if err != nil {
		return err
	}
	ignJSON, err := json.Marshal(p.Ignored)
	if err != nil {
		return err
	}
	prefJSON, err := json.Marshal(p.Preferred)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.checkTagIDs(ctx, tx, p.TagIDs); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE release_profiles SET name = ?, required_mode = ?, required_any = ?, required_all = ?, ignored = ?, preferred = ? WHERE id = ?`,
		p.Name, p.RequiredMode, string(anyJSON), string(allJSON), string(ignJSON), string(prefJSON), p.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM release_profile_tags WHERE release_profile_id = ?`, p.ID); err != nil {
		return err
	}
	for _, tagID := range p.TagIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO release_profile_tags (release_profile_id, tag_id) VALUES (?, ?)`, p.ID, tagID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteReleaseProfile(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM release_profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// checkTagIDs verifies every tag id exists inside the transaction, so an
// unknown id returns ErrTagNotFound and rolls back (no partial write).
func (s *Store) checkTagIDs(ctx context.Context, tx *sql.Tx, tagIDs []int64) error {
	for _, tagID := range tagIDs {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE id = ?`, tagID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrTagNotFound
		}
	}
	return nil
}

// SeriesReleaseProfileIDs returns every series' applicable release-profile ids
// in one query. Series with no applicable profiles are omitted. Built once per
// RSS sweep, not per release.
func (s *Store) SeriesReleaseProfileIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT st.series_id, rpt.release_profile_id
		 FROM series_tags st
		 JOIN release_profile_tags rpt ON rpt.tag_id = st.tag_id
		 ORDER BY st.series_id, rpt.release_profile_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var seriesID, profileID int64
		if err := rows.Scan(&seriesID, &profileID); err != nil {
			return nil, err
		}
		out[seriesID] = append(out[seriesID], profileID)
	}
	return out, rows.Err()
}

// MovieReleaseProfileIDs returns every movie's applicable release-profile ids
// in one query. Movies with no applicable profiles are omitted. Built once per
// RSS sweep, not per release.
func (s *Store) MovieReleaseProfileIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT mt.movie_id, rpt.release_profile_id
		 FROM movie_tags mt
		 JOIN release_profile_tags rpt ON rpt.tag_id = mt.tag_id
		 ORDER BY mt.movie_id, rpt.release_profile_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var movieID, profileID int64
		if err := rows.Scan(&movieID, &profileID); err != nil {
			return nil, err
		}
		out[movieID] = append(out[movieID], profileID)
	}
	return out, rows.Err()
}
```

Then modify `internal/core/store/tag_store.go` `DeleteTag` to also count `release_profile_tags`:

```go
func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	var seriesCount, movieCount, profileCount int
	err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM series_tags WHERE tag_id = ?),
		        (SELECT COUNT(*) FROM movie_tags  WHERE tag_id = ?),
		        (SELECT COUNT(*) FROM release_profile_tags WHERE tag_id = ?)`, id, id, id).
		Scan(&seriesCount, &movieCount, &profileCount)
	if err != nil {
		return err
	}
	if seriesCount > 0 || movieCount > 0 || profileCount > 0 {
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

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/core/store/ -count=1 -v`
Expected: PASS, including the Task 1 tests and the existing tag tests.

Then `go build ./... && go vet ./...` — both clean.

- [ ] **Step 7: Mutation-verify**

Run each, confirm the named test goes RED, then revert:

1. In `DeleteTag`, drop the `release_profile_tags` subquery → `TestDeleteTagInUseByReleaseProfile` fails.
2. In `SeriesReleaseProfileIDs`, change the join to `movie_tags` instead of `series_tags` → `TestSeriesAndMovieReleaseProfileIDs` fails on the series map.
3. In `MovieReleaseProfileIDs`, change the join to `series_tags` instead of `movie_tags` → `TestSeriesAndMovieReleaseProfileIDs` fails on the movie map.
4. In `UpdateReleaseProfile`, move the `checkTagIDs` call to after the delete-and-insert → `TestReleaseProfileUnknownTagRejectedAndRollsBack` fails on the preserved-set assertion.
5. In `CreateReleaseProfile`, remove the `checkTagIDs` call → `TestReleaseProfileUnknownTagRejectedAndRollsBack` fails on the create-with-unknown-tag assertion.
6. In `ListReleaseProfiles`, change `out[i].TagIDs = []int64{}` to `var out []ReleaseProfile` → the nil check in `TestReleaseProfileCRUD` fails.

Report any mutation that stays GREEN rather than adding an assertion to cover it.

- [ ] **Step 8: Commit**

```bash
git add internal/core/database/migrations/0011_release_profiles.sql internal/core/database/database_test.go internal/core/store/release_profile_store.go internal/core/store/release_profile_store_test.go internal/core/store/tag_store.go internal/core/store/tag_store_test.go
git commit -m "feat(store): release profiles table, CRUD, and batch readers" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: Matching engine

**Files:** Create `internal/quality/release_profile.go`, `internal/quality/release_profile_test.go`.

**Interfaces:** Consumes `store.ReleaseProfile` (Task 1). Produces `quality.ReleaseProfileMatch{Accepted bool, Score int, Reason string}`; `quality.MatchReleaseProfile(rawTitle string, p store.ReleaseProfile) ReleaseProfileMatch`.

- [ ] **Step 1: Write the failing tests** — `internal/quality/release_profile_test.go`:

```go
package quality

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/store"
)

func TestRequiredAny(t *testing.T) {
	p := store.ReleaseProfile{RequiredMode: "any", RequiredAny: []string{"1080p", "bluray"}}
	// Candidate matching exactly one of two terms — the only shape that
	// discriminates any from all.
	if m := MatchReleaseProfile("Show.S01E01.1080p.WEB-DL", p); !m.Accepted {
		t.Fatalf("expected accepted, got %+v", m)
	}
	if m := MatchReleaseProfile("Show.S01E01.720p.WEB-DL", p); m.Accepted {
		t.Fatalf("expected rejected (no required term), got %+v", m)
	}
}

func TestRequiredAll(t *testing.T) {
	p := store.ReleaseProfile{RequiredMode: "all", RequiredAny: []string{"Indigo", "1080p"}}
	// Candidate matching exactly one of two terms must be rejected under "all".
	if m := MatchReleaseProfile("Pokemon.Indigo.League.S01E01.720p", p); m.Accepted {
		t.Fatalf("expected rejected (only one of two required), got %+v", m)
	}
	if m := MatchReleaseProfile("Pokemon.Indigo.League.S01E01.1080p", p); !m.Accepted {
		t.Fatalf("expected accepted (both required), got %+v", m)
	}
}

// A bad required_mode value fails to the permissive "any" default.
func TestRequiredModeDefaultsToAny(t *testing.T) {
	p := store.ReleaseProfile{RequiredMode: "bogus", RequiredAny: []string{"1080p", "bluray"}}
	if m := MatchReleaseProfile("Show.S01E01.1080p", p); !m.Accepted {
		t.Fatalf("bogus mode must behave as any, got %+v", m)
	}
}

func TestIgnored(t *testing.T) {
	p := store.ReleaseProfile{Ignored: []string{"dub"}}
	if m := MatchReleaseProfile("Show.S01E01.HebDub.1080p", p); m.Accepted {
		t.Fatalf("expected rejected (ignored term), got %+v", m)
	}
	if m := MatchReleaseProfile("Show.S01E01.1080p", p); !m.Accepted {
		t.Fatalf("expected accepted (no ignored term), got %+v", m)
	}
}

func TestPreferredScores(t *testing.T) {
	p := store.ReleaseProfile{Preferred: []string{"bluray", "remux"}}
	low := MatchReleaseProfile("Show.S01E01.1080p.WEB-DL", p)
	high := MatchReleaseProfile("Show.S01E01.1080p.BluRay.Remux", p)
	if !low.Accepted || !high.Accepted {
		t.Fatalf("preferred must not gate acceptance: low=%+v high=%+v", low, high)
	}
	if high.Score != 2 || low.Score != 0 {
		t.Fatalf("expected high.Score=2 low.Score=0, got %d vs %d", high.Score, low.Score)
	}
}

// Matching is case-insensitive substring on the RAW title, so tokens parsing
// strips (HebDub) remain targetable.
func TestCaseInsensitiveSubstringOnRawTitle(t *testing.T) {
	p := store.ReleaseProfile{Ignored: []string{"hebdub"}}
	if m := MatchReleaseProfile("Show.S01E01.HebDub.1080p", p); m.Accepted {
		t.Fatalf("expected rejected (case-insensitive ignored), got %+v", m)
	}
}
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/quality/ -run 'TestRequired|TestIgnored|TestPreferred|TestCaseInsensitive|TestRequiredMode' -v` → FAIL to compile (`undefined: MatchReleaseProfile`).

- [ ] **Step 3: Write the implementation** — `internal/quality/release_profile.go`:

```go
package quality

import (
	"strings"

	"github.com/hellboundg/nexus/internal/core/store"
)

// ReleaseProfileMatch is the result of evaluating one release against one
// release profile.
type ReleaseProfileMatch struct {
	Accepted bool
	Score    int
	Reason   string // rejection reason, when not accepted
}

// MatchReleaseProfile evaluates a raw release title against a release profile.
// Matching is case-insensitive substring on the RAW title, not the parsed
// title, so tokens parsing strips (HebDub, -BurCyg) remain targetable.
//
// Required terms use required_mode: "any" (default) accepts when any term
// matches; "all" requires every term. Any value other than "all" is treated as
// "any", so a bad value fails to the permissive default rather than silently
// rejecting everything. Ignored terms reject when any matches. Preferred terms
// do not gate acceptance; each match adds one to the score.
func MatchReleaseProfile(rawTitle string, p store.ReleaseProfile) ReleaseProfileMatch {
	lower := strings.ToLower(rawTitle)
	contains := func(term string) bool { return strings.Contains(lower, strings.ToLower(term)) }

	for _, term := range p.Ignored {
		if term != "" && contains(term) {
			return ReleaseProfileMatch{Accepted: false, Reason: "ignored term: " + term}
		}
	}

	any := p.RequiredAny
	all := p.RequiredAll
	if p.RequiredMode == "all" {
		for _, term := range all {
			if term != "" && !contains(term) {
				return ReleaseProfileMatch{Accepted: false, Reason: "missing required term: " + term}
			}
		}
	} else {
		// "any" (default): at least one required-any term must match.
		matched := false
		for _, term := range any {
			if term != "" && contains(term) {
				matched = true
				break
			}
		}
		if len(any) > 0 && !matched {
			return ReleaseProfileMatch{Accepted: false, Reason: "no required term matched"}
		}
	}

	score := 0
	for _, term := range p.Preferred {
		if term != "" && contains(term) {
			score++
		}
	}
	return ReleaseProfileMatch{Accepted: true, Score: score}
}
```

- [ ] **Step 4: Run to verify they pass** — `go test ./internal/quality/ -count=1 -v`; then `go build ./... && go vet ./...`.

- [ ] **Step 5: Mutation-verify** (each must go RED, then revert):
1. Change `if p.RequiredMode == "all"` to `if p.RequiredMode == "bogus"` → `TestRequiredAll` fails.
2. Change `if len(any) > 0 && !matched` to `if !matched` → `TestRequiredAny`'s second assertion fails.
3. Remove the ignored-term loop → `TestIgnored` fails.
4. Change `score++` to `score += 2` → `TestPreferredScores` fails (pins exact counts).
5. Change `strings.ToLower(rawTitle)` to `rawTitle` → `TestCaseInsensitiveSubstringOnRawTitle` fails.

Report any mutation that stays GREEN rather than adding an assertion to cover it.

- [ ] **Step 6: Commit**

```bash
git add internal/quality/release_profile.go internal/quality/release_profile_test.go
git commit -m "feat(quality): release profile matching engine" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: Release profile API package

**Files:**
- Create: `internal/releaseprofile/api.go`
- Create: `internal/releaseprofile/api_test.go`
- Modify: `cmd/nexus/main.go` (around `:140` and `:189`)
- Modify: `cmd/nexus/main_test.go`

**Interfaces:**
- Consumes: Task 1's store methods and errors; `api.WriteJSON(w, status, v)`, `api.WriteError(w, status, code, message)` from `internal/core/api`.
- Produces: `releaseprofile.NewAPI(st *store.Store) *releaseprofile.API` with `(*API).Mount(r chi.Router)` registering `/releaseprofile` routes.

There is a service layer for validation, mirroring `internal/quality/service.go` (quality has a `Service` because it holds decision logic; release profiles hold validation logic, so they get one too).

- [ ] **Step 1: Write the failing tests**

Create `internal/releaseprofile/api_test.go`:

```go
package releaseprofile

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestReleaseProfileAPICRUD(t *testing.T) {
	r, st := newTestRouter(t)
	ctx := context.Background()
	tg, err := st.CreateTag(ctx, "anime")
	if err != nil {
		t.Fatal(err)
	}

	rec := do(t, r, http.MethodGet, "/api/v1/releaseprofile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Fatalf("empty list must serialise as [], got %q", got)
	}

	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "No Dub", "requiredMode": "any",
		"requiredAny": []string{"1080p"}, "ignored": []string{"dub"},
		"preferred": []string{"bluray"}, "tagIds": []int64{tg.ID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created store.ReleaseProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Name != "No Dub" {
		t.Fatalf("created = %+v", created)
	}
	if len(created.TagIDs) != 1 || created.TagIDs[0] != tg.ID {
		t.Fatalf("created TagIDs = %v", created.TagIDs)
	}

	rec = do(t, r, http.MethodPut, "/api/v1/releaseprofile/"+itoa(created.ID), map[string]any{
		"name": "No Dub v2", "requiredMode": "all",
		"requiredAny": []string{"Indigo", "1080p"}, "ignored": []string{},
		"preferred": []string{}, "tagIds": []int64{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, r, http.MethodGet, "/api/v1/releaseprofile/"+itoa(created.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d body=%s", rec.Code, rec.Body.String())
	}
	var got store.ReleaseProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "No Dub v2" || got.RequiredMode != "all" {
		t.Fatalf("got = %+v", got)
	}
	if len(got.RequiredAny) != 2 {
		t.Fatalf("requiredAny = %v, want 2", got.RequiredAny)
	}

	rec = do(t, r, http.MethodDelete, "/api/v1/releaseprofile/"+itoa(created.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodGet, "/api/v1/releaseprofile/"+itoa(created.ID), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d, want 404", rec.Code)
	}
}

func TestReleaseProfileAPIValidation(t *testing.T) {
	r, st := newTestRouter(t)
	ctx := context.Background()
	tg, err := st.CreateTag(ctx, "anime")
	if err != nil {
		t.Fatal(err)
	}

	// Empty name → 400.
	rec := do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "", "requiredAny": []string{"1080p"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name: %d, want 400", rec.Code)
	}

	// No terms → 400.
	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "Empty", "requiredAny": []string{}, "requiredAll": []string{},
		"ignored": []string{}, "preferred": []string{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no terms: %d, want 400", rec.Code)
	}

	// Bad required mode → 400.
	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "Bad", "requiredMode": "bogus", "requiredAny": []string{"1080p"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad mode: %d, want 400", rec.Code)
	}

	// Unknown tag id → 400.
	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "BadTag", "requiredAny": []string{"1080p"}, "tagIds": []int64{999},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown tag: %d, want 400", rec.Code)
	}

	// Valid create with a tag still works.
	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "Good", "requiredAny": []string{"1080p"}, "tagIds": []int64{tg.ID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid create: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReleaseProfileAPINotFound(t *testing.T) {
	r, _ := newTestRouter(t)
	rec := do(t, r, http.MethodGet, "/api/v1/releaseprofile/999", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d, want 404", rec.Code)
	}
	rec = do(t, r, http.MethodDelete, "/api/v1/releaseprofile/999", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/releaseprofile/ -v`
Expected: FAIL to compile — `undefined: NewAPI`.

- [ ] **Step 3: Write the implementation**

Create `internal/releaseprofile/api.go`:

```go
// Package releaseprofile exposes release-profile CRUD over HTTP. Release
// profiles are reusable named rules scoped to media by tag that filter and
// score releases by substring terms on the raw release title.
package releaseprofile

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/store"
)

// ErrInvalidProfile is returned when a release profile fails validation.
var ErrInvalidProfile = errors.New("releaseprofile: invalid profile")

// Service owns release-profile CRUD with validation over the store.
type Service struct {
	store *store.Store
}

func NewService(st *store.Store) *Service { return &Service{store: st} }

func validateProfile(p store.ReleaseProfile) error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrInvalidProfile
	}
	if p.RequiredMode != "any" && p.RequiredMode != "all" {
		return ErrInvalidProfile
	}
	if len(p.RequiredAny) == 0 && len(p.RequiredAll) == 0 && len(p.Ignored) == 0 && len(p.Preferred) == 0 {
		return ErrInvalidProfile
	}
	return nil
}

func (s *Service) CreateProfile(ctx context.Context, p store.ReleaseProfile) (store.ReleaseProfile, error) {
	if err := validateProfile(p); err != nil {
		return store.ReleaseProfile{}, err
	}
	return s.store.CreateReleaseProfile(ctx, p)
}

func (s *Service) GetProfile(ctx context.Context, id int64) (store.ReleaseProfile, error) {
	return s.store.GetReleaseProfile(ctx, id)
}

func (s *Service) ListProfiles(ctx context.Context) ([]store.ReleaseProfile, error) {
	return s.store.ListReleaseProfiles(ctx)
}

func (s *Service) UpdateProfile(ctx context.Context, p store.ReleaseProfile) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	return s.store.UpdateReleaseProfile(ctx, p)
}

func (s *Service) DeleteProfile(ctx context.Context, id int64) error {
	return s.store.DeleteReleaseProfile(ctx, id)
}

type API struct {
	svc *Service
}

func NewAPI(st *store.Store) *API { return &API{svc: NewService(st)} }

func (a *API) Mount(r chi.Router) {
	r.Route("/releaseprofile", func(r chi.Router) {
		r.Get("/", a.listProfiles)
		r.Post("/", a.createProfile)
		r.Get("/{id}", a.getProfile)
		r.Put("/{id}", a.updateProfile)
		r.Delete("/{id}", a.deleteProfile)
	})
}

func profileID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func writeProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidProfile):
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, store.ErrTagNotFound):
		api.WriteError(w, http.StatusBadRequest, "bad_request", "unknown tag id")
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "release profile not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (a *API) listProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.ListProfiles(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list release profiles")
		return
	}
	if rows == nil {
		rows = []store.ReleaseProfile{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) createProfile(w http.ResponseWriter, r *http.Request) {
	var p store.ReleaseProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	created, err := a.svc.CreateProfile(r.Context(), p)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, created)
}

func (a *API) getProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	p, err := a.svc.GetProfile(r.Context(), id)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, p)
}

func (a *API) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	var p store.ReleaseProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	p.ID = id
	if err := a.svc.UpdateProfile(r.Context(), p); err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	if err := a.svc.DeleteProfile(r.Context(), id); err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

Then in `cmd/nexus/main.go`, after `tagAPI := tag.NewAPI(st)` (line ~140), add:

```go
releaseProfileAPI := releaseprofile.NewAPI(st)
```

And add `releaseProfileAPI.Mount` to the `NewRouter` varargs (line ~189), after `tagAPI.Mount`.

Add the import `"github.com/hellboundg/nexus/internal/releaseprofile"` to `cmd/nexus/main.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/releaseprofile/ -count=1 -v`
Expected: PASS.

Then `go build ./... && go vet ./...` — both clean.

- [ ] **Step 5: Mutation-verify**

Run each, confirm the named test goes RED, then revert:

1. In `validateProfile`, remove the `RequiredMode` check → `TestReleaseProfileAPIValidation`'s bad-mode assertion fails.
2. In `validateProfile`, remove the "no terms" check → `TestReleaseProfileAPIValidation`'s no-terms assertion fails.
3. In `writeProfileError`, change `store.ErrTagNotFound` to `store.ErrNotFound` → `TestReleaseProfileAPIValidation`'s unknown-tag assertion fails (it would 404 instead of 400).
4. In `listProfiles`, change `rows = []store.ReleaseProfile{}` to `var rows []store.ReleaseProfile` → `TestReleaseProfileAPICRUD`'s `[]\n` assertion fails.

Report any mutation that stays GREEN rather than adding an assertion to cover it.

- [ ] **Step 6: Commit**

```bash
git add internal/releaseprofile/api.go internal/releaseprofile/api_test.go cmd/nexus/main.go cmd/nexus/main_test.go
git commit -m "feat(releaseprofile): release profile CRUD API" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Automation wiring

**Files:**
- Modify: `internal/automation/decide.go` — `Decide` gains a `rps []store.ReleaseProfile` param; `Candidate` gains `ReleaseProfileScore`.
- Modify: `internal/automation/search.go` — resolve applicable profiles per item; pass into `Decide` at the movie, season-pack, and episode paths.
- Modify: `internal/automation/upgrade.go` — same for the upgrade paths.
- Modify: `internal/automation/rss.go` — build batch profile maps once per sweep; resolve per item; pass into `Decide`.
- Modify: `internal/automation/decide_test.go`, `search_test.go`, `upgrade_test.go`, `rss_test.go` — per-path release-profile gate tests.

**Interfaces:**
- Consumes: `store.ReleaseProfile`, `store.SeriesReleaseProfileIDs`, `store.MovieReleaseProfileIDs`, `store.TagsForSeries`, `store.TagsForMovie`, `quality.MatchReleaseProfile` (Tasks 1-2).
- Produces: `Decide(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile, rps []store.ReleaseProfile) []Candidate`; `Candidate.ReleaseProfileScore int`.

- [ ] **Step 1: Change `Decide` and `compare`**

In `internal/automation/decide.go`:

```go
type Candidate struct {
	Release             provider.Release
	Parsed              parsing.ParsedRelease
	ReleaseProfileScore int
}

func Decide(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile, rps []store.ReleaseProfile) []Candidate {
	var out []Candidate
	for _, r := range releases {
		p := parsing.Parse(r.Title, kind)
		if !quality.Decide(p, profile).Accepted {
			continue
		}
		// Release profiles are orthogonal to quality: a release must pass every
		// applicable release profile's required/ignored checks. The score is the
		// sum of preferred-term matches across all applicable profiles.
		score := 0
		rejected := false
		for _, rp := range rps {
			m := quality.MatchReleaseProfile(r.Title, rp)
			if !m.Accepted {
				rejected = true
				break
			}
			score += m.Score
		}
		if rejected {
			continue
		}
		out = append(out, Candidate{Release: r, Parsed: p, ReleaseProfileScore: score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return compare(out[i], out[j], profile) > 0
	})
	return out
}
```

In `compare`, add the release-profile score as a tiebreaker after the quality comparison and before the torrent-seeder/usenet-age/size tiebreakers:

```go
	if a.ReleaseProfileScore != b.ReleaseProfileScore {
		if a.ReleaseProfileScore > b.ReleaseProfileScore {
			return 1
		}
		return -1
	}
```

- [ ] **Step 2: Add a helper to resolve applicable profiles**

In `internal/automation/automation.go` (or a new `release_profiles.go` in the package), add:

```go
// applicableReleaseProfiles returns the release profiles that apply to an item
// with the given tag ids: those whose TagIDs intersect the item's tags, plus
// any profile with no tags (a no-tag profile applies to everything).
func applicableReleaseProfiles(all []store.ReleaseProfile, itemTagIDs []int64) []store.ReleaseProfile {
	itemSet := map[int64]struct{}{}
	for _, id := range itemTagIDs {
		itemSet[id] = struct{}{}
	}
	var out []store.ReleaseProfile
	for _, rp := range all {
		if len(rp.TagIDs) == 0 {
			out = append(out, rp)
			continue
		}
		for _, tagID := range rp.TagIDs {
			if _, ok := itemSet[tagID]; ok {
				out = append(out, rp)
				break
			}
		}
	}
	return out
}
```

- [ ] **Step 3: Wire the search paths** (`internal/automation/search.go`)

Each search entry point loads the item's tags and the full release-profile list, resolves the applicable profiles, and passes them into `Decide`:

- `searchMovie`: after `profileFor`, load `s.store.TagsForMovie(ctx, m.ID)` and `s.store.ListReleaseProfiles(ctx)`, resolve, and pass into `Decide(releases, provider.KindMovie, profile, rps)`.
- `searchSeason` pack branch: load `s.store.TagsForSeries(ctx, se.ID)` and the profile list, resolve, pass into `Decide(releases, provider.KindTV, profile, rps)`.
- `searchEpisode`: same as the pack branch.

To avoid repeating the load in every path, add a helper on `Service`:

```go
func (s *Service) releaseProfilesForSeries(ctx context.Context, seriesID int64) ([]store.ReleaseProfile, error) {
	all, err := s.store.ListReleaseProfiles(ctx)
	if err != nil {
		return nil, err
	}
	tags, err := s.store.TagsForSeries(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	return applicableReleaseProfiles(all, tags), nil
}

func (s *Service) releaseProfilesForMovie(ctx context.Context, movieID int64) ([]store.ReleaseProfile, error) {
	all, err := s.store.ListReleaseProfiles(ctx)
	if err != nil {
		return nil, err
	}
	tags, err := s.store.TagsForMovie(ctx, movieID)
	if err != nil {
		return nil, err
	}
	return applicableReleaseProfiles(all, tags), nil
}
```

- [ ] **Step 4: Wire the upgrade paths** (`internal/automation/upgrade.go`)

`upgradeMovie` and `upgradeEpisode` resolve the applicable profiles (via the same helpers) and pass them into `Decide`.

- [ ] **Step 5: Wire the RSS paths** (`internal/automation/rss.go`)

In `RSSSync`, after building the library index, build the batch profile maps once per sweep:

```go
allProfiles, err := s.store.ListReleaseProfiles(ctx)
if err != nil {
	return res, err
}
seriesProfiles, err := s.store.SeriesReleaseProfileIDs(ctx)
if err != nil {
	return res, err
}
movieProfiles, err := s.store.MovieReleaseProfileIDs(ctx)
if err != nil {
	return res, err
}
```

Then, per movie/series, resolve the applicable profiles by intersecting the batch map with the item's tag ids (the batch maps already encode the tag intersection), plus any no-tag profile:

```go
func rpsForEntity(all []store.ReleaseProfile, byEntity map[int64][]int64, entityID int64) []store.ReleaseProfile {
	ids := map[int64]struct{}{}
	for _, id := range byEntity[entityID] {
		ids[id] = struct{}{}
	}
	var out []store.ReleaseProfile
	for _, rp := range all {
		if len(rp.TagIDs) == 0 {
			out = append(out, rp)
			continue
		}
		if _, ok := ids[rp.ID]; ok {
			out = append(out, rp)
		}
	}
	return out
}
```

Pass the resolved profiles into `Decide` at the movie path and the TV path.

- [ ] **Step 6: Write the per-path gate tests**

Each grab path gets a test proving it filters by release profile. A passing test on one path proves nothing about the others. For each path, seed a series/movie with a tag, a release profile scoped to that tag with an `Ignored` term, and a candidate release whose raw title contains the ignored term; assert the candidate is NOT grabbed. Then a candidate without the ignored term IS grabbed.

The `required_mode` fixture trap applies: use two terms and a candidate matching exactly one when testing `any` vs `all` at the automation level.

- [ ] **Step 7: Run the full suite**

Run: `go test ./internal/automation/ -count=1 -v`
Expected: PASS, including the existing automation tests (which call `Decide` — they must be updated to pass an empty `rps` slice).

Then `go build ./... && go vet ./...` — both clean.

- [ ] **Step 8: Mutation-verify**

Run each, confirm the named test goes RED, then revert:

1. In `Decide`, remove the release-profile rejection loop → every per-path gate test fails.
2. In `compare`, remove the `ReleaseProfileScore` tiebreaker → the score-tiebreaker test fails.
3. In `applicableReleaseProfiles`, drop the "no tags applies to everything" branch → a no-tag-profile test fails.
4. In `rpsForEntity`, change `byEntity[entityID]` to `byEntity[0]` → the RSS gate test fails.

Report any mutation that stays GREEN rather than adding an assertion to cover it.

- [ ] **Step 9: Commit**

```bash
git add internal/automation/
git commit -m "feat(automation): apply release profiles at every grab path" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: Frontend — Settings → Release Profiles

**Files:**
- Create: `web/src/features/settings/releaseProfileTypes.ts`
- Create: `web/src/features/settings/releaseProfileApi.ts`
- Create: `web/src/features/settings/ReleaseProfilesSection.tsx` + `.test.tsx`
- Create: `web/src/features/settings/ReleaseProfileDialog.tsx` + `.test.tsx`
- Modify: `web/src/features/settings/SettingsLayout.tsx` — a Release Profiles tab after Quality Profiles.
- Modify: `web/src/app/routes.tsx` — a `releaseprofiles` route.

**Interfaces:**
- Consumes: `apiGet`, `apiPost`, `apiPut`, `apiDelete` from `@/lib/api`; `useToast` from `@/lib/toast`; the tag hooks from `./tagApi`.
- Produces: `ReleaseProfile` type, CRUD hooks, `ReleaseProfilesSection`, `ReleaseProfileDialog`.

- [ ] **Step 1: Types and API hooks**

`web/src/features/settings/releaseProfileTypes.ts`:

```ts
export type ReleaseProfile = {
  id: number
  name: string
  requiredMode: "any" | "all"
  requiredAny: string[]
  requiredAll: string[]
  ignored: string[]
  preferred: string[]
  tagIds: number[]
  createdAt: string
}

export type ReleaseProfilePayload = {
  name: string
  requiredMode: "any" | "all"
  requiredAny: string[]
  requiredAll: string[]
  ignored: string[]
  preferred: string[]
  tagIds: number[]
}
```

`web/src/features/settings/releaseProfileApi.ts` mirrors `qualityApi.ts`:

```ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type { ReleaseProfile, ReleaseProfilePayload } from "./releaseProfileTypes"

export const releaseProfileKeys = {
  profiles: ["settings", "releaseprofiles"] as const,
}

export function useReleaseProfiles() {
  return useQuery({ queryKey: releaseProfileKeys.profiles, queryFn: () => apiGet<ReleaseProfile[]>("/releaseprofile") })
}

export function useSaveReleaseProfile() {
  const qc = useQueryClient()
  return useMutation<ReleaseProfile | { ok: boolean }, Error, { payload: ReleaseProfilePayload; id?: number }>({
    mutationFn: ({ payload, id }) =>
      id == null
        ? apiPost<ReleaseProfile>("/releaseprofile", payload)
        : apiPut<{ ok: boolean }>(`/releaseprofile/${id}`, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: releaseProfileKeys.profiles }),
  })
}

export function useDeleteReleaseProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/releaseprofile/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: releaseProfileKeys.profiles }),
  })
}
```

- [ ] **Step 2: The section and dialog**

`ReleaseProfilesSection.tsx` mirrors `QualityProfilesSection.tsx`: lists each profile with its name, term counts, and assigned tags, with add / edit / delete affordances. Delete surfaces a 400 as an error toast.

`ReleaseProfileDialog.tsx` edits the name, the four term lists (comma-separated text inputs), the required mode (a select), and the assigned tags (multi-select from existing tags, reusing the `TagInput`-style pattern from the tags feature). On save it calls `useSaveReleaseProfile` with the payload.

- [ ] **Step 3: Wire the tab and route**

In `SettingsLayout.tsx`, add `{ to: "/settings/releaseprofiles", label: "Release Profiles" }` after Quality Profiles and before Tags.

In `web/src/app/routes.tsx`, add a `releaseprofiles` route rendering `ReleaseProfilesSection`.

- [ ] **Step 4: Write the vitest tests**

- `ReleaseProfilesSection.test.tsx`: list renders, add/edit/delete round-trip through mocked hooks, 400 surfaces as a toast.
- `ReleaseProfileDialog.test.tsx`: term list editing, required-mode toggle, tag multi-select.

- [ ] **Step 5: Typecheck and test**

Run: `cd web && npx tsc -p tsconfig.app.json --noEmit` and `npx vitest run`.
Expected: PASS.

- [ ] **Step 6: Rebuild web/dist**

Run: `make web` (rebuilds `web/dist`), then `make verify-web` must stay green.

- [ ] **Step 7: Mutation-verify**

Run each, confirm the named test goes RED, then revert:

1. In `ReleaseProfilesSection`, remove the error-toast on 400 → the 400-surfaces test fails.
2. In `ReleaseProfileDialog`, remove the required-mode select → the required-mode-toggle test fails.

Report any mutation that stays GREEN rather than adding an assertion to cover it.

- [ ] **Step 8: Commit**

```bash
git add web/src web/dist
git commit -m "feat(web): Settings > Release Profiles page" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"