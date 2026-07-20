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
	sid, _ := seedSeries(t, st, true, 2)
	// Searcher returns only per-episode singles regardless of query (no pack).
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
		{Title: "The.Show.S01E02.1080p.BluRay.x264-GRP", DownloadURL: "e2", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(context.Background(), sid, 1)
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
	sid, epIDs := seedSeries(t, st, true, 2)
	// ep1 is already actively queued; only ep2 is truly missing.
	e1 := epIDs[0]
	if _, err := st.EnqueueGrab(context.Background(), store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{e1}, Status: store.QueueGrabbed,
	}); err != nil {
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
		t.Fatalf("only the un-queued missing episode 2 should be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
	// Not fully missing (ep1 in flight) → per-episode search, not a pack.
	if fs.lastQuery.Episode == nil {
		t.Fatalf("should be a per-episode search, got %+v", fs.lastQuery)
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

	// Two in-flight rows for this series, one of each active status.
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
	if got := inFlight[sid]; got != 2 {
		t.Fatalf("want 2 in flight for series %d, got %d (map=%v)", sid, got, inFlight)
	}
	if len(inFlight) != 1 {
		t.Fatalf("only the series should appear, got %v", inFlight)
	}
}
