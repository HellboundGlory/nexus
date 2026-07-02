package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type Indexer struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Implementation string     `json:"implementation"`
	BaseURL        string     `json:"baseUrl"`
	APIKey         string     `json:"apiKey"`
	Enabled        bool       `json:"enabled"`
	Priority       int        `json:"priority"`
	Categories     []int      `json:"categories"`
	Settings       string     `json:"settings"` // raw JSON object
	Caps           string     `json:"-"`        // raw JSON caps cache
	Status         string     `json:"status"`
	LastCheck      *time.Time `json:"lastCheck"`
	FailMessage    string     `json:"failMessage"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func (s *Store) CreateIndexer(ctx context.Context, ix Indexer) (int64, error) {
	cats, err := json.Marshal(nonNilInts(ix.Categories))
	if err != nil {
		return 0, err
	}
	settings := ix.Settings
	if settings == "" {
		settings = "{}"
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO indexers (name, implementation, base_url, api_key, enabled, priority, categories, settings)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ix.Name, ix.Implementation, ix.BaseURL, ix.APIKey, boolToInt(ix.Enabled), ix.Priority, string(cats), settings)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetIndexer(ctx context.Context, id int64) (*Indexer, error) {
	return s.scanIndexer(s.db.QueryRowContext(ctx, indexerSelect+` WHERE id = ?`, id))
}

func (s *Store) ListIndexers(ctx context.Context, enabledOnly bool) ([]Indexer, error) {
	q := indexerSelect
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY priority ASC, id ASC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Indexer
	for rows.Next() {
		ix, err := scanIndexerRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ix)
	}
	return out, rows.Err()
}

func (s *Store) UpdateIndexer(ctx context.Context, ix Indexer) error {
	cats, err := json.Marshal(nonNilInts(ix.Categories))
	if err != nil {
		return err
	}
	settings := ix.Settings
	if settings == "" {
		settings = "{}"
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE indexers SET name=?, implementation=?, base_url=?, api_key=?, enabled=?, priority=?,
		 categories=?, settings=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		ix.Name, ix.Implementation, ix.BaseURL, ix.APIKey, boolToInt(ix.Enabled), ix.Priority,
		string(cats), settings, ix.ID)
	return err
}

func (s *Store) DeleteIndexer(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM indexers WHERE id = ?`, id)
	return err
}

func (s *Store) SetIndexerStatus(ctx context.Context, id int64, status, failMessage, caps string) error {
	if caps == "" {
		_, err := s.db.ExecContext(ctx,
			`UPDATE indexers SET status=?, fail_message=?, last_check=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			status, failMessage, id)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE indexers SET status=?, fail_message=?, caps=?, last_check=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		status, failMessage, caps, id)
	return err
}

const indexerSelect = `SELECT id, name, implementation, base_url, api_key, enabled, priority,
	categories, settings, caps, status, last_check, fail_message, created_at, updated_at FROM indexers`

func (s *Store) scanIndexer(row *sql.Row) (*Indexer, error) {
	ix, err := scanIndexerRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ix, err
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanIndexerRow(row rowScanner) (*Indexer, error) {
	var ix Indexer
	var cats string
	var enabled int
	var lastCheck sql.NullTime
	err := row.Scan(&ix.ID, &ix.Name, &ix.Implementation, &ix.BaseURL, &ix.APIKey, &enabled, &ix.Priority,
		&cats, &ix.Settings, &ix.Caps, &ix.Status, &lastCheck, &ix.FailMessage, &ix.CreatedAt, &ix.UpdatedAt)
	if err != nil {
		return nil, err
	}
	ix.Enabled = enabled != 0
	if lastCheck.Valid {
		ix.LastCheck = &lastCheck.Time
	}
	if cats != "" {
		if err := json.Unmarshal([]byte(cats), &ix.Categories); err != nil {
			return nil, err
		}
	}
	return &ix, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nonNilInts(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}
