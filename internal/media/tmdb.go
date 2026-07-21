package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

const (
	tmdbBaseURL   = "https://api.themoviedb.org/3"
	tmdbImageBase = "https://image.tmdb.org/t/p/original"
	maxTMDBBytes  = 4 << 20 // 4 MiB cap on a metadata response
)

// TMDBClient implements provider.MetadataProvider against TMDb's v3 REST API.
type TMDBClient struct {
	apiKey string
	base   string
	http   *http.Client
}

// NewTMDBProvider builds the production TMDb client (default base, 30s timeout).
func NewTMDBProvider(apiKey string) *TMDBClient { return newTMDB(apiKey, "", nil) }

func newTMDB(apiKey, base string, hc *http.Client) *TMDBClient {
	if base == "" {
		base = tmdbBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &TMDBClient{apiKey: apiKey, base: strings.TrimRight(base, "/"), http: hc}
}

func imageURL(path string) string {
	if path == "" {
		return ""
	}
	return tmdbImageBase + path
}

func yearOf(date string) int {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil {
			return y
		}
	}
	return 0
}

// get performs a TMDb GET and decodes the JSON body into out.
func (c *TMDBClient) get(ctx context.Context, path string, q url.Values, out any) error {
	if c.apiKey == "" {
		return ErrProviderNotConfigured
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("api_key", c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+q.Encode(), nil)
	if err != nil {
		// Return the bare sentinel, never the raw error: a *url.Error here would
		// embed the full URL including ?api_key=, leaking the write-only credential.
		return ErrProviderUnavailable
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrProviderUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrProviderUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTMDBBytes))
	if err != nil {
		return ErrProviderUnavailable
	}
	if err := json.Unmarshal(body, out); err != nil {
		return ErrProviderUnavailable
	}
	return nil
}

type tmdbSearchTV struct {
	Results []struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		FirstAir   string `json:"first_air_date"`
		Overview   string `json:"overview"`
		PosterPath string `json:"poster_path"`
	} `json:"results"`
}

type tmdbSearchMovie struct {
	Results []struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		ReleaseDate string `json:"release_date"`
		Overview    string `json:"overview"`
		PosterPath  string `json:"poster_path"`
	} `json:"results"`
}

func (c *TMDBClient) SearchTV(ctx context.Context, term string) ([]provider.MetadataResult, error) {
	var r tmdbSearchTV
	if err := c.get(ctx, "/search/tv", url.Values{"query": {term}}, &r); err != nil {
		return nil, err
	}
	out := make([]provider.MetadataResult, 0, len(r.Results))
	for _, it := range r.Results {
		out = append(out, provider.MetadataResult{
			TMDBID: it.ID, Title: it.Name, Year: yearOf(it.FirstAir), Overview: it.Overview,
			PosterURL: imageURL(it.PosterPath), Kind: provider.KindTV,
		})
	}
	return out, nil
}

func (c *TMDBClient) SearchMovie(ctx context.Context, term string) ([]provider.MetadataResult, error) {
	var r tmdbSearchMovie
	if err := c.get(ctx, "/search/movie", url.Values{"query": {term}}, &r); err != nil {
		return nil, err
	}
	out := make([]provider.MetadataResult, 0, len(r.Results))
	for _, it := range r.Results {
		out = append(out, provider.MetadataResult{
			TMDBID: it.ID, Title: it.Title, Year: yearOf(it.ReleaseDate), Overview: it.Overview,
			PosterURL: imageURL(it.PosterPath), Kind: provider.KindMovie,
		})
	}
	return out, nil
}

type tmdbTVDetails struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Status       string `json:"status"`
	FirstAir     string `json:"first_air_date"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
	Seasons      []struct {
		SeasonNumber int `json:"season_number"`
	} `json:"seasons"`
}

type tmdbAltTitles struct {
	Results []struct {
		Country string `json:"iso_3166_1"`
		Title   string `json:"title"`
		Type    string `json:"type"`
	} `json:"results"`
}

type tmdbSeason struct {
	SeasonNumber int `json:"season_number"`
	Episodes     []struct {
		ID            int    `json:"id"`
		EpisodeNumber int    `json:"episode_number"`
		SeasonNumber  int    `json:"season_number"`
		Name          string `json:"name"`
		Overview      string `json:"overview"`
		AirDate       string `json:"air_date"`
	} `json:"episodes"`
}

func (c *TMDBClient) TVDetails(ctx context.Context, tmdbID int) (provider.SeriesMetadata, error) {
	var d tmdbTVDetails
	if err := c.get(ctx, "/tv/"+strconv.Itoa(tmdbID), nil, &d); err != nil {
		return provider.SeriesMetadata{}, err
	}
	s := provider.SeriesMetadata{
		TMDBID: d.ID, Title: d.Name, Overview: d.Overview, Status: d.Status,
		FirstAired: d.FirstAir, PosterURL: imageURL(d.PosterPath), FanartURL: imageURL(d.BackdropPath),
	}
	// Aliases are best-effort: a failure here must not fail the series add or
	// refresh that called us. The series simply has no aliases until next time.
	var alt tmdbAltTitles
	if err := c.get(ctx, "/tv/"+strconv.Itoa(tmdbID)+"/alternative_titles", nil, &alt); err != nil {
		slog.Warn("tmdb: alternative titles lookup failed", "tmdbId", tmdbID, "err", err)
	} else {
		for _, a := range alt.Results {
			s.Aliases = append(s.Aliases, provider.SeriesAlias{Title: a.Title, Country: a.Country, Type: a.Type})
		}
	}
	for _, sn := range d.Seasons {
		var sd tmdbSeason
		if err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", tmdbID, sn.SeasonNumber), nil, &sd); err != nil {
			return provider.SeriesMetadata{}, err
		}
		sm := provider.SeasonMetadata{SeasonNumber: sn.SeasonNumber}
		for _, e := range sd.Episodes {
			sm.Episodes = append(sm.Episodes, provider.EpisodeMetadata{
				SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, TMDBID: e.ID,
				Title: e.Name, Overview: e.Overview, AirDate: e.AirDate,
			})
		}
		s.Seasons = append(s.Seasons, sm)
	}
	return s, nil
}

type tmdbMovieDetails struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Overview     string `json:"overview"`
	Status       string `json:"status"`
	ReleaseDate  string `json:"release_date"`
	Runtime      int    `json:"runtime"`
	IMDbID       string `json:"imdb_id"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

func (c *TMDBClient) MovieDetails(ctx context.Context, tmdbID int) (provider.MovieMetadata, error) {
	var d tmdbMovieDetails
	if err := c.get(ctx, "/movie/"+strconv.Itoa(tmdbID), nil, &d); err != nil {
		return provider.MovieMetadata{}, err
	}
	return provider.MovieMetadata{
		TMDBID: d.ID, Title: d.Title, Overview: d.Overview, Status: d.Status,
		Year: yearOf(d.ReleaseDate), ReleaseDate: d.ReleaseDate, Runtime: d.Runtime, IMDbID: d.IMDbID,
		PosterURL: imageURL(d.PosterPath), FanartURL: imageURL(d.BackdropPath),
	}, nil
}

// Ensure the interface is satisfied at compile time.
var _ provider.MetadataProvider = (*TMDBClient)(nil)
