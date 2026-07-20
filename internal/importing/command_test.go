package importing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestImportCompletedScansGrabbedRows(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rfID, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show", RootFolderID: &rfID, QualityProfileID: &prof.ID})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"})
	eps, _ := st.ListEpisodes(ctx, sid)
	epID := eps[0].ID

	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.1080p.BluRay.x264-GRP.mkv"), 60*1024*1024)
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01E01.1080p.BluRay.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{epID}, QualityID: 9, Status: store.QueueGrabbed,
	})

	// not completed yet -> nothing imported
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusDownloading, OutputPath: dl}}
	if err := svc.ImportCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.GetQueueItem(ctx, q.ID); r.Status != store.QueueGrabbed {
		t.Fatalf("should still be grabbed, got %q", r.Status)
	}

	// now completed -> imported, queue row deleted (queue is transient)
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl}}
	if err := (NewImportCommand(svc)).Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetQueueItem(ctx, q.ID); err != store.ErrNotFound {
		t.Fatalf("expected queue row deleted after import, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "The Show", "Season 01", "The Show - S01E01 - Pilot [Bluray-1080p].mkv")); err != nil {
		t.Fatalf("file not imported: %v", err)
	}
}

// Regression: a row enqueued WITHOUT an explicit client override (DownloadClientID
// == "") must still match its completed download by item id — the live item
// carries the real client id that Grab routed to.
func TestImportCompletedMatchesRowWithoutClientOverride(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rfID, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show", RootFolderID: &rfID, QualityProfileID: &prof.ID})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"})
	eps, _ := st.ListEpisodes(ctx, sid)
	epID := eps[0].ID

	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.1080p.BluRay.x264-GRP.mkv"), 60*1024*1024)
	// Row has NO client id (enqueued without override); item reports client "sab1".
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "", ClientItemID: "nzo9", Protocol: "usenet",
		SourceTitle: "The.Show.S01E01.1080p.BluRay.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{epID}, QualityID: 9, Status: store.QueueGrabbed,
	})
	fq.items = []provider.DownloadItem{{
		ID: "nzo9", DownloadClientID: "sab1", Status: provider.StatusCompleted, OutputPath: dl,
	}}

	if err := svc.ImportCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetQueueItem(ctx, q.ID); err != store.ErrNotFound {
		t.Fatalf("expected queue row deleted for no-override row, got %v", err)
	}
}

type fakeResearcher struct {
	movies, episodes, series []int64

	// st, when set, lets ResearchSeries observe the number of queue rows still
	// in QueueGrabbed status for the series AT CALL TIME. This is how tests pin
	// the load-bearing ordering fact: the hook must only ever fire after the
	// row that triggered it (success or failure) has already been deleted, so
	// the per-series budget slot is actually free when the re-search runs.
	st             *store.Store
	seriesInFlight []int
}

func (f *fakeResearcher) ResearchMovie(_ context.Context, id int64) error {
	f.movies = append(f.movies, id)
	return nil
}

func (f *fakeResearcher) ResearchEpisode(_ context.Context, id int64) error {
	f.episodes = append(f.episodes, id)
	return nil
}

func (f *fakeResearcher) ResearchSeries(ctx context.Context, id int64) error {
	f.series = append(f.series, id)
	if f.st != nil {
		// Mirror the real per-series budget's status filter exactly
		// (internal/automation/search.go's activeQueue): a row counts as
		// in-flight while QueueGrabbed OR QueueImporting. ImportItem sets a
		// row to QueueImporting and only clears it via DeleteQueueItem, so
		// counting QueueGrabbed alone would let this observation read 0 while
		// the row is still mid-import — vacuously satisfying the ordering
		// pin regardless of whether the row was actually deleted first.
		rows, _ := f.st.ListQueue(ctx)
		n := 0
		for _, r := range rows {
			if r.Status != store.QueueGrabbed && r.Status != store.QueueImporting {
				continue
			}
			if r.SeriesID != nil && *r.SeriesID == id {
				n++
			}
		}
		f.seriesInFlight = append(f.seriesInFlight, n)
	}
	return nil
}

// A failed TV row re-searches the SERIES, not each episode. For a season pack
// the per-episode variant fired one search per episode and dropped straight to
// per-episode grabbing, so the next-best pack was never tried.
//
// Deviation from the brief's literal fixture: the row is given an explicit
// ClientItemID ("nzo_1") matching the fake queue item's ID. Without it,
// matchItem's early `row.ClientItemID == ""` guard means the row never
// matches the live item at all, handleFailed never runs, and the test would
// pass vacuously (res.series and res.episodes both empty) regardless of the
// routing logic under test — see TestReconcileFailedDownloadBlocklistsAndRetries
// just below, which already sets ClientItemID for the same reason.
func TestFailedTVDownloadResearchesSeriesNotEpisodes(t *testing.T) {
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	ctx := context.Background()
	sid, epID := seedSeriesWithProfile(t, st)
	res := &fakeResearcher{st: st}
	svc.SetResearcher(res)

	q, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epID},
		ClientItemID: "nzo_1",
		SourceTitle:  "The.Show.S01.1080p.BluRay.x264-GRP", Protocol: "usenet",
		QualityID: 9, Status: store.QueueGrabbed,
	})
	if err != nil {
		t.Fatal(err)
	}
	fq.items = []provider.DownloadItem{{
		ID: "nzo_1", DownloadClientID: "sab", Status: provider.StatusFailed,
		ErrorMessage: "unpack failed", Title: q.SourceTitle,
	}}

	if err := svc.ImportCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if len(res.series) != 1 || res.series[0] != sid {
		t.Fatalf("want ResearchSeries(%d), got series=%v", sid, res.series)
	}
	if len(res.episodes) != 0 {
		t.Fatalf("must not fire per-episode research for a TV row, got %v", res.episodes)
	}
	// Pin the addendum-A ordering: the queue row must already be deleted (so
	// the per-series budget slot is free) by the time ResearchSeries fires.
	if len(res.seriesInFlight) != 1 || res.seriesInFlight[0] != 0 {
		t.Fatalf("ResearchSeries must observe 0 in-flight rows for the series, got %v", res.seriesInFlight)
	}
}

// Three rows for ONE series complete in the same tick; the success hook must
// fire once, not three times (firing per row would launch several concurrent
// searches racing for the same freed slot).
func TestImportCompletedResearchesSeriesOncePerTick(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rfID, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{
		TMDBID: 1, Title: "The Show", RootFolderID: &rfID, QualityProfileID: &prof.ID,
	})
	for i := 1; i <= 3; i++ {
		_ = st.UpsertEpisode(ctx, store.Episode{
			SeriesID: sid, SeasonNumber: 1, EpisodeNumber: i, Title: fmt.Sprintf("Ep%d", i),
		})
	}
	eps, _ := st.ListEpisodes(ctx, sid)

	res := &fakeResearcher{st: st}
	svc.SetResearcher(res)

	var items []provider.DownloadItem
	for i, e := range eps {
		dl := t.TempDir()
		title := fmt.Sprintf("The.Show.S01E%02d.1080p.BluRay.x264-GRP", i+1)
		writeFile(t, filepath.Join(dl, title+".mkv"), 60*1024*1024)
		epID := e.ID
		if _, err := st.EnqueueGrab(ctx, store.QueueItem{
			DownloadClientID: "c1", ClientItemID: fmt.Sprintf("h%d", i+1), Protocol: "usenet",
			SourceTitle: title, MediaKind: "tv", SeriesID: &sid,
			EpisodeIDs: []int64{epID}, QualityID: 9, Status: store.QueueGrabbed,
		}); err != nil {
			t.Fatal(err)
		}
		items = append(items, provider.DownloadItem{
			ID: fmt.Sprintf("h%d", i+1), DownloadClientID: "c1",
			Status: provider.StatusCompleted, OutputPath: dl,
		})
	}
	fq.items = items

	if err := svc.ImportCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if len(res.series) != 1 || res.series[0] != sid {
		t.Fatalf("3 imports for one series must research it ONCE, got %v", res.series)
	}
	// Pin the addendum-A ordering on the success path too: by the time the
	// dedup'd hook fires, all 3 rows must already be gone.
	if len(res.seriesInFlight) != 1 || res.seriesInFlight[0] != 0 {
		t.Fatalf("ResearchSeries must observe 0 in-flight rows for the series, got %v", res.seriesInFlight)
	}
}

func TestReconcileFailedDownloadBlocklistsAndRetries(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 100, Title: "Dune", Year: 2021})
	if err != nil {
		t.Fatal(err)
	}
	// a grabbed movie queue row
	row, err := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "sab1", ClientItemID: "nzo_1", Protocol: "usenet",
		SourceTitle: "Dune.2021.1080p-GRP", MediaKind: "movie", MovieID: &mid,
		QualityID: 3, Status: store.QueueGrabbed,
	})
	if err != nil {
		t.Fatal(err)
	}
	// live client reports it FAILED
	q := &fakeQueue{items: []provider.DownloadItem{{
		ID: "nzo_1", DownloadClientID: "sab1", Status: provider.StatusFailed, ErrorMessage: "missing articles",
	}}}
	res := &fakeResearcher{}
	svc := NewService(st, &fakeGrabber{}, q, nil)
	svc.SetResearcher(res)

	if err := svc.ImportCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	// queue row deleted
	if _, err := st.GetQueueItem(ctx, row.ID); err != store.ErrNotFound {
		t.Fatalf("queue row should be deleted, got %v", err)
	}
	// release blocklisted for the movie
	bl, _ := st.BlocklistedTitles(ctx, &mid, nil)
	if !bl[store.NormReleaseTitle("Dune.2021.1080p-GRP")] {
		t.Fatalf("release not blocklisted: %v", bl)
	}
	// download_failed history recorded
	hist, _ := st.ListHistory(ctx, 10)
	if len(hist) == 0 || hist[0].EventType != "download_failed" {
		t.Fatalf("expected download_failed history, got %+v", hist)
	}
	// re-search triggered for the movie
	if len(res.movies) != 1 || res.movies[0] != mid {
		t.Fatalf("expected ResearchMovie(%d), got %v", mid, res.movies)
	}
	if len(res.series) != 0 {
		t.Fatalf("a movie row must never route to ResearchSeries, got %v", res.series)
	}
	// dead client item removed
	if !q.removed["nzo_1"] {
		t.Fatalf("client item should be removed")
	}
}
