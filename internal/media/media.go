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

// RefreshSeries re-pulls metadata and reconciles seasons/episodes. Descriptive
// fields are updated; user monitored choices are preserved (UpsertEpisode does
// not overwrite monitored on conflict). New episodes inherit their season's
// current monitored state.
func (s *Service) RefreshSeries(ctx context.Context, id int64) error {
	cur, err := s.store.GetSeries(ctx, id)
	if err != nil {
		return err
	}
	md, err := s.meta.TVDetails(ctx, cur.TMDBID)
	if err != nil {
		return err
	}
	cur.Title = md.Title
	cur.SortTitle = sortTitle(md.Title)
	cur.Overview = md.Overview
	cur.Status = md.Status
	cur.FirstAired = md.FirstAired
	cur.PosterURL = md.PosterURL
	cur.FanartURL = md.FanartURL
	if err := s.store.UpdateSeries(ctx, *cur); err != nil {
		return err
	}
	seasons, err := s.store.ListSeasons(ctx, id)
	if err != nil {
		return err
	}
	seasonMon := map[int]bool{}
	for _, sn := range seasons {
		seasonMon[sn.SeasonNumber] = sn.Monitored
	}
	for _, sn := range md.Seasons {
		mon, known := seasonMon[sn.SeasonNumber]
		if !known {
			mon = cur.Monitored
		}
		if err := s.store.UpsertSeason(ctx, store.Season{SeriesID: id, SeasonNumber: sn.SeasonNumber, Monitored: mon}); err != nil {
			return err
		}
		for _, e := range sn.Episodes {
			if err := s.store.UpsertEpisode(ctx, store.Episode{
				SeriesID: id, SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, TMDBID: e.TMDBID,
				Title: e.Title, Overview: e.Overview, AirDate: e.AirDate, Monitored: mon,
			}); err != nil {
				return err
			}
		}
	}
	s.emitSeries(ctx, id)
	return nil
}

func (s *Service) RefreshMovie(ctx context.Context, id int64) error {
	cur, err := s.store.GetMovie(ctx, id)
	if err != nil {
		return err
	}
	md, err := s.meta.MovieDetails(ctx, cur.TMDBID)
	if err != nil {
		return err
	}
	cur.Title = md.Title
	cur.SortTitle = sortTitle(md.Title)
	cur.Overview = md.Overview
	cur.Status = md.Status
	cur.Year = md.Year
	cur.ReleaseDate = md.ReleaseDate
	cur.Runtime = md.Runtime
	cur.IMDbID = md.IMDbID
	cur.PosterURL = md.PosterURL
	cur.FanartURL = md.FanartURL
	if err := s.store.UpdateMovie(ctx, *cur); err != nil {
		return err
	}
	s.emitMovie(ctx, id)
	return nil
}

// RefreshAll refreshes every monitored series and movie. It is best-effort per
// item: a single item's provider failure is swallowed so one bad item doesn't
// abort the sweep. It returns an error only if the initial ListSeries/ListMovies
// query fails.
func (s *Service) RefreshAll(ctx context.Context) error {
	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return err
	}
	for _, se := range series {
		if se.Monitored {
			_ = s.RefreshSeries(ctx, se.ID)
		}
	}
	movies, err := s.store.ListMovies(ctx)
	if err != nil {
		return err
	}
	for _, m := range movies {
		if m.Monitored {
			_ = s.RefreshMovie(ctx, m.ID)
		}
	}
	return nil
}

func (s *Service) SetSeriesMonitored(ctx context.Context, id int64, monitored bool) error {
	if err := s.store.SetSeriesMonitored(ctx, id, monitored); err != nil {
		return err
	}
	if err := s.store.SetSeriesEpisodesMonitored(ctx, id, monitored); err != nil {
		return err
	}
	// Cascade to seasons too.
	seasons, err := s.store.ListSeasons(ctx, id)
	if err != nil {
		return err
	}
	for _, sn := range seasons {
		if err := s.store.SetSeasonMonitored(ctx, sn.ID, monitored); err != nil {
			return err
		}
	}
	s.emitSeries(ctx, id)
	return nil
}

// SetSeasonMonitored sets a season and cascades to that season's episodes. It
// needs the owning series id, resolved from the season row.
func (s *Service) SetSeasonMonitored(ctx context.Context, seriesID, seasonID int64, seasonNumber int, monitored bool) error {
	if err := s.store.SetSeasonMonitored(ctx, seasonID, monitored); err != nil {
		return err
	}
	if err := s.store.SetSeasonEpisodesMonitored(ctx, seriesID, seasonNumber, monitored); err != nil {
		return err
	}
	s.emitSeries(ctx, seriesID)
	return nil
}

func (s *Service) SetEpisodeMonitored(ctx context.Context, seriesID, episodeID int64, monitored bool) error {
	if err := s.store.SetEpisodeMonitored(ctx, episodeID, monitored); err != nil {
		return err
	}
	s.emitSeries(ctx, seriesID)
	return nil
}

func (s *Service) SetMovieMonitored(ctx context.Context, id int64, monitored bool) error {
	if err := s.store.SetMovieMonitored(ctx, id, monitored); err != nil {
		return err
	}
	s.emitMovie(ctx, id)
	return nil
}
