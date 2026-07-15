package importing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/provider"
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

func TestBlocklistListAndDelete(t *testing.T) {
	h, st := newTestAPI(t)
	ctx := context.Background()
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 1, Title: "Dune"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.AddBlocklist(ctx, store.Blocklist{MediaKind: "movie", MovieID: &mid, SourceTitle: "Dune.2021-GRP", Reason: "boom"})
	if err != nil {
		t.Fatal(err)
	}

	// GET
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/blocklist", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Dune.2021-GRP") || !strings.Contains(rr.Body.String(), `"title":"Dune"`) {
		t.Fatalf("GET /blocklist = %d %s", rr.Code, rr.Body.String())
	}
	// DELETE
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/blocklist/"+strconv.FormatInt(id, 10), nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rr.Code)
	}
	// DELETE missing -> 404
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/blocklist/9999", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing = %d", rr.Code)
	}
}

func TestAPIQueueEnrichesLiveProgress(t *testing.T) {
	ctx := context.Background()
	prog := 42.5
	fq := &fakeQueue{items: []provider.DownloadItem{
		{ID: "h1", DownloadClientID: "sab", Status: provider.StatusDownloading, Progress: prog},
	}}
	svc, st := newSvcWithQueue(t, fq)
	r := chi.NewRouter()
	NewAPI(svc).Mount(r)

	// matched row (client item "h1" is live)
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "sab", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "Matched.Release", MediaKind: "movie", Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	// matchless row (no live item with id "ghost")
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "sab", ClientItemID: "ghost", Protocol: "usenet",
		SourceTitle: "Matchless.Release", MediaKind: "movie", Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/queue", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var got []queueItemDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows got %d", len(got))
	}
	var matched, matchless *queueItemDTO
	for i := range got {
		switch got[i].ClientItemID {
		case "h1":
			matched = &got[i]
		case "ghost":
			matchless = &got[i]
		}
	}
	if matched == nil || matchless == nil {
		t.Fatalf("rows not found: %+v", got)
	}
	if matched.Progress == nil || *matched.Progress != 42.5 || matched.DownloadStatus != "downloading" {
		t.Fatalf("matched enrichment wrong: progress=%v status=%q", matched.Progress, matched.DownloadStatus)
	}
	if matchless.Progress != nil || matchless.DownloadStatus != "" {
		t.Fatalf("matchless row should be unenriched: progress=%v status=%q", matchless.Progress, matchless.DownloadStatus)
	}

	// Raw-key presence check: json.Unmarshal collapses "key absent", null, and ""
	// into the same zero value, so the typed-DTO assertions above pass whether or
	// not the omitempty tags are actually doing their job. Re-decode into raw
	// maps to verify the wire shape itself: a matchless row must OMIT both keys
	// entirely, not just carry zero values for them.
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("raw unmarshal: %v body=%s", err, w.Body.String())
	}
	var rawMatched, rawMatchless map[string]json.RawMessage
	for _, m := range raw {
		var clientItemID string
		if err := json.Unmarshal(m["clientItemId"], &clientItemID); err != nil {
			t.Fatalf("unmarshal clientItemId: %v", err)
		}
		switch clientItemID {
		case "h1":
			rawMatched = m
		case "ghost":
			rawMatchless = m
		}
	}
	if rawMatched == nil || rawMatchless == nil {
		t.Fatalf("raw rows not found: %+v", raw)
	}
	if _, ok := rawMatchless["progress"]; ok {
		t.Fatalf("matchless row must omit \"progress\" key entirely, got: %s", rawMatchless["progress"])
	}
	if _, ok := rawMatchless["downloadStatus"]; ok {
		t.Fatalf("matchless row must omit \"downloadStatus\" key entirely, got: %s", rawMatchless["downloadStatus"])
	}
	if _, ok := rawMatched["progress"]; !ok {
		t.Fatalf("matched row must include \"progress\" key")
	}
	if _, ok := rawMatched["downloadStatus"]; !ok {
		t.Fatalf("matched row must include \"downloadStatus\" key")
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
