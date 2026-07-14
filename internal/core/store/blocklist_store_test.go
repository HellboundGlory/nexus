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
