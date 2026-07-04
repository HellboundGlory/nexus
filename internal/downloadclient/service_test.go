package downloadclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

// hostPort splits an httptest URL like http://127.0.0.1:port into host and port.
func hostPort(t *testing.T, raw string) (string, int) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := strconv.Atoi(u.Port())
	return u.Hostname(), p
}

func TestServiceReloadGrabQueue(t *testing.T) {
	sab := newSABServer(t)
	defer sab.Close()
	// A separate indexer-like server that serves the .nzb bytes for the grab fetch.
	nzb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("NZBDATA"))
	}))
	defer nzb.Close()

	st := newTestStore(t)
	ctx := context.Background()
	sabHost, sabPort := hostPort(t, sab.URL)
	if _, err := st.CreateDownloadClient(ctx, store.DownloadClient{
		Name: "sab", Implementation: "sabnzbd", Protocol: "usenet",
		Host: sabHost, Port: sabPort, APIKey: "KEY", Category: "tv", Enabled: true, Priority: 10,
	}); err != nil {
		t.Fatal(err)
	}
	// Disabled client must be ignored by Reload.
	if _, err := st.CreateDownloadClient(ctx, store.DownloadClient{
		Name: "off", Implementation: "qbittorrent", Protocol: "torrent",
		Host: "127.0.0.1", Port: 1, Enabled: false, Priority: 1,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(st).WithHTTPClient(sab.Client())
	if err := svc.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	// Grab a usenet release: fetched server-side, uploaded to SAB.
	id, err := svc.Grab(ctx, provider.DownloadRequest{
		URL: nzb.URL + "/get.nzb", Title: "New.Movie", Protocol: provider.ProtocolUsenet,
	}, "")
	if err != nil || id != "SABnzbd_nzo_new" {
		t.Fatalf("grab: id=%q err=%v", id, err)
	}

	// No torrent client is enabled → routing error.
	if _, err := svc.Grab(ctx, provider.DownloadRequest{
		URL: "magnet:?xt=urn:btih:x", Protocol: provider.ProtocolTorrent,
	}, ""); err != ErrUnsupportedProtocol {
		t.Fatalf("expected ErrUnsupportedProtocol, got %v", err)
	}

	// Queue aggregates the SAB client's items.
	res := svc.Queue(ctx)
	if len(res.Items) != 4 || len(res.ClientErrors) != 0 {
		t.Fatalf("queue: items=%d errors=%+v", len(res.Items), res.ClientErrors)
	}

	if err := svc.Remove(ctx, "1", "SABnzbd_nzo_aaa", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
}
