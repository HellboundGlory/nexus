package provider

import "context"

// MetadataResult is one hit from a metadata search (the add picker).
type MetadataResult struct {
	TMDBID    int       `json:"tmdbId"`
	Title     string    `json:"title"`
	Year      int       `json:"year"`
	Overview  string    `json:"overview"`
	PosterURL string    `json:"posterUrl"`
	Kind      MediaKind `json:"kind"`
}

// EpisodeMetadata is one episode from a series detail lookup.
type EpisodeMetadata struct {
	SeasonNumber  int
	EpisodeNumber int
	TMDBID        int
	Title         string
	Overview      string
	AirDate       string // date-only ("2008-01-20") or ""
}

// SeasonMetadata groups episodes under a season number.
type SeasonMetadata struct {
	SeasonNumber int
	Episodes     []EpisodeMetadata
}

// SeriesMetadata is a full TV detail lookup, including seasons and episodes.
type SeriesMetadata struct {
	TMDBID     int
	Title      string
	Overview   string
	Status     string
	FirstAired string // date-only or ""
	PosterURL  string
	FanartURL  string
	Seasons    []SeasonMetadata
}

// MovieMetadata is a full movie detail lookup.
type MovieMetadata struct {
	TMDBID      int
	Title       string
	Overview    string
	Status      string
	Year        int
	ReleaseDate string // date-only or ""
	Runtime     int
	IMDbID      string
	PosterURL   string
	FanartURL   string
}

// MetadataProvider is the contract every metadata source implements. Concrete
// providers (TMDb now; TVDB etc. later) isolate the external wire format.
type MetadataProvider interface {
	SearchTV(ctx context.Context, term string) ([]MetadataResult, error)
	SearchMovie(ctx context.Context, term string) ([]MetadataResult, error)
	TVDetails(ctx context.Context, tmdbID int) (SeriesMetadata, error)
	MovieDetails(ctx context.Context, tmdbID int) (MovieMetadata, error)
}
