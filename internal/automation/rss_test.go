package automation

import (
	"context"
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

func TestRSSSyncMatchesMovieAndGrabs(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true) // "The Film" (2020), tmdb 42, monitored, HD profile
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u", Protocol: provider.ProtocolUsenet, Categories: []int{2040}},
		{Title: "Unrelated.Thing.2019.1080p.WEB-DL-GRP", DownloadURL: "x", Protocol: provider.ProtocolUsenet, Categories: []int{2040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Considered != 2 || res.Matched != 1 || res.Grabbed != 1 {
		t.Fatalf("counts = %+v, want considered=2 matched=1 grabbed=1", res)
	}
	if len(fe.reqs) != 1 || fe.reqs[0].MovieID != id || fe.reqs[0].DownloadURL != "u" {
		t.Fatalf("bad enqueue: %+v", fe.reqs)
	}
	if fs.lastQuery.Type != provider.SearchGeneric {
		t.Fatalf("RSS must use a generic empty-term query, got %+v", fs.lastQuery)
	}
}

func TestRSSSyncPicksBestOfDuplicates(t *testing.T) {
	st := newStore(t)
	seedSeries(t, st, true, 1) // "The Show", 1 monitored episode S01E01
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.WEB-DL.x264-GRP", DownloadURL: "web", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 {
		t.Fatalf("two dupes should yield one grab: res=%+v reqs=%d", res, len(fe.reqs))
	}
	if fe.reqs[0].DownloadURL != "blu" {
		t.Fatalf("best (Bluray) should be chosen, got %q", fe.reqs[0].DownloadURL)
	}
}

func TestRSSSyncPrefersSeasonPack(t *testing.T) {
	st := newStore(t)
	sid, _ := seedSeries(t, st, true, 3) // 3 monitored missing episodes → fully missing
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "single", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "pack" {
		t.Fatalf("fully-missing season should grab the pack once: res=%+v reqs=%+v", res, fe.reqs)
	}
	if len(fe.reqs[0].EpisodeIDs) != 3 {
		t.Fatalf("pack should carry all 3 missing episode ids, got %v", fe.reqs[0].EpisodeIDs)
	}
	_ = sid
}

func TestRSSSyncSkipsFiledEpisode(t *testing.T) {
	st := newStore(t)
	_, epIDs := seedSeries(t, st, true, 1)
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{
		MediaKind: "tv", EpisodeID: &epIDs[0], RelativePath: "e1.mkv", QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Matched (it resolves to the series) but not grabbed (episode already filed).
	if res.Grabbed != 0 || len(fe.reqs) != 0 {
		t.Fatalf("already-filed episode must not be grabbed: res=%+v reqs=%d", res, len(fe.reqs))
	}
}

func TestRSSSyncSkipsInFlightEpisode(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 1)
	// Pre-existing in-flight grab for that episode.
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		SeriesID: &sid, MediaKind: "tv", EpisodeIDs: []int64{epIDs[0]},
		SourceTitle: "prev", Protocol: "usenet", QualityID: 9, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 0 || len(fe.reqs) != 0 {
		t.Fatalf("in-flight episode must not be re-grabbed: res=%+v reqs=%d", res, len(fe.reqs))
	}
}
