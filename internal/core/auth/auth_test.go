package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newService(t *testing.T) (*Service, *store.Store) {
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
	return NewService(s, "secret-api-key"), s
}

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword(h, "hunter2")
	if err != nil || !ok {
		t.Fatalf("verify correct password: ok=%v err=%v", ok, err)
	}
	ok, _ = VerifyPassword(h, "wrong")
	if ok {
		t.Fatal("verify should fail for wrong password")
	}
}

func TestLoginAuthenticate(t *testing.T) {
	a, s := newService(t)
	ctx := context.Background()
	h, _ := HashPassword("pw")
	if _, err := s.CreateUser(ctx, "admin", h); err != nil {
		t.Fatal(err)
	}
	tok, err := a.Login(ctx, "admin", "pw")
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.Authenticate(ctx, tok)
	if err != nil || u.Username != "admin" {
		t.Fatalf("authenticate: %+v err=%v", u, err)
	}
	if _, err := a.Login(ctx, "admin", "bad"); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestMiddlewareAPIKey(t *testing.T) {
	a, _ := newService(t)
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// No credentials → 401.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no creds: want 401 got %d", rec.Code)
	}
	// Valid API key → 200.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set(APIKeyHeader, "secret-api-key")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid key: want 200 got %d", rec.Code)
	}
}
