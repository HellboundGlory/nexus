package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestAPI(t *testing.T, fp *fakeProvider) (http.Handler, *store.Store) {
	t.Helper()
	svc, st := newTestService(t, fp)
	a := NewAPI(st, svc)
	r := chi.NewRouter()
	a.Mount(r)
	return r, st
}

func TestAPILookupAndAddSeries(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, _ := newTestAPI(t, fp)

	// lookup
	req := httptest.NewRequest(http.MethodGet, "/media/lookup?term=show&kind=tv", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("lookup status = %d", w.Code)
	}

	// add series
	body := `{"tmdbId":100,"monitorOption":"all"}`
	req = httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add series status = %d body=%s", w.Code, w.Body.String())
	}

	// duplicate → 409
	req = httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate add status = %d want 409", w.Code)
	}
}

func TestAPIGetSeriesNotFound(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, _ := newTestAPI(t, fp)
	req := httptest.NewRequest(http.MethodGet, "/series/999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d want 404", w.Code)
	}
}

func TestAPIRootFolderLifecycle(t *testing.T) {
	fp := &fakeProvider{}
	r, _ := newTestAPI(t, fp)
	req := httptest.NewRequest(http.MethodPost, "/rootfolder", strings.NewReader(`{"path":"/data/tv"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create rootfolder status = %d", w.Code)
	}
	var rf store.RootFolder
	_ = json.Unmarshal(w.Body.Bytes(), &rf)
	if rf.Path != "/data/tv" {
		t.Fatalf("unexpected rootfolder: %+v", rf)
	}
}

func TestAPIAssignQualityProfile(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, st := newTestAPI(t, fp)

	// create a profile directly in the store
	prof, err := st.CreateQualityProfile(context.Background(), store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// add a series to assign to
	addReq := httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(`{"tmdbId":100,"monitorOption":"all"}`))
	aw := httptest.NewRecorder()
	r.ServeHTTP(aw, addReq)
	if aw.Code != http.StatusCreated {
		t.Fatalf("add series status=%d", aw.Code)
	}
	var se store.Series
	_ = json.Unmarshal(aw.Body.Bytes(), &se)

	// assign
	body := `{"qualityProfileId":` + strconv.FormatInt(prof.ID, 10) + `}`
	req := httptest.NewRequest(http.MethodPut, "/series/"+strconv.FormatInt(se.ID, 10)+"/qualityprofile", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assign status=%d body=%s", w.Code, w.Body.String())
	}

	// assign to a missing series → 404
	req = httptest.NewRequest(http.MethodPut, "/series/9999/qualityprofile", strings.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("assign to missing series status=%d want 404", w.Code)
	}
}

// Regression (4a backlog item a): toggling monitored on a missing id must 404,
// not silently return 200 and emit a phantom media.*.updated event.
func TestAPIMonitorMissingIs404(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, _ := newTestAPI(t, fp)

	for _, path := range []string{"/series/9999/monitor", "/movies/9999/monitor"} {
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"monitored":false}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("PUT %s on missing id status=%d want 404", path, w.Code)
		}
	}
}

// Guard: the TMDb key must never surface; series/movie JSON has no such field.
// This test documents that the store structs carry no api key at all.
func TestAPINoCredentialLeak(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, _ := newTestAPI(t, fp)
	req := httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(`{"tmdbId":100,"monitorOption":"all"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if strings.Contains(strings.ToLower(w.Body.String()), "apikey") || strings.Contains(strings.ToLower(w.Body.String()), "api_key") {
		t.Fatalf("response leaked a credential field: %s", w.Body.String())
	}
}
