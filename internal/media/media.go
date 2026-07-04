// Package media manages TV series and movies (metadata, parsing, import).
// Foundation ships only the package; behavior lands in sub-project 4.
package media

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// Monitor options applied at add time.
const (
	MonitorAll    = "all"
	MonitorFuture = "future"
	MonitorNone   = "none"
)

// SeriesUpdated / MovieUpdated are published (async) on add / refresh / monitor changes.
type SeriesUpdated struct {
	ID int64 `json:"id"`
}

func (SeriesUpdated) Name() string { return "media.series.updated" }

type MovieUpdated struct {
	ID int64 `json:"id"`
}

func (MovieUpdated) Name() string { return "media.movie.updated" }

type AddSeriesRequest struct {
	TMDBID        int
	RootFolderID  *int64
	MonitorOption string
}

type AddMovieRequest struct {
	TMDBID       int
	RootFolderID *int64
	Monitored    bool
}

// Service owns all library mutations over the store and a metadata provider.
type Service struct {
	store *store.Store
	meta  provider.MetadataProvider
	bus   *events.Bus
}

func NewService(st *store.Store, mp provider.MetadataProvider) *Service {
	return &Service{store: st, meta: mp}
}

// WithBus attaches an event bus so add/refresh/monitor changes publish media events.
func (s *Service) WithBus(bus *events.Bus) *Service {
	s.bus = bus
	return s
}

func (s *Service) emitSeries(ctx context.Context, id int64) {
	if s.bus != nil {
		s.bus.PublishAsync(ctx, SeriesUpdated{ID: id})
	}
}

func (s *Service) emitMovie(ctx context.Context, id int64) {
	if s.bus != nil {
		s.bus.PublishAsync(ctx, MovieUpdated{ID: id})
	}
}

func sortTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, p := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(t, p) {
			return strings.TrimPrefix(t, p)
		}
	}
	return t
}

// aired reports whether a date-only string is on/before today.
func aired(airDate string) bool {
	if airDate == "" {
		return false
	}
	d, err := time.Parse("2006-01-02", airDate)
	if err != nil {
		return false
	}
	return !d.After(time.Now())
}

// episodeMonitored decides an episode's monitored flag from the add-time option.
func episodeMonitored(option, airDate string) bool {
	switch option {
	case MonitorAll:
		return true
	case MonitorFuture:
		return !aired(airDate)
	default: // MonitorNone or unknown
		return false
	}
}

func (s *Service) validateRootFolder(ctx context.Context, id *int64) error {
	if id == nil {
		return nil
	}
	if _, err := s.store.GetRootFolder(ctx, *id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidRootFolder
		}
		return err
	}
	return nil
}

func (s *Service) AddRootFolder(ctx context.Context, path string) (store.RootFolder, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return store.RootFolder{}, ErrInvalidRootFolder
	}
	id, err := s.store.CreateRootFolder(ctx, path)
	if err != nil {
		return store.RootFolder{}, err
	}
	rf, err := s.store.GetRootFolder(ctx, id)
	if err != nil {
		return store.RootFolder{}, err
	}
	return *rf, nil
}

func (s *Service) AddSeries(ctx context.Context, req AddSeriesRequest) (store.Series, error) {
	if err := s.validateRootFolder(ctx, req.RootFolderID); err != nil {
		return store.Series{}, err
	}
	md, err := s.meta.TVDetails(ctx, req.TMDBID)
	if err != nil {
		return store.Series{}, err
	}
	id, err := s.store.CreateSeries(ctx, store.Series{
		TMDBID: md.TMDBID, Title: md.Title, SortTitle: sortTitle(md.Title), Overview: md.Overview,
		Status: md.Status, FirstAired: md.FirstAired, PosterURL: md.PosterURL, FanartURL: md.FanartURL,
		RootFolderID: req.RootFolderID, Monitored: req.MonitorOption != MonitorNone,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.Series{}, ErrAlreadyExists
		}
		return store.Series{}, err
	}
	for _, sn := range md.Seasons {
		seasonMonitored := req.MonitorOption != MonitorNone
		if err := s.store.UpsertSeason(ctx, store.Season{SeriesID: id, SeasonNumber: sn.SeasonNumber, Monitored: seasonMonitored}); err != nil {
			return store.Series{}, err
		}
		for _, e := range sn.Episodes {
			if err := s.store.UpsertEpisode(ctx, store.Episode{
				SeriesID: id, SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, TMDBID: e.TMDBID,
				Title: e.Title, Overview: e.Overview, AirDate: e.AirDate,
				Monitored: episodeMonitored(req.MonitorOption, e.AirDate),
			}); err != nil {
				return store.Series{}, err
			}
		}
	}
	out, err := s.store.GetSeries(ctx, id)
	if err != nil {
		return store.Series{}, err
	}
	s.emitSeries(ctx, id)
	return *out, nil
}

func (s *Service) AddMovie(ctx context.Context, req AddMovieRequest) (store.Movie, error) {
	if err := s.validateRootFolder(ctx, req.RootFolderID); err != nil {
		return store.Movie{}, err
	}
	md, err := s.meta.MovieDetails(ctx, req.TMDBID)
	if err != nil {
		return store.Movie{}, err
	}
	id, err := s.store.CreateMovie(ctx, store.Movie{
		TMDBID: md.TMDBID, Title: md.Title, SortTitle: sortTitle(md.Title), Overview: md.Overview,
		Status: md.Status, Year: md.Year, ReleaseDate: md.ReleaseDate, Runtime: md.Runtime, IMDbID: md.IMDbID,
		PosterURL: md.PosterURL, FanartURL: md.FanartURL, RootFolderID: req.RootFolderID, Monitored: req.Monitored,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.Movie{}, ErrAlreadyExists
		}
		return store.Movie{}, err
	}
	out, err := s.store.GetMovie(ctx, id)
	if err != nil {
		return store.Movie{}, err
	}
	s.emitMovie(ctx, id)
	return *out, nil
}

// isUniqueViolation detects a SQLite UNIQUE constraint failure (duplicate tmdb_id).
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE")
}
