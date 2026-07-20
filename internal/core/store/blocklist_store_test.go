package store

import (
	"context"
	"fmt"
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

	movieID, err := st.CreateMovie(ctx, Movie{TMDBID: 1, Title: "Dune"})
	if err != nil {
		t.Fatal(err)
	}
	seriesID, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "Show"})
	if err != nil {
		t.Fatal(err)
	}
	mid := movieID
	sid := seriesID

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

func TestBlocklistedReasonsScopedByMovie(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	movieID, err := st.CreateMovie(ctx, Movie{TMDBID: 7, Title: "Some Movie"})
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := st.CreateMovie(ctx, Movie{TMDBID: 8, Title: "Other Movie"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.AddBlocklist(ctx, Blocklist{
		MediaKind: "movie", MovieID: &movieID,
		SourceTitle: "Some.Movie.2019.1080p.WEB-DL", Protocol: "usenet",
		QualityID: 3, Reason: "Not on your server(s)",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBlocklist(ctx, Blocklist{
		MediaKind: "movie", MovieID: &otherID,
		SourceTitle: "Other.Movie.2020.1080p.WEB-DL", Protocol: "usenet",
		QualityID: 3, Reason: "Aborted, cannot be completed",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.BlocklistedReasons(ctx, &movieID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry scoped to movie %d, got %d: %v", movieID, len(got), got)
	}
	key := NormReleaseTitle("Some.Movie.2019.1080p.WEB-DL")
	if got[key] != "Not on your server(s)" {
		t.Fatalf("reason for %q = %q, want %q", key, got[key], "Not on your server(s)")
	}
}

func TestBlocklistedReasonsNoTargetReturnsEmptyNonNil(t *testing.T) {
	st := newTestStore(t)
	got, err := st.BlocklistedReasons(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("want a non-nil empty map, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want empty map, got %v", got)
	}
}

func TestListBlocklistPageAndClear(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	movieID, err := st.CreateMovie(ctx, Movie{TMDBID: 1, Title: "Dune"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := st.AddBlocklist(ctx, Blocklist{
			MediaKind: "movie", MovieID: &movieID,
			SourceTitle: fmt.Sprintf("Dune.2021.%d-GRP", i), Reason: "boom",
		}); err != nil {
			t.Fatal(err)
		}
	}

	rows, total, err := st.ListBlocklistPage(ctx, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(rows) != 3 {
		t.Fatalf("page = %d rows, total %d; want 3 rows, total 4", len(rows), total)
	}
	if rows[0].SourceTitle != "Dune.2021.3-GRP" {
		t.Fatalf("rows[0] = %q, want newest (id DESC)", rows[0].SourceTitle)
	}

	n, err := st.ClearBlocklist(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("ClearBlocklist = %d, want 4", n)
	}
	if rows, _, _ := st.ListBlocklistPage(ctx, 0, 50); len(rows) != 0 {
		t.Fatalf("after clear: %d rows, want 0", len(rows))
	}
}
