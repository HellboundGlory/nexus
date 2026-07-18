package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/database"
)

func newImportTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func i64(v int64) *int64 { return &v }

func TestQueueCRUD(t *testing.T) {
	st := newImportTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S"}); err != nil {
		t.Fatal(err)
	}
	q := QueueItem{
		DownloadClientID: "c1", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "Show.S01E01.1080p.BluRay-GRP", MediaKind: "tv",
		SeriesID: i64(1), EpisodeIDs: []int64{10, 11}, QualityID: 9, Status: QueueGrabbed,
	}
	created, err := st.EnqueueGrab(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("bad created: %+v", created)
	}
	got, err := st.GetQueueItem(ctx, created.ID)
	if err != nil || len(got.EpisodeIDs) != 2 || got.EpisodeIDs[1] != 11 || got.QualityID != 9 {
		t.Fatalf("roundtrip mismatch: %+v err=%v", got, err)
	}
	if err := st.SetQueueStatus(ctx, created.ID, QueueFailed, "boom"); err != nil {
		t.Fatal(err)
	}
	failed, _ := st.QueueByStatus(ctx, QueueFailed)
	if len(failed) != 1 || failed[0].Error != "boom" {
		t.Fatalf("QueueByStatus = %+v", failed)
	}
	all, _ := st.ListQueue(ctx)
	if len(all) != 1 {
		t.Fatalf("ListQueue len = %d", len(all))
	}
	if err := st.DeleteQueueItem(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetQueueItem(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMediaFileUpsertAndLookup(t *testing.T) {
	st := newImportTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, Episode{SeriesID: 1, SeasonNumber: 1, EpisodeNumber: 1}); err != nil {
		t.Fatal(err)
	}
	eps, _ := st.ListEpisodes(ctx, 1)
	epID := eps[0].ID

	mf, err := st.UpsertMediaFile(ctx, MediaFile{MediaKind: "tv", EpisodeID: &epID, RelativePath: "S/Season 01/e1.mkv", Size: 100, QualityID: 7})
	if err != nil || mf.ID == 0 {
		t.Fatalf("upsert: %+v err=%v", mf, err)
	}
	got, err := st.MediaFileForEpisode(ctx, epID)
	if err != nil || got == nil || got.QualityID != 7 {
		t.Fatalf("lookup: %+v err=%v", got, err)
	}
	// upsert again for the same episode replaces (UNIQUE(episode_id))
	mf2, err := st.UpsertMediaFile(ctx, MediaFile{MediaKind: "tv", EpisodeID: &epID, RelativePath: "S/Season 01/e1.better.mkv", Size: 200, QualityID: 9})
	if err != nil {
		t.Fatal(err)
	}
	got2, _ := st.MediaFileForEpisode(ctx, epID)
	if got2.ID != mf2.ID || got2.QualityID != 9 {
		t.Fatalf("replace failed: %+v", got2)
	}
	var n int
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM media_files`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("media_files count = %d err=%v", n, err)
	}
	nf, err := st.MediaFileForMovie(ctx, 999)
	if err != nil || nf != nil {
		t.Fatalf("expected nil file, got %+v err=%v", nf, err)
	}
}

func TestHistoryAppendAndList(t *testing.T) {
	st := newImportTestStore(t)
	ctx := context.Background()
	if err := st.AddHistory(ctx, HistoryEvent{EventType: "grabbed", MediaKind: "tv", SourceTitle: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddHistory(ctx, HistoryEvent{EventType: "imported", MediaKind: "tv", SourceTitle: "B"}); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListHistory(ctx, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("history len = %d err=%v", len(list), err)
	}
	if list[0].SourceTitle != "B" { // newest first
		t.Fatalf("expected newest first, got %q", list[0].SourceTitle)
	}
}

func TestFilePresenceHelpers(t *testing.T) {
	st := newImportTestStore(t)
	ctx := context.Background()

	// Root folder + series + two monitored episodes; one gets a file.
	rfID, err := st.CreateRootFolder(ctx, "/data/tv")
	if err != nil {
		t.Fatal(err)
	}
	seriesID, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "Show", RootFolderID: &rfID, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, Episode{SeriesID: seriesID, SeasonNumber: 1, EpisodeNumber: 1, TMDBID: 11, Title: "E1", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, Episode{SeriesID: seriesID, SeasonNumber: 1, EpisodeNumber: 2, TMDBID: 12, Title: "E2", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	eps, err := st.ListEpisodes(ctx, seriesID)
	if err != nil || len(eps) != 2 {
		t.Fatalf("ListEpisodes: %+v err=%v", eps, err)
	}
	ep1ID := eps[0].ID // season 1 episode 1, ordered first

	// A movie with a file.
	movieID, err := st.CreateMovie(ctx, Movie{TMDBID: 2, Title: "Film", RootFolderID: &rfID, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, MediaFile{MediaKind: "episode", EpisodeID: &ep1ID, RelativePath: "e1.mkv", Size: 1, QualityID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, MediaFile{MediaKind: "movie", MovieID: &movieID, RelativePath: "film.mkv", Size: 1, QualityID: 1}); err != nil {
		t.Fatal(err)
	}

	epFiles, err := st.EpisodeFileIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !epFiles[ep1ID] || len(epFiles) != 1 {
		t.Fatalf("EpisodeFileIDs = %v, want only ep1", epFiles)
	}
	mvFiles, err := st.MovieFileIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !mvFiles[movieID] || len(mvFiles) != 1 {
		t.Fatalf("MovieFileIDs = %v, want only movie", mvFiles)
	}
	stats, err := st.SeriesEpisodeStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := stats[seriesID]
	if got.EpisodeCount != 2 || got.EpisodeFileCount != 1 {
		t.Fatalf("SeriesEpisodeStats[series] = %+v, want {2 1}", got)
	}
}

func TestGrabbedSince(t *testing.T) {
	st := newImportTestStore(t)
	ctx := context.Background()
	// movie_id/series_id left nil to avoid FK constraints; we only test the
	// event_type + time filtering here.
	if err := st.AddHistory(ctx, HistoryEvent{EventType: "grabbed", MediaKind: "movie", SourceTitle: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddHistory(ctx, HistoryEvent{EventType: "imported", MediaKind: "movie", SourceTitle: "B"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GrabbedSince(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SourceTitle != "A" || got[0].EventType != "grabbed" {
		t.Fatalf("want only the grabbed row A, got %+v", got)
	}
	future, err := st.GrabbedSince(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(future) != 0 {
		t.Fatalf("future since should return no rows, got %d", len(future))
	}
}

func TestMediaFilesForSeries(t *testing.T) {
	st := newImportTestStore(t) // existing helper at import_store_test.go:12 (Open+Migrate+New)
	ctx := context.Background()

	sid, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "Show"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSeason(ctx, Season{SeriesID: sid, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "E1"}); err != nil {
		t.Fatal(err)
	}
	eps, _ := st.ListEpisodes(ctx, sid)
	if len(eps) == 0 {
		t.Fatal("no episodes")
	}
	if _, err := st.UpsertMediaFile(ctx, MediaFile{
		MediaKind: "episode", EpisodeID: &eps[0].ID, RelativePath: "Show/Season 01/E01.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	// Second series with its own season/episode/file, to prove
	// MediaFilesForSeries scopes by series_id rather than returning all files.
	sid2, err := st.CreateSeries(ctx, Series{TMDBID: 2, Title: "Other Show"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSeason(ctx, Season{SeriesID: sid2, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, Episode{SeriesID: sid2, SeasonNumber: 1, EpisodeNumber: 1, Title: "E1"}); err != nil {
		t.Fatal(err)
	}
	eps2, _ := st.ListEpisodes(ctx, sid2)
	if len(eps2) == 0 {
		t.Fatal("no episodes for second series")
	}
	if _, err := st.UpsertMediaFile(ctx, MediaFile{
		MediaKind: "episode", EpisodeID: &eps2[0].ID, RelativePath: "Other Show/Season 01/E01.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	files, err := st.MediaFilesForSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].RelativePath != "Show/Season 01/E01.mkv" {
		t.Fatalf("want 1 series file, got %+v", files)
	}
}
