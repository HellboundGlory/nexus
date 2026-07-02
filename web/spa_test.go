package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Nexus is running") {
		t.Fatalf("index: got %d %q", rec.Code, rec.Body.String())
	}
}

func TestFallsBackToIndexForClientRoute(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/movies/123", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Nexus is running") {
		t.Fatalf("fallback: got %d %q", rec.Code, rec.Body.String())
	}
}
