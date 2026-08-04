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
	// The prior set must be intact - no partial write.
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
