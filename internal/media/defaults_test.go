package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hellboundg/nexus/internal/core/store"
)

func mustRootFolder(t *testing.T, st *store.Store) int64 {
	t.Helper()
	id, err := st.CreateRootFolder(context.Background(), "/data/movies")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustQualityProfile(t *testing.T, st *store.Store) int64 {
	t.Helper()
	prof, err := st.CreateQualityProfile(context.Background(), store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return prof.ID
}

func TestMediaDefaultsRoundTrip(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)
	profID := mustQualityProfile(t, st)

	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID, QualityProfileID: &profID},
		TV:    KindDefaults{RootFolderID: nil, QualityProfileID: &profID},
	}); err != nil {
		t.Fatalf("SetMediaDefaults: %v", err)
	}

	got, err := svc.GetMediaDefaults(ctx)
	if err != nil {
		t.Fatalf("GetMediaDefaults: %v", err)
	}
	if got.Movie.RootFolderID == nil || *got.Movie.RootFolderID != rootID {
		t.Fatalf("movie root = %v, want %d", got.Movie.RootFolderID, rootID)
	}
	if got.TV.RootFolderID != nil {
		t.Fatalf("tv root = %v, want nil", got.TV.RootFolderID)
	}
	if got.TV.QualityProfileID == nil || *got.TV.QualityProfileID != profID {
		t.Fatalf("tv profile = %v, want %d", got.TV.QualityProfileID, profID)
	}
}

func TestMediaDefaultsGetNullsStaleReference(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)

	// set the movie root default, then delete the folder out from under it
	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteRootFolder(ctx, rootID); err != nil {
		t.Fatal(err)
	}

	got, err := svc.GetMediaDefaults(ctx)
	if err != nil {
		t.Fatalf("GetMediaDefaults: %v", err)
	}
	if got.Movie.RootFolderID != nil {
		t.Fatalf("stale movie root should be nulled, got %v", got.Movie.RootFolderID)
	}
}

func TestMediaDefaultsGetKeepsValidReference(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	profID := mustQualityProfile(t, st)

	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{QualityProfileID: &profID},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetMediaDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// regression guard: a VALID id must not be nulled
	if got.Movie.QualityProfileID == nil || *got.Movie.QualityProfileID != profID {
		t.Fatalf("valid profile default was dropped: %v", got.Movie.QualityProfileID)
	}
}

func TestSetMediaDefaultsUnknownProfileRejectedAndUnchanged(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)
	profID := mustQualityProfile(t, st)

	// establish a good baseline
	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID, QualityProfileID: &profID},
	}); err != nil {
		t.Fatal(err)
	}

	bogus := int64(99999)
	err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID, QualityProfileID: &bogus},
	})
	if err == nil {
		t.Fatal("want an error for an unknown profile id")
	}
	// all-or-nothing: the baseline must be intact
	got, _ := svc.GetMediaDefaults(ctx)
	if got.Movie.QualityProfileID == nil || *got.Movie.QualityProfileID != profID {
		t.Fatalf("failed PUT must not overwrite; profile = %v", got.Movie.QualityProfileID)
	}
}

// Wire shape: an unset field is JSON null (not 0, not absent). 0 is never a
// valid id, so a typed round-trip could not tell "unset" from "id 0" — assert on
// the raw JSON.
func TestMediaDefaultsWireShapeNullNotZero(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)
	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID}, // tv left entirely unset
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetMediaDefaults(ctx)
	b, _ := json.Marshal(got)

	var raw struct {
		TV struct {
			RootFolderID     json.RawMessage `json:"rootFolderId"`
			QualityProfileID json.RawMessage `json:"qualityProfileId"`
		} `json:"tv"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw.TV.RootFolderID) != "null" {
		t.Errorf("tv rootFolderId = %s, want null", raw.TV.RootFolderID)
	}
	if string(raw.TV.QualityProfileID) != "null" {
		t.Errorf("tv qualityProfileId = %s, want null", raw.TV.QualityProfileID)
	}

	// Assert the underlying stored value directly. resolveDefault's existence
	// check treats id 0 as "not found" (there is no row with id 0), so a bug
	// that wrote "0" instead of "" for an unset id would be silently masked by
	// the round-trip assertions above. Pin the storage contract itself: unset
	// must persist as the empty string, never "0".
	tvRootRaw, found, err := st.GetSetting(ctx, keyDefaultTVRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !found || tvRootRaw != "" {
		t.Errorf("stored %s = %q (found=%v), want empty string", keyDefaultTVRoot, tvRootRaw, found)
	}
	tvProfileRaw, found, err := st.GetSetting(ctx, keyDefaultTVProfile)
	if err != nil {
		t.Fatal(err)
	}
	if !found || tvProfileRaw != "" {
		t.Errorf("stored %s = %q (found=%v), want empty string", keyDefaultTVProfile, tvProfileRaw, found)
	}
}

// HTTP-level: PUT with an unknown quality profile id -> 400 bad_request, and a
// valid PUT followed by GET round-trips through the mounted router.
func TestAPIMediaDefaultsEndpoint(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	rootID := mustRootFolder(t, st)

	// unknown profile id -> 400 bad_request, writes nothing
	badBody := `{"movie":{"rootFolderId":` + itoa(rootID) + `,"qualityProfileId":99999},"tv":{"rootFolderId":null,"qualityProfileId":null}}`
	req := httptest.NewRequest(http.MethodPut, "/config/media-defaults", strings.NewReader(badBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT with unknown profile id status = %d body=%s", w.Code, w.Body.String())
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if errBody.Error.Code != "bad_request" {
		t.Fatalf("error.code = %q, want bad_request", errBody.Error.Code)
	}

	// valid PUT
	profID := mustQualityProfile(t, st)
	goodBody := `{"movie":{"rootFolderId":` + itoa(rootID) + `,"qualityProfileId":` + itoa(profID) + `},"tv":{"rootFolderId":null,"qualityProfileId":null}}`
	req = httptest.NewRequest(http.MethodPut, "/config/media-defaults", strings.NewReader(goodBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT valid status = %d body=%s", w.Code, w.Body.String())
	}

	// GET reflects it back
	req = httptest.NewRequest(http.MethodGet, "/config/media-defaults", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", w.Code, w.Body.String())
	}
	var got MediaDefaults
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal GET body: %v", err)
	}
	if got.Movie.RootFolderID == nil || *got.Movie.RootFolderID != rootID {
		t.Fatalf("GET movie root = %v, want %d", got.Movie.RootFolderID, rootID)
	}
	if got.Movie.QualityProfileID == nil || *got.Movie.QualityProfileID != profID {
		t.Fatalf("GET movie profile = %v, want %d", got.Movie.QualityProfileID, profID)
	}
}

func itoa(id int64) string {
	b, _ := json.Marshal(id)
	return string(b)
}
