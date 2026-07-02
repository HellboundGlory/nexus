package indexer

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func mountedRouter(t *testing.T, svc *Service, a *API) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { a.Mount(r) })
	return r
}

func TestIndexerAPICreateAndSearch(t *testing.T) {
	body, _ := os.ReadFile("testdata/torznab_search.xml")
	caps, _ := os.ReadFile("testdata/caps.xml")
	idx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = w.Write(caps)
			return
		}
		_, _ = w.Write(body)
	}))
	defer idx.Close()

	st := newTestStore(t)
	svc := NewService(st).WithHTTPClient(idx.Client())
	a := NewAPI(st, svc, idx.Client())
	router := mountedRouter(t, svc, a)

	// Create indexer with a secret API key.
	secretKey := "SECRET-KEY-123"
	payload, _ := json.Marshal(map[string]any{
		"name": "t", "implementation": "torznab", "baseUrl": idx.URL, "apiKey": secretKey, "enabled": true, "priority": 25,
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/indexer", bytes.NewReader(payload)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	// Assert API key is not leaked in create response.
	if strings.Contains(rec.Body.String(), secretKey) {
		t.Fatalf("API key leaked in create response: %s", rec.Body.String())
	}

	// List.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/indexer", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	// Assert API key is not leaked in list response.
	if strings.Contains(rec.Body.String(), secretKey) {
		t.Fatalf("API key leaked in list response: %s", rec.Body.String())
	}

	// Search.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/search?query=the+show", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d body=%s", rec.Code, rec.Body.String())
	}
	var res SearchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Releases) != 1 || res.Releases[0].Protocol != provider.ProtocolTorrent {
		t.Fatalf("unexpected search result: %+v", res)
	}
}
