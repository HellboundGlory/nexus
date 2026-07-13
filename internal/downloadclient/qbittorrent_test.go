package downloadclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func newQBitServer(t *testing.T) *httptest.Server {
	t.Helper()
	info, _ := os.ReadFile("testdata/qbit_info.json")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("username") != "admin" || r.PostForm.Get("password") != "pw" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Fails."))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session123"})
		_, _ = w.Write([]byte("Ok."))
	})
	requireSID := func(r *http.Request) bool {
		c, err := r.Cookie("SID")
		return err == nil && c.Value == "session123"
	}
	mux.HandleFunc("/api/v2/app/version", func(w http.ResponseWriter, r *http.Request) {
		if !requireSID(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("v4.6.0"))
	})
	mux.HandleFunc("/api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
		if !requireSID(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(info)
	})
	mux.HandleFunc("/api/v2/torrents/add", func(w http.ResponseWriter, r *http.Request) {
		if !requireSID(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("Ok."))
	})
	mux.HandleFunc("/api/v2/torrents/delete", func(w http.ResponseWriter, r *http.Request) {
		if !requireSID(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = r.ParseForm()
		if !strings.Contains(r.PostForm.Get("hashes"), "aaa111") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("Ok."))
	})
	return httptest.NewServer(mux)
}

func TestQBittorrentItems(t *testing.T) {
	srv := newQBitServer(t)
	defer srv.Close()
	c := newQBittorrent("2", srv.URL, "admin", "pw", "movies", srv.Client())

	items, err := c.Items(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	byID := map[string]provider.DownloadItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if got := byID["aaa111"]; got.Status != provider.StatusDownloading || got.Progress != 42 || got.Protocol != provider.ProtocolTorrent || got.DownloadClientID != "2" {
		t.Fatalf("downloading item wrong: %+v", got)
	}
	if got := byID["bbb222"]; got.Status != provider.StatusCompleted || got.Progress != 100 || got.OutputPath != "/downloads/Old.Show.S01.COMPLETE" {
		t.Fatalf("completed item wrong: %+v", got)
	}
	if got := byID["ccc333"]; got.Status != provider.StatusFailed {
		t.Fatalf("errored item wrong: %+v", got)
	}
}

func TestQBittorrentAddMagnetTestRemove(t *testing.T) {
	srv := newQBitServer(t)
	defer srv.Close()
	c := newQBittorrent("2", srv.URL, "admin", "pw", "movies", srv.Client())
	ctx := context.Background()

	if err := c.Test(ctx); err != nil {
		t.Fatalf("test: %v", err)
	}
	// Magnet grab: Content is nil, URL carries the magnet. qBit derives the id from
	// the btih hash in the magnet URL.
	// A real BitTorrent v1 infohash is SHA-1 = 40 hex chars; upper-case here to
	// exercise the (?i) regex + ToLower normalization in Add.
	id, err := c.Add(ctx, provider.DownloadRequest{
		URL:      "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=x",
		Protocol: provider.ProtocolTorrent,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if id != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("add id = %q want lowercased 40-hex btih", id)
	}
	if err := c.Remove(ctx, "aaa111", true); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestQBittorrentAuthFailure(t *testing.T) {
	srv := newQBitServer(t)
	defer srv.Close()
	c := newQBittorrent("2", srv.URL, "admin", "WRONG", "movies", srv.Client())
	err := c.Test(context.Background())
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

// A 403 on a body-less request means the session expired: send() must re-login
// once and retry, transparently succeeding. This is the most subtle mandated
// behavior in the qBittorrent client.
func TestQBittorrentReloginRetryOn403(t *testing.T) {
	info, _ := os.ReadFile("testdata/qbit_info.json")
	var logins, infoCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		logins++
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: fmt.Sprintf("sid-%d", logins)})
		_, _ = w.Write([]byte("Ok."))
	})
	mux.HandleFunc("/api/v2/torrents/info", func(w http.ResponseWriter, _ *http.Request) {
		infoCalls++
		if infoCalls == 1 {
			// First hit: pretend the initial session expired.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(info)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newQBittorrent("2", srv.URL, "admin", "pw", "movies", srv.Client())

	items, err := c.Items(context.Background())
	if err != nil {
		t.Fatalf("Items after relogin: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items after relogin, got %d", len(items))
	}
	if logins != 2 {
		t.Fatalf("want 2 logins (initial + relogin), got %d", logins)
	}
	if infoCalls != 2 {
		t.Fatalf("want 2 info calls (403 then retry), got %d", infoCalls)
	}
}
