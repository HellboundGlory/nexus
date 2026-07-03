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
