package downloadclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func newSABServer(t *testing.T) *httptest.Server {
	t.Helper()
	queue, _ := os.ReadFile("testdata/sab_queue.json")
	history, _ := os.ReadFile("testdata/sab_history.json")
	addfile, _ := os.ReadFile("testdata/sab_addfile.json")
	version, _ := os.ReadFile("testdata/sab_version.json")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "KEY" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("mode") {
		case "version":
			_, _ = w.Write(version)
		case "queue":
			// A delete action carries name=delete; return a simple ok.
			if r.URL.Query().Get("name") == "delete" {
				_, _ = w.Write([]byte(`{"status":true}`))
				return
			}
			_, _ = w.Write(queue)
		case "history":
			if r.URL.Query().Get("name") == "delete" {
				_, _ = w.Write([]byte(`{"status":true}`))
				return
			}
			_, _ = w.Write(history)
		case "addfile":
			_, _ = w.Write(addfile)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func TestSABnzbdItems(t *testing.T) {
	srv := newSABServer(t)
	defer srv.Close()
	c := newSABnzbd("1", srv.URL, "KEY", "tv", srv.Client())

	items, err := c.Items(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("want 4 items (2 queue + 2 history), got %d: %+v", len(items), items)
	}
	byID := map[string]provider.DownloadItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if got := byID["SABnzbd_nzo_aaa"]; got.Status != provider.StatusDownloading || got.Progress != 42 || got.Protocol != provider.ProtocolUsenet || got.DownloadClientID != "1" {
		t.Fatalf("downloading item wrong: %+v", got)
	}
	if got := byID["SABnzbd_nzo_bbb"]; got.Status != provider.StatusQueued {
		t.Fatalf("queued item wrong: %+v", got)
	}
	if got := byID["SABnzbd_nzo_ccc"]; got.Status != provider.StatusCompleted || got.Size != 2147483648 || got.OutputPath != "/downloads/complete/Old.Movie.2020.1080p" {
		t.Fatalf("completed item wrong: %+v", got)
	}
	if got := byID["SABnzbd_nzo_ddd"]; got.Status != provider.StatusFailed || got.ErrorMessage != "Unpacking failed" {
		t.Fatalf("failed item wrong: %+v", got)
	}
}

func TestSABnzbdAddAndTestAndRemove(t *testing.T) {
	srv := newSABServer(t)
	defer srv.Close()
	c := newSABnzbd("1", srv.URL, "KEY", "tv", srv.Client())
	ctx := context.Background()

	if err := c.Test(ctx); err != nil {
		t.Fatalf("test: %v", err)
	}
	id, err := c.Add(ctx, provider.DownloadRequest{
		Title: "New.Movie", Protocol: provider.ProtocolUsenet, Content: []byte("NZBDATA"),
	})
	if err != nil || id != "SABnzbd_nzo_new" {
		t.Fatalf("add: id=%q err=%v", id, err)
	}
	if err := c.Remove(ctx, "SABnzbd_nzo_aaa", true); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestSABnzbdAuthFailure(t *testing.T) {
	srv := newSABServer(t)
	defer srv.Close()
	c := newSABnzbd("1", srv.URL, "WRONG", "tv", srv.Client())
	if err := c.Test(context.Background()); err == nil {
		t.Fatal("expected auth failure")
	}
}
