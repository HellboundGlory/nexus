package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return store.New(db)
}

func TestServiceReloadAndSearch(t *testing.T) {
	body, _ := os.ReadFile("testdata/torznab_search.xml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateIndexer(ctx, store.Indexer{
		Name: "t", Implementation: "torznab", BaseURL: srv.URL, APIKey: "",
		Enabled: true, Priority: 25,
	}); err != nil {
		t.Fatal(err)
	}
	// A disabled indexer must be ignored by Reload.
	if _, err := st.CreateIndexer(ctx, store.Indexer{
		Name: "off", Implementation: "newznab", BaseURL: srv.URL, Enabled: false, Priority: 1,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(st).WithHTTPClient(srv.Client())
	if err := svc.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	res := svc.Search(ctx, provider.Query{Term: "the show"})
	if len(res.Releases) != 1 {
		t.Fatalf("want 1 release, got %d (errors=%+v)", len(res.Releases), res.IndexerErrors)
	}
	if res.Releases[0].Protocol != provider.ProtocolTorrent {
		t.Fatalf("protocol = %q", res.Releases[0].Protocol)
	}
}
