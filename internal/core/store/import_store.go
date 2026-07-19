package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Queue lifecycle statuses.
const (
	QueueGrabbed   = "grabbed"
	QueueImporting = "importing"
	QueueImported  = "imported"
	QueueFailed    = "failed"
)

// QueueItem is one grab-tracked download awaiting or having completed import.
type QueueItem struct {
	ID               int64     `json:"id"`
	DownloadClientID string    `json:"downloadClientId"`
	ClientItemID     string    `json:"clientItemId"`
	Protocol         string    `json:"protocol"`
	SourceTitle      string    `json:"sourceTitle"`
	MediaKind        string    `json:"mediaKind"`
	SeriesID         *int64    `json:"seriesId,omitempty"`
	MovieID          *int64    `json:"movieId,omitempty"`
	EpisodeIDs       []int64   `json:"episodeIds"`
	QualityID        int       `json:"qualityId"`
	Status           string    `json:"status"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func (s *Store) EnqueueGrab(ctx context.Context, q QueueItem) (QueueItem, error) {
	eps, err := json.Marshal(q.EpisodeIDs)
	if err != nil {
		return QueueItem{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO download_queue
		 (download_client_id, client_item_id, protocol, source_title, media_kind,
		  series_id, movie_id, episode_ids, quality_id, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.DownloadClientID, q.ClientItemID, q.Protocol, q.SourceTitle, q.MediaKind,
		q.SeriesID, q.MovieID, string(eps), q.QualityID, q.Status)
	if err != nil {
		return QueueItem{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetQueueItem(ctx, id)
}

func scanQueueItem(sc interface{ Scan(...any) error }) (QueueItem, error) {
	var (
		q   QueueItem
		eps string
	)
	if err := sc.Scan(&q.ID, &q.DownloadClientID, &q.ClientItemID, &q.Protocol, &q.SourceTitle,
		&q.MediaKind, &q.SeriesID, &q.MovieID, &eps, &q.QualityID, &q.Status, &q.Error,
		&q.CreatedAt, &q.UpdatedAt); err != nil {
		return QueueItem{}, err
	}
	if err := json.Unmarshal([]byte(eps), &q.EpisodeIDs); err != nil {
		return QueueItem{}, err
	}
	return q, nil
}

const queueCols = `id, download_client_id, client_item_id, protocol, source_title, media_kind,
	series_id, movie_id, episode_ids, quality_id, status, error, created_at, updated_at`

func (s *Store) GetQueueItem(ctx context.Context, id int64) (QueueItem, error) {
	q, err := scanQueueItem(s.db.QueryRowContext(ctx, `SELECT `+queueCols+` FROM download_queue WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return QueueItem{}, ErrNotFound
	}
	return q, err
}

func (s *Store) ListQueue(ctx context.Context) ([]QueueItem, error) {
	return s.queueQuery(ctx, `SELECT `+queueCols+` FROM download_queue ORDER BY id`)
}

func (s *Store) QueueByStatus(ctx context.Context, status string) ([]QueueItem, error) {
	return s.queueQuery(ctx, `SELECT `+queueCols+` FROM download_queue WHERE status = ? ORDER BY id`, status)
}

func (s *Store) queueQuery(ctx context.Context, query string, args ...any) ([]QueueItem, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueItem
	for rows.Next() {
		q, err := scanQueueItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) SetQueueStatus(ctx context.Context, id int64, status, errMsg string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE download_queue SET status = ?, error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, errMsg, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteQueueItem(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM download_queue WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MediaFile is one imported file on disk, linked to an episode or a movie.
type MediaFile struct {
	ID           int64     `json:"id"`
	MediaKind    string    `json:"mediaKind"`
	EpisodeID    *int64    `json:"episodeId,omitempty"`
	MovieID      *int64    `json:"movieId,omitempty"`
	RelativePath string    `json:"relativePath"`
	Size         int64     `json:"size"`
	QualityID    int       `json:"qualityId"`
	AddedAt      time.Time `json:"addedAt"`
}

// UpsertMediaFile inserts a file row, replacing any existing file for the same
// episode or movie (one current file per item, enforced by UNIQUE constraints).
func (s *Store) UpsertMediaFile(ctx context.Context, f MediaFile) (MediaFile, error) {
	if f.EpisodeID != nil {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM media_files WHERE episode_id = ?`, *f.EpisodeID); err != nil {
			return MediaFile{}, err
		}
	}
	if f.MovieID != nil {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM media_files WHERE movie_id = ?`, *f.MovieID); err != nil {
			return MediaFile{}, err
		}
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO media_files (media_kind, episode_id, movie_id, relative_path, size, quality_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		f.MediaKind, f.EpisodeID, f.MovieID, f.RelativePath, f.Size, f.QualityID)
	if err != nil {
		return MediaFile{}, err
	}
	id, _ := res.LastInsertId()
	return s.getMediaFile(ctx, id)
}

func (s *Store) getMediaFile(ctx context.Context, id int64) (MediaFile, error) {
	f, err := scanMediaFile(s.db.QueryRowContext(ctx,
		`SELECT id, media_kind, episode_id, movie_id, relative_path, size, quality_id, added_at FROM media_files WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return MediaFile{}, ErrNotFound
	}
	return f, err
}

func scanMediaFile(sc interface{ Scan(...any) error }) (MediaFile, error) {
	var f MediaFile
	err := sc.Scan(&f.ID, &f.MediaKind, &f.EpisodeID, &f.MovieID, &f.RelativePath, &f.Size, &f.QualityID, &f.AddedAt)
	return f, err
}

func (s *Store) MediaFileForEpisode(ctx context.Context, episodeID int64) (*MediaFile, error) {
	return s.mediaFileWhere(ctx, "episode_id", episodeID)
}

func (s *Store) MediaFileForMovie(ctx context.Context, movieID int64) (*MediaFile, error) {
	return s.mediaFileWhere(ctx, "movie_id", movieID)
}

func (s *Store) mediaFileWhere(ctx context.Context, col string, id int64) (*MediaFile, error) {
	f, err := scanMediaFile(s.db.QueryRowContext(ctx,
		`SELECT id, media_kind, episode_id, movie_id, relative_path, size, quality_id, added_at FROM media_files WHERE `+col+` = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) DeleteMediaFile(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// HistoryEvent is one append-only library event.
type HistoryEvent struct {
	ID          int64     `json:"id"`
	EventType   string    `json:"eventType"`
	MediaKind   string    `json:"mediaKind"`
	SeriesID    *int64    `json:"seriesId,omitempty"`
	EpisodeID   *int64    `json:"episodeId,omitempty"`
	MovieID     *int64    `json:"movieId,omitempty"`
	SourceTitle string    `json:"sourceTitle,omitempty"`
	QualityID   *int      `json:"qualityId,omitempty"`
	Message     string    `json:"message,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Store) AddHistory(ctx context.Context, h HistoryEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO history (event_type, media_kind, series_id, episode_id, movie_id, source_title, quality_id, message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		h.EventType, h.MediaKind, h.SeriesID, h.EpisodeID, h.MovieID, h.SourceTitle, h.QualityID, h.Message)
	return err
}

func (s *Store) ListHistory(ctx context.Context, limit int) ([]HistoryEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, media_kind, series_id, episode_id, movie_id, source_title, quality_id, message, created_at
		 FROM history ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEvent
	for rows.Next() {
		var h HistoryEvent
		if err := rows.Scan(&h.ID, &h.EventType, &h.MediaKind, &h.SeriesID, &h.EpisodeID, &h.MovieID,
			&h.SourceTitle, &h.QualityID, &h.Message, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// defaultPageSize is the fallback when a caller passes a non-positive limit.
const defaultPageSize = 50

// ListHistoryPage returns one page of history newest-first plus the total row
// count across all pages. Ordering matches ListHistory so paging is stable.
func (s *Store) ListHistoryPage(ctx context.Context, offset, limit int) ([]HistoryEvent, int, error) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM history`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, media_kind, series_id, episode_id, movie_id, source_title, quality_id, message, created_at
		 FROM history ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []HistoryEvent{}
	for rows.Next() {
		var h HistoryEvent
		if err := rows.Scan(&h.ID, &h.EventType, &h.MediaKind, &h.SeriesID, &h.EpisodeID, &h.MovieID,
			&h.SourceTitle, &h.QualityID, &h.Message, &h.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// ClearHistory deletes every history row, returning how many were removed.
func (s *Store) ClearHistory(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM history`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MovieFileIDs returns the set of movie ids that currently have a file.
func (s *Store) MovieFileIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT movie_id FROM media_files WHERE movie_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// EpisodeFileIDs returns the set of episode ids that currently have a file.
func (s *Store) EpisodeFileIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT episode_id FROM media_files WHERE episode_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SeriesEpisodeStats reports, per series, the count of monitored episodes and
// how many of those have a file. Series with no monitored episodes are absent
// from the map (callers treat a missing key as the zero value).
type SeriesEpisodeStats struct {
	EpisodeCount     int
	EpisodeFileCount int
}

func (s *Store) SeriesEpisodeStats(ctx context.Context) (map[int64]SeriesEpisodeStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.series_id,
		       SUM(CASE WHEN e.monitored = 1 THEN 1 ELSE 0 END) AS monitored_count,
		       SUM(CASE WHEN e.monitored = 1 AND mf.episode_id IS NOT NULL THEN 1 ELSE 0 END) AS monitored_with_file
		FROM episodes e
		LEFT JOIN media_files mf ON mf.episode_id = e.id
		GROUP BY e.series_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]SeriesEpisodeStats)
	for rows.Next() {
		var seriesID int64
		var count, withFile int
		if err := rows.Scan(&seriesID, &count, &withFile); err != nil {
			return nil, err
		}
		out[seriesID] = SeriesEpisodeStats{EpisodeCount: count, EpisodeFileCount: withFile}
	}
	return out, rows.Err()
}

// sqliteTimeLayout matches SQLite's CURRENT_TIMESTAMP text format so a bound
// time compares correctly (lexicographically == chronologically) against
// created_at values written by the DB default.
const sqliteTimeLayout = "2006-01-02 15:04:05"

// GrabbedSince returns "grabbed" history events created at or after since,
// newest first. Used by the automation upgrade sweep to build its cooldown guard.
func (s *Store) GrabbedSince(ctx context.Context, since time.Time) ([]HistoryEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, media_kind, series_id, episode_id, movie_id, source_title, quality_id, message, created_at
		 FROM history WHERE event_type = 'grabbed' AND created_at >= ? ORDER BY id DESC`,
		since.UTC().Format(sqliteTimeLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEvent
	for rows.Next() {
		var h HistoryEvent
		if err := rows.Scan(&h.ID, &h.EventType, &h.MediaKind, &h.SeriesID, &h.EpisodeID, &h.MovieID,
			&h.SourceTitle, &h.QualityID, &h.Message, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
