package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type DownloadClient struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Implementation string     `json:"implementation"`
	Protocol       string     `json:"protocol"`
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	UseSSL         bool       `json:"useSsl"`
	URLBase        string     `json:"urlBase"`
	Username       string     `json:"username"`
	APIKey         string     `json:"-"` // write-only: SABnzbd key or qBittorrent password
	Category       string     `json:"category"`
	Enabled        bool       `json:"enabled"`
	Priority       int        `json:"priority"`
	Settings       string     `json:"settings"` // raw JSON object
	Status         string     `json:"status"`
	LastCheck      *time.Time `json:"lastCheck"`
	FailMessage    string     `json:"failMessage"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func (s *Store) CreateDownloadClient(ctx context.Context, dc DownloadClient) (int64, error) {
	settings := dc.Settings
	if settings == "" {
		settings = "{}"
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO download_clients
		 (name, implementation, protocol, host, port, use_ssl, url_base, username, api_key, category, enabled, priority, settings)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dc.Name, dc.Implementation, dc.Protocol, dc.Host, dc.Port, boolToInt(dc.UseSSL), dc.URLBase,
		dc.Username, dc.APIKey, dc.Category, boolToInt(dc.Enabled), dc.Priority, settings)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetDownloadClient(ctx context.Context, id int64) (*DownloadClient, error) {
	dc, err := scanDownloadClientRow(s.db.QueryRowContext(ctx, downloadClientSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return dc, err
}

func (s *Store) ListDownloadClients(ctx context.Context, enabledOnly bool) ([]DownloadClient, error) {
	q := downloadClientSelect
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY priority ASC, id ASC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DownloadClient
	for rows.Next() {
		dc, err := scanDownloadClientRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *dc)
	}
	return out, rows.Err()
}

func (s *Store) UpdateDownloadClient(ctx context.Context, dc DownloadClient) error {
	settings := dc.Settings
	if settings == "" {
		settings = "{}"
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE download_clients SET name=?, implementation=?, protocol=?, host=?, port=?, use_ssl=?,
		 url_base=?, username=?, api_key=?, category=?, enabled=?, priority=?, settings=?,
		 updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		dc.Name, dc.Implementation, dc.Protocol, dc.Host, dc.Port, boolToInt(dc.UseSSL), dc.URLBase,
		dc.Username, dc.APIKey, dc.Category, boolToInt(dc.Enabled), dc.Priority, settings, dc.ID)
	return err
}

func (s *Store) DeleteDownloadClient(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM download_clients WHERE id = ?`, id)
	return err
}

func (s *Store) SetDownloadClientStatus(ctx context.Context, id int64, status, failMessage string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE download_clients SET status=?, fail_message=?, last_check=CURRENT_TIMESTAMP,
		 updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		status, failMessage, id)
	return err
}

const downloadClientSelect = `SELECT id, name, implementation, protocol, host, port, use_ssl, url_base,
	username, api_key, category, enabled, priority, settings, status, last_check, fail_message,
	created_at, updated_at FROM download_clients`

func scanDownloadClientRow(row rowScanner) (*DownloadClient, error) {
	var dc DownloadClient
	var useSSL, enabled int
	var lastCheck sql.NullTime
	err := row.Scan(&dc.ID, &dc.Name, &dc.Implementation, &dc.Protocol, &dc.Host, &dc.Port, &useSSL,
		&dc.URLBase, &dc.Username, &dc.APIKey, &dc.Category, &enabled, &dc.Priority, &dc.Settings,
		&dc.Status, &lastCheck, &dc.FailMessage, &dc.CreatedAt, &dc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	dc.UseSSL = useSSL != 0
	dc.Enabled = enabled != 0
	if lastCheck.Valid {
		dc.LastCheck = &lastCheck.Time
	}
	return &dc, nil
}
