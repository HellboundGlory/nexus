package releaseprofile

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

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestReleaseProfileAPICRUD(t *testing.T) {
	r, st := newTestRouter(t)
	ctx := context.Background()
	tg, err := st.CreateTag(ctx, "anime")
	if err != nil {
		t.Fatal(err)
	}

	rec := do(t, r, http.MethodGet, "/api/v1/releaseprofile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Fatalf("empty list must serialise as [], got %q", got)
	}

	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "No Dub", "requiredMode": "any",
		"requiredAny": []string{"1080p"}, "ignored": []string{"dub"},
		"preferred": []string{"bluray"}, "tagIds": []int64{tg.ID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created store.ReleaseProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Name != "No Dub" {
		t.Fatalf("created = %+v", created)
	}
	if len(created.TagIDs) != 1 || created.TagIDs[0] != tg.ID {
		t.Fatalf("created TagIDs = %v", created.TagIDs)
	}

	rec = do(t, r, http.MethodPut, "/api/v1/releaseprofile/"+itoa(created.ID), map[string]any{
		"name": "No Dub v2", "requiredMode": "all",
		"requiredAny": []string{"Indigo", "1080p"}, "ignored": []string{},
		"preferred": []string{}, "tagIds": []int64{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, r, http.MethodGet, "/api/v1/releaseprofile/"+itoa(created.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d body=%s", rec.Code, rec.Body.String())
	}
	var got store.ReleaseProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "No Dub v2" || got.RequiredMode != "all" {
		t.Fatalf("got = %+v", got)
	}
	if len(got.RequiredAny) != 2 {
		t.Fatalf("requiredAny = %v, want 2", got.RequiredAny)
	}

	rec = do(t, r, http.MethodDelete, "/api/v1/releaseprofile/"+itoa(created.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, r, http.MethodGet, "/api/v1/releaseprofile/"+itoa(created.ID), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d, want 404", rec.Code)
	}
}

func TestReleaseProfileAPIValidation(t *testing.T) {
	r, st := newTestRouter(t)
	ctx := context.Background()
	tg, err := st.CreateTag(ctx, "anime")
	if err != nil {
		t.Fatal(err)
	}

	// Empty name → 400.
	rec := do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "", "requiredAny": []string{"1080p"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name: %d, want 400", rec.Code)
	}

	// No terms → 400.
	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "Empty", "requiredMode": "any", "requiredAny": []string{}, "requiredAll": []string{},
		"ignored": []string{}, "preferred": []string{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no terms: %d, want 400", rec.Code)
	}

	// Bad required mode → 400.
	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "Bad", "requiredMode": "bogus", "requiredAny": []string{"1080p"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad mode: %d, want 400", rec.Code)
	}

	// Unknown tag id → 400.
	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "BadTag", "requiredMode": "any", "requiredAny": []string{"1080p"}, "tagIds": []int64{999},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown tag: %d, want 400", rec.Code)
	}

	// Valid create with a tag still works.
	rec = do(t, r, http.MethodPost, "/api/v1/releaseprofile", map[string]any{
		"name": "Good", "requiredMode": "any", "requiredAny": []string{"1080p"}, "tagIds": []int64{tg.ID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid create: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReleaseProfileAPINotFound(t *testing.T) {
	r, _ := newTestRouter(t)
	rec := do(t, r, http.MethodGet, "/api/v1/releaseprofile/999", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d, want 404", rec.Code)
	}
	rec = do(t, r, http.MethodDelete, "/api/v1/releaseprofile/999", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d, want 404", rec.Code)
	}
}