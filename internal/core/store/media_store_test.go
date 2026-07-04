package store

import (
	"context"
	"testing"
)

func TestRootFolderCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateRootFolder(ctx, "/data/tv")
	if err != nil {
		t.Fatal(err)
	}
	rf, err := s.GetRootFolder(ctx, id)
	if err != nil || rf.Path != "/data/tv" {
		t.Fatalf("get: %+v err=%v", rf, err)
	}
	all, err := s.ListRootFolders(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("list: %+v err=%v", all, err)
	}
	if err := s.DeleteRootFolder(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRootFolder(ctx, id); err != ErrNotFound {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}
