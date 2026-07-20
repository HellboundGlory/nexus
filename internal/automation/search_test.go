package automation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
)

type fakeSearcher struct {
	lastQuery     provider.Query
	releases      []provider.Release
	err           error
	indexerErrors []IndexerError
}

func (f *fakeSearcher) Search(_ context.Context, q provider.Query) ([]provider.Release, error) {
	f.lastQuery = q
	return f.releases, f.err
}

func (f *fakeSearcher) SearchDetailed(_ context.Context, q provider.Query) ([]provider.Release, []IndexerError) {
	f.lastQuery = q
	return f.releases, f.indexerErrors
}

type fakeEnqueuer struct {
	reqs  []importing.EnqueueRequest
	errOn func(importing.EnqueueRequest) error // optional per-request error
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, req importing.EnqueueRequest) (store.QueueItem, error) {
	f.reqs = append(f.reqs, req)
	if f.errOn != nil {
		if err := f.errOn(req); err != nil {
			return store.QueueItem{}, err
		}
	}
	return store.QueueItem{ID: int64(len(f.reqs))}, nil
}

var movieSeq int

func seedMovie(t *testing.T, st *store.Store, monitored bool, withProfile bool) int64 {
	t.Helper()
	ctx := context.Background()
	movieSeq++
	id, err := st.CreateMovie(ctx, store.Movie{
		TMDBID: 42 + movieSeq, IMDbID: fmt.Sprintf("tt%d", 42+movieSeq),
		Title: "The Film", Year: 2020, Monitored: monitored,
	})
	if err != nil {
		t.Fatal(err)
	}
	if withProfile {
		p := hdProfile()
		p.Name = fmt.Sprintf("HD-m%d", movieSeq)
		prof, err := st.CreateQualityProfile(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetMovieQualityProfileID(ctx, id, &prof.ID); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestSearchMovieEnqueuesBest(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.WEB-DL.x264-GRP", DownloadURL: "u1", Protocol: provider.ProtocolUsenet, IndexerID: "nz"},
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u2", Protocol: provider.ProtocolUsenet, IndexerID: "nz"},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchMovie(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("want 1 grab, got n=%d reqs=%d", n, len(fe.reqs))
	}
	if fe.reqs[0].DownloadURL != "u2" {
		t.Fatalf("Bluray should be chosen, got %q", fe.reqs[0].DownloadURL)
	}
	if fe.reqs[0].MediaKind != provider.KindMovie || fe.reqs[0].MovieID != id {
		t.Fatalf("bad enqueue request: %+v", fe.reqs[0])
	}
	if fs.lastQuery.Type != provider.SearchMovie || fs.lastQuery.IMDbID == "" || fs.lastQuery.TMDBID == 0 {
		t.Fatalf("query should carry the movie's ids: %+v", fs.lastQuery)
	}
}

func TestSearchMovieSkipsWhenNotMonitored(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, false, true)
	fs := &fakeSearcher{}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchMovie(context.Background(), id)
	if err != nil || n != 0 {
		t.Fatalf("unmonitored movie must not search; n=%d err=%v", n, err)
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("no search should have run, got query %+v", fs.lastQuery)
	}
}

func TestSearchMovieFallsThroughOnGrabFailure(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-BEST", DownloadURL: "best", Protocol: provider.ProtocolUsenet},
		{Title: "The.Film.2020.1080p.WEB-DL.x264-NEXT", DownloadURL: "next", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{errOn: func(r importing.EnqueueRequest) error {
		if r.DownloadURL == "best" {
			return errors.New("grab boom")
		}
		return nil
	}}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchMovie(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 2 || fe.reqs[1].DownloadURL != "next" {
		t.Fatalf("should fall through to next candidate: n=%d reqs=%+v", n, fe.reqs)
	}
}

func TestSearchMovieNoProfileStops(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, false) // monitored, but no quality profile
	fs := &fakeSearcher{}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchMovie(context.Background(), id)
	if err != nil || n != 0 {
		t.Fatalf("no-profile movie should skip cleanly; n=%d err=%v", n, err)
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("no search without a profile, got %+v", fs.lastQuery)
	}
}

func seedSeries(t *testing.T, st *store.Store, monitored bool, epCount int) (seriesID int64, epIDs []int64) {
	t.Helper()
	ctx := context.Background()
	prof, err := st.CreateQualityProfile(ctx, hdProfile())
	if err != nil {
		t.Fatal(err)
	}
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 7, Title: "The Show", Monitored: monitored})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesQualityProfileID(ctx, sid, &prof.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSeason(ctx, store.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= epCount; i++ {
		if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: i, Monitored: true}); err != nil {
			t.Fatal(err)
		}
	}
	eps, _ := st.ListEpisodes(ctx, sid)
	for _, e := range eps {
		epIDs = append(epIDs, e.ID)
	}
	return sid, epIDs
}

func TestSearchSeasonFullyMissingPrefersPack(t *testing.T) {
	st := newStore(t)
	sid, epIDs := seedSeries(t, st, true, 3)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "single", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(context.Background(), sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "pack" {
		t.Fatalf("fully-missing season should grab the pack once: n=%d reqs=%+v", n, fe.reqs)
	}
	if len(fe.reqs[0].EpisodeIDs) != 3 {
		t.Fatalf("pack should carry all 3 missing episode ids, got %v", fe.reqs[0].EpisodeIDs)
	}
	if fs.lastQuery.Season == nil || *fs.lastQuery.Season != 1 || fs.lastQuery.Episode != nil {
		t.Fatalf("pack search query should set season and not episode: %+v", fs.lastQuery)
	}
	_ = epIDs
}

func TestSearchSeasonNoPackFallsBackToEpisodes(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 2)
	// Searcher returns only per-episode singles regardless of query (no pack).
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01E02.1080p.BluRay.x264-GRP", DownloadURL: "e2", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	// This test is about per-episode fallback covering every missing episode,
	// not about the per-series concurrency gate — disable the gate so both of
	// the two seeded missing episodes are grabbed as originally intended.
	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 0
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}

	n, err := svc.SearchSeason(ctx, sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(fe.reqs) != 2 {
		t.Fatalf("no acceptable pack should fall back to 2 per-episode grabs: n=%d reqs=%d", n, len(fe.reqs))
	}
	for _, r := range fe.reqs {
		if len(r.EpisodeIDs) != 1 {
			t.Fatalf("per-episode grab should carry exactly one episode id, got %v", r.EpisodeIDs)
		}
	}
}

func TestSearchSeasonPartiallyMissingSearchesEpisodesOnly(t *testing.T) {
	st := newStore(t)
	sid, epIDs := seedSeries(t, st, true, 2)
	// Give episode 1 a media file so the season is only partially missing.
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{MediaKind: "tv", EpisodeID: &epIDs[0], RelativePath: "e1.mkv", QualityID: 9}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E02.1080p.BluRay.x264-GRP", DownloadURL: "e2", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(context.Background(), sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].EpisodeIDs[0] != epIDs[1] {
		t.Fatalf("only the missing episode 2 should be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
	if fs.lastQuery.Episode == nil {
		t.Fatalf("partially-missing season must use per-episode queries: %+v", fs.lastQuery)
	}
}

func TestSearchEpisodeSingle(t *testing.T) {
	st := newStore(t)
	sid, epIDs := seedSeries(t, st, true, 2)
	_ = sid
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchEpisode(context.Background(), epIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].EpisodeIDs[0] != epIDs[0] {
		t.Fatalf("episode search should grab that one episode: n=%d reqs=%+v", n, fe.reqs)
	}
}

func TestSearchEpisodeSkipsWhenFiled(t *testing.T) {
	st := newStore(t)
	_, epIDs := seedSeries(t, st, true, 1)
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{MediaKind: "tv", EpisodeID: &epIDs[0], RelativePath: "e1.mkv", QualityID: 9}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchEpisode(context.Background(), epIDs[0])
	if err != nil || n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("already-filed episode must be skipped: n=%d err=%v reqs=%d", n, err, len(fe.reqs))
	}
}

func TestSearchMovieSkipsWhenAlreadyQueued(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true)
	mv := id
	// An in-flight grab exists (no media file yet) — must not be re-grabbed.
	if _, err := st.EnqueueGrab(context.Background(), store.QueueItem{
		MediaKind: "movie", MovieID: &mv, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchMovie(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("queued movie must not be re-grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("no search for an already-queued movie, got %+v", fs.lastQuery)
	}
}

func TestSearchEpisodeSkipsWhenAlreadyQueued(t *testing.T) {
	st := newStore(t)
	sid, epIDs := seedSeries(t, st, true, 1)
	ep := epIDs[0]
	if _, err := st.EnqueueGrab(context.Background(), store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{ep}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.SearchEpisode(context.Background(), ep)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("queued episode must not be re-grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
}

func TestSearchSeasonTreatsQueuedEpisodeAsNotMissing(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 2)
	// ep1 is already actively queued; only ep2 is truly missing.
	e1 := epIDs[0]
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{e1}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E02.1080p.BluRay.x264-GRP", DownloadURL: "e2", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	// This test is about missing-detection (a queued episode is not
	// re-searched), not the per-series concurrency gate. ep1's in-flight row
	// would otherwise exhaust the default budget of 1 before ep2 is even
	// considered, which is a different concern — disable the gate.
	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 0
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	n, err := svc.SearchSeason(ctx, sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].EpisodeIDs[0] != epIDs[1] {
		t.Fatalf("only the un-queued missing episode 2 should be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
	// Not fully missing (ep1 in flight) → per-episode search, not a pack.
	if fs.lastQuery.Episode == nil {
		t.Fatalf("should be a per-episode search, got %+v", fs.lastQuery)
	}
}

// episodeReleases returns one release per episode of season 1, so a series
// search would grab every episode if nothing stopped it.
func episodeReleases(n int) []provider.Release {
	var rs []provider.Release
	for i := 1; i <= n; i++ {
		rs = append(rs, provider.Release{
			Title:       fmt.Sprintf("The.Show.S01E%02d.1080p.BluRay.x264-GRP", i),
			DownloadURL: fmt.Sprintf("e%d", i),
			Protocol:    provider.ProtocolUsenet,
		})
	}
	return rs
}

func TestSearchSeriesStopsAtPerSeriesLimit(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 5)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	// Default config → limit 1.

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("limit 1 must grab exactly 1 of 5 missing episodes: n=%d reqs=%d", n, len(fe.reqs))
	}
}

func TestSearchSeriesRespectsHigherLimit(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 5)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 3
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || len(fe.reqs) != 3 {
		t.Fatalf("limit 3 must grab exactly 3: n=%d reqs=%d", n, len(fe.reqs))
	}
}

func TestSearchSeriesUngatedWhenLimitZero(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 5)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 0
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || len(fe.reqs) != 5 {
		t.Fatalf("limit 0 disables the gate, want all 5: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// An in-flight row created directly (as a manual grab would) occupies the slot.
func TestSearchSeriesCountsExistingInFlightRow(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 5)
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[0]}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("series already at limit must grab nothing: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// The budget spans seasons: two fully-missing seasons must still yield 1 grab.
func TestSearchSeriesBudgetSpansSeasons(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 2) // creates season 1 only
	if err := st.UpsertSeason(ctx, store.Season{SeriesID: sid, SeasonNumber: 2, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err := st.UpsertEpisode(ctx, store.Episode{
			SeriesID: sid, SeasonNumber: 2, EpisodeNumber: i, Monitored: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "p1", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S02.1080p.BluRay.x264-GRP", DownloadURL: "p2", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("budget is per series, not per season: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// TestSearchSeriesIgnoresOtherSeriesInFlight is the controller-addendum test:
// the five gate tests above cannot distinguish inFlight[seriesID] from a sum
// across all series, because they only ever have one series with in-flight
// rows. Here a DIFFERENT series (distinct TMDBID, since seedSeries hardcodes
// TMDBID 7 and download_queue.series_id has a real FK to series(id)) is
// saturated with in-flight rows, and the searched series must still grab its
// full default budget of 1.
func TestSearchSeriesIgnoresOtherSeriesInFlight(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 5)

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
	// Saturate the OTHER series with several in-flight rows — enough that if
	// the gate summed across all series instead of keying on seriesID, the
	// searched series would be refused too. Each row needs a distinct
	// ClientItemID or the UNIQUE(download_client_id, client_item_id) insert fails.
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

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("another series' in-flight rows must not affect this series' budget: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// TestSearchEpisodeRespectsBudgetOnDirectEntry isolates searchEpisode's own
// "!bud.allows()" guard. A direct SearchEpisode call never goes through
// searchSeason's per-episode loop (whose own "!bud.allows() { break }" guard
// would otherwise mask this guard's absence), so this is the only test that
// can catch that specific guard being deleted.
func TestSearchEpisodeRespectsBudgetOnDirectEntry(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 2)
	// Saturate the series' default budget of 1 with an in-flight row for ep1.
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		ClientItemID: "budget-holder", MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[0]}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E02.1080p.BluRay.x264-GRP", DownloadURL: "e2", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchEpisode(ctx, epIDs[1])
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("series already at its budget must refuse a direct episode search: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// TestSearchSeasonPackRespectsBudgetWhenAlreadyExhausted isolates
// searchSeason's early "!bud.allows()" return, which guards the season-pack
// branch specifically. A direct SearchSeason call never goes through
// searchSeries' outer per-season loop (whose own defense-in-depth guard would
// otherwise mask this guard's absence), and the fixture here only offers a
// season pack — no singles — so the per-episode loop's guard never even comes
// into play. This is the only test that can catch this guard being deleted.
func TestSearchSeasonPackRespectsBudgetWhenAlreadyExhausted(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 2) // season 1: 2 fully-missing episodes
	if err := st.UpsertSeason(ctx, store.Season{SeriesID: sid, SeasonNumber: 2, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 2, EpisodeNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	eps, err := st.ListEpisodes(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	var s2ep int64
	for _, e := range eps {
		if e.SeasonNumber == 2 {
			s2ep = e.ID
		}
	}
	if s2ep == 0 {
		t.Fatal("season 2 episode not found")
	}
	// Saturate the series' default budget of 1 with an in-flight row for the
	// season-2 episode, leaving season 1 fully missing but budget-exhausted.
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		ClientItemID: "budget-holder", MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{s2ep}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	// Only a season-1 pack is offered — no per-episode singles — so a grab
	// here can only come from the pack branch, which only searchSeason's
	// early guard protects.
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(ctx, sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("series already at its budget must refuse a fully-missing season's pack: n=%d reqs=%d", n, len(fe.reqs))
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("budget-exhausted season must not even search: %+v", fs.lastQuery)
	}
}

func TestSearchSeasonTriesNextPackWhenFirstIsBlocklisted(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 3)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack1", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01.1080p.WEB-DL.x264-GRP", DownloadURL: "pack2", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "ep1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	// The first pack has already failed and been blocklisted, exactly as
	// handleFailed would leave things before calling ResearchSeries.
	if _, err := st.AddBlocklist(ctx, store.Blocklist{
		MediaKind: "tv", SeriesID: &sid,
		SourceTitle: "The.Show.S01.1080p.BluRay.x264-GRP",
		Protocol:    "usenet", Reason: "unpack failed",
	}); err != nil {
		t.Fatal(err)
	}

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("want exactly 1 grab, got n=%d reqs=%d", n, len(fe.reqs))
	}
	if fe.reqs[0].DownloadURL != "pack2" {
		t.Fatalf("must try the next PACK before per-episode, got %q", fe.reqs[0].DownloadURL)
	}
	if len(fe.reqs[0].EpisodeIDs) != 3 {
		t.Fatalf("a pack grab covers every missing episode, got %v", fe.reqs[0].EpisodeIDs)
	}
}

func TestSearchSeasonFallsBackToEpisodesWhenPacksExhausted(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 3)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack1", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "ep1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	if _, err := st.AddBlocklist(ctx, store.Blocklist{
		MediaKind: "tv", SeriesID: &sid,
		SourceTitle: "The.Show.S01.1080p.BluRay.x264-GRP",
		Protocol:    "usenet", Reason: "unpack failed",
	}); err != nil {
		t.Fatal(err)
	}

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("want exactly 1 grab, got n=%d reqs=%d", n, len(fe.reqs))
	}
	if fe.reqs[0].DownloadURL != "ep1" {
		t.Fatalf("with no packs left it must fall back per-episode, got %q", fe.reqs[0].DownloadURL)
	}
}

func TestFakeSearcherSatisfiesDetailedSearcher(t *testing.T) {
	f := &fakeSearcher{
		releases:      []provider.Release{{Title: "Some.Movie.2019.1080p.WEB-DL", IndexerID: "1"}},
		indexerErrors: []IndexerError{{IndexerID: "3", Message: "timeout"}},
	}
	var s Searcher = f

	rel, errs := s.SearchDetailed(context.Background(), provider.Query{Term: "some movie"})
	if len(rel) != 1 || rel[0].IndexerID != "1" {
		t.Fatalf("releases = %+v, want the one succeeding indexer's release", rel)
	}
	if len(errs) != 1 || errs[0].IndexerID != "3" || errs[0].Message != "timeout" {
		t.Fatalf("indexerErrors = %+v, want the failing indexer named with its message", errs)
	}
}

func TestActiveQueueCountsRowsPerSeries(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 3)

	// Two single-episode in-flight rows for this series, one of each active status.
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		ClientItemID: "c1", MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[0]}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		ClientItemID: "c2", MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[1]}, Status: store.QueueImporting,
	}); err != nil {
		t.Fatal(err)
	}
	// A season-pack row covering all 3 episodes in one grab: it must still count
	// as a single in-flight row, not one per episode id.
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		ClientItemID: "c4", MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[0], epIDs[1], epIDs[2]}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	// A finished row (imported) for the same series must not count as in-flight.
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		ClientItemID: "c5", MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[2]}, Status: store.QueueImported,
	}); err != nil {
		t.Fatal(err)
	}
	// A movie row must not be counted against any series.
	mid := seedMovie(t, st, true, true)
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		ClientItemID: "c3", MediaKind: "movie", MovieID: &mid, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(st, &fakeSearcher{}, &fakeEnqueuer{}, nil)
	_, _, inFlight, err := svc.activeQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// c1 + c2 + c4 = 3 rows in flight; c5 (imported) must not add to the count.
	if got := inFlight[sid]; got != 3 {
		t.Fatalf("want 3 in flight for series %d, got %d (map=%v)", sid, got, inFlight)
	}
	if len(inFlight) != 1 {
		t.Fatalf("only the series should appear, got %v", inFlight)
	}
}

// unmonitorSeasonRow clears the season's own monitored flag WITHOUT cascading to
// its episodes, which is the state the UI leaves behind: SetSeasonMonitored(false)
// cascades every episode off, and re-monitoring individual episodes
// (media.Service.SetEpisodeMonitored) never touches the season row again.
func unmonitorSeasonRow(t *testing.T, st *store.Store, seriesID int64, seasonNumber int) {
	t.Helper()
	ctx := context.Background()
	seasons, err := st.ListSeasons(ctx, seriesID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sn := range seasons {
		if sn.SeasonNumber == seasonNumber {
			if err := st.SetSeasonMonitored(ctx, sn.ID, false); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("season %d not found for series %d", seasonNumber, seriesID)
}

// A season row can be unmonitored while individual episodes inside it are
// monitored — that is exactly what "unmonitor the season, then tick 3 episodes"
// produces. The missing sweep must still search those episodes; skipping the
// whole season is what made a partially-monitored show grab nothing at all.
func TestSearchSeriesSearchesMonitoredEpisodesInUnmonitoredSeason(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 5)
	// Keep only episodes 1-3 monitored, then clear the season's own flag.
	for _, id := range epIDs[3:] {
		if err := st.SetEpisodeMonitored(ctx, id, false); err != nil {
			t.Fatal(err)
		}
	}
	unmonitorSeasonRow(t, st, sid, 1)

	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("monitored episodes in an unmonitored season must still be searched: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// A season pack covers the WHOLE season, so it may only be grabbed when the
// whole season is wanted. With 3 of 5 episodes monitored, grabbing the pack
// would download every episode of the season to satisfy three.
func TestSearchSeasonSkipsPackWhenSeasonOnlyPartiallyMonitored(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 5)
	for _, id := range epIDs[3:] {
		if err := st.SetEpisodeMonitored(ctx, id, false); err != nil {
			t.Fatal(err)
		}
	}

	fs := &fakeSearcher{releases: append(
		[]provider.Release{{
			Title: "The.Show.S01.1080p.BluRay.x264-GRP", DownloadURL: "pack", Protocol: provider.ProtocolUsenet,
		}},
		episodeReleases(5)...,
	)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(ctx, sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("want exactly 1 grab, got n=%d reqs=%d", n, len(fe.reqs))
	}
	if fe.reqs[0].DownloadURL == "pack" {
		t.Fatal("a partially-monitored season must not grab a full-season pack")
	}
	if len(fe.reqs[0].EpisodeIDs) != 1 {
		t.Fatalf("want a single-episode grab, got EpisodeIDs=%v", fe.reqs[0].EpisodeIDs)
	}
}

// seedPartiallyMonitoredShow reproduces the state the UI leaves behind when a
// user unmonitors a whole show and then ticks a few episodes back on:
// series flag off, season row off, but N episodes monitored. Both cascades run
// downward only, so nothing ever turns the parent flags back on.
func seedPartiallyMonitoredShow(t *testing.T, st *store.Store, total, keepMonitored int) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, total)
	for _, id := range epIDs[keepMonitored:] {
		if err := st.SetEpisodeMonitored(ctx, id, false); err != nil {
			t.Fatal(err)
		}
	}
	unmonitorSeasonRow(t, st, sid, 1)
	if err := st.SetSeriesMonitored(ctx, sid, false); err != nil {
		t.Fatal(err)
	}
	return sid, epIDs
}

func TestSearchSeriesSearchesMonitoredEpisodesOfUnmonitoredSeries(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedPartiallyMonitoredShow(t, st, 5, 3)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("monitored episodes must be searched even when the series flag is off: n=%d reqs=%d", n, len(fe.reqs))
	}
}

func TestMissingSweepReachesMonitoredEpisodesOfUnmonitoredSeries(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedPartiallyMonitoredShow(t, st, 5, 3)
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.MissingSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("the sweep must reach monitored episodes of an unmonitored series: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// THE MASTER SWITCH MUST STILL WORK. Decision A keeps "unmonitor the series =
// stop everything" working ONLY because unmonitoring cascades every episode off.
// If someone later loosens the per-episode filter, every unmonitored series in
// the library silently starts downloading again. This pins that.
func TestUnmonitoredSeriesWithNoMonitoredEpisodesGrabsNothing(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, 5)
	for _, id := range epIDs {
		if err := st.SetEpisodeMonitored(ctx, id, false); err != nil {
			t.Fatal(err)
		}
	}
	unmonitorSeasonRow(t, st, sid, 1)
	if err := st.SetSeriesMonitored(ctx, sid, false); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: episodeReleases(5)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	sweepN, err := svc.MissingSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || sweepN != 0 || len(fe.reqs) != 0 {
		t.Fatalf("a fully unmonitored series must grab nothing: search=%d sweep=%d reqs=%d", n, sweepN, len(fe.reqs))
	}
}
