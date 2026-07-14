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
	// queue is transient: the row is deleted on successful import, not marked "imported".
	if _, err := st.GetQueueItem(ctx, q.ID); err != store.ErrNotFound {
		t.Fatalf("expected queue row deleted, got %v", err)
	}
	hist, _ := st.ListHistory(ctx, 10)
	if len(hist) == 0 || hist[0].EventType != "imported" {
		t.Fatalf("expected imported history, got %+v", hist)
	}
}

func TestImportUpgradeReplacesLowerQuality(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rfID, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 6, Allowed: true}, {QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show", RootFolderID: &rfID, QualityProfileID: &prof.ID})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"})
	eps, _ := st.ListEpisodes(ctx, sid)
	epID := eps[0].ID

	oldPath := filepath.Join(root, "The Show", "Season 01", "old.mkv")
	writeFile(t, oldPath, 60*1024*1024)
	_, _ = st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "tv", EpisodeID: &epID, RelativePath: "The Show/Season 01/old.mkv", Size: 1, QualityID: 6})

	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.1080p.BluRay.x264-GRP.mkv"), 70*1024*1024)
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01E01.1080p.BluRay.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{epID}, QualityID: 9, Status: store.QueueGrabbed,
	})
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl}}

	if err := svc.ImportItem(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	mf, _ := st.MediaFileForEpisode(ctx, epID)
	if mf == nil || mf.QualityID != 9 {
		t.Fatalf("expected upgraded to 9, got %+v", mf)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file should be deleted, stat err = %v", err)
	}
	hist, _ := st.ListHistory(ctx, 10)
	if hist[0].EventType != "upgraded" {
		t.Fatalf("expected upgraded history, got %q", hist[0].EventType)
	}
}

func TestImportRejectsNonUpgrade(t *testing.T) {
	ctx := context.Background()
	fq := &fakeQueue{}
	svc, st := newSvcWithQueue(t, fq)
	root := t.TempDir()
	rfID, _ := st.CreateRootFolder(ctx, root)
	prof, _ := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 6, Allowed: true}, {QualityID: 9, Allowed: true}},
	})
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "The Show", RootFolderID: &rfID, QualityProfileID: &prof.ID})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"})
	eps, _ := st.ListEpisodes(ctx, sid)
	epID := eps[0].ID
	_, _ = st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "tv", EpisodeID: &epID, RelativePath: "keep.mkv", Size: 1, QualityID: 9})

	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.720p.WEB-DL.x264-GRP.mkv"), 60*1024*1024)
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01E01.720p.WEB-DL.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{epID}, QualityID: 6, Status: store.QueueGrabbed,
	})
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl}}

	_ = svc.ImportItem(ctx, q.ID)
	mf, _ := st.MediaFileForEpisode(ctx, epID)
	if mf.QualityID != 9 || mf.RelativePath != "keep.mkv" {
		t.Fatalf("existing file should be kept, got %+v", mf)
	}
	updated, _ := st.GetQueueItem(ctx, q.ID)
	if updated.Status != store.QueueFailed {
		t.Fatalf("status = %q want failed (nothing new placed)", updated.Status)
	}
}

func TestImportSeasonPack(t *testing.T) {
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
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "One"})
	_ = st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 2, Title: "Two"})
	eps, _ := st.ListEpisodes(ctx, sid)
	var ep1, ep2 int64
	for _, e := range eps {
		if e.EpisodeNumber == 1 {
			ep1 = e.ID
		} else {
			ep2 = e.ID
		}
	}
	dl := t.TempDir()
	writeFile(t, filepath.Join(dl, "The.Show.S01E01.1080p.BluRay.x264-GRP.mkv"), 60*1024*1024)
	writeFile(t, filepath.Join(dl, "The.Show.S01E02.1080p.BluRay.x264-GRP.mkv"), 60*1024*1024)
	q, _ := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "The.Show.S01.1080p.BluRay.x264-GRP", MediaKind: "tv",
		SeriesID: &sid, EpisodeIDs: []int64{ep1, ep2}, QualityID: 9, Status: store.QueueGrabbed,
	})
	fq.items = []provider.DownloadItem{{ID: "h1", DownloadClientID: "c1", Status: provider.StatusCompleted, OutputPath: dl}}

	if err := svc.ImportItem(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	if mf, _ := st.MediaFileForEpisode(ctx, ep1); mf == nil {
		t.Fatal("episode 1 not imported")
	}
	if mf, _ := st.MediaFileForEpisode(ctx, ep2); mf == nil {
		t.Fatal("episode 2 not imported")
	}
	// queue is transient: the row is deleted on successful import, not marked "imported".
	if _, err := st.GetQueueItem(ctx, q.ID); err != store.ErrNotFound {
		t.Fatalf("expected queue row deleted, got %v", err)
	}
}
