package store

import (
	"context"
	"testing"
)

func TestIndexerCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateIndexer(ctx, Indexer{
		Name: "nzbgeek", Implementation: "newznab",
		BaseURL: "https://api.nzbgeek.info", APIKey: "k",
		Enabled: true, Priority: 25, Categories: []int{5000, 5040},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetIndexer(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "nzbgeek" || got.Implementation != "newznab" || len(got.Categories) != 2 || got.Priority != 25 {
		t.Fatalf("unexpected indexer: %+v", got)
	}

	got.Enabled = false
	got.Name = "renamed"
	if err := s.UpdateIndexer(ctx, *got); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListIndexers(ctx, false)
	if err != nil || len(all) != 1 || all[0].Name != "renamed" {
		t.Fatalf("list all: %+v err=%v", all, err)
	}
	enabled, err := s.ListIndexers(ctx, true)
	if err != nil || len(enabled) != 0 {
		t.Fatalf("list enabled: %+v err=%v", enabled, err)
	}

	if err := s.SetIndexerStatus(ctx, id, "failed", "boom", `{"x":1}`); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetIndexer(ctx, id)
	if got.Status != "failed" || got.FailMessage != "boom" || got.Caps != `{"x":1}` {
		t.Fatalf("status not persisted: %+v", got)
	}

	if err := s.DeleteIndexer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetIndexer(ctx, id); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
