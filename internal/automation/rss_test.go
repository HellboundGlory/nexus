package automation

import (
	"context"
	"fmt"
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
		nil,
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

func TestMatchSeriesBySeasonPackTitle(t *testing.T) {
	idx := testIndex()
	// Season-pack-only title, no episode group: p.Title from parsing.Parse still
	// carries the bare season token ("The Show S01"), which must be stripped
	// (after the un-stripped lookup misses) to resolve by title.
	r := provider.Release{Title: "The.Show.S01.1080p.BluRay.x264-GRP"}
	se, ok := idx.matchSeries(r, parsing.Parse(r.Title, provider.KindTV))
	if !ok || se.ID != 10 {
		t.Fatalf("season-pack title series match failed: ok=%v se=%v", ok, se)
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

func TestRSSSyncMultiEpisodeReleaseNotDoubleGrabbedInPartialSeason(t *testing.T) {
	st := newStore(t)
	// 3 monitored episodes; E01 already filed, E02+E03 missing.
	_, epIDs := seedSeries(t, st, true, 3)
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{
		MediaKind: "tv", EpisodeID: &epIDs[0], RelativePath: "e1.mkv", QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}
	// A single multi-episode release covering E02+E03 (not a season pack, since
	// E01 is already filed the season isn't fully missing, so the pack path is
	// skipped and this release is only reachable via the per-episode loop).
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E02E03.1080p.BluRay.x264-GRP", DownloadURL: "e2e3", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(fe.reqs) != 1 {
		t.Fatalf("multi-episode release must be grabbed exactly once, got %d reqs: %+v", len(fe.reqs), fe.reqs)
	}
	if res.Grabbed != 1 {
		t.Fatalf("want Grabbed=1, got %+v", res)
	}
	got := map[int64]bool{}
	for _, id := range fe.reqs[0].EpisodeIDs {
		got[id] = true
	}
	if !got[epIDs[1]] || !got[epIDs[2]] {
		t.Fatalf("single grab should cover both E02 and E03, got EpisodeIDs=%v", fe.reqs[0].EpisodeIDs)
	}
}

func TestRSSSyncSkipsBlocklistedRelease(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	id := seedMovie(t, st, true, true) // "The Film" (2020), tmdb 42+seq, monitored, HD profile
	blockedTitle := "The.Film.2020.1080p.BluRay.x264-GRP"
	if _, err := st.AddBlocklist(ctx, store.Blocklist{
		MediaKind: "movie", MovieID: &id, SourceTitle: blockedTitle,
		Protocol: "usenet", Reason: "test",
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		// Best release (Bluray) is blocklisted; must not be re-grabbed.
		{Title: blockedTitle, DownloadURL: "blocked", Protocol: provider.ProtocolUsenet, Categories: []int{2040}},
		// Clean alternative (WEB-DL) must still be grabbed.
		{Title: "The.Film.2020.1080p.WEB-DL.x264-GRP", DownloadURL: "clean", Protocol: provider.ProtocolUsenet, Categories: []int{2040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 {
		t.Fatalf("want exactly 1 grab (the clean alternative): res=%+v reqs=%+v", res, fe.reqs)
	}
	if fe.reqs[0].DownloadURL != "clean" {
		t.Fatalf("blocklisted release must not be grabbed; want clean alternative, got %q", fe.reqs[0].DownloadURL)
	}
}

func TestRSSSyncStopsAtPerSeriesLimit(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedSeries(t, st, true, 5)
	// One release per episode: ungated, RSS would place all five.
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	// Default config → limit 1.

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 {
		t.Fatalf("RSS must respect the per-series limit: grabbed=%d reqs=%d", res.Grabbed, len(fe.reqs))
	}
}

// TestRSSSyncPackBranchRespectsPerSeriesLimit isolates rssPlaceTV's season-pack
// branch: two fully-missing monitored seasons, each with its own acceptable
// season-pack release. If bud.take() were missing from the pack branch, both
// packs would be grabbed; the per-series budget (default 1) must stop it at
// one, proving the pack branch's take() call is load-bearing.
func TestRSSSyncPackBranchRespectsPerSeriesLimit(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 2) // season 1: 2 fully-missing episodes
	if err := st.UpsertSeason(ctx, store.Season{SeriesID: sid, SeasonNumber: 2, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 2, EpisodeNumber: i, Monitored: true}); err != nil {
			t.Fatal(err)
		}
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "p1", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
		{Title: "The.Show.S02.1080p.BluRay.x264-GRP", DownloadURL: "p2", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	// Default config → limit 1.

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 {
		t.Fatalf("pack branch must respect the per-series limit across seasons: grabbed=%d reqs=%d", res.Grabbed, len(fe.reqs))
	}
}

// TestRSSSyncUngatedWhenLimitZero is the RSS-path off-switch test: limit 0
// disables the gate entirely, so the Step-1 fixture's five per-episode
// releases must all be grabbed.
func TestRSSSyncUngatedWhenLimitZero(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedSeries(t, st, true, 5)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 0
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 5 || len(fe.reqs) != 5 {
		t.Fatalf("limit 0 disables the gate, want all 5: grabbed=%d reqs=%d", res.Grabbed, len(fe.reqs))
	}
}

// TestRSSSyncIgnoresOtherSeriesInFlight is the RSS-path cross-series isolation
// test: a DIFFERENT series (distinct TMDBID, since seedSeries hardcodes
// TMDBID 7 and download_queue.series_id has a real FK to series(id)) is
// saturated with in-flight rows (each needing a distinct ClientItemID, or the
// UNIQUE(download_client_id, client_item_id) insert fails), and the series
// under test must still grab its full default budget of 1.
func TestRSSSyncIgnoresOtherSeriesInFlight(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedSeries(t, st, true, 5)

	otherID, err := st.CreateSeries(ctx, store.Series{TMDBID: 999, Title: "Other Show", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSeason(ctx, store.Season{SeriesID: otherID, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: otherID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	otherEps, err := st.ListEpisodes(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := st.EnqueueGrab(ctx, store.QueueItem{
			ClientItemID: fmt.Sprintf("other-%d", i),
			MediaKind:    "tv", SeriesID: &otherID, EpisodeIDs: []int64{otherEps[0].ID}, Status: store.QueueGrabbed,
		}); err != nil {
			t.Fatal(err)
		}
	}

	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 {
		t.Fatalf("another series' in-flight rows must not affect this series' budget: grabbed=%d reqs=%d", res.Grabbed, len(fe.reqs))
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

func TestRSSSyncPlacesMonitoredEpisodesOfUnmonitoredSeries(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedPartiallyMonitoredShow(t, st, 5, 3)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 {
		t.Fatalf("RSS must place monitored episodes of an unmonitored series: grabbed=%d reqs=%d", res.Grabbed, len(fe.reqs))
	}
}

func TestRSSSyncMatchesAliasNamedRelease(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 3)
	if err := st.ReplaceSeriesAliases(ctx, sid, []store.SeriesAlias{{Title: "The Show Alternate", Country: "US"}}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.Alternate.S01E01.1080p.WEB-DL.x264-GRP", DownloadURL: "alias", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "alias" {
		t.Fatalf("RSS must resolve an alias-named release to its series: grabbed=%d reqs=%+v", res.Grabbed, fe.reqs)
	}
}

// A TMDB alias can normalize to the series' OWN primary title ("Pokemon"
// aliasing "Pokemon" with an accent). Indexing it a second time under the same
// key would make matchSeries see two candidates and, with no year to
// disambiguate, refuse -- silently breaking primary-title matching for that
// show. The alias loop dedups by series id to prevent this.
func TestRSSSyncAliasEqualToPrimaryTitleStillMatches(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 3) // primary title "The Show", no first-aired year
	if err := st.ReplaceSeriesAliases(ctx, sid, []store.SeriesAlias{{Title: "the show", Country: "US"}}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.WEB-DL.x264-GRP", DownloadURL: "primary", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 1 || len(fe.reqs) != 1 {
		t.Fatalf("a primary-title release must still match when an alias duplicates the primary title: grabbed=%d reqs=%+v", res.Grabbed, fe.reqs)
	}
}

func TestRSSSyncSkipsSeriesWithNoMonitoredEpisodes(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 5)
	for _, id := range epIDs {
		if err := st.SetEpisodeMonitored(ctx, id, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSeriesMonitored(ctx, sid, false); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 0 || len(fe.reqs) != 0 {
		t.Fatalf("a fully unmonitored series must be skipped: grabbed=%d reqs=%d", res.Grabbed, len(fe.reqs))
	}
}
