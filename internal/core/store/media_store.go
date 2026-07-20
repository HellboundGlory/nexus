package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrRootFolderInUse is returned by DeleteRootFolder when a series or movie
// still references the root folder.
var ErrRootFolderInUse = errors.New("store: root folder in use")

type RootFolder struct {
	ID        int64     `json:"id"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Store) CreateRootFolder(ctx context.Context, path string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO root_folders (path) VALUES (?)`, path)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetRootFolder(ctx context.Context, id int64) (*RootFolder, error) {
	var rf RootFolder
	err := s.db.QueryRowContext(ctx,
		`SELECT id, path, created_at FROM root_folders WHERE id = ?`, id).
		Scan(&rf.ID, &rf.Path, &rf.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rf, nil
}

func (s *Store) ListRootFolders(ctx context.Context) ([]RootFolder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, path, created_at FROM root_folders ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RootFolder
	for rows.Next() {
		var rf RootFolder
		if err := rows.Scan(&rf.ID, &rf.Path, &rf.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rf)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRootFolder(ctx context.Context, id int64) error {
	var refs int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM series WHERE root_folder_id = ?) +
		        (SELECT COUNT(*) FROM movies WHERE root_folder_id = ?)`, id, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrRootFolderInUse
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM root_folders WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type Series struct {
	ID               int64      `json:"id"`
	TMDBID           int        `json:"tmdbId"`
	Title            string     `json:"title"`
	SortTitle        string     `json:"sortTitle"`
	Overview         string     `json:"overview"`
	Status           string     `json:"status"`
	FirstAired       string     `json:"firstAired"`
	PosterURL        string     `json:"posterUrl"`
	FanartURL        string     `json:"fanartUrl"`
	RootFolderID     *int64     `json:"rootFolderId"`
	QualityProfileID *int64     `json:"qualityProfileId"`
	Monitored        bool       `json:"monitored"`
	AddedAt          time.Time  `json:"addedAt"`
	LastRefreshedAt  *time.Time `json:"lastRefreshedAt"`
}

type Season struct {
	ID           int64 `json:"id"`
	SeriesID     int64 `json:"seriesId"`
	SeasonNumber int   `json:"seasonNumber"`
	Monitored    bool  `json:"monitored"`
}

type Episode struct {
	ID            int64  `json:"id"`
	SeriesID      int64  `json:"seriesId"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
	TMDBID        int    `json:"tmdbId"`
	Title         string `json:"title"`
	Overview      string `json:"overview"`
	AirDate       string `json:"airDate"`
	Monitored     bool   `json:"monitored"`
}

const seriesSelect = `SELECT id, tmdb_id, title, sort_title, overview, status, first_aired,
	poster_url, fanart_url, root_folder_id, quality_profile_id, monitored, added_at, last_refreshed_at FROM series`

func scanSeriesRow(row rowScanner) (*Series, error) {
	var s Series
	var monitored int
	var rootID, qpID sql.NullInt64
	var lastRef sql.NullTime
	err := row.Scan(&s.ID, &s.TMDBID, &s.Title, &s.SortTitle, &s.Overview, &s.Status, &s.FirstAired,
		&s.PosterURL, &s.FanartURL, &rootID, &qpID, &monitored, &s.AddedAt, &lastRef)
	if err != nil {
		return nil, err
	}
	s.Monitored = monitored != 0
	if rootID.Valid {
		s.RootFolderID = &rootID.Int64
	}
	if qpID.Valid {
		s.QualityProfileID = &qpID.Int64
	}
	if lastRef.Valid {
		s.LastRefreshedAt = &lastRef.Time
	}
	return &s, nil
}

func (s *Store) CreateSeries(ctx context.Context, se Series) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO series (tmdb_id, title, sort_title, overview, status, first_aired, poster_url,
		 fanart_url, root_folder_id, quality_profile_id, monitored)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		se.TMDBID, se.Title, se.SortTitle, se.Overview, se.Status, se.FirstAired, se.PosterURL,
		se.FanartURL, se.RootFolderID, se.QualityProfileID, boolToInt(se.Monitored))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetSeries(ctx context.Context, id int64) (*Series, error) {
	se, err := scanSeriesRow(s.db.QueryRowContext(ctx, seriesSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return se, err
}

func (s *Store) ListSeries(ctx context.Context) ([]Series, error) {
	rows, err := s.db.QueryContext(ctx, seriesSelect+` ORDER BY sort_title ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Series
	for rows.Next() {
		se, err := scanSeriesRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *se)
	}
	return out, rows.Err()
}

// UpdateSeries updates descriptive fields only (not monitored — use SetSeriesMonitored).
func (s *Store) UpdateSeries(ctx context.Context, se Series) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE series SET title=?, sort_title=?, overview=?, status=?, first_aired=?, poster_url=?,
		 fanart_url=?, last_refreshed_at=CURRENT_TIMESTAMP WHERE id=?`,
		se.Title, se.SortTitle, se.Overview, se.Status, se.FirstAired, se.PosterURL, se.FanartURL, se.ID)
	return err
}

func (s *Store) DeleteSeries(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM series WHERE id = ?`, id)
	return err
}

// MediaFilesForSeries returns every media file belonging to a series' episodes.
func (s *Store) MediaFilesForSeries(ctx context.Context, seriesID int64) ([]MediaFile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT mf.id, mf.media_kind, mf.episode_id, mf.movie_id, mf.relative_path, mf.size, mf.quality_id, mf.added_at
		 FROM media_files mf JOIN episodes e ON mf.episode_id = e.id
		 WHERE e.series_id = ?`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaFile
	for rows.Next() {
		f, err := scanMediaFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) SetSeriesMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE series SET monitored=? WHERE id=?`, boolToInt(monitored), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpsertSeason(ctx context.Context, se Season) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO seasons (series_id, season_number, monitored) VALUES (?, ?, ?)
		 ON CONFLICT(series_id, season_number) DO NOTHING`,
		se.SeriesID, se.SeasonNumber, boolToInt(se.Monitored))
	return err
}

func (s *Store) ListSeasons(ctx context.Context, seriesID int64) ([]Season, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, series_id, season_number, monitored FROM seasons WHERE series_id=? ORDER BY season_number ASC`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Season
	for rows.Next() {
		var se Season
		var m int
		if err := rows.Scan(&se.ID, &se.SeriesID, &se.SeasonNumber, &m); err != nil {
			return nil, err
		}
		se.Monitored = m != 0
		out = append(out, se)
	}
	return out, rows.Err()
}

func (s *Store) SetSeasonMonitored(ctx context.Context, id int64, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE seasons SET monitored=? WHERE id=?`, boolToInt(monitored), id)
	return err
}

// UpsertEpisode inserts a new episode or updates the descriptive fields of an
// existing one (keyed on series_id+season+episode). It does NOT touch monitored
// on update, so user/season monitoring choices survive a refresh.
func (s *Store) UpsertEpisode(ctx context.Context, e Episode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO episodes (series_id, season_number, episode_number, tmdb_id, title, overview, air_date, monitored)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(series_id, season_number, episode_number)
		 DO UPDATE SET tmdb_id=excluded.tmdb_id, title=excluded.title, overview=excluded.overview, air_date=excluded.air_date`,
		e.SeriesID, e.SeasonNumber, e.EpisodeNumber, e.TMDBID, e.Title, e.Overview, e.AirDate, boolToInt(e.Monitored))
	return err
}

func (s *Store) ListEpisodes(ctx context.Context, seriesID int64) ([]Episode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, series_id, season_number, episode_number, tmdb_id, title, overview, air_date, monitored
		 FROM episodes WHERE series_id=? ORDER BY season_number ASC, episode_number ASC`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Episode
	for rows.Next() {
		var e Episode
		var m int
		if err := rows.Scan(&e.ID, &e.SeriesID, &e.SeasonNumber, &e.EpisodeNumber, &e.TMDBID,
			&e.Title, &e.Overview, &e.AirDate, &m); err != nil {
			return nil, err
		}
		e.Monitored = m != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SetEpisodeMonitored(ctx context.Context, id int64, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE episodes SET monitored=? WHERE id=?`, boolToInt(monitored), id)
	return err
}

func (s *Store) SetSeasonEpisodesMonitored(ctx context.Context, seriesID int64, seasonNumber int, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE episodes SET monitored=? WHERE series_id=? AND season_number=?`,
		boolToInt(monitored), seriesID, seasonNumber)
	return err
}

func (s *Store) SetSeriesEpisodesMonitored(ctx context.Context, seriesID int64, monitored bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE episodes SET monitored=? WHERE series_id=?`, boolToInt(monitored), seriesID)
	return err
}

// HasMonitoredEpisodes reports whether the series has at least one monitored
// episode. Automation's sweeps use it instead of the series' own monitored flag:
// a series row can be unmonitored while individual episodes inside it are
// monitored, and those episodes are still wanted. Kept as an EXISTS so a sweep
// can skip a series with nothing monitored without loading its episode list —
// a long-running show carries well over a thousand episode rows.
func (s *Store) HasMonitoredEpisodes(ctx context.Context, seriesID int64) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM episodes WHERE series_id=? AND monitored=1)`, seriesID).Scan(&found)
	if err != nil {
		return false, err
	}
	return found == 1, nil
}

// CalendarEpisode is an episode joined to its parent series title, for the
// calendar view (episodes carry no series title of their own).
type CalendarEpisode struct {
	Episode
	SeriesTitle string `json:"seriesTitle"`
}

// CalendarEpisodes returns monitored episodes of monitored series whose air_date
// falls within [start, end] inclusive. Dates are "YYYY-MM-DD" strings compared
// lexically; empty air_date rows fall below any start bound and are excluded.
func (s *Store) CalendarEpisodes(ctx context.Context, start, end string) ([]CalendarEpisode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.series_id, e.season_number, e.episode_number, e.tmdb_id,
		       e.title, e.overview, e.air_date, e.monitored, s.title
		FROM episodes e JOIN series s ON e.series_id = s.id
		WHERE s.monitored = 1 AND e.monitored = 1
		  AND e.air_date >= ? AND e.air_date <= ?
		ORDER BY e.air_date, s.sort_title, e.season_number, e.episode_number`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarEpisode
	for rows.Next() {
		var c CalendarEpisode
		var m int
		if err := rows.Scan(&c.ID, &c.SeriesID, &c.SeasonNumber, &c.EpisodeNumber, &c.TMDBID,
			&c.Title, &c.Overview, &c.AirDate, &m, &c.SeriesTitle); err != nil {
			return nil, err
		}
		c.Monitored = m != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

type Movie struct {
	ID               int64      `json:"id"`
	TMDBID           int        `json:"tmdbId"`
	Title            string     `json:"title"`
	SortTitle        string     `json:"sortTitle"`
	Overview         string     `json:"overview"`
	Status           string     `json:"status"`
	Year             int        `json:"year"`
	ReleaseDate      string     `json:"releaseDate"`
	Runtime          int        `json:"runtime"`
	IMDbID           string     `json:"imdbId"`
	PosterURL        string     `json:"posterUrl"`
	FanartURL        string     `json:"fanartUrl"`
	RootFolderID     *int64     `json:"rootFolderId"`
	QualityProfileID *int64     `json:"qualityProfileId"`
	Monitored        bool       `json:"monitored"`
	AddedAt          time.Time  `json:"addedAt"`
	LastRefreshedAt  *time.Time `json:"lastRefreshedAt"`
}

const movieSelect = `SELECT id, tmdb_id, title, sort_title, overview, status, year, release_date,
	runtime, imdb_id, poster_url, fanart_url, root_folder_id, quality_profile_id, monitored,
	added_at, last_refreshed_at FROM movies`

func scanMovieRow(row rowScanner) (*Movie, error) {
	var m Movie
	var monitored int
	var rootID, qpID sql.NullInt64
	var lastRef sql.NullTime
	err := row.Scan(&m.ID, &m.TMDBID, &m.Title, &m.SortTitle, &m.Overview, &m.Status, &m.Year,
		&m.ReleaseDate, &m.Runtime, &m.IMDbID, &m.PosterURL, &m.FanartURL, &rootID, &qpID,
		&monitored, &m.AddedAt, &lastRef)
	if err != nil {
		return nil, err
	}
	m.Monitored = monitored != 0
	if rootID.Valid {
		m.RootFolderID = &rootID.Int64
	}
	if qpID.Valid {
		m.QualityProfileID = &qpID.Int64
	}
	if lastRef.Valid {
		m.LastRefreshedAt = &lastRef.Time
	}
	return &m, nil
}

func (s *Store) CreateMovie(ctx context.Context, m Movie) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO movies (tmdb_id, title, sort_title, overview, status, year, release_date, runtime,
		 imdb_id, poster_url, fanart_url, root_folder_id, quality_profile_id, monitored)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.TMDBID, m.Title, m.SortTitle, m.Overview, m.Status, m.Year, m.ReleaseDate, m.Runtime,
		m.IMDbID, m.PosterURL, m.FanartURL, m.RootFolderID, m.QualityProfileID, boolToInt(m.Monitored))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetMovie(ctx context.Context, id int64) (*Movie, error) {
	m, err := scanMovieRow(s.db.QueryRowContext(ctx, movieSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) ListMovies(ctx context.Context) ([]Movie, error) {
	rows, err := s.db.QueryContext(ctx, movieSelect+` ORDER BY sort_title ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movie
	for rows.Next() {
		m, err := scanMovieRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// CalendarMovies returns monitored movies whose release_date falls within
// [start, end] inclusive, ordered by release_date then sort_title. Empty
// release_date rows are excluded by the >= start bound.
func (s *Store) CalendarMovies(ctx context.Context, start, end string) ([]Movie, error) {
	rows, err := s.db.QueryContext(ctx, movieSelect+`
		WHERE monitored = 1 AND release_date >= ? AND release_date <= ?
		ORDER BY release_date, sort_title`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movie
	for rows.Next() {
		m, err := scanMovieRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// UpdateMovie updates descriptive fields only (not monitored — use SetMovieMonitored).
func (s *Store) UpdateMovie(ctx context.Context, m Movie) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE movies SET title=?, sort_title=?, overview=?, status=?, year=?, release_date=?, runtime=?,
		 imdb_id=?, poster_url=?, fanart_url=?, last_refreshed_at=CURRENT_TIMESTAMP WHERE id=?`,
		m.Title, m.SortTitle, m.Overview, m.Status, m.Year, m.ReleaseDate, m.Runtime,
		m.IMDbID, m.PosterURL, m.FanartURL, m.ID)
	return err
}

func (s *Store) DeleteMovie(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM movies WHERE id = ?`, id)
	return err
}

func (s *Store) SetMovieMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE movies SET monitored=? WHERE id=?`, boolToInt(monitored), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetSeason(ctx context.Context, id int64) (*Season, error) {
	var se Season
	var m int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, series_id, season_number, monitored FROM seasons WHERE id = ?`, id).
		Scan(&se.ID, &se.SeriesID, &se.SeasonNumber, &m)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	se.Monitored = m != 0
	return &se, nil
}

func (s *Store) GetEpisode(ctx context.Context, id int64) (*Episode, error) {
	var e Episode
	var m int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, series_id, season_number, episode_number, tmdb_id, title, overview, air_date, monitored
		 FROM episodes WHERE id = ?`, id).
		Scan(&e.ID, &e.SeriesID, &e.SeasonNumber, &e.EpisodeNumber, &e.TMDBID, &e.Title, &e.Overview, &e.AirDate, &m)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Monitored = m != 0
	return &e, nil
}
