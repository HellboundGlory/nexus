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

	// Two series and one movie, so the ids on the two sides differ.
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
