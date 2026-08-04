package tag

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestRouter(t *testing.T) (http.Handler, *store.Store) {
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
	r.Route("/api/v1", func(r chi.Router) { NewAPI(st).Mount(r) })
	return r, st
}

func do(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body == nil {
		rdr = bytes.NewReader(nil)
	} else {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTagAPICRUD(t *testing.T) {
	r, _ := newTestRouter(t)

	rec := do(t, r, http.MethodGet, "/api/v1/tag", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", rec.Code, rec.Body.String())
	}
	// api.WriteJSON uses json.NewEncoder().Encode, which appends a newline.
	if got := rec.Body.String(); got != "[]\n" {
		t.Fatalf("empty list must serialise as [], got %q", got)
	}

	rec = do(t, r, http.MethodPost, "/api/v1/tag", map[string]string{"label": "anime"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created store.Tag
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Label != "anime" {
		t.Fatalf("created = %+v", created)
	}

	rec = do(t, r, http.MethodPut, "/api/v1/tag/"+itoa(created.ID), map[string]string{"label": "anime-dub"})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodDelete, "/api/v1/tag/"+itoa(created.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTagAPIConflictsAndErrors(t *testing.T) {
	r, st := newTestRouter(t)
	ctx := context.Background()

	if rec := do(t, r, http.MethodPost, "/api/v1/tag", map[string]string{"label": "hd"}); rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rec.Code)
	}
	// Case-insensitive duplicate create -> 409.
	rec := do(t, r, http.MethodPost, "/api/v1/tag", map[string]string{"label": "HD"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create: %d body=%s", rec.Code, rec.Body.String())
	}
	// Blank label -> 400.
	rec = do(t, r, http.MethodPost, "/api/v1/tag", map[string]string{"label": "  "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank label: %d body=%s", rec.Code, rec.Body.String())
	}
	// Unknown id -> 404 on both rename and delete.
	if rec = do(t, r, http.MethodPut, "/api/v1/tag/999", map[string]string{"label": "x"}); rec.Code != http.StatusNotFound {
		t.Fatalf("rename missing: %d", rec.Code)
	}
	if rec = do(t, r, http.MethodDelete, "/api/v1/tag/999", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", rec.Code)
	}
	// Non-numeric id -> 400.
	if rec = do(t, r, http.MethodDelete, "/api/v1/tag/abc", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rec.Code)
	}

	// In-use delete -> 409, and the message names the counts.
	tg, err := st.CreateTag(ctx, "used")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesTags(ctx, sid, []int64{tg.ID}); err != nil {
		t.Fatal(err)
	}
	rec = do(t, r, http.MethodDelete, "/api/v1/tag/"+itoa(tg.ID), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("in-use delete: %d body=%s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Error.Code != "tag_in_use" {
		t.Fatalf("code = %q want tag_in_use", errBody.Error.Code)
	}
	if !bytes.Contains([]byte(errBody.Error.Message), []byte("1 series")) {
		t.Fatalf("message must name the counts, got %q", errBody.Error.Message)
	}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
