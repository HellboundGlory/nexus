package indexer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

type fakeClient struct {
	id       string
	priority int
	releases []provider.Release
	err      error
	supports bool
}

func (f *fakeClient) ID() string    { return f.id }
func (f *fakeClient) Priority() int { return f.priority }
func (f *fakeClient) Supports(provider.Query) bool {
	return f.supports
}
func (f *fakeClient) Search(context.Context, provider.Query) ([]provider.Release, error) {
	return f.releases, f.err
}

func rel(guid string, pub time.Time) provider.Release {
	return provider.Release{GUID: guid, Title: guid, PublishDate: pub, Protocol: provider.ProtocolUsenet}
}

func TestSearchAllAggregatesDedupesSorts(t *testing.T) {
	older := time.Unix(1000, 0)
	newer := time.Unix(2000, 0)

	a := &fakeClient{id: "1", priority: 10, supports: true, releases: []provider.Release{
		rel("dup", older), rel("a-old", older),
	}}
	b := &fakeClient{id: "2", priority: 20, supports: true, releases: []provider.Release{
		rel("dup", older), rel("b-new", newer),
	}}
	failing := &fakeClient{id: "3", priority: 5, supports: true, err: errors.New("down")}
	skipped := &fakeClient{id: "4", priority: 1, supports: false, releases: []provider.Release{rel("x", newer)}}

	res := searchAll(context.Background(), []searchable{a, b, failing, skipped}, provider.Query{}, time.Second)

	// dup collapsed → 3 unique releases (dup, a-old, b-new)
	if len(res.Releases) != 3 {
		t.Fatalf("want 3 releases, got %d: %+v", len(res.Releases), res.Releases)
	}
	// newest first
	if res.Releases[0].GUID != "b-new" {
		t.Errorf("first should be b-new, got %q", res.Releases[0].GUID)
	}
	// failing indexer surfaced; skipped indexer produced nothing and no error
	if len(res.IndexerErrors) != 1 || res.IndexerErrors[0].IndexerID != "3" {
		t.Fatalf("indexer errors = %+v", res.IndexerErrors)
	}
}
