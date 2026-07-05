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
