package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("index: got %d %q", rec.Code, rec.Body.String())
	}
}

func TestFallsBackToIndexForClientRoute(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/movies/123", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("fallback: got %d %q", rec.Code, rec.Body.String())
	}
}

// TestServesHashedAsset verifies a built JS asset is served with a JS content type.
func TestServesHashedAsset(t *testing.T) {
	sub, err := fs.Sub(distFS, "dist/assets")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	var asset string
	_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, _ error) error {
		if !d.IsDir() && strings.HasSuffix(p, ".js") {
			asset = "/assets/" + p
		}
		return nil
	})
	if asset == "" {
		t.Fatal("no built .js asset found under dist/assets")
	}
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset %s: got %d", asset, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("asset %s: content-type %q not javascript", asset, ct)
	}
}
