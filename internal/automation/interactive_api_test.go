package automation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// newInteractiveAPI mirrors newTestAPI (api_test.go) but takes the fakeSearcher
// so callers can inject releases/indexerErrors before the request is served.
func newInteractiveAPI(t *testing.T, fs *fakeSearcher) (http.Handler, *store.Store) {
	t.Helper()
	st := newStore(t)
	svc := NewService(st, fs, &fakeEnqueuer{}, nil)
	r := chi.NewRouter()
	NewAPI(svc, &fakeDispatcher{}).Mount(r)
	return r, st
}

func TestInteractiveMovieReturnsScoredReleasesAndIndexerErrors(t *testing.T) {
	// hdProfile (the profile seedMovie assigns) allows ONLY WEBDL-1080p(7) and
	// Bluray-1080p(9). So here the 1080p WEB-DL release is the accepted one and
	// the 480p HDTV release (SDTV, id 1) is the rejected one — the opposite of
	// the 480p-only fixture used elsewhere in this package.
	// Input order is deliberately rejected-first, accepted-second: this proves
	// DecideAll's accepted-first sort actually reorders rather than the
	// assertion passing merely because it matches input order.
	fs := &fakeSearcher{
		releases: []provider.Release{
			{Title: "Some.Movie.2019.480p.HDTV.x264-GRP", IndexerID: "1", DownloadURL: "http://x/2", Protocol: provider.ProtocolUsenet},
			{Title: "Some.Movie.2019.1080p.WEB-DL.x264-GRP", IndexerID: "1", DownloadURL: "http://x/1", Protocol: provider.ProtocolUsenet},
		},
		indexerErrors: []IndexerError{{IndexerID: "3", Message: "timeout"}},
	}
	r, st := newInteractiveAPI(t, fs)
	id := seedMovie(t, st, true, true)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/automation/search/movie/"+itoa(id)+"/interactive", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var res InteractiveResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Releases) != 2 {
		t.Fatalf("want both releases incl. the quality-rejected one, got %d", len(res.Releases))
	}
	if len(res.Releases[0].Rejections) != 0 {
		t.Fatalf("row 1 must be the accepted 1080p WEB-DL, got %+v", res.Releases[0])
	}
	if len(res.Releases[1].Rejections) == 0 {
		t.Fatal("row 2 (480p HDTV) must carry a rejection reason, not be dropped")
	}
	// indexerErrors is load-bearing: a partial list with no banner reproduces the
	// invisibility this feature exists to remove.
	if len(res.IndexerErrors) != 1 || res.IndexerErrors[0].IndexerID != "3" {
		t.Fatalf("indexerErrors = %+v, want the failing indexer named", res.IndexerErrors)
	}
}

func TestInteractiveMovieNoProfileReturns400(t *testing.T) {
	r, st := newInteractiveAPI(t, &fakeSearcher{})
	id := seedMovie(t, st, true, false) // monitored, but no quality profile

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/automation/search/movie/"+itoa(id)+"/interactive", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when the item has no quality profile", rec.Code)
	}
}

func TestInteractiveMovieNotFoundReturns404(t *testing.T) {
	r, _ := newInteractiveAPI(t, &fakeSearcher{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/automation/search/movie/99999/interactive", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestInteractiveSeasonReturnsPackAndAnnotatesNonCoveringEpisode(t *testing.T) {
	// hdProfile allows ONLY WEBDL-1080p(7) and Bluray-1080p(9), so both releases
	// below are quality-accepted — the only thing that can separate them is
	// SeasonPackCoverage. The pack (right season, no episode numbers) covers;
	// the single episode does not.
	// Input order is deliberately single-episode-first, pack-second: this proves
	// the pack reaching row 1 is coverage annotation doing the sorting, not
	// input order.
	fs := &fakeSearcher{
		releases: []provider.Release{
			{Title: "The.Show.S01E03.1080p.WEB-DL.x264-GRP", IndexerID: "1", DownloadURL: "http://x/ep", Protocol: provider.ProtocolUsenet},
			{Title: "The.Show.S01.1080p.WEB-DL.x264-GRP", IndexerID: "1", DownloadURL: "http://x/pack", Protocol: provider.ProtocolUsenet},
		},
	}
	r, st := newInteractiveAPI(t, fs)
	sid, _ := seedSeries(t, st, true, 3)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/automation/search/series/"+itoa(sid)+"/season/1/interactive", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var res InteractiveResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Releases) != 2 {
		t.Fatalf("want both releases incl. the non-covering one, got %d", len(res.Releases))
	}
	if res.Releases[0].DownloadURL != "http://x/pack" || len(res.Releases[0].Rejections) != 0 {
		t.Fatalf("row 1 must be the covering season pack with no rejections, got %+v", res.Releases[0])
	}
	if len(res.Releases[1].Rejections) == 0 {
		t.Fatal("row 2 (single episode) must carry a coverage rejection reason, not be dropped")
	}
}

func TestInteractiveSeasonUnknownSeriesReturns404(t *testing.T) {
	r, _ := newInteractiveAPI(t, &fakeSearcher{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/automation/search/series/99999/season/1/interactive", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestInteractiveSeasonBadSeasonParamReturns400(t *testing.T) {
	r, st := newInteractiveAPI(t, &fakeSearcher{})
	sid, _ := seedSeries(t, st, true, 1)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/automation/search/series/"+itoa(sid)+"/season/abc/interactive", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unparseable season param", rec.Code)
	}
}

func TestInteractiveEpisodeReturnsCoveringReleaseAndAnnotatesOther(t *testing.T) {
	// Both releases are 1080p WEB-DL, so quality accepts both — only
	// EpisodeCoverage can separate them. Feed the non-covering episode (E01)
	// first so the covering one (E03) reaching row 1 proves the coverage
	// annotation sorted it there, not input order.
	fs := &fakeSearcher{
		releases: []provider.Release{
			{Title: "The.Show.S01E01.1080p.WEB-DL.x264-GRP", IndexerID: "1", DownloadURL: "http://x/other", Protocol: provider.ProtocolUsenet},
			{Title: "The.Show.S01E03.1080p.WEB-DL.x264-GRP", IndexerID: "1", DownloadURL: "http://x/target", Protocol: provider.ProtocolUsenet},
		},
	}
	r, st := newInteractiveAPI(t, fs)
	_, epIDs := seedSeries(t, st, true, 3)
	targetEpID := epIDs[2] // episode 3, matching S01E03 above

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/automation/search/episode/"+itoa(targetEpID)+"/interactive", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var res InteractiveResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Releases) != 2 {
		t.Fatalf("want both releases incl. the non-covering one, got %d", len(res.Releases))
	}
	if res.Releases[0].DownloadURL != "http://x/target" || len(res.Releases[0].Rejections) != 0 {
		t.Fatalf("row 1 must be the covering S01E03 release with no rejections, got %+v", res.Releases[0])
	}
	if len(res.Releases[1].Rejections) == 0 {
		t.Fatal("row 2 (non-covering episode) must carry a coverage rejection reason, not be dropped")
	}
}

func TestInteractiveEpisodeUnknownEpisodeReturns404(t *testing.T) {
	r, _ := newInteractiveAPI(t, &fakeSearcher{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/automation/search/episode/99999/interactive", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// WIRE SHAPE — assert on the raw JSON map, never a typed round-trip. Go collapses
// absent/null/zero into the zero value, so a typed unmarshal cannot tell "key
// absent" from "zero value" and this guard would pass regardless of omitempty,
// going silently inert.
//
// Both releases are 1080p WEB-DL (quality id 7, allowed by hdProfile) so both
// are ACCEPTED — the point of this test is the seeders/rejections/quality wire
// shape, not the accept/reject split (that's covered above).
func TestInteractiveWireShape(t *testing.T) {
	seeders := 0
	fs := &fakeSearcher{
		releases: []provider.Release{
			// usenet: no seeders → the key must be ABSENT
			{Title: "Some.Movie.2019.1080p.WEB-DL.x264-GRP", IndexerID: "1", DownloadURL: "http://x/1", Protocol: provider.ProtocolUsenet},
			// torrent with a REAL 0 seeders → the key must be PRESENT with value 0
			{Title: "Some.Movie.2019.1080p.WEB-DL.x264-OTHER", IndexerID: "2", DownloadURL: "http://x/2", Protocol: provider.ProtocolTorrent, Seeders: &seeders},
		},
	}
	r, st := newInteractiveAPI(t, fs)
	id := seedMovie(t, st, true, true)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/automation/search/movie/"+itoa(id)+"/interactive", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var raw struct {
		Releases      []map[string]json.RawMessage `json:"releases"`
		IndexerErrors json.RawMessage              `json:"indexerErrors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}

	var usenet, torrent map[string]json.RawMessage
	for _, row := range raw.Releases {
		if string(row["protocol"]) == `"usenet"` {
			usenet = row
		} else {
			torrent = row
		}
	}
	if usenet == nil || torrent == nil {
		t.Fatalf("expected one usenet and one torrent row, got %+v", raw.Releases)
	}

	if _, present := usenet["seeders"]; present {
		t.Error("usenet row must OMIT seeders entirely")
	}
	v, present := torrent["seeders"]
	if !present {
		t.Fatal("torrent row must PRESENT seeders even at a real 0")
	}
	if string(v) != "0" {
		t.Errorf("torrent seeders = %s, want 0", v)
	}

	// rejections is always [], never absent and never null
	rj, present := usenet["rejections"]
	if !present {
		t.Fatal("rejections key must always be present")
	}
	if string(rj) != "[]" {
		t.Errorf("rejections = %s, want [] for a clean row", rj)
	}

	// quality is always present, even for an unparseable title
	if _, present := usenet["quality"]; !present {
		t.Error("quality key must always be present")
	}

	// indexerErrors is [] when empty, never null
	if string(raw.IndexerErrors) != "[]" {
		t.Errorf("indexerErrors = %s, want []", raw.IndexerErrors)
	}
}
