package importing

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestAPI(t *testing.T) (http.Handler, *store.Store) {
	svc, st := newSvcWithQueue(t, &fakeQueue{})
	r := chi.NewRouter()
	NewAPI(svc).Mount(r)
	return r, st
}

func TestAPIQueueListAndHistory(t *testing.T) {
	r, _ := newTestAPI(t)
	for _, path := range []string{"/queue", "/history"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(w.Body.String()), "[") {
			t.Fatalf("GET %s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestAPIEnqueueRejectMaps400(t *testing.T) {
	r, st := newTestAPI(t)
	sid, epID := seedSeriesWithProfile(t, st)
	body := `{"downloadUrl":"http://x","title":"The.Show.S01E01.2160p.BluRay.x265-GRP","protocol":"usenet","mediaKind":"tv","seriesId":` +
		itoa(sid) + `,"episodeIds":[` + itoa(epID) + `]}`
	req := httptest.NewRequest(http.MethodPost, "/queue", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("reject status=%d want 400 body=%s", w.Code, w.Body.String())
	}
}

func TestAPINamingConfigRoundTrip(t *testing.T) {
	r, _ := newTestAPI(t)
	put := `{"seriesFolder":"{Series Title}","seasonFolder":"S{season:00}","episodeFile":"{Series Title} S{season:00}E{episode:00}","movieFolder":"{Movie Title}","movieFile":"{Movie Title} ({year})"}`
	req := httptest.NewRequest(http.MethodPut, "/config/naming", strings.NewReader(put))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put naming status=%d", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/config/naming", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "S{season:00}") {
		t.Fatalf("naming not persisted: %s", w.Body.String())
	}
}
