package importing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportSingleEpisode(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)

	root := t.TempDir()
	rfID, err := st.CreateRootFolder(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
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
	writeFile(t, filepath.Join(dl, "sample.mkv"), 5*1024*1024)
	writeFile(t, filepath.Join(dl, "readme.nfo"), 10)

	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01E01.1080p.BluRay.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{epID}, QualityID: 9, Status: store.QueueGrabbed,
	})
	fq.items = []provider.DownloadItem{{
		ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl,
	}}

	if err := svc.ImportItem(ctx, q.ID); err != nil {
		t.Fatalf("import: %v", err)
	}

	mf, _ := st.MediaFileForEpisode(ctx, epID)
	if mf == nil {
		t.Fatal("no media file recorded")
	}
	want := filepath.Join(root, "The Show", "Season 01", "The Show - S01E01 - Pilot [Bluray-1080p].mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected imported file at %s: %v", want, err)
	}
	updated, _ := st.GetQueueItem(ctx, q.ID)
	if updated.Status != store.QueueImported {
		t.Fatalf("status = %q want imported", updated.Status)
	}
	hist, _ := st.ListHistory(ctx, 10)
	if len(hist) == 0 || hist[0].EventType != "imported" {
		t.Fatalf("expected imported history, got %+v", hist)
	}
}
