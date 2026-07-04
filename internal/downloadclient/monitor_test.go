package downloadclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestMonitorEmitsChanges(t *testing.T) {
	sab := newSABServer(t)
	defer sab.Close()

	st := newTestStore(t)
	ctx := context.Background()
	sabHost, sabPort := hostPort(t, sab.URL)
	if _, err := st.CreateDownloadClient(ctx, store.DownloadClient{
		Name: "sab", Implementation: "sabnzbd", Protocol: "usenet",
		Host: sabHost, Port: sabPort, APIKey: "KEY", Enabled: true, Priority: 10,
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st).WithHTTPClient(sab.Client())
	if err := svc.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	bus := events.New()
	var emitted []DownloadStatusChanged
	bus.Subscribe("download.status", func(_ context.Context, e events.Event) {
		if sc, ok := e.(DownloadStatusChanged); ok {
			emitted = append(emitted, sc)
		}
	})

	mon := NewMonitor(svc, bus)
	if err := mon.Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}
	// First run: all 4 SAB items are new → 4 events.
	if len(emitted) != 4 {
		t.Fatalf("first run: want 4 events, got %d", len(emitted))
	}

	// Second run with identical queue: nothing changed → no new events.
	emitted = nil
	if err := mon.Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 0 {
		t.Fatalf("second run: want 0 events, got %d", len(emitted))
	}
}

// A second server with an empty queue lets us assert "removed" events fire.
func TestMonitorEmitsRemovals(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "queue":
			_, _ = w.Write([]byte(`{"queue":{"slots":[]}}`))
		case "history":
			_, _ = w.Write([]byte(`{"history":{"slots":[]}}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer empty.Close()

	c := newSABnzbd("9", empty.URL, "", "", empty.Client())
	bus := events.New()
	var removed int
	bus.Subscribe("download.status", func(_ context.Context, e events.Event) {
		if sc, ok := e.(DownloadStatusChanged); ok && sc.Removed {
			removed++
		}
	})
	mon := NewMonitor(newServiceWithClients(c), bus)
	// Prime the monitor with a synthetic prior item the now-empty queue no longer has.
	mon.last = map[string]lastItem{"9|old": {clientID: "9"}}
	if err := mon.Run(context.Background(), nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("want 1 removal event, got %d", removed)
	}
}
