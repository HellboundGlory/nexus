package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

func testIndex() *libraryIndex {
	return buildLibraryIndex(
		[]store.Movie{
			{ID: 1, TMDBID: 603, IMDbID: "tt0133093", Title: "The Matrix", Year: 1999, Monitored: true},
			{ID: 2, Title: "The Office", Year: 2005, Monitored: true},
			{ID: 3, Title: "The Office", Year: 2001, Monitored: true},
			{ID: 9, Title: "Unmonitored", Year: 2010, Monitored: false},
		},
		[]store.Series{
			{ID: 10, TMDBID: 7, Title: "The Show", FirstAired: "2018-01-01", Monitored: true},
			{ID: 11, Title: "Clone", FirstAired: "1999-01-01", Monitored: true},
			{ID: 12, Title: "Clone", FirstAired: "2015-01-01", Monitored: true},
		},
	)
}

func TestNormTitle(t *testing.T) {
	if got := normTitle("The.Matrix: Reloaded!"); got != "the matrix reloaded" {
		t.Fatalf("normTitle = %q", got)
	}
}

func TestMatchMovieByID(t *testing.T) {
	idx := testIndex()
	// tmdbid wins even if the title is wrong.
	r := provider.Release{TMDBID: 603, Title: "Totally.Different.2020.1080p.BluRay-GRP"}
	m, ok := idx.matchMovie(r, parsing.Parse(r.Title, provider.KindMovie))
	if !ok || m.ID != 1 {
		t.Fatalf("tmdbid match failed: ok=%v m=%v", ok, m)
	}
	// imdbid (release stores it tt-stripped, as Task 1 does).
	r2 := provider.Release{IMDbID: "0133093", Title: "x"}
	m2, ok2 := idx.matchMovie(r2, parsing.Parse(r2.Title, provider.KindMovie))
	if !ok2 || m2.ID != 1 {
		t.Fatalf("imdbid match failed: ok=%v m=%v", ok2, m2)
	}
}

func TestMatchMovieByTitleYear(t *testing.T) {
	idx := testIndex()
	r := provider.Release{Title: "The.Office.2005.1080p.WEB-DL.x264-GRP"}
	m, ok := idx.matchMovie(r, parsing.Parse(r.Title, provider.KindMovie))
	if !ok || m.ID != 2 {
		t.Fatalf("title+year should pick the 2005 Office, got ok=%v m=%v", ok, m)
	}
}

func TestMatchMovieAmbiguousTitleDropped(t *testing.T) {
	idx := testIndex()
	// No year in the release, two "The Office" movies → ambiguous → drop.
	r := provider.Release{Title: "The.Office.1080p.WEB-DL.x264-GRP"}
	if _, ok := idx.matchMovie(r, parsing.Parse(r.Title, provider.KindMovie)); ok {
		t.Fatalf("ambiguous same-title movies must be dropped")
	}
}

func TestMatchMovieUnmonitoredNotIndexed(t *testing.T) {
	idx := testIndex()
	r := provider.Release{Title: "Unmonitored.2010.1080p.BluRay-GRP"}
	if _, ok := idx.matchMovie(r, parsing.Parse(r.Title, provider.KindMovie)); ok {
		t.Fatalf("unmonitored movie must not be matchable")
	}
}

func TestMatchSeriesByID(t *testing.T) {
	idx := testIndex()
	r := provider.Release{TMDBID: 7, Title: "Wrong.Title.S01E01-GRP"}
	se, ok := idx.matchSeries(r, parsing.Parse(r.Title, provider.KindTV))
	if !ok || se.ID != 10 {
		t.Fatalf("tmdbid series match failed: ok=%v se=%v", ok, se)
	}
}

func TestMatchSeriesByTitle(t *testing.T) {
	idx := testIndex()
	r := provider.Release{Title: "The.Show.S02E05.1080p.WEB-DL-GRP"}
	se, ok := idx.matchSeries(r, parsing.Parse(r.Title, provider.KindTV))
	if !ok || se.ID != 10 {
		t.Fatalf("title series match failed: ok=%v se=%v", ok, se)
	}
}

func TestMatchSeriesAmbiguousDroppedWithoutYear(t *testing.T) {
	idx := testIndex()
	// Two "Clone" series, release title has no year → ambiguous → drop.
	r := provider.Release{Title: "Clone.S01E01.1080p.WEB-DL-GRP"}
	if _, ok := idx.matchSeries(r, parsing.Parse(r.Title, provider.KindTV)); ok {
		t.Fatalf("ambiguous same-title series must be dropped without a year")
	}
}

func TestMatchSeriesDisambiguatedByYear(t *testing.T) {
	idx := testIndex()
	// Release carries a year matching the 2015 "Clone" first-aired year.
	r := provider.Release{Title: "Clone.2015.S01E01.1080p.WEB-DL-GRP"}
	se, ok := idx.matchSeries(r, parsing.Parse(r.Title, provider.KindTV))
	if !ok || se.ID != 12 {
		t.Fatalf("year should disambiguate to the 2015 Clone, got ok=%v se=%v", ok, se)
	}
}

func TestRouteKind(t *testing.T) {
	cases := []struct {
		name string
		r    provider.Release
		kind provider.MediaKind
		ok   bool
	}{
		{"category movie", provider.Release{Categories: []int{2040}, Title: "x"}, provider.KindMovie, true},
		{"category tv", provider.Release{Categories: []int{5040}, Title: "x"}, provider.KindTV, true},
		{"heuristic tv", provider.Release{Title: "The.Show.S01E01.1080p.WEB-DL-GRP"}, provider.KindTV, true},
		{"heuristic movie", provider.Release{Title: "The.Film.2020.1080p.BluRay-GRP"}, provider.KindMovie, true},
		{"undecidable", provider.Release{Title: "Just Some Words"}, "", false},
	}
	for _, c := range cases {
		k, ok := routeKind(c.r)
		if ok != c.ok || k != c.kind {
			t.Errorf("%s: routeKind = (%q,%v), want (%q,%v)", c.name, k, ok, c.kind, c.ok)
		}
	}
}
