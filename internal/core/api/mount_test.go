package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/auth"
)

func TestMountedRouteRequiresAuthAndWorks(t *testing.T) {
	authSvc := auth.NewService(nil, "secret-key")
	mount := func(r chi.Router) {
		r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			WriteJSON(w, http.StatusOK, map[string]string{"pong": "ok"})
		})
	}
	router := NewRouter(Deps{Auth: authSvc}, http.NotFoundHandler(), mount)

	// Without API key: 401.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-auth: got %d want 401", rec.Code)
	}

	// With API key: 200.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set(auth.APIKeyHeader, "secret-key")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth: got %d want 200 body=%s", rec.Code, rec.Body.String())
	}
}
