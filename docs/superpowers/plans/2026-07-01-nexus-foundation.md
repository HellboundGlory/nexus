# Nexus Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Foundation sub-project of Nexus — the single-binary base (config, DB, event bus, scheduler/commands, auth, HTTP+WebSocket API, SPA serving, provider interfaces) every later module stands on.

**Architecture:** A Go modular monolith. Internal modules never import each other; they communicate through an in-process event bus and a shared SQLite `Store`. External integrations sit behind provider interfaces (declared here, implemented later). The built React SPA is embedded and served from the same binary/port.

**Tech Stack:** Go 1.22+, `github.com/go-chi/chi/v5`, `modernc.org/sqlite` (pure-Go, no CGO), `github.com/gorilla/websocket`, `golang.org/x/crypto/argon2`, stdlib `log/slog`, stdlib `embed`.

## Global Constraints

- **Language/version:** Go 1.22 or newer (uses `log/slog`, `math/rand/v2` not required).
- **No CGO:** SQLite driver is `modernc.org/sqlite`. Builds must succeed with `CGO_ENABLED=0`.
- **Module boundaries:** packages under `internal/indexer`, `internal/downloadclient`, `internal/media`, `internal/automation` MUST NOT import each other. They may import `internal/core/*` only. Enforced by review.
- **Data layer:** hand-written `database/sql` behind the `Store` interface (deviation from spec's sqlc — same intent, kept for self-contained tasks; interface is stable so sqlc can replace the impl later).
- **Password hashing:** argon2id via `golang.org/x/crypto/argon2`. Never store plaintext.
- **API surface:** REST under `/api/v1`, WebSocket at `/api/v1/ws`, consistent JSON error envelope `{"error":{"code":"...","message":"..."}}`.
- **Commits:** conventional-commit prefixes (`feat:`, `test:`, `chore:`, `docs:`). Commit at the end of each task.
- **Module path:** `github.com/hellboundg/nexus` (used in all import paths below).

---

## File Structure

| File | Responsibility |
|------|----------------|
| `go.mod`, `Makefile` | Module + build/test/lint targets |
| `cmd/nexus/main.go` | Composition root: load config, wire modules, start server, graceful shutdown |
| `internal/core/logging/logging.go` | `slog` logger: console + rotating file |
| `internal/core/config/config.go` | Bootstrap config from env → file → defaults |
| `internal/core/database/database.go` | Open SQLite, run embedded migrations |
| `internal/core/database/migrations/*.sql` | Versioned schema files (embedded) |
| `internal/core/store/store.go` + `*_store.go` | Typed data access (settings, users, sessions, tasks) behind `Store` iface |
| `internal/core/events/events.go` | Typed in-process pub/sub bus |
| `internal/core/command/command.go` | Command interface, manager, worker pool, progress |
| `internal/core/scheduler/scheduler.go` | Cron/interval registration driving commands |
| `internal/core/auth/auth.go` | argon2id hashing, session + API-key auth, middleware |
| `internal/core/api/api.go`, `errors.go`, `system.go` | chi router, error envelope, middleware, system/health routes |
| `internal/core/api/ws.go` | WebSocket hub bridging event bus → clients |
| `internal/core/provider/provider.go` | `Indexer`/`DownloadClient`/`MetadataProvider` interfaces + registries |
| `web/embed.go` + `web/dist/` | Embed built SPA, SPA-fallback file server |

---

## Task 1: Project scaffold & smoke test

**Files:**
- Create: `go.mod`, `Makefile`, `internal/core/version/version.go`, `internal/core/version/version_test.go`

**Interfaces:**
- Produces: `version.Version() string` (returns build version string, default `"dev"`).

- [ ] **Step 1: Initialize module**

Run:
```bash
go mod init github.com/hellboundg/nexus
go get github.com/go-chi/chi/v5 modernc.org/sqlite github.com/gorilla/websocket golang.org/x/crypto/argon2
```

- [ ] **Step 2: Write the failing test**

Create `internal/core/version/version_test.go`:
```go
package version

import "testing"

func TestVersionDefault(t *testing.T) {
	if got := Version(); got != "dev" {
		t.Fatalf("Version() = %q, want %q", got, "dev")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/core/version/...`
Expected: FAIL — `undefined: Version`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/core/version/version.go`:
```go
package version

// value is overridden at build time via -ldflags "-X ...version.value=..."
var value = "dev"

// Version returns the build version string.
func Version() string { return value }
```

- [ ] **Step 5: Add Makefile**

Create `Makefile`:
```makefile
.PHONY: build test lint run
build:
	CGO_ENABLED=0 go build -o nexus ./cmd/nexus
test:
	go test ./...
run:
	go run ./cmd/nexus
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum Makefile internal/core/version
git commit -m "chore: scaffold Go module and version package"
```

---

## Task 2: Structured logging

**Files:**
- Create: `internal/core/logging/logging.go`, `internal/core/logging/logging_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `logging.New(level string, w io.Writer) *slog.Logger` — parses level (`debug|info|warn|error`, default `info`) and returns a JSON `slog.Logger` writing to `w`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/logging/logging_test.go`:
```go
package logging

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewLogsAtInfo(t *testing.T) {
	var buf bytes.Buffer
	log := New("info", &buf)
	log.Info("hello", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line not JSON: %v", err)
	}
	if rec["msg"] != "hello" || rec["k"] != "v" {
		t.Fatalf("unexpected record: %v", rec)
	}
}

func TestDebugSuppressedAtInfo(t *testing.T) {
	var buf bytes.Buffer
	log := New("info", &buf)
	log.Debug("secret")
	if buf.Len() != 0 {
		t.Fatalf("debug should be suppressed at info level, got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/logging/...`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/logging/logging.go`:
```go
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// New returns a JSON slog.Logger writing to w at the given level.
func New(level string, w io.Writer) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/logging/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/logging
git commit -m "feat: add structured slog logging"
```

> Note: rotating-file output is wired in `main.go` (Task 13) by passing an `io.MultiWriter(os.Stdout, rotatingFile)`. The rotating file uses `gopkg.in/natefinch/lumberjack.v2`; add it with `go get gopkg.in/natefinch/lumberjack.v2` when reaching Task 13.

---

## Task 3: Bootstrap configuration

**Files:**
- Create: `internal/core/config/config.go`, `internal/core/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Config struct { DataDir string; Host string; Port int; URLBase string; LogLevel string; APIKey string }`
  - `config.Load(getenv func(string) string) (*Config, error)` — precedence env → (file, later) → default. Generates a random `APIKey` if unset.
  - `func (c *Config) Addr() string` → `"host:port"`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/config/config_test.go`:
```go
package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	c, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 9494 || c.Host != "0.0.0.0" || c.LogLevel != "info" {
		t.Fatalf("bad defaults: %+v", c)
	}
	if c.APIKey == "" {
		t.Fatal("APIKey should be generated when unset")
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	env := map[string]string{
		"NEXUS_PORT":      "8080",
		"NEXUS_LOG_LEVEL": "debug",
		"NEXUS_API_KEY":   "fixedkey",
	}
	c, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8080 || c.LogLevel != "debug" || c.APIKey != "fixedkey" {
		t.Fatalf("env not applied: %+v", c)
	}
}

func TestLoadRejectsBadPort(t *testing.T) {
	env := map[string]string{"NEXUS_PORT": "notanumber"}
	if _, err := Load(func(k string) string { return env[k] }); err == nil {
		t.Fatal("expected error for non-numeric port")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/config/...`
Expected: FAIL — `undefined: Load`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/config/config.go`:
```go
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
)

type Config struct {
	DataDir  string
	Host     string
	Port     int
	URLBase  string
	LogLevel string
	APIKey   string
}

func (c *Config) Addr() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

// Load builds Config from environment (via getenv), falling back to defaults.
func Load(getenv func(string) string) (*Config, error) {
	c := &Config{
		DataDir:  "./data",
		Host:     "0.0.0.0",
		Port:     9494,
		URLBase:  "",
		LogLevel: "info",
	}
	if v := getenv("NEXUS_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := getenv("NEXUS_HOST"); v != "" {
		c.Host = v
	}
	if v := getenv("NEXUS_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid NEXUS_PORT %q: %w", v, err)
		}
		c.Port = p
	}
	if v := getenv("NEXUS_URL_BASE"); v != "" {
		c.URLBase = v
	}
	if v := getenv("NEXUS_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := getenv("NEXUS_API_KEY"); v != "" {
		c.APIKey = v
	} else {
		key, err := generateAPIKey()
		if err != nil {
			return nil, err
		}
		c.APIKey = key
	}
	return c, nil
}

func generateAPIKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/config/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/config
git commit -m "feat: add bootstrap configuration"
```

---

## Task 4: SQLite database & migrations

**Files:**
- Create: `internal/core/database/database.go`, `internal/core/database/database_test.go`, `internal/core/database/migrations/0001_init.sql`

**Interfaces:**
- Produces:
  - `database.Open(path string) (*sql.DB, error)` — opens modernc SQLite with WAL + foreign keys.
  - `database.Migrate(db *sql.DB) error` — applies embedded `migrations/*.sql` in filename order, idempotently, tracked in `schema_migrations`.

- [ ] **Step 1: Write the migration**

Create `internal/core/database/migrations/0001_init.sql`:
```sql
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL
);

CREATE TABLE tasks (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL,
    progress    INTEGER NOT NULL DEFAULT 0,
    message     TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 2: Write the failing test**

Create `internal/core/database/database_test.go`:
```go
package database

import (
	"path/filepath"
	"testing"
)

func TestOpenAndMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate (idempotent): %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM settings`).Scan(&n); err != nil {
		t.Fatalf("settings table missing: %v", err)
	}
	var applied int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("expected 1 applied migration, got %d", applied)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/core/database/...`
Expected: FAIL — `undefined: Open`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/core/database/database.go`:
```go
package database

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Open opens a modernc SQLite database with WAL journaling and FK enforcement.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: serialize writes to avoid "database is locked"
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

// Migrate applies embedded migrations in filename order, tracked in schema_migrations.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var exists int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE name = ?`, name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/core/database/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/database
git commit -m "feat: add SQLite database with embedded migrations"
```

---

## Task 5: Store layer

**Files:**
- Create: `internal/core/store/store.go`, `internal/core/store/store_test.go`

**Interfaces:**
- Consumes: `*sql.DB` from Task 4.
- Produces:
  - `type Store struct { db *sql.DB }`; `store.New(db *sql.DB) *Store`.
  - Settings: `GetSetting(ctx, key) (string, bool, error)`, `SetSetting(ctx, key, value) error`.
  - Users: `CreateUser(ctx, username, passwordHash string) (int64, error)`, `GetUserByUsername(ctx, username) (*User, error)`, `CountUsers(ctx) (int, error)`, `GetUserByID(ctx, id int64) (*User, error)`.
  - Sessions: `CreateSession(ctx, token string, userID int64, expiresAt time.Time) error`, `GetSession(ctx, token) (*Session, error)`, `DeleteSession(ctx, token) error`.
  - Tasks: `UpsertTask(ctx, t Task) error`, `GetTask(ctx, id) (*Task, error)`, `ListTasks(ctx, limit int) ([]Task, error)`.
  - Types: `type User struct { ID int64; Username, PasswordHash string; CreatedAt time.Time }`; `type Session struct { Token string; UserID int64; ExpiresAt time.Time }`; `type Task struct { ID, Name, Status, Message string; Progress int; CreatedAt, UpdatedAt time.Time }`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/store/store_test.go`:
```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/database"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestSettingsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, _ := s.GetSetting(ctx, "x"); ok {
		t.Fatal("expected missing key")
	}
	if err := s.SetSetting(ctx, "x", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "x", "2"); err != nil { // upsert
		t.Fatal(err)
	}
	v, ok, err := s.GetSetting(ctx, "x")
	if err != nil || !ok || v != "2" {
		t.Fatalf("got %q ok=%v err=%v", v, ok, err)
	}
}

func TestUsersAndSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if n, _ := s.CountUsers(ctx); n != 0 {
		t.Fatalf("expected 0 users, got %d", n)
	}
	id, err := s.CreateUser(ctx, "admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUserByUsername(ctx, "admin")
	if err != nil || u.ID != id || u.PasswordHash != "hash" {
		t.Fatalf("bad user: %+v err=%v", u, err)
	}
	tok := "tok123"
	if err := s.CreateSession(ctx, tok, id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sess, err := s.GetSession(ctx, tok)
	if err != nil || sess.UserID != id {
		t.Fatalf("bad session: %+v err=%v", sess, err)
	}
	if err := s.DeleteSession(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, tok); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTasksUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := Task{ID: "a", Name: "Test", Status: "queued"}
	if err := s.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	task.Status = "completed"
	task.Progress = 100
	if err := s.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(ctx, "a")
	if err != nil || got.Status != "completed" || got.Progress != 100 {
		t.Fatalf("bad task: %+v err=%v", got, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/store/...`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/store/store.go`:
```go
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
	ID        string
	Name      string
	Status    string
	Progress  int
	Message   string
	CreatedAt time.Time
	UpdatedAt time.Time
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
		`INSERT INTO tasks (id, name, status, progress, message)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   status = excluded.status,
		   progress = excluded.progress,
		   message = excluded.message,
		   updated_at = CURRENT_TIMESTAMP`,
		t.ID, t.Name, t.Status, t.Progress, t.Message)
	return err
}

func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	var t Task
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, status, progress, message, created_at, updated_at FROM tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.Status, &t.Progress, &t.Message, &t.CreatedAt, &t.UpdatedAt)
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
		`SELECT id, name, status, progress, message, created_at, updated_at
		 FROM tasks ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Name, &t.Status, &t.Progress, &t.Message, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/store
git commit -m "feat: add typed Store data-access layer"
```

---

## Task 6: Event bus

**Files:**
- Create: `internal/core/events/events.go`, `internal/core/events/events_test.go`

**Interfaces:**
- Produces:
  - `type Event interface { Name() string }`.
  - `type Handler func(context.Context, Event)`.
  - `type Bus struct { ... }`; `events.New() *Bus`.
  - `func (b *Bus) Subscribe(name string, h Handler)`.
  - `func (b *Bus) Publish(ctx context.Context, e Event)` — synchronous, in registration order.
  - `func (b *Bus) PublishAsync(ctx context.Context, e Event)` — each handler in its own goroutine with panic recovery.

- [ ] **Step 1: Write the failing test**

Create `internal/core/events/events_test.go`:
```go
package events

import (
	"context"
	"sync"
	"testing"
	"time"
)

type testEvent struct{ v int }

func (testEvent) Name() string { return "test.event" }

func TestPublishSyncOrdered(t *testing.T) {
	b := New()
	var got []int
	b.Subscribe("test.event", func(_ context.Context, e Event) { got = append(got, 1) })
	b.Subscribe("test.event", func(_ context.Context, e Event) { got = append(got, 2) })
	b.Publish(context.Background(), testEvent{})
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("handlers not run in order: %v", got)
	}
}

func TestPublishAsyncRecoversPanic(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	wg.Add(1)
	b.Subscribe("test.event", func(_ context.Context, e Event) { panic("boom") })
	b.Subscribe("test.event", func(_ context.Context, e Event) { defer wg.Done() })
	b.PublishAsync(context.Background(), testEvent{})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second handler did not run after first panicked")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/events/...`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/events/events.go`:
```go
package events

import (
	"context"
	"log/slog"
	"sync"
)

type Event interface{ Name() string }

type Handler func(context.Context, Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
	log      *slog.Logger
}

func New() *Bus {
	return &Bus{handlers: make(map[string][]Handler), log: slog.Default()}
}

// WithLogger sets the logger used for async panic recovery. Returns the bus for chaining.
func (b *Bus) WithLogger(l *slog.Logger) *Bus { b.log = l; return b }

func (b *Bus) Subscribe(name string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[name] = append(b.handlers[name], h)
}

func (b *Bus) snapshot(name string) []Handler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	hs := b.handlers[name]
	out := make([]Handler, len(hs))
	copy(out, hs)
	return out
}

// Publish runs handlers synchronously in registration order.
func (b *Bus) Publish(ctx context.Context, e Event) {
	for _, h := range b.snapshot(e.Name()) {
		h(ctx, e)
	}
}

// PublishAsync runs each handler in its own goroutine with panic recovery.
func (b *Bus) PublishAsync(ctx context.Context, e Event) {
	for _, h := range b.snapshot(e.Name()) {
		h := h
		go func() {
			defer func() {
				if r := recover(); r != nil {
					b.log.Error("event handler panicked", "event", e.Name(), "recover", r)
				}
			}()
			h(ctx, e)
		}()
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/events/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/events
git commit -m "feat: add in-process typed event bus"
```

---

## Task 7: Command queue & scheduler

**Files:**
- Create: `internal/core/command/command.go`, `internal/core/command/command_test.go`, `internal/core/scheduler/scheduler.go`, `internal/core/scheduler/scheduler_test.go`

**Interfaces:**
- Consumes: `*store.Store` (Task 5), `*events.Bus` (Task 6).
- Produces (command):
  - `type Reporter interface { Progress(pct int, msg string) }`.
  - `type Command interface { Name() string; Run(ctx context.Context, r Reporter) error }`.
  - `type Manager struct { ... }`; `command.NewManager(s *store.Store, bus *events.Bus, workers int) *Manager`.
  - `func (m *Manager) Start()` / `func (m *Manager) Stop()` (drains in-flight).
  - `func (m *Manager) Enqueue(c Command) (id string, err error)` — persists task `queued`, returns id.
  - Emits events `TaskUpdated{Task store.Task}` (implements `events.Event`, `Name()=="task.updated"`) on every state change.
- Produces (scheduler):
  - `type Scheduler struct { ... }`; `scheduler.New(m *command.Manager) *Scheduler`.
  - `func (s *Scheduler) Every(d time.Duration, factory func() command.Command)`.
  - `func (s *Scheduler) Start()` / `func (s *Scheduler) Stop()`.

- [ ] **Step 1: Write the failing command test**

Create `internal/core/command/command_test.go`:
```go
package command

import (
	"context"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type fakeCmd struct{ done chan struct{} }

func (fakeCmd) Name() string { return "Fake" }
func (f fakeCmd) Run(_ context.Context, r Reporter) error {
	r.Progress(50, "halfway")
	close(f.done)
	return nil
}

func newMgr(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	m := NewManager(s, events.New(), 2)
	return m, s
}

func TestEnqueueRunsAndCompletes(t *testing.T) {
	m, s := newMgr(t)
	m.Start()
	defer m.Stop()

	fc := fakeCmd{done: make(chan struct{})}
	id, err := m.Enqueue(fc)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-fc.done:
	case <-time.After(2 * time.Second):
		t.Fatal("command never ran")
	}
	// Allow the worker to persist the terminal state.
	deadline := time.Now().Add(2 * time.Second)
	for {
		task, err := s.GetTask(context.Background(), id)
		if err == nil && task.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not completed: %+v err=%v", task, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/command/...`
Expected: FAIL — `undefined: NewManager`.

- [ ] **Step 3: Write the command implementation**

Create `internal/core/command/command.go`:
```go
package command

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type Reporter interface{ Progress(pct int, msg string) }

type Command interface {
	Name() string
	Run(ctx context.Context, r Reporter) error
}

// TaskUpdated is emitted on every task state change.
type TaskUpdated struct{ Task store.Task }

func (TaskUpdated) Name() string { return "task.updated" }

type job struct {
	id  string
	cmd Command
}

type Manager struct {
	store   *store.Store
	bus     *events.Bus
	workers int
	queue   chan job
	wg      sync.WaitGroup
	log     *slog.Logger
}

func NewManager(s *store.Store, bus *events.Bus, workers int) *Manager {
	if workers < 1 {
		workers = 1
	}
	return &Manager{
		store:   s,
		bus:     bus,
		workers: workers,
		queue:   make(chan job, 256),
		log:     slog.Default(),
	}
}

func (m *Manager) WithLogger(l *slog.Logger) *Manager { m.log = l; return m }

func (m *Manager) Start() {
	for i := 0; i < m.workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
}

// Stop closes the queue and waits for in-flight jobs to drain.
func (m *Manager) Stop() {
	close(m.queue)
	m.wg.Wait()
}

func (m *Manager) Enqueue(c Command) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	t := store.Task{ID: id, Name: c.Name(), Status: "queued"}
	if err := m.store.UpsertTask(context.Background(), t); err != nil {
		return "", err
	}
	m.emit(t)
	m.queue <- job{id: id, cmd: c}
	return id, nil
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for j := range m.queue {
		m.run(j)
	}
}

func (m *Manager) run(j job) {
	ctx := context.Background()
	rep := &reporter{m: m, id: j.id, name: j.cmd.Name()}
	m.update(j.id, j.cmd.Name(), "running", 0, "")

	defer func() {
		if r := recover(); r != nil {
			m.log.Error("command panicked", "id", j.id, "recover", r)
			m.update(j.id, j.cmd.Name(), "failed", rep.pct, "panic")
		}
	}()

	if err := j.cmd.Run(ctx, rep); err != nil {
		m.update(j.id, j.cmd.Name(), "failed", rep.pct, err.Error())
		return
	}
	m.update(j.id, j.cmd.Name(), "completed", 100, "")
}

func (m *Manager) update(id, name, status string, pct int, msg string) {
	t := store.Task{ID: id, Name: name, Status: status, Progress: pct, Message: msg}
	if err := m.store.UpsertTask(context.Background(), t); err != nil {
		m.log.Error("persist task", "id", id, "err", err)
		return
	}
	m.emit(t)
}

func (m *Manager) emit(t store.Task) {
	m.bus.PublishAsync(context.Background(), TaskUpdated{Task: t})
}

type reporter struct {
	m    *Manager
	id   string
	name string
	pct  int
}

func (r *reporter) Progress(pct int, msg string) {
	r.pct = pct
	r.m.update(r.id, r.name, "running", pct, msg)
}

func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/command/...`
Expected: PASS.

- [ ] **Step 5: Write the failing scheduler test**

Create `internal/core/scheduler/scheduler_test.go`:
```go
package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type countCmd struct{ n *int32 }

func (countCmd) Name() string { return "Count" }
func (c countCmd) Run(context.Context, command.Reporter) error {
	atomic.AddInt32(c.n, 1)
	return nil
}

func TestEveryFiresRepeatedly(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/t.db")
	defer db.Close()
	database.Migrate(db)
	m := command.NewManager(store.New(db), events.New(), 2)
	m.Start()
	defer m.Stop()

	var n int32
	sch := New(m)
	sch.Every(20*time.Millisecond, func() command.Command { return countCmd{n: &n} })
	sch.Start()
	defer sch.Stop()

	time.Sleep(120 * time.Millisecond)
	if atomic.LoadInt32(&n) < 2 {
		t.Fatalf("expected >=2 firings, got %d", n)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/core/scheduler/...`
Expected: FAIL — `undefined: New`.

- [ ] **Step 7: Write the scheduler implementation**

Create `internal/core/scheduler/scheduler.go`:
```go
package scheduler

import (
	"time"

	"github.com/hellboundg/nexus/internal/core/command"
)

type entry struct {
	interval time.Duration
	factory  func() command.Command
}

type Scheduler struct {
	mgr     *command.Manager
	entries []entry
	stop    chan struct{}
}

func New(m *command.Manager) *Scheduler {
	return &Scheduler{mgr: m, stop: make(chan struct{})}
}

// Every registers a recurring command produced by factory at each interval.
func (s *Scheduler) Every(d time.Duration, factory func() command.Command) {
	s.entries = append(s.entries, entry{interval: d, factory: factory})
}

func (s *Scheduler) Start() {
	for _, e := range s.entries {
		e := e
		go func() {
			ticker := time.NewTicker(e.interval)
			defer ticker.Stop()
			for {
				select {
				case <-s.stop:
					return
				case <-ticker.C:
					_, _ = s.mgr.Enqueue(e.factory())
				}
			}
		}()
	}
}

func (s *Scheduler) Stop() { close(s.stop) }
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/core/command/... ./internal/core/scheduler/...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/core/command internal/core/scheduler
git commit -m "feat: add command queue and scheduler"
```

---

## Task 8: Auth (argon2id + sessions + API key)

**Files:**
- Create: `internal/core/auth/auth.go`, `internal/core/auth/auth_test.go`

**Interfaces:**
- Consumes: `*store.Store` (Task 5).
- Produces:
  - `auth.HashPassword(plain string) (string, error)` — argon2id, encoded string with salt+params.
  - `auth.VerifyPassword(encoded, plain string) (bool, error)`.
  - `type Service struct { ... }`; `auth.NewService(s *store.Store, apiKey string) *Service`.
  - `func (a *Service) Login(ctx, username, password string) (token string, err error)` — verifies, creates 30-day session.
  - `func (a *Service) Logout(ctx, token string) error`.
  - `func (a *Service) Authenticate(ctx, token string) (*store.User, error)` — validates session, rejects expired (`ErrUnauthorized`).
  - `func (a *Service) Middleware(next http.Handler) http.Handler` — allows request if API key header matches OR session cookie `nexus_session` authenticates; else 401 JSON.
  - `var ErrUnauthorized = errors.New("unauthorized")`.
  - Cookie name constant `CookieName = "nexus_session"`; header constant `APIKeyHeader = "X-Api-Key"`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/auth/auth_test.go`:
```go
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	return NewService(s, "secret-api-key"), s
}

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword(h, "hunter2")
	if err != nil || !ok {
		t.Fatalf("verify correct password: ok=%v err=%v", ok, err)
	}
	ok, _ = VerifyPassword(h, "wrong")
	if ok {
		t.Fatal("verify should fail for wrong password")
	}
}

func TestLoginAuthenticate(t *testing.T) {
	a, s := newService(t)
	ctx := context.Background()
	h, _ := HashPassword("pw")
	if _, err := s.CreateUser(ctx, "admin", h); err != nil {
		t.Fatal(err)
	}
	tok, err := a.Login(ctx, "admin", "pw")
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.Authenticate(ctx, tok)
	if err != nil || u.Username != "admin" {
		t.Fatalf("authenticate: %+v err=%v", u, err)
	}
	if _, err := a.Login(ctx, "admin", "bad"); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestMiddlewareAPIKey(t *testing.T) {
	a, _ := newService(t)
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// No credentials → 401.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no creds: want 401 got %d", rec.Code)
	}
	// Valid API key → 200.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Header.Set(APIKeyHeader, "secret-api-key")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid key: want 200 got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/auth/...`
Expected: FAIL — `undefined: HashPassword`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/auth/auth.go`:
```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/hellboundg/nexus/internal/core/store"
)

var ErrUnauthorized = errors.New("unauthorized")

const (
	CookieName   = "nexus_session"
	APIKeyHeader = "X-Api-Key"

	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

// HashPassword returns an argon2id encoded hash of plain.
func HashPassword(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether plain matches the argon2id encoded hash.
func VerifyPassword(encoded, plain string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid hash format")
	}
	var mem, tme, par uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &tme, &par); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(plain), salt, tme, mem, uint8(par), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

type Service struct {
	store  *store.Store
	apiKey string
}

func NewService(s *store.Store, apiKey string) *Service {
	return &Service{store: s, apiKey: apiKey}
}

func (a *Service) Login(ctx context.Context, username, password string) (string, error) {
	u, err := a.store.GetUserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", err
	}
	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrUnauthorized
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := a.store.CreateSession(ctx, token, u.ID, time.Now().Add(30*24*time.Hour)); err != nil {
		return "", err
	}
	return token, nil
}

func (a *Service) Logout(ctx context.Context, token string) error {
	return a.store.DeleteSession(ctx, token)
}

func (a *Service) Authenticate(ctx context.Context, token string) (*store.User, error) {
	sess, err := a.store.GetSession(ctx, token)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = a.store.DeleteSession(ctx, token)
		return nil, ErrUnauthorized
	}
	return a.store.GetUserByID(ctx, sess.UserID)
}

// Middleware allows the request if the API key matches or a session cookie authenticates.
func (a *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if k := r.Header.Get(APIKeyHeader); k != "" &&
			subtle.ConstantTimeCompare([]byte(k), []byte(a.apiKey)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(CookieName); err == nil {
			if _, err := a.Authenticate(r.Context(), c.Value); err == nil {
				next.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"authentication required"}}`))
	})
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/auth/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/auth
git commit -m "feat: add argon2id auth with sessions and API key"
```

---

## Task 9: HTTP API skeleton

**Files:**
- Create: `internal/core/api/errors.go`, `internal/core/api/api.go`, `internal/core/api/system.go`, `internal/core/api/api_test.go`

**Interfaces:**
- Consumes: `*auth.Service` (Task 8), `version.Version` (Task 1), `*command.Manager` (Task 7, for status counts — optional; use `store.ListTasks` via `*store.Store`).
- Produces:
  - `type Deps struct { Auth *auth.Service; Store *store.Store; Version string }`.
  - `api.NewRouter(d Deps, spa http.Handler) http.Handler` — mounts middleware, `/health` (public), `/api/v1/auth/login|logout` (public login), protected `/api/v1/system/status`, WebSocket (Task 10), and the SPA handler as fallback.
  - `api.WriteError(w http.ResponseWriter, status int, code, msg string)` and `api.WriteJSON(w, status, v any)`.

- [ ] **Step 1: Write the error helpers + test**

Create `internal/core/api/errors.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, code, msg string) {
	WriteJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: msg}})
}
```

Create `internal/core/api/api_test.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newRouter(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("SPA"))
	})
	d := Deps{Auth: auth.NewService(s, "k"), Store: s, Version: "test"}
	return NewRouter(d, spa), s
}

func TestHealthIsPublic(t *testing.T) {
	r, _ := newRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health: want 200 got %d", rec.Code)
	}
}

func TestSystemStatusRequiresAuth(t *testing.T) {
	r, _ := newRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status without auth: want 401 got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	req.Header.Set(auth.APIKeyHeader, "k")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status with key: want 200 got %d", rec.Code)
	}
	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)
	if body["version"] != "test" {
		t.Fatalf("unexpected status body: %v", body)
	}
}

func TestLoginSetsCookie(t *testing.T) {
	r, s := newRouter(t)
	h, _ := auth.HashPassword("pw")
	s.CreateUser(context.Background(), "admin", h)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"pw"}`))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200 got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected session cookie")
	}
}

func TestSPAFallback(t *testing.T) {
	r, _ := newRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/movies", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "SPA" {
		t.Fatalf("SPA fallback: got %d %q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/api/...`
Expected: FAIL — `undefined: NewRouter`.

- [ ] **Step 3: Write the system handlers**

Create `internal/core/api/system.go`:
```go
package api

import (
	"net/http"
)

type statusResponse struct {
	Version   string `json:"version"`
	AppName   string `json:"appName"`
	Healthy   bool   `json:"healthy"`
	TaskCount int    `json:"taskCount"`
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.deps.Store.ListTasks(r.Context(), 100)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, statusResponse{
		Version:   s.deps.Version,
		AppName:   "Nexus",
		Healthy:   true,
		TaskCount: len(tasks),
	})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Write the router + auth handlers**

Create `internal/core/api/api.go`:
```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/store"
)

type Deps struct {
	Auth    *auth.Service
	Store   *store.Store
	Version string
}

type server struct{ deps Deps }

func NewRouter(d Deps, spa http.Handler) http.Handler {
	s := &server{deps: d}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Get("/health", s.handleHealth)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.Middleware)
			r.Get("/system/status", s.handleStatus)
			// WebSocket route is registered in ws.go via RegisterWebSocket (Task 10).
			s.registerWebSocket(r)
		})
	})

	// SPA fallback for everything else.
	r.NotFound(spa.ServeHTTP)
	return r
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	token, err := s.deps.Auth.Login(r.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrUnauthorized) {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.deps.Auth.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1})
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

> Note: `registerWebSocket` is defined in Task 10. To keep this task compiling on its own, add a temporary stub `func (s *server) registerWebSocket(r chi.Router) {}` in `api.go`; Task 10 replaces it with the real implementation in `ws.go` and removes this stub.

- [ ] **Step 5: Add temporary WebSocket stub, run tests**

Add to `internal/core/api/api.go` (temporary, removed in Task 10):
```go
func (s *server) registerWebSocket(r chi.Router) {}
```

Run: `go test ./internal/core/api/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/api
git commit -m "feat: add HTTP API skeleton with auth-guarded routes"
```

---

## Task 10: WebSocket hub

**Files:**
- Create: `internal/core/api/ws.go`, `internal/core/api/ws_test.go`
- Modify: `internal/core/api/api.go` (remove the temporary `registerWebSocket` stub from Task 9; add `Bus` to `Deps`)

**Interfaces:**
- Consumes: `*events.Bus` (Task 6).
- Produces:
  - `Deps` gains field `Bus *events.Bus`.
  - `server.registerWebSocket(r chi.Router)` mounts `GET /ws`, upgrades the connection, subscribes to `task.updated`, and forwards each event as JSON `{"type":"task.updated","data":{...}}`.
  - Hub is internal; broadcast is driven by event-bus subscription established once in `NewRouter`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/api/ws_test.go`:
```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

func TestWebSocketReceivesTaskUpdate(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/t.db")
	defer db.Close()
	database.Migrate(db)
	s := store.New(db)
	bus := events.New()

	d := Deps{Auth: auth.NewService(s, "k"), Store: s, Version: "test", Bus: bus}
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	srv := httptest.NewServer(NewRouter(d, spa))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"
	header := http.Header{auth.APIKeyHeader: []string{"k"}}
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Publish a task update through the bus.
	bus.PublishAsync(context.Background(), command.TaskUpdated{Task: store.Task{ID: "x", Name: "Y", Status: "running", Progress: 42}})

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(msg), `"task.updated"`) || !strings.Contains(string(msg), `"progress":42`) {
		t.Fatalf("unexpected ws message: %s", msg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/api/ -run TestWebSocket`
Expected: FAIL — compile error (`Deps` has no field `Bus`) / stub sends nothing.

- [ ] **Step 3: Update Deps and remove the stub**

In `internal/core/api/api.go`: add `Bus *events.Bus` to `Deps` (import `"github.com/hellboundg/nexus/internal/core/events"`), and **delete** the temporary `func (s *server) registerWebSocket(r chi.Router) {}` stub.

- [ ] **Step 4: Write the WebSocket implementation**

Create `internal/core/api/ws.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/hellboundg/nexus/internal/core/command"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsMessage struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func newHub() *hub { return &hub{clients: make(map[*websocket.Conn]struct{})} }

func (h *hub) add(c *websocket.Conn) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) remove(c *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	_ = c.Close()
}

func (h *hub) broadcast(m wsMessage) {
	payload, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.clients))
	for c := range h.clients {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
			h.remove(c)
		}
	}
}

func (s *server) registerWebSocket(r chi.Router) {
	if s.hub == nil {
		s.hub = newHub()
		// Subscribe once to bridge bus → connected clients.
		s.deps.Bus.Subscribe("task.updated", func(_ context.Context, e interface {
			Name() string
		}) {
			if tu, ok := e.(command.TaskUpdated); ok {
				s.hub.broadcast(wsMessage{Type: "task.updated", Data: tu.Task})
			}
		})
	}
	r.Get("/ws", func(w http.ResponseWriter, req *http.Request) {
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			return
		}
		s.hub.add(conn)
		// Reader loop: discard client messages, detect close to clean up.
		go func() {
			defer s.hub.remove(conn)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	})
}
```

> Note: add field `hub *hub` to the `server` struct in `api.go`, and change the bus subscription handler signature to match `events.Handler` — i.e. `func(context.Context, events.Event)`. Concretely, in `api.go` the `server` struct becomes `type server struct { deps Deps; hub *hub }`, and in `ws.go` the Subscribe callback is `func(_ context.Context, e events.Event) { ... }` with `events` imported.

- [ ] **Step 5: Fix the subscription signature**

Edit the `Subscribe` call in `ws.go` to use the real handler type:
```go
s.deps.Bus.Subscribe("task.updated", func(_ context.Context, e events.Event) {
	if tu, ok := e.(command.TaskUpdated); ok {
		s.hub.broadcast(wsMessage{Type: "task.updated", Data: tu.Task})
	}
})
```
Add `"github.com/hellboundg/nexus/internal/core/events"` to imports.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/core/api/...`
Expected: PASS (all API + WS tests).

- [ ] **Step 7: Commit**

```bash
git add internal/core/api
git commit -m "feat: add WebSocket hub bridging event bus to clients"
```

---

## Task 11: Provider interfaces

**Files:**
- Create: `internal/core/provider/provider.go`, `internal/core/provider/provider_test.go`
- Create: `internal/indexer/indexer.go`, `internal/downloadclient/downloadclient.go`, `internal/media/media.go` (each a one-line package doc stub asserting they compile and depend only on `core`)

**Interfaces:**
- Produces:
  - `type Indexer interface { ID() string; Search(ctx context.Context, q Query) ([]Release, error) }`.
  - `type DownloadClient interface { ID() string; Add(ctx context.Context, d DownloadRequest) (string, error) }`.
  - `type MetadataProvider interface { ID() string; Kind() MediaKind }` where `MediaKind` is `"tv"|"movie"`.
  - Supporting types: `Query`, `Release`, `DownloadRequest` (minimal fields — extended in later sub-projects).
  - `type Registry[T any] struct { ... }` with `Register(id string, v T) error` (rejects duplicates), `Get(id string) (T, bool)`, `All() []T`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/provider/provider_test.go`:
```go
package provider

import (
	"context"
	"testing"
)

type fakeIndexer struct{ id string }

func (f fakeIndexer) ID() string                                  { return f.id }
func (fakeIndexer) Search(context.Context, Query) ([]Release, error) { return nil, nil }

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry[Indexer]()
	if err := reg.Register("a", fakeIndexer{id: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("a", fakeIndexer{id: "a"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
	got, ok := reg.Get("a")
	if !ok || got.ID() != "a" {
		t.Fatalf("get: ok=%v got=%v", ok, got)
	}
	if len(reg.All()) != 1 {
		t.Fatalf("All() len = %d", len(reg.All()))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/provider/...`
Expected: FAIL — `undefined: NewRegistry`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/provider/provider.go`:
```go
package provider

import (
	"context"
	"fmt"
	"sync"
)

type MediaKind string

const (
	KindTV    MediaKind = "tv"
	KindMovie MediaKind = "movie"
)

// Query is a minimal search request; extended by the indexer sub-project.
type Query struct {
	Term string
	Kind MediaKind
}

// Release is a minimal indexer result; extended by the indexer sub-project.
type Release struct {
	Title       string
	DownloadURL string
	Size        int64
	IndexerID   string
}

// DownloadRequest is a minimal grab request; extended by the download sub-project.
type DownloadRequest struct {
	URL   string
	Title string
}

type Indexer interface {
	ID() string
	Search(ctx context.Context, q Query) ([]Release, error)
}

type DownloadClient interface {
	ID() string
	Add(ctx context.Context, d DownloadRequest) (string, error)
}

type MetadataProvider interface {
	ID() string
	Kind() MediaKind
}

// Registry is a concurrency-safe id→provider map.
type Registry[T any] struct {
	mu    sync.RWMutex
	items map[string]T
}

func NewRegistry[T any]() *Registry[T] {
	return &Registry[T]{items: make(map[string]T)}
}

func (r *Registry[T]) Register(id string, v T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[id]; exists {
		return fmt.Errorf("provider %q already registered", id)
	}
	r.items[id] = v
	return nil
}

func (r *Registry[T]) Get(id string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.items[id]
	return v, ok
}

func (r *Registry[T]) All() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]T, 0, len(r.items))
	for _, v := range r.items {
		out = append(out, v)
	}
	return out
}
```

- [ ] **Step 4: Create module stubs**

Create `internal/indexer/indexer.go`:
```go
// Package indexer implements Prowlarr-equivalent indexer search.
// Foundation ships only the package; behavior lands in sub-project 2.
package indexer
```

Create `internal/downloadclient/downloadclient.go`:
```go
// Package downloadclient integrates usenet and torrent download clients.
// Foundation ships only the package; behavior lands in sub-project 3.
package downloadclient
```

Create `internal/media/media.go`:
```go
// Package media manages TV series and movies (metadata, parsing, import).
// Foundation ships only the package; behavior lands in sub-project 4.
package media
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/core/provider/... ./internal/indexer/... ./internal/downloadclient/... ./internal/media/...`
Expected: PASS (provider tests pass; stub packages compile).

- [ ] **Step 6: Commit**

```bash
git add internal/core/provider internal/indexer internal/downloadclient internal/media
git commit -m "feat: add provider interfaces and generic registry"
```

---

## Task 12: SPA embedding & serving

**Files:**
- Create: `web/embed.go`, `web/dist/index.html`, `web/spa_test.go`

**Interfaces:**
- Produces:
  - `web.Handler() http.Handler` — serves embedded files from `dist/`, falling back to `dist/index.html` for any path without a matching file (client-side routing).

> The real React app is built in Sub-project 6; here we embed a minimal placeholder `index.html` so the binary serves *something* and the fallback logic is testable now.

- [ ] **Step 1: Create the placeholder SPA**

Create `web/dist/index.html`:
```html
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Nexus</title></head>
<body><div id="root">Nexus is running. UI ships in a later sub-project.</div></body>
</html>
```

- [ ] **Step 2: Write the failing test**

Create `web/spa_test.go`:
```go
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Nexus is running") {
		t.Fatalf("index: got %d %q", rec.Code, rec.Body.String())
	}
}

func TestFallsBackToIndexForClientRoute(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/movies/123", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Nexus is running") {
		t.Fatalf("fallback: got %d %q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./web/...`
Expected: FAIL — `undefined: Handler`.

- [ ] **Step 4: Write minimal implementation**

Create `web/embed.go`:
```go
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var distFS embed.FS

// Handler serves the embedded SPA, falling back to index.html for client routes.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If the requested file exists, serve it; else serve index.html.
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			serveIndex(w, r, sub)
			return
		}
		if _, err := fs.Stat(sub, p); err != nil {
			serveIndex(w, r, sub)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./web/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web
git commit -m "feat: embed and serve SPA with client-route fallback"
```

---

## Task 13: Composition root & graceful shutdown

**Files:**
- Create: `cmd/nexus/main.go`, `cmd/nexus/main_test.go`
- Modify: `Makefile` (nothing required; already builds `./cmd/nexus`)

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `cmd/nexus/main.go` `run(ctx context.Context) error` — testable entrypoint: load config, open+migrate DB, build store/auth/bus/command manager/scheduler/api, ensure first-run admin, start HTTP server, block until ctx canceled, then graceful shutdown (stop scheduler, stop command manager, close DB).
  - First-run behavior: if `CountUsers()==0`, create admin from `NEXUS_ADMIN_USER`/`NEXUS_ADMIN_PASSWORD` (defaults `admin`/random-logged password), log the credentials once.

- [ ] **Step 1: Write the failing test**

Create `cmd/nexus/main_test.go`:
```go
package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRunStartsAndShutsDown(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9599")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()

	// Poll until the health endpoint responds.
	deadline := time.Now().Add(5 * time.Second)
	var ok bool
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:9599/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			ok = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok {
		t.Fatal("server never became healthy")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not shut down after cancel")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/nexus/...`
Expected: FAIL — `undefined: run`.

- [ ] **Step 3: Add rotating-file dependency**

Run: `go get gopkg.in/natefinch/lumberjack.v2`

- [ ] **Step 4: Write the implementation**

Create `cmd/nexus/main.go`:
```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/config"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/logging"
	"github.com/hellboundg/nexus/internal/core/scheduler"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/core/version"
	"github.com/hellboundg/nexus/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}

	logWriter := io.MultiWriter(os.Stdout, &lumberjack.Logger{
		Filename: filepath.Join(cfg.DataDir, "nexus.log"), MaxSize: 10, MaxBackups: 3, MaxAge: 28,
	})
	log := logging.New(cfg.LogLevel, logWriter)
	slog.SetDefault(log)

	db, err := database.Open(filepath.Join(cfg.DataDir, "nexus.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		return err
	}

	st := store.New(db)
	if err := ensureAdmin(ctx, st, log); err != nil {
		return err
	}

	bus := events.New().WithLogger(log)
	mgr := command.NewManager(st, bus, 4).WithLogger(log)
	mgr.Start()
	sch := scheduler.New(mgr)
	sch.Start()

	authSvc := auth.NewService(st, cfg.APIKey)
	router := api.NewRouter(api.Deps{
		Auth: authSvc, Store: st, Version: version.Version(), Bus: bus,
	}, web.Handler())

	srv := &http.Server{Addr: cfg.Addr(), Handler: router}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		sch.Stop()
		mgr.Stop()
	}()

	log.Info("nexus starting", "addr", cfg.Addr(), "version", version.Version())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func ensureAdmin(ctx context.Context, st *store.Store, log *slog.Logger) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	username := os.Getenv("NEXUS_ADMIN_USER")
	if username == "" {
		username = "admin"
	}
	password := os.Getenv("NEXUS_ADMIN_PASSWORD")
	generated := false
	if password == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		password = hex.EncodeToString(b)
		generated = true
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := st.CreateUser(ctx, username, hash); err != nil {
		return err
	}
	if generated {
		log.Warn("created initial admin user", "username", username, "password", password)
	} else {
		log.Info("created initial admin user", "username", username)
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/nexus/...`
Expected: PASS.

- [ ] **Step 6: Full build + test sweep**

Run:
```bash
CGO_ENABLED=0 go build ./cmd/nexus
go test ./...
```
Expected: build succeeds; all packages PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/nexus go.mod go.sum
git commit -m "feat: add composition root with first-run admin and graceful shutdown"
```

---

## Self-Review Notes (author)

- **Spec coverage:** config (Task 3), DB+migrations (Task 4), Store (Task 5), event bus (Task 6), scheduler+command queue (Task 7), auth/single-admin/argon2id/API key (Task 8, 13), HTTP API + error envelope + system/status + health (Task 9), WebSocket hub (Task 10), provider interfaces + registries (Task 11), SPA embedding/serving (Task 12), logging (Task 2, 13), graceful shutdown + first-run admin (Task 13). All Foundation acceptance criteria (spec §6) map to tasks 4/13, 8/13, 9/12, 7/10, 11, 2/13, 13 respectively.
- **Deviation from spec:** `database/sql` instead of sqlc (documented in Global Constraints); `Store` interface stable for later swap.
- **Cross-task type consistency:** `store.Task`, `command.TaskUpdated` (`"task.updated"`), `events.Event`/`Handler`, `auth.CookieName`/`APIKeyHeader`, `api.Deps` fields, `provider.Registry[T]` verified consistent across tasks 5–13.
- **Module path** `github.com/hellboundg/nexus` used uniformly; change once in `go.mod` + imports if a different path is preferred.
