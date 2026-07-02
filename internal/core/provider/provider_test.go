package provider

import (
	"context"
	"testing"
)

type fakeIndexer struct{ id string }

func (f fakeIndexer) ID() string                                     { return f.id }
func (fakeIndexer) Search(context.Context, Query) ([]Release, error) { return nil, nil }

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry[Indexer]()
	if err := reg.Register("a", fakeIndexer{id: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("a", fakeIndexer{id: "a"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
	got, ok := reg.Get("a")
	if !ok || got.ID() != "a" {
		t.Fatalf("get: ok=%v got=%v", ok, got)
	}
	if len(reg.All()) != 1 {
		t.Fatalf("All() len = %d", len(reg.All()))
	}
}
