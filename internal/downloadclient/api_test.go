package downloadclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
)

func mountedRouter(t *testing.T, a *API) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { a.Mount(r) })
	return r
}

func TestDownloadClientAPICreateListGrabQueue(t *testing.T) {
	sab := newSABServer(t)
	defer sab.Close()
	nzb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("NZBDATA"))
	}))
	defer nzb.Close()

	st := newTestStore(t)
	svc := NewService(st).WithHTTPClient(sab.Client())
	a := NewAPI(st, svc)
	router := mountedRouter(t, a)

	sabHost, sabPort := hostPort(t, sab.URL)
	payload, _ := json.Marshal(map[string]any{
		"name": "sab", "implementation": "sabnzbd",
		"host": sabHost, "port": sabPort, "apiKey": "KEY", "category": "tv",
		"enabled": true, "priority": 10,
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/downloadclient", bytes.NewReader(payload)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}

	// List (credential must not leak).
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/downloadclient", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("KEY")) {
		t.Fatal("api key leaked in list response")
	}

	// Grab.
	grab, _ := json.Marshal(map[string]any{
		"downloadUrl": nzb.URL + "/get.nzb", "title": "New.Movie", "protocol": "usenet",
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/download", bytes.NewReader(grab)))
	if rec.Code != http.StatusOK {
		t.Fatalf("grab: %d body=%s", rec.Code, rec.Body.String())
	}
	var gres struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &gres)
	if gres.ID != "SABnzbd_nzo_new" {
		t.Fatalf("grab id = %q", gres.ID)
	}

	// Queue.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/queue", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("queue: %d", rec.Code)
	}
	var qres QueueResult
	if err := json.Unmarshal(rec.Body.Bytes(), &qres); err != nil {
		t.Fatal(err)
	}
	if len(qres.Items) != 4 {
		t.Fatalf("queue items = %d want 4", len(qres.Items))
	}

	// Remove.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/queue/1/SABnzbd_nzo_aaa", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDownloadClientUpdatePreservesStoredSecretWhenBlank(t *testing.T) {
	st := newTestStore(t)
	svc := NewService(st)
	a := NewAPI(st, svc)
	router := mountedRouter(t, a) // NB: downloadclient's helper is (t, a), unlike indexer's (t, svc, a)

	create, _ := json.Marshal(map[string]any{
		"name": "sab", "implementation": "sabnzbd", "host": "localhost", "port": 8080, "apiKey": "SECRET-PW-1", "enabled": true, "priority": 25,
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/downloadclient", bytes.NewReader(create)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	update, _ := json.Marshal(map[string]any{
		"name": "sab-renamed", "implementation": "sabnzbd", "host": "localhost", "port": 8080, "apiKey": "", "enabled": true, "priority": 30,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/downloadclient/"+strconv.FormatInt(created.ID, 10), bytes.NewReader(update)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := st.GetDownloadClient(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.APIKey != "SECRET-PW-1" {
		t.Fatalf("stored secret wiped: got %q", got.APIKey)
	}
	if got.Name != "sab-renamed" {
		t.Fatalf("name not updated: %q", got.Name)
	}

	update2, _ := json.Marshal(map[string]any{
		"name": "sab-renamed", "implementation": "sabnzbd", "host": "localhost", "port": 8080, "apiKey": "NEW-PW-2", "enabled": true, "priority": 30,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/downloadclient/"+strconv.FormatInt(created.ID, 10), bytes.NewReader(update2)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update2: %d", rec.Code)
	}
	got, _ = st.GetDownloadClient(context.Background(), created.ID)
	if got.APIKey != "NEW-PW-2" {
		t.Fatalf("new secret not stored: got %q", got.APIKey)
	}
}

func TestGrabUnsupportedProtocol(t *testing.T) {
	st := newTestStore(t)
	svc := NewService(st)
	_ = svc.Reload(context.Background())
	a := NewAPI(st, svc)
	router := mountedRouter(t, a)

	grab, _ := json.Marshal(map[string]any{
		"downloadUrl": "magnet:?xt=urn:btih:x", "title": "x", "protocol": "torrent",
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/download", bytes.NewReader(grab)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for no matching client, got %d", rec.Code)
	}
}
