# Nexus Indexer Engine Implementation Plan (Sub-project 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Prowlarr-equivalent indexer engine — Newznab/Torznab clients, capabilities discovery, a fan-out search service, indexer CRUD + health, and a REST API — filling the `provider.Indexer` contract from Foundation.

**Architecture:** A Go feature module `internal/indexer` that imports `internal/core/*` only, persists indexer configs through `core/store`, and mounts an authenticated sub-router via a new variadic parameter on `api.NewRouter`. Newznab and Torznab are one protocol family, so a single `NewznabClient` (with a protocol flag) serves both. Manual search is a direct synchronous service call; health checks run on the Foundation scheduler and emit events forwarded to the WebSocket.

**Tech Stack:** Go 1.22+, `github.com/go-chi/chi/v5`, stdlib `encoding/xml`, `net/http`, `net/http/httptest`, `modernc.org/sqlite` (via `core/database`), `log/slog`.

## Global Constraints

- **Language/version:** Go 1.22 or newer.
- **No CGO:** builds must succeed with `CGO_ENABLED=0`.
- **Module boundaries:** `internal/indexer` MUST import only `internal/core/*` (and stdlib / chi). It must NOT import other feature modules (`internal/downloadclient`, `internal/media`, `internal/automation`).
- **Data layer:** hand-written `database/sql` behind `*store.Store`. New persistence lives in `internal/core/store`.
- **API surface:** REST under `/api/v1`, consistent JSON error envelope via `api.WriteError`; success via `api.WriteJSON`.
- **Commits:** conventional-commit prefixes (`feat:`, `test:`, `fix:`, `chore:`, `docs:`). Commit at the end of each task.
- **Module path:** `github.com/hellboundg/nexus` (all import paths below).
- **Tests:** deterministic and offline — no real network. Use `httptest.Server` and `testdata/` XML fixtures.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/core/api/api.go` | MODIFY: `NewRouter` gains variadic `mounts`; `Deps` gains `WSForward []string` |
| `internal/core/api/ws.go` | MODIFY: bridge each `WSForward` event name to connected clients |
| `internal/core/provider/provider.go` | MODIFY: extend `Query`/`Release`; add `SearchType`, `Protocol` |
| `internal/core/database/migrations/0002_indexers.sql` | CREATE: `indexers` table |
| `internal/core/store/indexer_store.go` | CREATE: `store.Indexer` + CRUD |
| `internal/indexer/request.go` | CREATE: build search request URLs |
| `internal/indexer/caps.go` | CREATE: capabilities model, parse + discover |
| `internal/indexer/parse.go` | CREATE: parse Newznab/Torznab XML → `[]provider.Release` |
| `internal/indexer/ratelimit.go` | CREATE: per-indexer minimum-interval limiter |
| `internal/indexer/newznab.go` | CREATE: `NewznabClient` implementing `provider.Indexer` |
| `internal/indexer/search.go` | CREATE: `SearchService` fan-out/aggregate/dedupe/sort |
| `internal/indexer/indexer.go` | CREATE: package doc + `Service` (live clients, reload) |
| `internal/indexer/health.go` | CREATE: `HealthCheck` command + `IndexerStatusChanged` event |
| `internal/indexer/api.go` | CREATE: chi sub-router (CRUD/test/schema/search) + `Mount` |
| `cmd/nexus/main.go` | MODIFY: construct `Service`, register health, mount routes, set `WSForward` |
| `internal/indexer/testdata/*.xml` | CREATE: recorded caps + search fixtures |

Existing `internal/indexer/indexer.go` currently holds only a package-doc stub (from Foundation Task 11); Task 8 replaces it.

---

## Task 1: Foundation API — route mounters + WS event forwarding

**Files:**
- Modify: `internal/core/api/api.go`
- Modify: `internal/core/api/ws.go`
- Test: `internal/core/api/mount_test.go` (create)

**Interfaces:**
- Consumes: existing `Deps`, `NewRouter`, `server`, `hub`, `registerWebSocket`.
- Produces:
  - `func NewRouter(d Deps, spa http.Handler, mounts ...func(chi.Router)) http.Handler` — each `mount` is invoked inside the authenticated `/api/v1` group.
  - `Deps.WSForward []string` — event names whose values are broadcast to WS clients as `{type:<name>, data:<event>}`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/api/mount_test.go`:
```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/auth"
)

func TestMountedRouteRequiresAuthAndWorks(t *testing.T) {
	authSvc := auth.NewService(nil, "secret-key")
	mount := func(r chi.Router) {
		r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) {
			WriteJSON(w, http.StatusOK, map[string]string{"pong": "ok"})
		})
	}
	router := NewRouter(Deps{Auth: authSvc}, http.NotFoundHandler(), mount)

	// Without API key: 401.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-auth: got %d want 401", rec.Code)
	}

	// With API key: 200.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set(auth.APIKeyHeader, "secret-key")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth: got %d want 200 body=%s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/api/ -run TestMountedRouteRequiresAuthAndWorks`
Expected: FAIL — compile error, `NewRouter` takes 2 args not 3.

- [ ] **Step 3: Modify `NewRouter` to accept mounts**

In `internal/core/api/api.go`, change the signature and the authed group. Replace:
```go
func NewRouter(d Deps, spa http.Handler) http.Handler {
	s := &server{deps: d}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(requestLogger)

	r.Get("/health", s.handleHealth)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.Middleware)
			r.Get("/system/status", s.handleStatus)
			s.registerWebSocket(r)
		})
	})
```
with:
```go
func NewRouter(d Deps, spa http.Handler, mounts ...func(chi.Router)) http.Handler {
	s := &server{deps: d}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(requestLogger)

	r.Get("/health", s.handleHealth)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.Middleware)
			r.Get("/system/status", s.handleStatus)
			s.registerWebSocket(r)
			for _, m := range mounts {
				m(r)
			}
		})
	})
```

- [ ] **Step 4: Add `WSForward` to `Deps`**

In `internal/core/api/api.go`, extend the `Deps` struct:
```go
type Deps struct {
	Auth      *auth.Service
	Store     *store.Store
	Version   string
	Bus       *events.Bus
	WSForward []string
}
```

- [ ] **Step 5: Bridge forwarded events in `ws.go`**

In `internal/core/api/ws.go`, inside `registerWebSocket`, after the existing `task.updated` subscription block (still inside `if s.hub == nil { ... }`), add:
```go
		for _, name := range s.deps.WSForward {
			name := name
			if s.deps.Bus == nil {
				break
			}
			s.deps.Bus.Subscribe(name, func(_ context.Context, e events.Event) {
				s.hub.broadcast(wsMessage{Type: e.Name(), Data: e})
			})
		}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/core/api/...`
Expected: PASS (new test + existing api tests; `main.go`'s existing `NewRouter(...)` call still compiles because the variadic is optional).

- [ ] **Step 7: Commit**

```bash
git add internal/core/api
git commit -m "feat: add router mounts and WS event forwarding to core api"
```

---

## Task 2: Extend provider contracts

**Files:**
- Modify: `internal/core/provider/provider.go`
- Test: `internal/core/provider/provider_ext_test.go` (create)

**Interfaces:**
- Consumes: existing `MediaKind`, `Indexer`, `Release`, `Query`.
- Produces:
  - `type SearchType string` with `SearchGeneric`, `SearchTV`, `SearchMovie`.
  - `type Protocol string` with `ProtocolUsenet`, `ProtocolTorrent`.
  - Extended `Query` and `Release` structs (see Step 3).

- [ ] **Step 1: Write the failing test**

Create `internal/core/provider/provider_ext_test.go`:
```go
package provider

import (
	"testing"
	"time"
)

func TestQueryAndReleaseExtensions(t *testing.T) {
	season := 2
	q := Query{
		Type:       SearchTV,
		Term:       "the show",
		Categories: []int{5000, 5040},
		Season:     &season,
		Limit:      100,
	}
	if q.Type != SearchTV || *q.Season != 2 || len(q.Categories) != 2 {
		t.Fatal("query fields not set as expected")
	}

	seeders := 12
	r := Release{
		Title:       "The.Show.S02E05",
		DownloadURL: "http://x/t.torrent",
		Size:        1024,
		Protocol:    ProtocolTorrent,
		Seeders:     &seeders,
		PublishDate: time.Unix(0, 0),
	}
	if r.Protocol != ProtocolTorrent || *r.Seeders != 12 {
		t.Fatal("release fields not set as expected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/provider/ -run TestQueryAndReleaseExtensions`
Expected: FAIL — undefined `SearchTV`, `Season`, `Protocol`, etc.

- [ ] **Step 3: Extend the contracts**

In `internal/core/provider/provider.go`, add `"time"` to imports, then add these types and replace the existing `Query` and `Release` structs:
```go
type SearchType string

const (
	SearchGeneric SearchType = "search"
	SearchTV      SearchType = "tvsearch"
	SearchMovie   SearchType = "movie"
)

type Protocol string

const (
	ProtocolUsenet  Protocol = "usenet"
	ProtocolTorrent Protocol = "torrent"
)

// Query is a search request across indexers. Typed-search fields are used when
// Type is SearchTV or SearchMovie; they are ignored for SearchGeneric.
type Query struct {
	Type       SearchType
	Term       string
	Categories []int
	Season     *int
	Episode    *int
	IMDbID     string
	TVDBID     int
	TMDBID     int
	Limit      int
	Offset     int
	Kind       MediaKind
}

// Release is a single indexer result. Seeders/Leechers are set only for torrents.
type Release struct {
	Title       string
	DownloadURL string
	InfoURL     string
	Size        int64
	IndexerID   string
	Categories  []int
	PublishDate time.Time
	GUID        string
	Protocol    Protocol
	Seeders     *int
	Leechers    *int
}
```
Delete the old `Query` and `Release` definitions so only these remain.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/provider/...`
Expected: PASS (new test + the existing `TestRegistryRegisterAndGet`).

- [ ] **Step 5: Commit**

```bash
git add internal/core/provider
git commit -m "feat: extend provider Query/Release with search + torrent fields"
```

---

## Task 3: Store — indexers table + CRUD

**Files:**
- Create: `internal/core/database/migrations/0002_indexers.sql`
- Create: `internal/core/store/indexer_store.go`
- Test: `internal/core/store/indexer_store_test.go`

**Interfaces:**
- Consumes: `*store.Store`, `store.ErrNotFound`.
- Produces:
  - `type store.Indexer struct { ... }` (see Step 3).
  - `CreateIndexer(ctx, Indexer) (int64, error)`, `GetIndexer(ctx, int64) (*Indexer, error)`, `ListIndexers(ctx, enabledOnly bool) ([]Indexer, error)`, `UpdateIndexer(ctx, Indexer) error`, `DeleteIndexer(ctx, int64) error`, `SetIndexerStatus(ctx, id int64, status, failMessage string, caps string) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/store/indexer_store_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestIndexerCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateIndexer(ctx, Indexer{
		Name: "nzbgeek", Implementation: "newznab",
		BaseURL: "https://api.nzbgeek.info", APIKey: "k",
		Enabled: true, Priority: 25, Categories: []int{5000, 5040},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetIndexer(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "nzbgeek" || got.Implementation != "newznab" || len(got.Categories) != 2 || got.Priority != 25 {
		t.Fatalf("unexpected indexer: %+v", got)
	}

	got.Enabled = false
	got.Name = "renamed"
	if err := s.UpdateIndexer(ctx, *got); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListIndexers(ctx, false)
	if err != nil || len(all) != 1 || all[0].Name != "renamed" {
		t.Fatalf("list all: %+v err=%v", all, err)
	}
	enabled, err := s.ListIndexers(ctx, true)
	if err != nil || len(enabled) != 0 {
		t.Fatalf("list enabled: %+v err=%v", enabled, err)
	}

	if err := s.SetIndexerStatus(ctx, id, "failed", "boom", `{"x":1}`); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetIndexer(ctx, id)
	if got.Status != "failed" || got.FailMessage != "boom" || got.Caps != `{"x":1}` {
		t.Fatalf("status not persisted: %+v", got)
	}

	if err := s.DeleteIndexer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetIndexer(ctx, id); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/store/ -run TestIndexerCRUD`
Expected: FAIL — undefined `Indexer`, `CreateIndexer`, etc.

- [ ] **Step 3: Create the migration**

Create `internal/core/database/migrations/0002_indexers.sql`:
```sql
CREATE TABLE indexers (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    implementation TEXT NOT NULL,
    base_url       TEXT NOT NULL,
    api_key        TEXT NOT NULL DEFAULT '',
    enabled        INTEGER NOT NULL DEFAULT 1,
    priority       INTEGER NOT NULL DEFAULT 25,
    categories     TEXT NOT NULL DEFAULT '[]',
    settings       TEXT NOT NULL DEFAULT '{}',
    caps           TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'unknown',
    last_check     DATETIME,
    fail_message   TEXT NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 4: Create the store methods**

Create `internal/core/store/indexer_store.go`:
```go
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/core/store/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/store internal/core/database/migrations/0002_indexers.sql
git commit -m "feat: add indexers table and store CRUD"
```

---

## Task 4: Request URL builder

**Files:**
- Create: `internal/indexer/request.go`
- Test: `internal/indexer/request_test.go`

**Interfaces:**
- Consumes: `provider.Query`, `provider.SearchType`.
- Produces: `func buildSearchURL(base, apiKey string, q provider.Query) (string, error)` — returns `<base>/api?...` with the correct `t`, `apikey`, and search params.

- [ ] **Step 1: Write the failing test**

Create `internal/indexer/request_test.go`:
```go
package indexer

import (
	"net/url"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestBuildSearchURL(t *testing.T) {
	season, ep := 2, 5
	q := provider.Query{
		Type: provider.SearchTV, Term: "the show",
		Categories: []int{5000, 5040}, Season: &season, Episode: &ep,
		TVDBID: 12345, Limit: 100, Offset: 20,
	}
	raw, err := buildSearchURL("https://idx.test/", "KEY", q)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/api" {
		t.Fatalf("path = %q", u.Path)
	}
	vals := u.Query()
	checks := map[string]string{
		"t": "tvsearch", "apikey": "KEY", "q": "the show",
		"cat": "5000,5040", "season": "2", "ep": "5",
		"tvdbid": "12345", "limit": "100", "offset": "20",
	}
	for k, want := range checks {
		if got := vals.Get(k); got != want {
			t.Errorf("%s = %q want %q", k, got, want)
		}
	}
}

func TestBuildSearchURLDefaultsToGenericSearch(t *testing.T) {
	raw, err := buildSearchURL("https://idx.test", "K", provider.Query{Term: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	if u.Query().Get("t") != "search" {
		t.Fatalf("t = %q want search", u.Query().Get("t"))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexer/ -run TestBuildSearchURL`
Expected: FAIL — undefined `buildSearchURL`.

- [ ] **Step 3: Write the implementation**

Create `internal/indexer/request.go`:
```go
package indexer

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// buildSearchURL constructs a Newznab/Torznab API request URL.
func buildSearchURL(base, apiKey string, q provider.Query) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/api")
	if err != nil {
		return "", err
	}
	t := q.Type
	if t == "" {
		t = provider.SearchGeneric
	}
	v := url.Values{}
	v.Set("t", string(t))
	if apiKey != "" {
		v.Set("apikey", apiKey)
	}
	if q.Term != "" {
		v.Set("q", q.Term)
	}
	if len(q.Categories) > 0 {
		cats := make([]string, len(q.Categories))
		for i, c := range q.Categories {
			cats[i] = strconv.Itoa(c)
		}
		v.Set("cat", strings.Join(cats, ","))
	}
	if q.Season != nil {
		v.Set("season", strconv.Itoa(*q.Season))
	}
	if q.Episode != nil {
		v.Set("ep", strconv.Itoa(*q.Episode))
	}
	if q.IMDbID != "" {
		v.Set("imdbid", strings.TrimPrefix(q.IMDbID, "tt"))
	}
	if q.TVDBID != 0 {
		v.Set("tvdbid", strconv.Itoa(q.TVDBID))
	}
	if q.TMDBID != 0 {
		v.Set("tmdbid", strconv.Itoa(q.TMDBID))
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	u.RawQuery = v.Encode()
	return u.String(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/indexer/ -run TestBuildSearchURL`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/request.go internal/indexer/request_test.go
git commit -m "feat: add Newznab/Torznab request URL builder"
```

---

## Task 5: Capabilities parsing + discovery

**Files:**
- Create: `internal/indexer/caps.go`
- Create: `internal/indexer/testdata/caps.xml`
- Test: `internal/indexer/caps_test.go`

**Interfaces:**
- Consumes: `provider.SearchType`.
- Produces:
  - `type Capabilities struct { ... }` with method `func (c Capabilities) supports(t provider.SearchType) bool`.
  - `func parseCaps(data []byte) (Capabilities, error)`.
  - `func discoverCaps(ctx context.Context, hc *http.Client, base, apiKey string) (Capabilities, error)`.

- [ ] **Step 1: Create the fixture**

Create `internal/indexer/testdata/caps.xml`:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <limits max="100" default="50"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <tv-search available="yes" supportedParams="q,season,ep,tvdbid"/>
    <movie-search available="no" supportedParams="q,imdbid"/>
  </searching>
  <categories>
    <category id="5000" name="TV"/>
    <category id="2000" name="Movies"/>
  </categories>
</caps>
```

- [ ] **Step 2: Write the failing test**

Create `internal/indexer/caps_test.go`:
```go
package indexer

import (
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestParseCaps(t *testing.T) {
	data, err := os.ReadFile("testdata/caps.xml")
	if err != nil {
		t.Fatal(err)
	}
	caps, err := parseCaps(data)
	if err != nil {
		t.Fatal(err)
	}
	if caps.Limits.Max != 100 || caps.Limits.Default != 50 {
		t.Fatalf("limits: %+v", caps.Limits)
	}
	if !caps.supports(provider.SearchGeneric) {
		t.Error("expected generic search supported")
	}
	if !caps.supports(provider.SearchTV) {
		t.Error("expected tv search supported")
	}
	if caps.supports(provider.SearchMovie) {
		t.Error("expected movie search NOT supported")
	}
	if len(caps.Categories) != 2 {
		t.Fatalf("categories: %+v", caps.Categories)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/indexer/ -run TestParseCaps`
Expected: FAIL — undefined `parseCaps`.

- [ ] **Step 4: Write the implementation**

Create `internal/indexer/caps.go`:
```go
package indexer

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/hellboundg/nexus/internal/core/provider"
)

type CapCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Capabilities struct {
	Limits struct {
		Max     int `json:"max"`
		Default int `json:"default"`
	} `json:"limits"`
	Search      bool          `json:"search"`
	TVSearch    bool          `json:"tvSearch"`
	MovieSearch bool          `json:"movieSearch"`
	Categories  []CapCategory `json:"categories"`
}

func (c Capabilities) supports(t provider.SearchType) bool {
	switch t {
	case provider.SearchTV:
		return c.TVSearch
	case provider.SearchMovie:
		return c.MovieSearch
	default:
		return c.Search
	}
}

// xmlCaps mirrors the Newznab/Torznab caps document.
type xmlCaps struct {
	XMLName xml.Name `xml:"caps"`
	Limits  struct {
		Max     int `xml:"max,attr"`
		Default int `xml:"default,attr"`
	} `xml:"limits"`
	Searching struct {
		Search      xmlAvail `xml:"search"`
		TVSearch    xmlAvail `xml:"tv-search"`
		MovieSearch xmlAvail `xml:"movie-search"`
	} `xml:"searching"`
	Categories struct {
		Categories []struct {
			ID   int    `xml:"id,attr"`
			Name string `xml:"name,attr"`
		} `xml:"category"`
	} `xml:"categories"`
}

type xmlAvail struct {
	Available string `xml:"available,attr"`
}

func parseCaps(data []byte) (Capabilities, error) {
	var x xmlCaps
	if err := xml.Unmarshal(data, &x); err != nil {
		return Capabilities{}, fmt.Errorf("parse caps: %w", err)
	}
	var c Capabilities
	c.Limits.Max = x.Limits.Max
	c.Limits.Default = x.Limits.Default
	c.Search = x.Searching.Search.Available == "yes"
	c.TVSearch = x.Searching.TVSearch.Available == "yes"
	c.MovieSearch = x.Searching.MovieSearch.Available == "yes"
	for _, cat := range x.Categories.Categories {
		c.Categories = append(c.Categories, CapCategory{ID: cat.ID, Name: cat.Name})
	}
	return c, nil
}

func discoverCaps(ctx context.Context, hc *http.Client, base, apiKey string) (Capabilities, error) {
	raw, err := buildSearchURL(base, apiKey, provider.Query{Type: provider.SearchType("caps")})
	if err != nil {
		return Capabilities{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return Capabilities{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Capabilities{}, fmt.Errorf("%w: %v", ErrIndexerUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return Capabilities{}, ErrAuthFailed
	}
	if resp.StatusCode != http.StatusOK {
		return Capabilities{}, fmt.Errorf("%w: caps status %d", ErrIndexerUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Capabilities{}, err
	}
	return parseCaps(body)
}
```

Note: `ErrIndexerUnavailable` and `ErrAuthFailed` are defined in Task 8 (`newznab.go`). This file will not compile standalone until Task 8 lands; that is expected — Step 5 runs only the caps parse test, which does not touch those errors. If you prefer to run the full package build now, temporarily skip; it is resolved by Task 8.

- [ ] **Step 5: Run the caps parse test**

Run: `go test ./internal/indexer/ -run TestParseCaps`
Expected: FAIL to build if run before Task 8 (missing error vars). To unblock immediately, define the error vars now by creating `internal/indexer/errors.go`:
```go
package indexer

import "errors"

var (
	ErrIndexerUnavailable = errors.New("indexer: unavailable")
	ErrAuthFailed         = errors.New("indexer: authentication failed")
	ErrInvalidResponse    = errors.New("indexer: invalid response")
	ErrUnsupportedSearch  = errors.New("indexer: search type not supported")
)
```
Then run: `go test ./internal/indexer/ -run TestParseCaps`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/indexer/caps.go internal/indexer/errors.go internal/indexer/caps_test.go internal/indexer/testdata/caps.xml
git commit -m "feat: add capabilities parsing, discovery, and indexer errors"
```

---

## Task 6: Result parsing (Newznab/Torznab XML → releases)

**Files:**
- Create: `internal/indexer/parse.go`
- Create: `internal/indexer/testdata/newznab_search.xml`, `internal/indexer/testdata/torznab_search.xml`
- Test: `internal/indexer/parse_test.go`

**Interfaces:**
- Consumes: `provider.Release`, `provider.Protocol`.
- Produces: `func parseReleases(data []byte, indexerID string, proto provider.Protocol) ([]provider.Release, error)`.

- [ ] **Step 1: Create the fixtures**

Create `internal/indexer/testdata/newznab_search.xml`:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
  <channel>
    <item>
      <title>The.Show.S02E05.1080p.WEB-DL</title>
      <guid>https://idx.test/details/abc123</guid>
      <comments>https://idx.test/details/abc123#comments</comments>
      <link>https://idx.test/getnzb/abc123.nzb</link>
      <pubDate>Mon, 15 Jun 2026 10:00:00 +0000</pubDate>
      <enclosure url="https://idx.test/getnzb/abc123.nzb" length="1610612736" type="application/x-nzb"/>
      <newznab:attr name="category" value="5000"/>
      <newznab:attr name="category" value="5040"/>
      <newznab:attr name="size" value="1610612736"/>
    </item>
  </channel>
</rss>
```

Create `internal/indexer/testdata/torznab_search.xml`:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <item>
      <title>The.Show.S02E05.1080p.BluRay.x264-GROUP</title>
      <guid>https://idx.test/t/999</guid>
      <comments>https://idx.test/t/999</comments>
      <link>https://idx.test/download/999.torrent</link>
      <pubDate>Tue, 16 Jun 2026 08:30:00 +0000</pubDate>
      <enclosure url="https://idx.test/download/999.torrent" length="2147483648" type="application/x-bittorrent"/>
      <torznab:attr name="category" value="5040"/>
      <torznab:attr name="seeders" value="42"/>
      <torznab:attr name="peers" value="50"/>
    </item>
  </channel>
</rss>
```

- [ ] **Step 2: Write the failing test**

Create `internal/indexer/parse_test.go`:
```go
package indexer

import (
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestParseNewznab(t *testing.T) {
	data, _ := os.ReadFile("testdata/newznab_search.xml")
	rels, err := parseReleases(data, "1", provider.ProtocolUsenet)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Fatalf("want 1 release, got %d", len(rels))
	}
	r := rels[0]
	if r.Title != "The.Show.S02E05.1080p.WEB-DL" {
		t.Errorf("title = %q", r.Title)
	}
	if r.Size != 1610612736 {
		t.Errorf("size = %d", r.Size)
	}
	if r.Protocol != provider.ProtocolUsenet {
		t.Errorf("protocol = %q", r.Protocol)
	}
	if len(r.Categories) != 2 || r.Categories[0] != 5000 {
		t.Errorf("categories = %v", r.Categories)
	}
	if r.DownloadURL == "" || r.IndexerID != "1" {
		t.Errorf("download/indexer = %q %q", r.DownloadURL, r.IndexerID)
	}
	if r.Seeders != nil {
		t.Errorf("usenet should have nil seeders")
	}
	if r.PublishDate.IsZero() {
		t.Errorf("pubdate not parsed")
	}
}

func TestParseTorznab(t *testing.T) {
	data, _ := os.ReadFile("testdata/torznab_search.xml")
	rels, err := parseReleases(data, "2", provider.ProtocolTorrent)
	if err != nil {
		t.Fatal(err)
	}
	r := rels[0]
	if r.Protocol != provider.ProtocolTorrent {
		t.Errorf("protocol = %q", r.Protocol)
	}
	if r.Seeders == nil || *r.Seeders != 42 {
		t.Errorf("seeders = %v", r.Seeders)
	}
	if r.Leechers == nil || *r.Leechers != 8 { // peers(50) - seeders(42)
		t.Errorf("leechers = %v (want 8)", r.Leechers)
	}
	if r.Size != 2147483648 {
		t.Errorf("size = %d", r.Size)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/indexer/ -run TestParse`
Expected: FAIL — undefined `parseReleases`.

- [ ] **Step 4: Write the implementation**

Create `internal/indexer/parse.go`:
```go
package indexer

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

type xmlRSS struct {
	Channel struct {
		Items []xmlItem `xml:"item"`
	} `xml:"channel"`
}

type xmlItem struct {
	Title     string `xml:"title"`
	GUID      string `xml:"guid"`
	Comments  string `xml:"comments"`
	Link      string `xml:"link"`
	PubDate   string `xml:"pubDate"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
	// Matches both newznab:attr and torznab:attr (encoding/xml matches local name).
	Attrs []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

var pubDateLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822}

func parseReleases(data []byte, indexerID string, proto provider.Protocol) ([]provider.Release, error) {
	var rss xmlRSS
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	out := make([]provider.Release, 0, len(rss.Channel.Items))
	for _, it := range rss.Channel.Items {
		r := provider.Release{
			Title:       it.Title,
			GUID:        it.GUID,
			InfoURL:     it.Comments,
			DownloadURL: it.Enclosure.URL,
			Size:        it.Enclosure.Length,
			IndexerID:   indexerID,
			Protocol:    proto,
		}
		if r.DownloadURL == "" {
			r.DownloadURL = it.Link
		}
		if r.InfoURL == "" {
			r.InfoURL = it.GUID
		}
		if t, ok := parsePubDate(it.PubDate); ok {
			r.PublishDate = t
		}

		var seeders, peers *int
		for _, a := range it.Attrs {
			switch a.Name {
			case "category":
				if n, err := strconv.Atoi(a.Value); err == nil {
					r.Categories = append(r.Categories, n)
				}
			case "size":
				if r.Size == 0 {
					if n, err := strconv.ParseInt(a.Value, 10, 64); err == nil {
						r.Size = n
					}
				}
			case "seeders":
				if n, err := strconv.Atoi(a.Value); err == nil {
					seeders = &n
				}
			case "peers":
				if n, err := strconv.Atoi(a.Value); err == nil {
					peers = &n
				}
			}
		}
		if proto == provider.ProtocolTorrent {
			r.Seeders = seeders
			if seeders != nil && peers != nil {
				l := *peers - *seeders
				if l < 0 {
					l = 0
				}
				r.Leechers = &l
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func parsePubDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range pubDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/indexer/ -run TestParse`
Expected: PASS (both Newznab and Torznab).

- [ ] **Step 6: Commit**

```bash
git add internal/indexer/parse.go internal/indexer/parse_test.go internal/indexer/testdata/newznab_search.xml internal/indexer/testdata/torznab_search.xml
git commit -m "feat: parse Newznab/Torznab XML results into releases"
```

---

## Task 7: Per-indexer rate limiter

**Files:**
- Create: `internal/indexer/ratelimit.go`
- Test: `internal/indexer/ratelimit_test.go`

**Interfaces:**
- Produces:
  - `func newLimiter(interval time.Duration) *limiter`.
  - `func (l *limiter) wait(ctx context.Context) error` — blocks until `interval` has elapsed since the previous `wait`, or returns `ctx.Err()` if the context is canceled first.

- [ ] **Step 1: Write the failing test**

Create `internal/indexer/ratelimit_test.go`:
```go
package indexer

import (
	"context"
	"testing"
	"time"
)

func TestLimiterEnforcesInterval(t *testing.T) {
	l := newLimiter(50 * time.Millisecond)
	ctx := context.Background()

	start := time.Now()
	if err := l.wait(ctx); err != nil { // first call: immediate
		t.Fatal(err)
	}
	if err := l.wait(ctx); err != nil { // second call: waits ~50ms
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 45*time.Millisecond {
		t.Fatalf("second wait returned too early: %v", elapsed)
	}
}

func TestLimiterRespectsContext(t *testing.T) {
	l := newLimiter(time.Hour)
	ctx := context.Background()
	_ = l.wait(ctx) // consume the first immediate slot

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.wait(cctx); err == nil {
		t.Fatal("expected context error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexer/ -run TestLimiter`
Expected: FAIL — undefined `newLimiter`.

- [ ] **Step 3: Write the implementation**

Create `internal/indexer/ratelimit.go`:
```go
package indexer

import (
	"context"
	"sync"
	"time"
)

// limiter enforces a minimum interval between successive wait() calls.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newLimiter(interval time.Duration) *limiter {
	return &limiter{interval: interval}
}

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	wait := time.Until(l.next)
	if wait < 0 {
		wait = 0
	}
	l.next = now.Add(wait).Add(l.interval)
	l.mu.Unlock()

	if wait == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/indexer/ -run TestLimiter`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/ratelimit.go internal/indexer/ratelimit_test.go
git commit -m "feat: add per-indexer minimum-interval rate limiter"
```

---

## Task 8: NewznabClient (implements provider.Indexer)

**Files:**
- Modify: `internal/indexer/indexer.go` (replace the Foundation stub with package doc; `Service` is added in Task 10 — for now just the package clause + doc)
- Create: `internal/indexer/newznab.go`
- Test: `internal/indexer/newznab_test.go`

**Interfaces:**
- Consumes: `provider.Indexer`, `provider.Query`, `provider.Release`, `provider.Protocol`, `buildSearchURL`, `parseReleases`, `Capabilities`, `limiter`, error vars from Task 5.
- Produces:
  - `type NewznabClient struct { ... }`.
  - `func newClient(id, name, base, apiKey string, proto provider.Protocol, priority int, caps Capabilities, hc *http.Client, lim *limiter) *NewznabClient`.
  - Methods: `ID() string`, `Priority() int`, `Supports(provider.Query) bool`, `Search(ctx, provider.Query) ([]provider.Release, error)`.
  - `NewznabClient` satisfies both `provider.Indexer` and the `searchable` interface (defined in Task 9).

- [ ] **Step 1: Replace the stub package doc**

Overwrite `internal/indexer/indexer.go` with just the package clause (the `Service` type lands in Task 10):
```go
// Package indexer implements Prowlarr-equivalent indexer management and search
// over the Newznab and Torznab protocols. It fills the provider.Indexer contract
// declared in core/provider and imports only internal/core/*.
package indexer
```

- [ ] **Step 2: Write the failing test**

Create `internal/indexer/newznab_test.go`:
```go
package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func newTorznabFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	body, _ := os.ReadFile("testdata/torznab_search.xml")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "KEY" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(body)
	}))
}

func TestNewznabClientSearch(t *testing.T) {
	srv := newTorznabFixtureServer(t)
	defer srv.Close()

	caps := Capabilities{Search: true}
	c := newClient("7", "fixture", srv.URL, "KEY", provider.ProtocolTorrent, 25, caps,
		srv.Client(), newLimiter(0))

	rels, err := c.Search(context.Background(), provider.Query{Term: "the show"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].IndexerID != "7" || rels[0].Seeders == nil {
		t.Fatalf("unexpected results: %+v", rels)
	}
}

func TestNewznabClientAuthFailure(t *testing.T) {
	srv := newTorznabFixtureServer(t)
	defer srv.Close()

	c := newClient("7", "fixture", srv.URL, "WRONG", provider.ProtocolTorrent, 25,
		Capabilities{Search: true}, srv.Client(), newLimiter(0))
	_, err := c.Search(context.Background(), provider.Query{Term: "x"})
	if err != ErrAuthFailed {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

func TestNewznabClientSupports(t *testing.T) {
	c := newClient("1", "n", "http://x", "", provider.ProtocolUsenet, 25,
		Capabilities{Search: true, TVSearch: false}, http.DefaultClient, newLimiter(0))
	if !c.Supports(provider.Query{Type: provider.SearchGeneric}) {
		t.Error("generic should be supported")
	}
	if c.Supports(provider.Query{Type: provider.SearchTV}) {
		t.Error("tv should NOT be supported")
	}
	_ = time.Second
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/indexer/ -run TestNewznabClient`
Expected: FAIL — undefined `newClient`.

- [ ] **Step 4: Write the implementation**

Create `internal/indexer/newznab.go`:
```go
package indexer

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// NewznabClient is a single configured Newznab/Torznab indexer.
type NewznabClient struct {
	id       string
	name     string
	base     string
	apiKey   string
	protocol provider.Protocol
	priority int
	caps     Capabilities
	http     *http.Client
	lim      *limiter
}

func newClient(id, name, base, apiKey string, proto provider.Protocol, priority int,
	caps Capabilities, hc *http.Client, lim *limiter) *NewznabClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	if lim == nil {
		lim = newLimiter(0)
	}
	return &NewznabClient{
		id: id, name: name, base: base, apiKey: apiKey, protocol: proto,
		priority: priority, caps: caps, http: hc, lim: lim,
	}
}

func (c *NewznabClient) ID() string    { return c.id }
func (c *NewznabClient) Priority() int { return c.priority }

func (c *NewznabClient) Supports(q provider.Query) bool {
	t := q.Type
	if t == "" {
		t = provider.SearchGeneric
	}
	return c.caps.supports(t)
}

func (c *NewznabClient) Search(ctx context.Context, q provider.Query) ([]provider.Release, error) {
	if !c.Supports(q) {
		return nil, ErrUnsupportedSearch
	}
	if err := c.lim.wait(ctx); err != nil {
		return nil, err
	}
	raw, err := buildSearchURL(c.base, c.apiKey, q)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIndexerUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrAuthFailed
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrIndexerUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return parseReleases(body, c.id, c.protocol)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/indexer/ -run TestNewznabClient`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/indexer/indexer.go internal/indexer/newznab.go internal/indexer/newznab_test.go
git commit -m "feat: add NewznabClient implementing provider.Indexer"
```

---

## Task 9: SearchService — fan-out, aggregate, dedupe, sort

**Files:**
- Create: `internal/indexer/search.go`
- Test: `internal/indexer/search_test.go`

**Interfaces:**
- Consumes: `provider.Indexer`, `provider.Query`, `provider.Release`, `provider.Protocol`.
- Produces:
  - `type searchable interface { provider.Indexer; Priority() int; Supports(provider.Query) bool }`.
  - `type IndexerError struct { IndexerID string; Message string }`.
  - `type SearchResult struct { Releases []provider.Release; IndexerErrors []IndexerError }`.
  - `func searchAll(ctx context.Context, clients []searchable, q provider.Query, perTimeout time.Duration) SearchResult`.

- [ ] **Step 1: Write the failing test**

Create `internal/indexer/search_test.go`:
```go
package indexer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

type fakeClient struct {
	id       string
	priority int
	releases []provider.Release
	err      error
	supports bool
}

func (f *fakeClient) ID() string    { return f.id }
func (f *fakeClient) Priority() int { return f.priority }
func (f *fakeClient) Supports(provider.Query) bool {
	return f.supports
}
func (f *fakeClient) Search(context.Context, provider.Query) ([]provider.Release, error) {
	return f.releases, f.err
}

func rel(guid string, pub time.Time) provider.Release {
	return provider.Release{GUID: guid, Title: guid, PublishDate: pub, Protocol: provider.ProtocolUsenet}
}

func TestSearchAllAggregatesDedupesSorts(t *testing.T) {
	older := time.Unix(1000, 0)
	newer := time.Unix(2000, 0)

	a := &fakeClient{id: "1", priority: 10, supports: true, releases: []provider.Release{
		rel("dup", older), rel("a-old", older),
	}}
	b := &fakeClient{id: "2", priority: 20, supports: true, releases: []provider.Release{
		rel("dup", older), rel("b-new", newer),
	}}
	failing := &fakeClient{id: "3", priority: 5, supports: true, err: errors.New("down")}
	skipped := &fakeClient{id: "4", priority: 1, supports: false, releases: []provider.Release{rel("x", newer)}}

	res := searchAll(context.Background(), []searchable{a, b, failing, skipped}, provider.Query{}, time.Second)

	// dup collapsed → 3 unique releases (dup, a-old, b-new)
	if len(res.Releases) != 3 {
		t.Fatalf("want 3 releases, got %d: %+v", len(res.Releases), res.Releases)
	}
	// newest first
	if res.Releases[0].GUID != "b-new" {
		t.Errorf("first should be b-new, got %q", res.Releases[0].GUID)
	}
	// failing indexer surfaced; skipped indexer produced nothing and no error
	if len(res.IndexerErrors) != 1 || res.IndexerErrors[0].IndexerID != "3" {
		t.Fatalf("indexer errors = %+v", res.IndexerErrors)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexer/ -run TestSearchAll`
Expected: FAIL — undefined `searchAll`.

- [ ] **Step 3: Write the implementation**

Create `internal/indexer/search.go`:
```go
package indexer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

type searchable interface {
	provider.Indexer
	Priority() int
	Supports(provider.Query) bool
}

type IndexerError struct {
	IndexerID string `json:"indexerId"`
	Message   string `json:"message"`
}

type SearchResult struct {
	Releases      []provider.Release `json:"releases"`
	IndexerErrors []IndexerError     `json:"indexerErrors"`
}

func searchAll(ctx context.Context, clients []searchable, q provider.Query, perTimeout time.Duration) SearchResult {
	type outcome struct {
		id       string
		priority int
		releases []provider.Release
		err      error
	}

	var wg sync.WaitGroup
	results := make([]outcome, len(clients))
	for i, c := range clients {
		if !c.Supports(q) {
			results[i] = outcome{id: c.ID(), priority: c.Priority()} // skipped, no error
			continue
		}
		wg.Add(1)
		go func(i int, c searchable) {
			defer wg.Done()
			cctx := ctx
			var cancel context.CancelFunc
			if perTimeout > 0 {
				cctx, cancel = context.WithTimeout(ctx, perTimeout)
				defer cancel()
			}
			rels, err := c.Search(cctx, q)
			results[i] = outcome{id: c.ID(), priority: c.Priority(), releases: rels, err: err}
		}(i, c)
	}
	wg.Wait()

	// Process in ascending priority so higher-priority indexers win dedupe ties.
	order := make([]int, len(results))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return results[order[a]].priority < results[order[b]].priority
	})

	priorityByID := make(map[string]int, len(results))
	seen := make(map[string]struct{})
	var out SearchResult
	for _, idx := range order {
		o := results[idx]
		priorityByID[o.id] = o.priority
		if o.err != nil {
			out.IndexerErrors = append(out.IndexerErrors, IndexerError{IndexerID: o.id, Message: o.err.Error()})
			continue
		}
		for _, r := range o.releases {
			key := dedupeKey(r)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out.Releases = append(out.Releases, r)
		}
	}

	sort.SliceStable(out.Releases, func(a, b int) bool {
		ra, rb := out.Releases[a], out.Releases[b]
		if !ra.PublishDate.Equal(rb.PublishDate) {
			return ra.PublishDate.After(rb.PublishDate)
		}
		return priorityByID[ra.IndexerID] < priorityByID[rb.IndexerID]
	})
	return out
}

func dedupeKey(r provider.Release) string {
	if r.GUID != "" {
		return string(r.Protocol) + "|guid|" + r.GUID
	}
	return string(r.Protocol) + "|ts|" + strings.ToLower(strings.TrimSpace(r.Title)) + "|" + fmt.Sprint(r.Size)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/indexer/ -run TestSearchAll`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/search.go internal/indexer/search_test.go
git commit -m "feat: add fan-out search aggregation with dedupe and sort"
```

---

## Task 10: Service — live clients + reload

**Files:**
- Modify: `internal/indexer/indexer.go` (add `Service`)
- Test: `internal/indexer/service_test.go`

**Interfaces:**
- Consumes: `*store.Store`, `store.Indexer`, `NewznabClient`, `newClient`, `Capabilities`, `searchAll`, `SearchResult`.
- Produces:
  - `func NewService(st *store.Store) *Service`.
  - `func (s *Service) WithHTTPClient(hc *http.Client) *Service`.
  - `func (s *Service) Reload(ctx context.Context) error` — rebuilds the live client set from enabled indexers in the store.
  - `func (s *Service) Search(ctx context.Context, q provider.Query) SearchResult`.

- [ ] **Step 1: Write the failing test**

Create `internal/indexer/service_test.go`:
```go
package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return store.New(db)
}

func TestServiceReloadAndSearch(t *testing.T) {
	body, _ := os.ReadFile("testdata/torznab_search.xml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateIndexer(ctx, store.Indexer{
		Name: "t", Implementation: "torznab", BaseURL: srv.URL, APIKey: "",
		Enabled: true, Priority: 25,
	}); err != nil {
		t.Fatal(err)
	}
	// A disabled indexer must be ignored by Reload.
	if _, err := st.CreateIndexer(ctx, store.Indexer{
		Name: "off", Implementation: "newznab", BaseURL: srv.URL, Enabled: false, Priority: 1,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(st).WithHTTPClient(srv.Client())
	if err := svc.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	res := svc.Search(ctx, provider.Query{Term: "the show"})
	if len(res.Releases) != 1 {
		t.Fatalf("want 1 release, got %d (errors=%+v)", len(res.Releases), res.IndexerErrors)
	}
	if res.Releases[0].Protocol != provider.ProtocolTorrent {
		t.Fatalf("protocol = %q", res.Releases[0].Protocol)
	}
}
```

Note: this test defines `newTestStore` for the `indexer` package (distinct from the one in `core/store`, which is in a different package).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexer/ -run TestServiceReloadAndSearch`
Expected: FAIL — undefined `NewService`.

- [ ] **Step 3: Write the implementation**

Append to `internal/indexer/indexer.go` (below the package doc):
```go
import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

const (
	defaultRateInterval  = 2 * time.Second
	defaultSearchTimeout = 30 * time.Second
)

// Service owns the live set of configured indexer clients and runs searches.
type Service struct {
	store         *store.Store
	http          *http.Client
	rateInterval  time.Duration
	searchTimeout time.Duration

	mu      sync.RWMutex
	clients []searchable
}

func NewService(st *store.Store) *Service {
	return &Service{
		store:         st,
		http:          &http.Client{Timeout: 60 * time.Second},
		rateInterval:  defaultRateInterval,
		searchTimeout: defaultSearchTimeout,
	}
}

func (s *Service) WithHTTPClient(hc *http.Client) *Service {
	s.http = hc
	return s
}

// Reload rebuilds the live client set from enabled indexers in the store.
func (s *Service) Reload(ctx context.Context) error {
	rows, err := s.store.ListIndexers(ctx, true)
	if err != nil {
		return err
	}
	clients := make([]searchable, 0, len(rows))
	for _, ix := range rows {
		var caps Capabilities
		if ix.Caps != "" {
			_ = json.Unmarshal([]byte(ix.Caps), &caps)
		}
		proto := provider.ProtocolUsenet
		if ix.Implementation == "torznab" {
			proto = provider.ProtocolTorrent
		}
		clients = append(clients, newClient(
			strconv.FormatInt(ix.ID, 10), ix.Name, ix.BaseURL, ix.APIKey,
			proto, ix.Priority, caps, s.http, newLimiter(s.rateInterval),
		))
	}
	s.mu.Lock()
	s.clients = clients
	s.mu.Unlock()
	return nil
}

func (s *Service) snapshot() []searchable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]searchable, len(s.clients))
	copy(out, s.clients)
	return out
}

func (s *Service) Search(ctx context.Context, q provider.Query) SearchResult {
	return searchAll(ctx, s.snapshot(), q, s.searchTimeout)
}
```

Note: the Task 8 `indexer.go` is only a package clause + doc comment with no imports, so this single `import` block is the file's first and is valid.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/indexer/ -run TestServiceReloadAndSearch`
Expected: PASS.

- [ ] **Step 5: Full package test + tidy**

Run: `go test ./internal/indexer/...` then `go vet ./internal/indexer/...`
Expected: PASS, no vet errors. Remove any unused import/var flagged.

- [ ] **Step 6: Commit**

```bash
git add internal/indexer/indexer.go internal/indexer/service_test.go
git commit -m "feat: add indexer Service with live-client reload and search"
```

---

## Task 11: Health check command + event

**Files:**
- Create: `internal/indexer/health.go`
- Test: `internal/indexer/health_test.go`

**Interfaces:**
- Consumes: `*store.Store`, `*events.Bus`, `command.Reporter`, `discoverCaps`, `store.Indexer`.
- Produces:
  - `type IndexerStatusChanged struct { IndexerID int64; Status, Message string }` with `Name() string == "indexer.status"`.
  - `func NewHealthCheck(st *store.Store, bus *events.Bus, hc *http.Client) *HealthCheck`.
  - Methods: `Name() string` (`"IndexerHealthCheck"`), `Run(ctx context.Context, r command.Reporter) error` — satisfies `command.Command`.

- [ ] **Step 1: Write the failing test**

Create `internal/indexer/health_test.go`:
```go
package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestHealthCheckUpdatesStatusAndEmits(t *testing.T) {
	caps, _ := os.ReadFile("testdata/caps.xml")
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(caps)
	}))
	defer okSrv.Close()
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badSrv.Close()

	st := newTestStore(t)
	ctx := context.Background()
	goodID, _ := st.CreateIndexer(ctx, store.Indexer{Name: "good", Implementation: "newznab", BaseURL: okSrv.URL, Enabled: true, Priority: 25})
	badID, _ := st.CreateIndexer(ctx, store.Indexer{Name: "bad", Implementation: "newznab", BaseURL: badSrv.URL, Enabled: true, Priority: 25})

	bus := events.New()
	var emitted []IndexerStatusChanged
	bus.Subscribe("indexer.status", func(_ context.Context, e events.Event) {
		if sc, ok := e.(IndexerStatusChanged); ok {
			emitted = append(emitted, sc)
		}
	})

	hc := NewHealthCheck(st, bus, okSrv.Client())
	if err := hc.Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}

	good, _ := st.GetIndexer(ctx, goodID)
	if good.Status != "ok" {
		t.Errorf("good status = %q want ok", good.Status)
	}
	bad, _ := st.GetIndexer(ctx, badID)
	if bad.Status != "failed" || bad.FailMessage == "" {
		t.Errorf("bad status = %q msg = %q", bad.Status, bad.FailMessage)
	}
	if len(emitted) != 2 {
		t.Fatalf("want 2 events, got %d", len(emitted))
	}
}
```

Note: `okSrv.Client()` is used for both servers; both are `httptest` loopback servers so a shared client is fine.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexer/ -run TestHealthCheck`
Expected: FAIL — undefined `NewHealthCheck`.

- [ ] **Step 3: Write the implementation**

Create `internal/indexer/health.go`:
```go
package indexer

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

// IndexerStatusChanged is published after each health evaluation.
type IndexerStatusChanged struct {
	IndexerID int64  `json:"indexerId"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

func (IndexerStatusChanged) Name() string { return "indexer.status" }

// HealthCheck pings every enabled indexer's caps endpoint and records status.
type HealthCheck struct {
	store *store.Store
	bus   *events.Bus
	http  *http.Client
}

func NewHealthCheck(st *store.Store, bus *events.Bus, hc *http.Client) *HealthCheck {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &HealthCheck{store: st, bus: bus, http: hc}
}

func (h *HealthCheck) Name() string { return "IndexerHealthCheck" }

func (h *HealthCheck) Run(ctx context.Context, r command.Reporter) error {
	rows, err := h.store.ListIndexers(ctx, true)
	if err != nil {
		return err
	}
	for i, ix := range rows {
		status, msg, capsJSON := "ok", "", ""
		caps, err := discoverCaps(ctx, h.http, ix.BaseURL, ix.APIKey)
		if err != nil {
			status, msg = "failed", err.Error()
		} else if b, mErr := json.Marshal(caps); mErr == nil {
			capsJSON = string(b)
		}
		if err := h.store.SetIndexerStatus(ctx, ix.ID, status, msg, capsJSON); err != nil {
			return err
		}
		if h.bus != nil {
			h.bus.Publish(ctx, IndexerStatusChanged{IndexerID: ix.ID, Status: status, Message: msg})
		}
		if len(rows) > 0 {
			r.Progress((i+1)*100/len(rows), ix.Name)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/indexer/ -run TestHealthCheck`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/health.go internal/indexer/health_test.go
git commit -m "feat: add indexer health-check command emitting status events"
```

---

## Task 12: Indexer API sub-router

**Files:**
- Create: `internal/indexer/api.go`
- Test: `internal/indexer/api_test.go`

**Interfaces:**
- Consumes: `*store.Store`, `*Service`, `store.Indexer`, `provider.Query`, `api.WriteJSON`, `api.WriteError`, chi.
- Produces:
  - `func NewAPI(st *store.Store, svc *Service, hc *http.Client) *API`.
  - `func (a *API) Mount(r chi.Router)` — registers `/indexer*` and `/search` on the given (already-authed) router.

- [ ] **Step 1: Write the failing test**

Create `internal/indexer/api_test.go`:
```go
package indexer

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func mountedRouter(t *testing.T, svc *Service, a *API) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { a.Mount(r) })
	return r
}

func TestIndexerAPICreateAndSearch(t *testing.T) {
	body, _ := os.ReadFile("testdata/torznab_search.xml")
	caps, _ := os.ReadFile("testdata/caps.xml")
	idx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = w.Write(caps)
			return
		}
		_, _ = w.Write(body)
	}))
	defer idx.Close()

	st := newTestStore(t)
	svc := NewService(st).WithHTTPClient(idx.Client())
	a := NewAPI(st, svc, idx.Client())
	router := mountedRouter(t, svc, a)

	// Create indexer.
	payload, _ := json.Marshal(map[string]any{
		"name": "t", "implementation": "torznab", "baseUrl": idx.URL, "enabled": true, "priority": 25,
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/indexer", bytes.NewReader(payload)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}

	// List.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/indexer", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}

	// Search.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/search?query=the+show", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d body=%s", rec.Code, rec.Body.String())
	}
	var res SearchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Releases) != 1 || res.Releases[0].Protocol != provider.ProtocolTorrent {
		t.Fatalf("unexpected search result: %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexer/ -run TestIndexerAPI`
Expected: FAIL — undefined `NewAPI`.

- [ ] **Step 3: Write the implementation**

Create `internal/indexer/api.go`:
```go
package indexer

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type API struct {
	store *store.Store
	svc   *Service
	http  *http.Client
}

func NewAPI(st *store.Store, svc *Service, hc *http.Client) *API {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &API{store: st, svc: svc, http: hc}
}

// Mount registers indexer routes on an already-authenticated router (the
// /api/v1 group). Call via api.NewRouter(..., indexerAPI.Mount).
func (a *API) Mount(r chi.Router) {
	r.Route("/indexer", func(r chi.Router) {
		r.Get("/", a.list)
		r.Post("/", a.create)
		r.Get("/schema", a.schema)
		r.Post("/test", a.testUnsaved)
		r.Get("/{id}", a.get)
		r.Put("/{id}", a.update)
		r.Delete("/{id}", a.delete)
		r.Post("/{id}/test", a.testSaved)
	})
	r.Get("/search", a.search)
}

type indexerPayload struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation"`
	BaseURL        string `json:"baseUrl"`
	APIKey         string `json:"apiKey"`
	Enabled        bool   `json:"enabled"`
	Priority       int    `json:"priority"`
	Categories     []int  `json:"categories"`
}

func (p indexerPayload) toStore() store.Indexer {
	pri := p.Priority
	if pri == 0 {
		pri = 25
	}
	return store.Indexer{
		Name: p.Name, Implementation: p.Implementation, BaseURL: p.BaseURL,
		APIKey: p.APIKey, Enabled: p.Enabled, Priority: pri, Categories: p.Categories,
	}
}

func (p indexerPayload) valid() (string, bool) {
	if strings.TrimSpace(p.Name) == "" {
		return "name is required", false
	}
	if p.Implementation != "newznab" && p.Implementation != "torznab" {
		return "implementation must be newznab or torznab", false
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return "baseUrl is required", false
	}
	return "", true
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListIndexers(r.Context(), false)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list indexers")
		return
	}
	if rows == nil {
		rows = []store.Indexer{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var p indexerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	id, err := a.store.CreateIndexer(r.Context(), p.toStore())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to create indexer")
		return
	}
	// Best-effort caps discovery; failure is non-fatal (status becomes failed).
	a.refreshOne(r, id, p.BaseURL, p.APIKey)
	_ = a.svc.Reload(r.Context())
	ix, _ := a.store.GetIndexer(r.Context(), id)
	api.WriteJSON(w, http.StatusCreated, ix)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	ix, err := a.store.GetIndexer(r.Context(), id)
	if err == store.ErrNotFound {
		api.WriteError(w, http.StatusNotFound, "not_found", "indexer not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load indexer")
		return
	}
	api.WriteJSON(w, http.StatusOK, ix)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var p indexerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	ix := p.toStore()
	ix.ID = id
	if err := a.store.UpdateIndexer(r.Context(), ix); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to update indexer")
		return
	}
	_ = a.svc.Reload(r.Context())
	updated, _ := a.store.GetIndexer(r.Context(), id)
	api.WriteJSON(w, http.StatusOK, updated)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteIndexer(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete indexer")
		return
	}
	_ = a.svc.Reload(r.Context())
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) testSaved(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	ix, err := a.store.GetIndexer(r.Context(), id)
	if err == store.ErrNotFound {
		api.WriteError(w, http.StatusNotFound, "not_found", "indexer not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load indexer")
		return
	}
	a.writeTestResult(w, r, ix.BaseURL, ix.APIKey)
	a.refreshOne(r, id, ix.BaseURL, ix.APIKey)
	_ = a.svc.Reload(r.Context())
}

func (a *API) testUnsaved(w http.ResponseWriter, r *http.Request) {
	var p indexerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	a.writeTestResult(w, r, p.BaseURL, p.APIKey)
}

func (a *API) writeTestResult(w http.ResponseWriter, r *http.Request, base, apiKey string) {
	caps, err := discoverCaps(r.Context(), a.http, base, apiKey)
	if err != nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "capabilities": caps})
}

func (a *API) schema(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, []map[string]any{
		{"implementation": "newznab", "protocol": "usenet", "fields": indexerSchemaFields()},
		{"implementation": "torznab", "protocol": "torrent", "fields": indexerSchemaFields()},
	})
}

func indexerSchemaFields() []map[string]any {
	return []map[string]any{
		{"name": "name", "type": "string", "required": true},
		{"name": "baseUrl", "type": "string", "required": true},
		{"name": "apiKey", "type": "string", "required": false},
		{"name": "categories", "type": "int[]", "required": false},
		{"name": "priority", "type": "int", "required": false, "default": 25},
		{"name": "enabled", "type": "bool", "required": false, "default": true},
	}
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := provider.Query{
		Type: provider.SearchType(defaultStr(r.URL.Query().Get("type"), string(provider.SearchGeneric))),
		Term: r.URL.Query().Get("query"),
	}
	for _, c := range strings.Split(r.URL.Query().Get("categories"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			if n, err := strconv.Atoi(c); err == nil {
				q.Categories = append(q.Categories, n)
			}
		}
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		q.Limit = n
	}
	res := a.svc.Search(r.Context(), q)
	if res.Releases == nil {
		res.Releases = []provider.Release{}
	}
	if res.IndexerErrors == nil {
		res.IndexerErrors = []IndexerError{}
	}
	api.WriteJSON(w, http.StatusOK, res)
}

// refreshOne runs caps discovery for one indexer and records the result.
func (a *API) refreshOne(r *http.Request, id int64, base, apiKey string) {
	caps, err := discoverCaps(r.Context(), a.http, base, apiKey)
	if err != nil {
		_ = a.store.SetIndexerStatus(r.Context(), id, "failed", err.Error(), "")
		return
	}
	if b, mErr := json.Marshal(caps); mErr == nil {
		_ = a.store.SetIndexerStatus(r.Context(), id, "ok", "", string(b))
	}
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func defaultStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/indexer/ -run TestIndexerAPI`
Expected: PASS.

- [ ] **Step 5: Full indexer package sweep**

Run: `go test ./internal/indexer/...`
Expected: PASS (all tasks' tests).

- [ ] **Step 6: Commit**

```bash
git add internal/indexer/api.go internal/indexer/api_test.go
git commit -m "feat: add indexer REST API (CRUD, test, schema, search)"
```

---

## Task 13: Composition wiring + full sweep

**Files:**
- Modify: `cmd/nexus/main.go`
- Test: `cmd/nexus/main_test.go` (extend)

**Interfaces:**
- Consumes: `indexer.NewService`, `indexer.NewAPI`, `indexer.NewHealthCheck`, `api.Deps.WSForward`, `scheduler.Every`.
- Produces: a running server that mounts `/api/v1/indexer*` and `/api/v1/search`, reloads indexers at startup, and schedules health checks.

- [ ] **Step 1: Extend the run test**

Add to `cmd/nexus/main_test.go` a check that the indexer route is mounted (append this test to the file):
```go
func TestRunMountsIndexerRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9598")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9598/api/v1/indexer", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/indexer status = %d want 200", status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/nexus/ -run TestRunMountsIndexerRoutes`
Expected: FAIL — route returns 404 (indexer not yet wired).

- [ ] **Step 3: Wire the indexer module into `main.go`**

In `cmd/nexus/main.go`, add the import:
```go
	"github.com/hellboundg/nexus/internal/indexer"
```
After the scheduler is created (`sch := scheduler.New(mgr)`) and before it starts, register the health check. Locate:
```go
	bus := events.New().WithLogger(log)
	mgr := command.NewManager(st, bus, 4).WithLogger(log)
	mgr.Start()
	sch := scheduler.New(mgr)
	sch.Start()
```
Replace with:
```go
	bus := events.New().WithLogger(log)
	mgr := command.NewManager(st, bus, 4).WithLogger(log)
	mgr.Start()

	idxSvc := indexer.NewService(st)
	if err := idxSvc.Reload(ctx); err != nil {
		return err
	}
	idxAPI := indexer.NewAPI(st, idxSvc, nil)

	sch := scheduler.New(mgr)
	sch.Every(15*time.Minute, func() command.Command {
		return indexer.NewHealthCheck(st, bus, nil)
	})
	sch.Start()
```
Then update the router construction. Locate:
```go
	authSvc := auth.NewService(st, cfg.APIKey)
	router := api.NewRouter(api.Deps{
		Auth: authSvc, Store: st, Version: version.Version(), Bus: bus,
	}, web.Handler())
```
Replace with:
```go
	authSvc := auth.NewService(st, cfg.APIKey)
	router := api.NewRouter(api.Deps{
		Auth: authSvc, Store: st, Version: version.Version(), Bus: bus,
		WSForward: []string{"indexer.status"},
	}, web.Handler(), idxAPI.Mount)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/nexus/ -run TestRunMountsIndexerRoutes`
Expected: PASS.

- [ ] **Step 5: Full build + test sweep**

Run:
```bash
CGO_ENABLED=0 go build ./cmd/nexus
go vet ./...
go test ./...
```
Expected: build succeeds; vet clean; all packages PASS. Remove the built binary if produced (`rm -f nexus nexus.exe`).

- [ ] **Step 6: Verify module boundaries**

Run: `go list -deps ./internal/indexer | grep hellboundg`
Expected: only `internal/core/*` packages appear — no `internal/downloadclient`, `internal/media`, or `internal/automation`.

- [ ] **Step 7: Commit**

```bash
git add cmd/nexus/main.go cmd/nexus/main_test.go
git commit -m "feat: wire indexer engine into composition root with health scheduling"
```

---

## Self-Review Notes (author)

- **Spec coverage:** protocols/one-client (Tasks 4–8), caps discovery + cache (Tasks 5, 11, 12), fan-out/aggregate/dedupe/sort with partial success (Task 9), rate limiting (Task 7), health + `indexer.status` event → WS (Tasks 1, 11, 13), CRUD/test/schema/search API (Task 12), persistence (Task 3), extended contracts (Task 2), the two Foundation touch-ups (Task 1 router mounts + WSForward; Task 3 store), composition wiring (Task 13). Acceptance criteria §8.1–8.7 map to Tasks 12, 12/9, 6, 11/13, 7, 13, 13.
- **Deviations:** WS forwarding is implemented generically via `Deps.WSForward` (a third small api addition beyond the two named in the spec) so `core/api` need not import `internal/indexer` to type-assert the event — this preserves the module boundary. Rate-limit default is a package constant (`defaultRateInterval = 2s`); per-indexer override deferred.
- **Type consistency:** `store.Indexer`, `provider.Query`/`Release`/`SearchType`/`Protocol`, `searchable`, `SearchResult`/`IndexerError`, `Capabilities`, `NewznabClient`/`newClient`, `Service`/`NewService`/`Reload`/`Search`, `HealthCheck`/`IndexerStatusChanged` (`"indexer.status"`), `API`/`NewAPI`/`Mount` verified consistent across Tasks 2–13.
- **Module path** `github.com/hellboundg/nexus` used throughout.
- **Ordering note:** Task 5 introduces `errors.go` (needed by both caps and newznab). It is created in Task 5 Step 5 so the package builds before Task 8.
