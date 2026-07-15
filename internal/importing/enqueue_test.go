package importing

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type fakeGrabber struct {
	lastReq  provider.DownloadRequest
	returnID string
	err      error
}

func (f *fakeGrabber) Grab(_ context.Context, req provider.DownloadRequest, _ string) (string, error) {
	f.lastReq = req
	return f.returnID, f.err
}

type fakeQueue struct {
	items   []provider.DownloadItem
	removed map[string]bool
}

func (f *fakeQueue) Queue(context.Context) []provider.DownloadItem { return f.items }
func (f *fakeQueue) Remove(_ context.Context, _, itemID string, _ bool) error {
	if f.removed == nil {
		f.removed = map[string]bool{}
	}
	f.removed[itemID] = true
	return nil
}

func newSvc(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	return newSvcWithQueue(t, &fakeQueue{})
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return store.New(db)
}

func newSvcWithQueue(t *testing.T, q QueueReader) (*Service, *store.Store) {
	t.Helper()
	st := newTestStore(t)
	return NewService(st, &fakeGrabber{returnID: "h1"}, q, nil), st
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// seed a series with a quality profile that allows Bluray-1080p(9).
func seedSeriesWithProfile(t *testing.T, st *store.Store) (seriesID int64, epID int64) {
	t.Helper()
	ctx := context.Background()
	prof, err := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 7, Allowed: true}, {QualityID: 9, Allowed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesQualityProfileID(ctx, sid, &prof.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"}); err != nil {
		t.Fatal(err)
	}
	eps, _ := st.ListEpisodes(ctx, sid)
	return sid, eps[0].ID
}

func TestEnqueueAcceptsAndTracks(t *testing.T) {
	svc, st := newSvc(t)
	ctx := context.Background()
	sid, epID := seedSeriesWithProfile(t, st)

	q, err := svc.Enqueue(ctx, EnqueueRequest{
		DownloadURL: "http://x/nzb", Title: "The.Show.S01E01.1080p.BluRay.x264-GRP",
		Protocol: provider.ProtocolUsenet, MediaKind: provider.KindTV,
		SeriesID: sid, EpisodeIDs: []int64{epID},
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if q.Status != store.QueueGrabbed || q.ClientItemID != "h1" || q.QualityID != 9 {
		t.Fatalf("bad queue row: %+v", q)
	}
	hist, _ := st.ListHistory(ctx, 10)
	if len(hist) != 1 || hist[0].EventType != "grabbed" {
		t.Fatalf("expected grabbed history, got %+v", hist)
	}
}

func TestEnqueueRejectsDisallowedQuality(t *testing.T) {
	svc, st := newSvc(t)
	ctx := context.Background()
	sid, epID := seedSeriesWithProfile(t, st)
	_, err := svc.Enqueue(ctx, EnqueueRequest{
		DownloadURL: "http://x/nzb", Title: "The.Show.S01E01.2160p.BluRay.x265-GRP",
		Protocol: provider.ProtocolUsenet, MediaKind: provider.KindTV,
		SeriesID: sid, EpisodeIDs: []int64{epID},
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("expected ErrRejected, got %v", err)
	}
	if all, _ := st.ListQueue(ctx); len(all) != 0 {
		t.Fatalf("no row should be written on reject, got %d", len(all))
	}
}
