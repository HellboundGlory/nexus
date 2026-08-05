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
	// s1 is tagged tagA (pA applies); s2 is tagged tagB (no profile applies),
	// so the series map has exactly one entry and s2 is absent.
	if len(seriesMap) != 1 {
		t.Fatalf("series map has %d entries, want 1: %v", len(seriesMap), seriesMap)
	}
	if len(seriesMap[s1]) != 1 || seriesMap[s1][0] != pA.ID {
		t.Fatalf("series map[%d] = %v, want [%d]", s1, seriesMap[s1], pA.ID)
	}
	if _, ok := seriesMap[s2]; ok {
		t.Fatalf("series map[%d] = %v, want absent (s2 has tagB, no profile applies)", s2, seriesMap[s2])
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