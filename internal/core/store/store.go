package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("store: not found")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

type Task struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Progress  int        `json:"progress"`
	Message   string     `json:"message"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
}

// --- settings ---

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// --- users ---

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, passwordHash)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE username = ?`, username))
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE id = ?`, id))
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// --- sessions ---

func (s *Store) CreateSession(ctx context.Context, token string, userID int64, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`, token, userID, expiresAt)
	return err
}

func (s *Store) GetSession(ctx context.Context, token string) (*Session, error) {
	var sess Session
	err := s.db.QueryRowContext(ctx,
		`SELECT token, user_id, expires_at FROM sessions WHERE token = ?`, token).
		Scan(&sess.Token, &sess.UserID, &sess.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// --- tasks ---

func (s *Store) UpsertTask(ctx context.Context, t Task) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (id, name, status, progress, message, started_at, ended_at)
		 VALUES (?, ?, ?, ?, ?,
		         CASE WHEN ? = 'running' THEN CURRENT_TIMESTAMP ELSE NULL END,
		         CASE WHEN ? IN ('completed','failed') THEN CURRENT_TIMESTAMP ELSE NULL END)
		 ON CONFLICT(id) DO UPDATE SET
		   status = excluded.status,
		   progress = excluded.progress,
		   message = excluded.message,
		   updated_at = CURRENT_TIMESTAMP,
		   started_at = CASE WHEN excluded.status = 'running' AND started_at IS NULL
		                     THEN CURRENT_TIMESTAMP ELSE started_at END,
		   ended_at   = CASE WHEN excluded.status IN ('completed','failed')
		                     THEN CURRENT_TIMESTAMP ELSE ended_at END`,
		t.ID, t.Name, t.Status, t.Progress, t.Message, t.Status, t.Status)
	return err
}

func scanTask(sc interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var sa, ea sql.NullTime
	if err := sc.Scan(&t.ID, &t.Name, &t.Status, &t.Progress, &t.Message, &t.CreatedAt, &t.UpdatedAt, &sa, &ea); err != nil {
		return Task{}, err
	}
	if sa.Valid {
		t.StartedAt = &sa.Time
	}
	if ea.Valid {
		t.EndedAt = &ea.Time
	}
	return t, nil
}

func (s *Store) LastTaskByName(ctx context.Context, name string) (*Task, error) {
	t, err := scanTask(s.db.QueryRowContext(ctx,
		`SELECT id, name, status, progress, message, created_at, updated_at, started_at, ended_at
		 FROM tasks WHERE name = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	t, err := scanTask(s.db.QueryRowContext(ctx,
		`SELECT id, name, status, progress, message, created_at, updated_at, started_at, ended_at
		 FROM tasks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTasks(ctx context.Context, limit int) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, status, progress, message, created_at, updated_at, started_at, ended_at
		 FROM tasks ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PruneTasksPerName deletes terminal (completed/failed) task rows beyond the
// newest `keep` per task name. Queued/running rows are never deleted. Returns
// the number of rows removed. Per-name retention keeps every task's most recent
// terminal row so the Scheduled table's Last Execution never goes blank.
func (s *Store) PruneTasksPerName(ctx context.Context, keep int) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM tasks
		 WHERE status IN ('completed','failed')
		   AND id NOT IN (
		     SELECT id FROM (
		       SELECT id, ROW_NUMBER() OVER (
		         PARTITION BY name ORDER BY created_at DESC, rowid DESC) AS rn
		       FROM tasks
		       WHERE status IN ('completed','failed'))
		     WHERE rn <= ?)`, keep)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
