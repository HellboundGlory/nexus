package store

import (
	"context"
	"errors"
	"testing"

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
