package quality

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

// seedProfile creates a valid quality profile via the API and returns its id.
func seedProfile(t *testing.T, r http.Handler) int64 {
	t.Helper()
	good := `{"name":"HD","cutoffQualityId":9,"upgradeAllowed":true,"items":[{"qualityId":7,"allowed":true},{"qualityId":9,"allowed":true}]}`
	req := httptest.NewRequest(http.MethodPost, "/qualityprofile", strings.NewReader(good))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed profile status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.ID == 0 {
		t.Fatalf("seed profile id: err=%v body=%s", err, w.Body.String())
	}
	return out.ID
}

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

func TestAPIParseWithProfileReturnsDecision(t *testing.T) {
	r, _ := newTestAPI(t)
	pid := seedProfile(t, r)
	body := `{"title":"Show.S01E01.1080p.BluRay.x264-GRP","kind":"tv","profileId":` + strconv.FormatInt(pid, 10) + `}`
	req := httptest.NewRequest(http.MethodPost, "/parse", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("parse status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "decision") {
		t.Fatalf("parse with profileId should include a decision: %s", w.Body.String())
	}
}

func TestAPIDeleteProfileInUseIs409(t *testing.T) {
	r, st := newTestAPI(t)
	ctx := context.Background()
	pid := seedProfile(t, r)

	// Reference the profile from a series so the delete is blocked.
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 42, Title: "Ref", SortTitle: "ref"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesQualityProfileID(ctx, sid, &pid); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/qualityprofile/"+strconv.FormatInt(pid, 10), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("delete in-use status=%d want 409 body=%s", w.Code, w.Body.String())
	}
}
