package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

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
	_, err := s.db.ExecContext(ctx, `DELETE FROM root_folders WHERE id = ?`, id)
	return err
}
