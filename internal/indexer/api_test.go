package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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

func TestIndexerAPINegativePaths(t *testing.T) {
	st := newTestStore(t)
	svc := NewService(st)
	a := NewAPI(st, svc, http.DefaultClient)
	router := mountedRouter(t, svc, a)

	// GET a non-existent indexer → 404.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/indexer/999", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d want 404 body=%s", rec.Code, rec.Body.String())
	}

	// GET with a non-numeric id → 400.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/indexer/abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("get bad id: %d want 400", rec.Code)
	}

	// GET the schema (static route must win over /{id}) → 200.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/indexer/schema", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("schema: %d want 200 body=%s", rec.Code, rec.Body.String())
	}
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

func TestIndexerUpdatePreservesStoredKeyWhenBlank(t *testing.T) {
	st := newTestStore(t)
	svc := NewService(st).WithHTTPClient(http.DefaultClient)
	a := NewAPI(st, svc, http.DefaultClient)
	router := mountedRouter(t, svc, a)

	// Create with a secret key (bad URL is fine: caps discovery is best-effort).
	create, _ := json.Marshal(map[string]any{
		"name": "ix", "implementation": "torznab", "baseUrl": "http://127.0.0.1:1", "apiKey": "SECRET-KEY-123", "enabled": true, "priority": 25,
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/indexer", bytes.NewReader(create)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Update WITHOUT apiKey (rename only).
	update, _ := json.Marshal(map[string]any{
		"name": "ix-renamed", "implementation": "torznab", "baseUrl": "http://127.0.0.1:1", "apiKey": "", "enabled": true, "priority": 30,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/indexer/"+strconv.FormatInt(created.ID, 10), bytes.NewReader(update)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", rec.Code, rec.Body.String())
	}

	// The stored key must survive (read in-process; it's json:"-").
	got, err := st.GetIndexer(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.APIKey != "SECRET-KEY-123" {
		t.Fatalf("stored key wiped: got %q want %q", got.APIKey, "SECRET-KEY-123")
	}
	if got.Name != "ix-renamed" {
		t.Fatalf("name not updated: %q", got.Name)
	}

	// Update WITH a new apiKey overwrites.
	update2, _ := json.Marshal(map[string]any{
		"name": "ix-renamed", "implementation": "torznab", "baseUrl": "http://127.0.0.1:1", "apiKey": "NEW-KEY-456", "enabled": true, "priority": 30,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/indexer/"+strconv.FormatInt(created.ID, 10), bytes.NewReader(update2)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update2: %d", rec.Code)
	}
	got, _ = st.GetIndexer(context.Background(), created.ID)
	if got.APIKey != "NEW-KEY-456" {
		t.Fatalf("new key not stored: got %q", got.APIKey)
	}
}
