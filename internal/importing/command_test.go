package importing

import (
	"context"
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

	// now completed -> imported
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl}}
	if err := (NewImportCommand(svc)).Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.GetQueueItem(ctx, q.ID); r.Status != store.QueueImported {
		t.Fatalf("expected imported, got %q", r.Status)
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
	if r, _ := st.GetQueueItem(ctx, q.ID); r.Status != store.QueueImported {
		t.Fatalf("expected imported for no-override row, got %q", r.Status)
	}
}
