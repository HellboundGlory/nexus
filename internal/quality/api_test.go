package quality

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestAPI(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	r := chi.NewRouter()
	NewAPI(NewService(st)).Mount(r)
	return r, st
}

func TestAPIDefinitions(t *testing.T) {
	r, _ := newTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/quality/definitions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Bluray-1080p") {
		t.Fatalf("definitions status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIProfileCRUDAndValidation(t *testing.T) {
	r, _ := newTestAPI(t)

	// invalid (empty items) → 400
	bad := `{"name":"X","cutoffQualityId":9,"upgradeAllowed":true,"items":[]}`
	req := httptest.NewRequest(http.MethodPost, "/qualityprofile", strings.NewReader(bad))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d want 400", w.Code)
	}

	// valid → 201
	good := `{"name":"HD","cutoffQualityId":9,"upgradeAllowed":true,"items":[{"qualityId":7,"allowed":true},{"qualityId":9,"allowed":true}]}`
	req = httptest.NewRequest(http.MethodPost, "/qualityprofile", strings.NewReader(good))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("valid create status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIParsePreview(t *testing.T) {
	r, _ := newTestAPI(t)
	body := `{"title":"Show.S01E01.1080p.BluRay.x264-GRP","kind":"tv"}`
	req := httptest.NewRequest(http.MethodPost, "/parse", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("parse status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Bluray-1080p") {
		t.Fatalf("parse body missing resolved quality: %s", w.Body.String())
	}
}
