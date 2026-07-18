package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/quality"
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

func TestDeleteRootFolderStatuses(t *testing.T) {
	fp := &fakeProvider{}
	r, st := newTestAPI(t, fp)

	// Missing → 404.
	req := httptest.NewRequest(http.MethodDelete, "/rootfolder/99999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing: want 404, got %d", rec.Code)
	}

	// In-use → 409.
	rfID, err := st.CreateRootFolder(context.Background(), "/data/x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateMovie(context.Background(), store.Movie{TMDBID: 777, Title: "M", RootFolderID: &rfID}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodDelete, "/rootfolder/"+strconv.FormatInt(rfID, 10), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("in-use: want 409, got %d", rec.Code)
	}

	// Unused → 200.
	rf2, err := st.CreateRootFolder(context.Background(), "/data/y")
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodDelete, "/rootfolder/"+strconv.FormatInt(rf2, 10), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unused: want 200, got %d", rec.Code)
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

func TestAPIListEnrichment(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries(), movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()

	// Add a movie and a series through the API so ids exist.
	post(t, r, "/movies", `{"tmdbId":200,"monitored":true}`, http.StatusCreated)
	post(t, r, "/series", `{"tmdbId":100,"monitorOption":"all"}`, http.StatusCreated)

	// Attach a file to the movie directly in the store.
	movies, _ := st.ListMovies(ctx)
	if len(movies) == 0 {
		t.Fatal("no movie added")
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &movies[0].ID, RelativePath: "m.mkv", Size: 1, QualityID: 1}); err != nil {
		t.Fatal(err)
	}

	// Movie list carries hasFile=true.
	body := get(t, r, "/movies", http.StatusOK)
	var ml []map[string]any
	mustJSON(t, body, &ml)
	if len(ml) != 1 || ml[0]["hasFile"] != true {
		t.Fatalf("movie list missing hasFile: %s", body)
	}

	// Series list carries episodeCount / episodeFileCount.
	body = get(t, r, "/series", http.StatusOK)
	var sl []map[string]any
	mustJSON(t, body, &sl)
	if len(sl) != 1 {
		t.Fatalf("series list len: %s", body)
	}
	if _, ok := sl[0]["episodeCount"]; !ok {
		t.Fatalf("series list missing episodeCount: %s", body)
	}
	if _, ok := sl[0]["episodeFileCount"]; !ok {
		t.Fatalf("series list missing episodeFileCount: %s", body)
	}

	// Series detail episodes carry hasFile.
	sid := int64(sl[0]["id"].(float64))
	body = get(t, r, "/series/"+strconv.FormatInt(sid, 10), http.StatusOK)
	var detail map[string]any
	mustJSON(t, body, &detail)
	eps, _ := detail["episodes"].([]any)
	if len(eps) == 0 {
		t.Fatalf("series detail has no episodes: %s", body)
	}
	first := eps[0].(map[string]any)
	if _, ok := first["hasFile"]; !ok {
		t.Fatalf("episode missing hasFile: %s", body)
	}
	// Series detail also carries the monitored-only progress counts the header
	// badge reads (must mirror the list view, not be omitted).
	if _, ok := detail["episodeCount"]; !ok {
		t.Fatalf("series detail missing episodeCount: %s", body)
	}
	if _, ok := detail["episodeFileCount"]; !ok {
		t.Fatalf("series detail missing episodeFileCount: %s", body)
	}
}

// small helpers local to the test file
func post(t *testing.T, r http.Handler, path, body string, want int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("POST %s = %d want %d body=%s", path, w.Code, want, w.Body.String())
	}
}
func get(t *testing.T, r http.Handler, path string, want int) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("GET %s = %d want %d body=%s", path, w.Code, want, w.Body.String())
	}
	return w.Body.String()
}
func mustJSON(t *testing.T, body string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
}

func TestAPICalendar(t *testing.T) {
	fp := &fakeProvider{}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()

	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "Show", SortTitle: "show", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 2, Title: "Two", AirDate: "2026-07-15", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 9, Title: "Out", AirDate: "2026-09-01", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	// season-0 special in-window (TMDb Specials); seasonNumber must serialize as 0, not be omitted
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 0, EpisodeNumber: 3, Title: "Special", AirDate: "2026-07-12", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateMovie(ctx, store.Movie{TMDBID: 2, Title: "Aaa Film", SortTitle: "aaa film", Year: 2026, ReleaseDate: "2026-07-15", Monitored: true}); err != nil {
		t.Fatal(err)
	}

	// happy path: special (07-12) + episode & movie (07-15); episode sorts before movie same date
	req := httptest.NewRequest(http.MethodGet, "/calendar?start=2026-07-10&end=2026-07-31", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries got %d: %s", len(got), w.Body.String())
	}
	// entries: [0] special 07-12 episode, [1] episode 07-15, [2] movie 07-15
	if got[1]["type"] != "episode" || got[2]["type"] != "movie" {
		t.Fatalf("same-day order want episode,movie got %v,%v", got[1]["type"], got[2]["type"])
	}
	if got[1]["seriesTitle"] != "Show" {
		t.Fatalf("seriesTitle: %v", got[1])
	}
	// season-0 special: seasonNumber key must be present with value 0 (not omitted)
	special := got[0]
	if special["date"] != "2026-07-12" || special["type"] != "episode" {
		t.Fatalf("first entry should be the special, got %v", special)
	}
	if v, ok := special["seasonNumber"]; !ok {
		t.Fatalf("seasonNumber key missing for season-0 special: %v", special)
	} else if v != float64(0) {
		t.Fatalf("seasonNumber want 0 got %v", v)
	}

	// bad date → 400
	req = httptest.NewRequest(http.MethodGet, "/calendar?start=nope&end=2026-07-31", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad start status=%d want 400", w.Code)
	}

	// empty window → [] (never null)
	req = httptest.NewRequest(http.MethodGet, "/calendar?start=2030-01-01&end=2030-01-02", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty window body=%q want []", w.Body.String())
	}
}

func TestGetMovieIncludesFile(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()

	rf, err := st.CreateRootFolder(ctx, "/data/movies")
	if err != nil {
		t.Fatal(err)
	}
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rf})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: "Film (2020)/Film.2020.1080p.mkv",
		Size: 8455160320, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/movies/"+strconv.FormatInt(mid, 10), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}

	var got struct {
		HasFile bool `json:"hasFile"`
		File    *struct {
			RelativePath string `json:"relativePath"`
			Size         int64  `json:"size"`
			QualityID    int    `json:"qualityId"`
			Quality      string `json:"quality"`
		} `json:"file"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.HasFile || got.File == nil {
		t.Fatalf("want hasFile+file, got %+v", got)
	}
	if got.File.Size != 8455160320 || got.File.QualityID != 9 {
		t.Fatalf("file fields wrong: %+v", got.File)
	}
	wantName, _ := quality.DefinitionByID(9)
	if got.File.Quality != wantName.Name {
		t.Fatalf("quality name = %q want %q", got.File.Quality, wantName.Name)
	}
}

func TestGetMovieOmitsFileWhenAbsent(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	mid, err := st.CreateMovie(context.Background(), store.Movie{TMDBID: 200, Title: "Film"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/movies/"+strconv.FormatInt(mid, 10), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["file"]; present {
		t.Fatalf("file key must be absent when no media file, got %s", w.Body.String())
	}
}

func TestDeleteMovieFileRoute(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: "a/b.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/movies/"+strconv.FormatInt(mid, 10)+"/file", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	f, _ := st.MediaFileForMovie(ctx, mid)
	if f != nil {
		t.Fatal("file row should be gone after DELETE")
	}
}

func TestDeleteMovieRouteParsesDeleteFiles(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()

	root := t.TempDir()
	rid, _ := st.CreateRootFolder(ctx, root)
	mid, _ := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rid})
	folder := filepath.Join(root, "Film (2020)")
	os.MkdirAll(folder, 0o755)
	os.WriteFile(filepath.Join(folder, "f.mkv"), []byte("x"), 0o644)
	st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &mid, RelativePath: "Film (2020)/f.mkv", Size: 1, QualityID: 9})

	req := httptest.NewRequest(http.MethodDelete, "/movies/"+strconv.FormatInt(mid, 10)+"?deleteFiles=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder should be gone after deleteFiles=true, stat err = %v", err)
	}
}
