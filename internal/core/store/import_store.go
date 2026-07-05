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
