package automation

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
)

type fakeSearcher struct {
	lastQuery provider.Query
	releases  []provider.Release
	err       error
}

func (f *fakeSearcher) Search(_ context.Context, q provider.Query) ([]provider.Release, error) {
	f.lastQuery = q
	return f.releases, f.err
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

func seedMovie(t *testing.T, st *store.Store, monitored bool, withProfile bool) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := st.CreateMovie(ctx, store.Movie{TMDBID: 42, IMDbID: "tt42", Title: "The Film", Year: 2020, Monitored: monitored})
	if err != nil {
		t.Fatal(err)
	}
	if withProfile {
		prof, err := st.CreateQualityProfile(ctx, hdProfile())
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
	if fs.lastQuery.Type != provider.SearchMovie || fs.lastQuery.IMDbID != "tt42" || fs.lastQuery.TMDBID != 42 {
		t.Fatalf("bad query: %+v", fs.lastQuery)
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
