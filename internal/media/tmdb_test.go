package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tmdbTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/tv"):
			w.Write(fixture(t, "tmdb_search_tv.json"))
		case strings.HasPrefix(r.URL.Path, "/tv/1396/season/1"):
			w.Write(fixture(t, "tmdb_tv_season1.json"))
		case strings.HasPrefix(r.URL.Path, "/tv/1396"):
			w.Write(fixture(t, "tmdb_tv_details.json"))
		case strings.HasPrefix(r.URL.Path, "/movie/603"):
			w.Write(fixture(t, "tmdb_movie_details.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestTMDBSearchTV(t *testing.T) {
	srv := tmdbTestServer(t)
	defer srv.Close()
	c := newTMDB("key", srv.URL, srv.Client())

	res, err := c.SearchTV(context.Background(), "breaking bad")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].TMDBID != 1396 || res[0].Title != "Breaking Bad" ||
		res[0].Year != 2008 || res[0].Kind != provider.KindTV {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestTMDBTVDetails(t *testing.T) {
	srv := tmdbTestServer(t)
	defer srv.Close()
	c := newTMDB("key", srv.URL, srv.Client())

	s, err := c.TVDetails(context.Background(), 1396)
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Breaking Bad" || s.Status != "Ended" || len(s.Seasons) != 1 ||
		len(s.Seasons[0].Episodes) != 2 || s.Seasons[0].Episodes[0].Title != "Pilot" {
		t.Fatalf("unexpected: %+v", s)
	}
}

func TestTMDBMovieDetails(t *testing.T) {
	srv := tmdbTestServer(t)
	defer srv.Close()
	c := newTMDB("key", srv.URL, srv.Client())

	m, err := c.MovieDetails(context.Background(), 603)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "The Matrix" || m.Year != 1999 || m.Runtime != 136 || m.IMDbID != "tt0133093" {
		t.Fatalf("unexpected: %+v", m)
	}
}

func TestTMDBNotConfigured(t *testing.T) {
	c := newTMDB("", "", nil)
	if _, err := c.SearchTV(context.Background(), "x"); err != ErrProviderNotConfigured {
		t.Fatalf("want ErrProviderNotConfigured got %v", err)
	}
}

func TestTMDBServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTMDB("key", srv.URL, srv.Client())
	if _, err := c.SearchMovie(context.Background(), "x"); err != ErrProviderUnavailable {
		t.Fatalf("want ErrProviderUnavailable got %v", err)
	}
}
