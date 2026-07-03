package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestHealthCheckUpdatesStatusAndEmits(t *testing.T) {
	caps, _ := os.ReadFile("testdata/caps.xml")
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(caps)
	}))
	defer okSrv.Close()
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badSrv.Close()

	st := newTestStore(t)
	ctx := context.Background()
	goodID, _ := st.CreateIndexer(ctx, store.Indexer{Name: "good", Implementation: "newznab", BaseURL: okSrv.URL, Enabled: true, Priority: 25})
	badID, _ := st.CreateIndexer(ctx, store.Indexer{Name: "bad", Implementation: "newznab", BaseURL: badSrv.URL, Enabled: true, Priority: 25})

	bus := events.New()
	var emitted []IndexerStatusChanged
	bus.Subscribe("indexer.status", func(_ context.Context, e events.Event) {
		if sc, ok := e.(IndexerStatusChanged); ok {
			emitted = append(emitted, sc)
		}
	})

	hc := NewHealthCheck(st, bus, okSrv.Client())
	if err := hc.Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}

	good, _ := st.GetIndexer(ctx, goodID)
	if good.Status != "ok" {
		t.Errorf("good status = %q want ok", good.Status)
	}
	bad, _ := st.GetIndexer(ctx, badID)
	if bad.Status != "failed" || bad.FailMessage == "" {
		t.Errorf("bad status = %q msg = %q", bad.Status, bad.FailMessage)
	}
	if len(emitted) != 2 {
		t.Fatalf("want 2 events, got %d", len(emitted))
	}
}
