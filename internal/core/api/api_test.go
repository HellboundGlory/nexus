package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newRouter(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("SPA"))
	})
	d := Deps{Auth: auth.NewService(s, "k"), Store: s, Version: "test"}
	return NewRouter(d, spa), s
}

func TestHealthIsPublic(t *testing.T) {
	r, _ := newRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health: want 200 got %d", rec.Code)
	}
}

func TestSystemStatusRequiresAuth(t *testing.T) {
	r, _ := newRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status without auth: want 401 got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	req.Header.Set(auth.APIKeyHeader, "k")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status with key: want 200 got %d", rec.Code)
	}
	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)
	if body["version"] != "test" {
		t.Fatalf("unexpected status body: %v", body)
	}
}

func TestLoginSetsCookie(t *testing.T) {
	r, s := newRouter(t)
	h, _ := auth.HashPassword("pw")
	s.CreateUser(context.Background(), "admin", h)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"pw"}`))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200 got %d (%s)", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.CookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected cookie named %q, got %v", auth.CookieName, cookies)
	}
	if !sessionCookie.HttpOnly {
		t.Fatalf("expected session cookie to be HttpOnly, got %+v", sessionCookie)
	}
}

func TestUnknownAPIRouteReturns404JSON(t *testing.T) {
	r, _ := newRouter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil)
	req.Header.Set(auth.APIKeyHeader, "k")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown api route: want 404 got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("expected JSON 404 envelope with \"not_found\", got %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SPA") {
		t.Fatalf("unknown api route should not fall back to SPA, got %q", rec.Body.String())
	}
}

func TestSPAFallback(t *testing.T) {
	r, _ := newRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/movies", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "SPA" {
		t.Fatalf("SPA fallback: got %d %q", rec.Code, rec.Body.String())
	}
}
