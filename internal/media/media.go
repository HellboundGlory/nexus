// Package media manages TV series and movies (metadata, parsing, import).
// Foundation ships only the package; behavior lands in sub-project 4.
package media

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
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
	TMDBID           int
	RootFolderID     *int64
	MonitorOption    string
	QualityProfileID *int64
}

type AddMovieRequest struct {
	TMDBID           int
	RootFolderID     *int64
	Monitored        bool
	QualityProfileID *int64
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

// validateQualityProfile mirrors validateRootFolder: nil is allowed (no profile),
// an unknown id surfaces ErrInvalidQualityProfile (→400).
func (s *Service) validateQualityProfile(ctx context.Context, id *int64) error {
	if id == nil {
		return nil
	}
	if _, err := s.store.GetQualityProfile(ctx, *id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidQualityProfile
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
	if err := s.validateQualityProfile(ctx, req.QualityProfileID); err != nil {
		return store.Series{}, err
	}
	md, err := s.meta.TVDetails(ctx, req.TMDBID)
	if err != nil {
		return store.Series{}, err
	}
	id, err := s.store.CreateSeries(ctx, store.Series{
		TMDBID: md.TMDBID, Title: md.Title, SortTitle: sortTitle(md.Title), Overview: md.Overview,
		Status: md.Status, FirstAired: md.FirstAired, PosterURL: md.PosterURL, FanartURL: md.FanartURL,
		RootFolderID: req.RootFolderID, QualityProfileID: req.QualityProfileID, Monitored: req.MonitorOption != MonitorNone,
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
	if err := s.validateQualityProfile(ctx, req.QualityProfileID); err != nil {
		return store.Movie{}, err
	}
	md, err := s.meta.MovieDetails(ctx, req.TMDBID)
	if err != nil {
		return store.Movie{}, err
	}
	id, err := s.store.CreateMovie(ctx, store.Movie{
		TMDBID: md.TMDBID, Title: md.Title, SortTitle: sortTitle(md.Title), Overview: md.Overview,
		Status: md.Status, Year: md.Year, ReleaseDate: md.ReleaseDate, Runtime: md.Runtime, IMDbID: md.IMDbID,
		PosterURL: md.PosterURL, FanartURL: md.FanartURL, RootFolderID: req.RootFolderID,
		QualityProfileID: req.QualityProfileID, Monitored: req.Monitored,
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

// SetSeriesQualityProfile validates the profile exists, then assigns it. A
// missing series or profile surfaces store.ErrNotFound (→404 at the handler).
func (s *Service) SetSeriesQualityProfile(ctx context.Context, seriesID, profileID int64) error {
	if _, err := s.store.GetQualityProfile(ctx, profileID); err != nil {
		return err
	}
	pid := profileID
	if err := s.store.SetSeriesQualityProfileID(ctx, seriesID, &pid); err != nil {
		return err
	}
	s.emitSeries(ctx, seriesID)
	return nil
}

// SetMovieQualityProfile validates the profile exists, then assigns it. A
// missing movie or profile surfaces store.ErrNotFound (→404 at the handler).
func (s *Service) SetMovieQualityProfile(ctx context.Context, movieID, profileID int64) error {
	if _, err := s.store.GetQualityProfile(ctx, profileID); err != nil {
		return err
	}
	pid := profileID
	if err := s.store.SetMovieQualityProfileID(ctx, movieID, &pid); err != nil {
		return err
	}
	s.emitMovie(ctx, movieID)
	return nil
}

// DeleteMovieFile removes a movie's imported file: best-effort disk removal
// (real errors logged, never fatal; already-gone counts as success) then the
// DB row is always deleted, flipping the movie back to missing. No-op (nil)
// when the movie has no file.
func (s *Service) DeleteMovieFile(ctx context.Context, movieID int64) error {
	file, err := s.store.MediaFileForMovie(ctx, movieID)
	if err != nil {
		return err
	}
	if file == nil {
		return nil
	}
	m, err := s.store.GetMovie(ctx, movieID)
	if err != nil {
		slog.Warn("media: load movie for file delete failed", "movieId", movieID, "err", err)
	} else if m.RootFolderID != nil {
		if root, err := s.store.GetRootFolder(ctx, *m.RootFolderID); err == nil {
			abs := filepath.Join(root.Path, filepath.FromSlash(file.RelativePath))
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				slog.Warn("media: delete movie file from disk failed", "movieId", movieID, "err", err)
			}
		}
	}
	if err := s.store.DeleteMediaFile(ctx, file.ID); err != nil {
		return err
	}
	s.emitMovie(ctx, movieID)
	return nil
}

// itemFolderTarget returns the absolute folder to remove for a file's relative
// path under rootPath, or "" if it cannot be safely derived. The result is
// always a direct child of rootPath: an empty/"."/".." first segment or any
// path that would escape the root returns "" (so RemoveAll can never hit the
// root itself or climb out of it).
func itemFolderTarget(rootPath, relPath string) string {
	seg := strings.SplitN(filepath.ToSlash(relPath), "/", 2)[0]
	if seg == "" || seg == "." || seg == ".." {
		return ""
	}
	abs := filepath.Join(rootPath, seg)
	rel, err := filepath.Rel(rootPath, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ""
	}
	return abs
}

// removeItemFolders best-effort deletes each distinct item folder derived from
// the given files. Errors are logged, never fatal.
func (s *Service) removeItemFolders(root store.RootFolder, files []store.MediaFile) {
	seen := map[string]bool{}
	for _, f := range files {
		target := itemFolderTarget(root.Path, f.RelativePath)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		if err := os.RemoveAll(target); err != nil {
			slog.Warn("media: delete item folder from disk failed", "path", target, "err", err)
		}
	}
}

// diskTargetsForMovie gathers (root, files) for a movie's disk cleanup, or
// (nil, nil) when deleteFiles is false, the movie has no file, or the root
// can't be resolved. Best-effort; logs on a gather error.
func (s *Service) diskTargetsForMovie(ctx context.Context, id int64, deleteFiles bool) (*store.RootFolder, []store.MediaFile) {
	if !deleteFiles {
		return nil, nil
	}
	file, err := s.store.MediaFileForMovie(ctx, id)
	if err != nil {
		slog.Warn("media: gather movie file for disk delete failed", "movieId", id, "err", err)
		return nil, nil
	}
	if file == nil {
		return nil, nil
	}
	m, err := s.store.GetMovie(ctx, id)
	if err != nil || m.RootFolderID == nil {
		return nil, nil
	}
	root, err := s.store.GetRootFolder(ctx, *m.RootFolderID)
	if err != nil {
		return nil, nil
	}
	return root, []store.MediaFile{*file}
}

// DeleteMovie removes a movie from the library. When deleteFiles is set, its
// on-disk folder is also removed (best-effort, after the DB delete).
func (s *Service) DeleteMovie(ctx context.Context, id int64, deleteFiles bool) error {
	root, files := s.diskTargetsForMovie(ctx, id, deleteFiles)
	if err := s.store.DeleteMovie(ctx, id); err != nil {
		return err
	}
	if root != nil {
		s.removeItemFolders(*root, files)
	}
	return nil
}
