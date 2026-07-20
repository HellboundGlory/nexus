package automation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/store"
)

type fakeDispatcher struct {
	last command.Command
}

func (f *fakeDispatcher) Enqueue(c command.Command) (string, error) {
	f.last = c
	return "task-1", nil
}

func newTestAPI(t *testing.T) (http.Handler, *store.Store, *fakeDispatcher) {
	t.Helper()
	st := newStore(t)
	svc := NewService(st, &fakeSearcher{}, &fakeEnqueuer{}, nil)
	fd := &fakeDispatcher{}
	r := chi.NewRouter()
	NewAPI(svc, fd).Mount(r)
	return r, st, fd
}

func TestAPISearchMovieDispatches(t *testing.T) {
	r, st, fd := newTestAPI(t)
	id := seedMovie(t, st, true, true)
	req := httptest.NewRequest(http.MethodPost, "/automation/search/movie/"+itoa(id), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d (%s)", w.Code, w.Body.String())
	}
	if fd.last == nil || fd.last.Name() != "SearchMovie" {
		t.Fatalf("expected SearchMovie dispatched, got %v", fd.last)
	}
	var body map[string]string
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["taskId"] != "task-1" {
		t.Fatalf("want taskId in body, got %v", body)
	}
}

func TestAPISearchMovieUnknownIs404(t *testing.T) {
	r, _, fd := newTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/automation/search/movie/999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown movie, got %d", w.Code)
	}
	if fd.last != nil {
		t.Fatalf("nothing should be dispatched for a bad id")
	}
}

func TestAPIConfigRoundTrip(t *testing.T) {
	r, _, _ := newTestAPI(t)
	put := httptest.NewRequest(http.MethodPut, "/automation/config",
		strings.NewReader(`{"missingSearchIntervalHours":8,"missingSearchBatchSize":10}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, put)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT config want 200, got %d", w.Code)
	}
	get := httptest.NewRequest(http.MethodGet, "/automation/config", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, get)
	var c Config
	if err := json.NewDecoder(w2.Body).Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.MissingSearchIntervalHours != 8 || c.MissingSearchBatchSize != 10 {
		t.Fatalf("config not persisted: %+v", c)
	}
}

// TestAPIConfigPutOmittedMaxConcurrentPerSeriesIsUnlimited pins the exact
// mechanism a user relies on to turn off the per-series gate: the settings
// form (Task 7) omits maxConcurrentPerSeries from the PUT body entirely when
// the field is cleared to 0, rather than sending a literal 0. This only
// works because of three independent facts (see config.go and Config()):
// (1) putConfig decodes into a zero-value Config, so an omitted key decodes
// as 0; (2) the Config struct tag has no `omitempty`, so SetConfig persists
// the literal 0; (3) Config()'s read-side clamp loop deliberately excludes
// MaxConcurrentPerSeries. Exercise the full HTTP PUT -> GET chain so a
// regression in any one of those three facts fails this test.
func TestAPIConfigPutOmittedMaxConcurrentPerSeriesIsUnlimited(t *testing.T) {
	r, _, _ := newTestAPI(t)
	// Deliberately omit maxConcurrentPerSeries - this is exactly what the
	// frontend sends when the user types 0 into the "0 = unlimited" field.
	put := httptest.NewRequest(http.MethodPut, "/automation/config",
		strings.NewReader(`{"missingSearchIntervalHours":8,"missingSearchBatchSize":10}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, put)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT config want 200, got %d (%s)", w.Code, w.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/automation/config", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, get)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET config want 200, got %d (%s)", w2.Code, w2.Body.String())
	}
	var c Config
	if err := json.NewDecoder(w2.Body).Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.MaxConcurrentPerSeries != 0 {
		t.Fatalf("omitting maxConcurrentPerSeries on PUT must read back as 0 (unlimited), got %d", c.MaxConcurrentPerSeries)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
