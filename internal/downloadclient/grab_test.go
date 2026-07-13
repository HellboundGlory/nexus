package downloadclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchContentMagnetPassthrough(t *testing.T) {
	body, err := fetchContent(context.Background(), http.DefaultClient, "magnet:?xt=urn:btih:abc")
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		t.Fatalf("magnet should yield nil content, got %d bytes", len(body))
	}
}

func TestFetchContentDownloadsBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("NZBDATA"))
	}))
	defer srv.Close()

	body, err := fetchContent(context.Background(), srv.Client(), srv.URL+"/get.nzb")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "NZBDATA" {
		t.Fatalf("body = %q", body)
	}
}

func TestFetchContentRejectsNonHTTPScheme(t *testing.T) {
	// A non-http(s) URL must be rejected before any request is issued. A file://
	// URL that would otherwise resolve to a readable path is a good probe.
	for _, raw := range []string{"file:///etc/passwd", "gopher://example.com/", "ftp://example.com/x"} {
		body, err := fetchContent(context.Background(), http.DefaultClient, raw)
		if !errors.Is(err, ErrReleaseUnavailable) {
			t.Fatalf("%s: want ErrReleaseUnavailable, got %v", raw, err)
		}
		if body != nil {
			t.Fatalf("%s: expected no body, got %d bytes", raw, len(body))
		}
	}
}

func TestFetchContentGoneIsReleaseUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchContent(context.Background(), srv.Client(), srv.URL+"/gone.nzb")
	if !errors.Is(err, ErrReleaseUnavailable) {
		t.Fatalf("want ErrReleaseUnavailable, got %v", err)
	}
}
