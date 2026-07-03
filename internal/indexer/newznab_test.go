package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func newTorznabFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	body, _ := os.ReadFile("testdata/torznab_search.xml")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "KEY" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(body)
	}))
}

func TestNewznabClientSearch(t *testing.T) {
	srv := newTorznabFixtureServer(t)
	defer srv.Close()

	caps := Capabilities{Search: true}
	c := newClient("7", "fixture", srv.URL, "KEY", provider.ProtocolTorrent, 25, caps,
		srv.Client(), newLimiter(0))

	rels, err := c.Search(context.Background(), provider.Query{Term: "the show"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].IndexerID != "7" || rels[0].Seeders == nil {
		t.Fatalf("unexpected results: %+v", rels)
	}
}

func TestNewznabClientAuthFailure(t *testing.T) {
	srv := newTorznabFixtureServer(t)
	defer srv.Close()

	c := newClient("7", "fixture", srv.URL, "WRONG", provider.ProtocolTorrent, 25,
		Capabilities{Search: true}, srv.Client(), newLimiter(0))
	_, err := c.Search(context.Background(), provider.Query{Term: "x"})
	if err != ErrAuthFailed {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

func TestNewznabClientSupports(t *testing.T) {
	c := newClient("1", "n", "http://x", "", provider.ProtocolUsenet, 25,
		Capabilities{Search: true, TVSearch: false}, http.DefaultClient, newLimiter(0))
	if !c.Supports(provider.Query{Type: provider.SearchGeneric}) {
		t.Error("generic should be supported")
	}
	if c.Supports(provider.Query{Type: provider.SearchTV}) {
		t.Error("tv should NOT be supported")
	}
	_ = time.Second
}
