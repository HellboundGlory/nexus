# Nexus Download Clients Implementation Plan (Sub-project 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the download-client layer — SABnzbd (usenet) + qBittorrent (torrent) adapters, server-side grabbing, protocol+priority routing, a live queue monitor, and a REST API — filling the `provider.DownloadClient` contract from Foundation.

**Architecture:** A Go feature module `internal/downloadclient` that imports `internal/core/*` only, persists client configs through `core/store`, and mounts an authenticated sub-router via the variadic `api.NewRouter` parameter (added in Sub-project 2). Grabs are fetched server-side (the release's `DownloadURL` already carries the indexer apikey, so no `internal/indexer` import is needed) and the fetched bytes are handed to the client; magnets pass through as URLs. The queue monitor runs on the Foundation scheduler and emits `download.status` events forwarded to the WebSocket.

**Tech Stack:** Go 1.22+, `github.com/go-chi/chi/v5`, stdlib `encoding/json`, `net/http`, `net/http/httptest`, `mime/multipart`, `modernc.org/sqlite` (via `core/database`), `log/slog`.

## Global Constraints

- **Language/version:** Go 1.22 or newer.
- **No CGO:** builds must succeed with `CGO_ENABLED=0`. `go test -race` is unavailable (no C compiler); verify concurrency with `-count=N`.
- **Module boundaries:** `internal/downloadclient` MUST import only `internal/core/*` (and stdlib / chi). It must NOT import other feature modules (`internal/indexer`, `internal/media`, `internal/automation`).
- **Data layer:** hand-written `database/sql` behind `*store.Store`. New persistence lives in `internal/core/store`.
- **API surface:** REST under `/api/v1`, consistent JSON error envelope via `api.WriteError(w, status, code, msg)`; success via `api.WriteJSON(w, status, v)`.
- **Commits:** conventional-commit prefixes (`feat:`, `test:`, `fix:`, `chore:`, `docs:`). Commit at the end of each task.
- **Module path:** `github.com/hellboundg/nexus` (all import paths below).
- **Tests:** deterministic and offline — no real network. Use `httptest.Server` and recorded JSON fixtures in `testdata/`.
- **Go env (this machine):** Go is at `C:\Program Files\Go\bin`, NOT on the session PATH. Prefix shell commands with `export PATH="/c/Program Files/Go/bin:$PATH"`.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/core/provider/provider.go` | MODIFY: add `DownloadStatus`, `DownloadItem`; extend `DownloadRequest`; extend `DownloadClient` interface |
| `internal/core/provider/provider_dc_test.go` | CREATE: fake implementing the extended `DownloadClient` (proves the contract) |
| `internal/core/database/migrations/0003_download_clients.sql` | CREATE: `download_clients` table |
| `internal/core/database/database_test.go` | MODIFY: bump applied-migration count 2 → 3 |
| `internal/core/store/downloadclient_store.go` | CREATE: `store.DownloadClient` + CRUD |
| `internal/downloadclient/downloadclient.go` | MODIFY (replace stub): package doc + `Service` (reload, route, Grab, Queue, Remove) |
| `internal/downloadclient/errors.go` | CREATE: typed errors |
| `internal/downloadclient/grab.go` | CREATE: server-side content fetch (magnet passthrough + byte fetch) |
| `internal/downloadclient/sabnzbd.go` | CREATE: `SABnzbdClient` implementing `provider.DownloadClient` |
| `internal/downloadclient/qbittorrent.go` | CREATE: `QBittorrentClient` implementing `provider.DownloadClient` |
| `internal/downloadclient/monitor.go` | CREATE: queue-poll `command.Command` + `DownloadStatusChanged` event |
| `internal/downloadclient/api.go` | CREATE: chi sub-router (CRUD/test/schema/grab/queue/remove) + `Mount` |
| `internal/downloadclient/testdata/*.json` | CREATE: recorded SAB + qBittorrent API fixtures |
| `cmd/nexus/main.go` | MODIFY: construct `Service`, register monitor, mount routes, add `"download.status"` to `WSForward` |

Existing `internal/downloadclient/downloadclient.go` currently holds only a package-doc stub (from Foundation); Task 6 replaces it. The store helpers `boolToInt`, `nonNilInts`, and the `rowScanner` interface already exist in `internal/core/store` (from `indexer_store.go`) — REUSE them; do NOT redefine them.

---

## Task 1: Extend provider download contracts

**Files:**
- Modify: `internal/core/provider/provider.go`
- Test: `internal/core/provider/provider_dc_test.go` (create)

**Interfaces:**
- Consumes: existing `Protocol`, `DownloadRequest`, `DownloadClient`.
- Produces:
  - `type DownloadStatus string` with `StatusQueued`, `StatusDownloading`, `StatusCompleted`, `StatusPaused`, `StatusFailed`, `StatusWarning`.
  - `type DownloadItem struct { ID, Title string; Status DownloadStatus; Progress float64; Size, Downloaded int64; DownloadClientID string; Protocol Protocol; ErrorMessage string }`.
  - Extended `DownloadRequest struct { URL, Title string; Protocol Protocol; IndexerID, Category string; Content []byte }`.
  - Extended `DownloadClient interface { ID() string; Protocol() Protocol; Add(ctx, DownloadRequest) (string, error); Items(ctx) ([]DownloadItem, error); Remove(ctx, id string, deleteData bool) error; Test(ctx) error }`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/provider/provider_dc_test.go`:
```go
package provider

import (
	"context"
	"testing"
)

// fakeDownloadClient proves the extended DownloadClient interface is satisfiable.
type fakeDownloadClient struct {
	id    string
	proto Protocol
	items []DownloadItem
}

func (f fakeDownloadClient) ID() string         { return f.id }
func (f fakeDownloadClient) Protocol() Protocol { return f.proto }
func (f fakeDownloadClient) Add(_ context.Context, d DownloadRequest) (string, error) {
	return d.Title, nil
}
func (f fakeDownloadClient) Items(context.Context) ([]DownloadItem, error) { return f.items, nil }
func (fakeDownloadClient) Remove(context.Context, string, bool) error      { return nil }
func (fakeDownloadClient) Test(context.Context) error                      { return nil }

func TestDownloadClientContract(t *testing.T) {
	var dc DownloadClient = fakeDownloadClient{
		id:    "1",
		proto: ProtocolTorrent,
		items: []DownloadItem{{
			ID: "h1", Title: "x", Status: StatusDownloading, Progress: 42.5,
			Size: 100, Downloaded: 42, DownloadClientID: "1", Protocol: ProtocolTorrent,
		}},
	}
	if dc.Protocol() != ProtocolTorrent {
		t.Fatalf("protocol = %q", dc.Protocol())
	}
	req := DownloadRequest{URL: "magnet:?xt=x", Title: "grab", Protocol: ProtocolTorrent, Content: nil}
	id, err := dc.Add(context.Background(), req)
	if err != nil || id != "grab" {
		t.Fatalf("add: id=%q err=%v", id, err)
	}
	items, _ := dc.Items(context.Background())
	if len(items) != 1 || items[0].Status != StatusDownloading || items[0].Progress != 42.5 {
		t.Fatalf("items = %+v", items)
	}
}

// Registry works with the extended interface too.
func TestDownloadClientRegistry(t *testing.T) {
	reg := NewRegistry[DownloadClient]()
	if err := reg.Register("a", fakeDownloadClient{id: "a", proto: ProtocolUsenet}); err != nil {
		t.Fatal(err)
	}
	got, ok := reg.Get("a")
	if !ok || got.ID() != "a" {
		t.Fatalf("get: ok=%v", ok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/provider/ -run TestDownloadClient`
Expected: FAIL — undefined `DownloadStatus`, `StatusDownloading`, `DownloadItem`, and `DownloadClient` missing `Protocol`/`Items`/`Remove`/`Test`.

- [ ] **Step 3: Extend the contracts**

In `internal/core/provider/provider.go`, replace the `DownloadRequest` struct and the `DownloadClient` interface (currently lines defining `DownloadRequest{URL,Title}` and `DownloadClient{ID();Add()}`) with:
```go
type DownloadStatus string

const (
	StatusQueued      DownloadStatus = "queued"
	StatusDownloading DownloadStatus = "downloading"
	StatusCompleted   DownloadStatus = "completed"
	StatusPaused      DownloadStatus = "paused"
	StatusFailed      DownloadStatus = "failed"
	StatusWarning     DownloadStatus = "warning"
)

// DownloadItem is one entry in a client's queue/history snapshot.
type DownloadItem struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Status           DownloadStatus `json:"status"`
	Progress         float64        `json:"progress"` // 0..100
	Size             int64          `json:"size"`
	Downloaded       int64          `json:"downloaded"`
	DownloadClientID string         `json:"downloadClientId"`
	Protocol         Protocol       `json:"protocol"`
	ErrorMessage     string         `json:"errorMessage,omitempty"`
}

// DownloadRequest is a grab. Content holds pre-fetched .nzb/.torrent bytes; it is
// nil for magnet links (URL is passed through to the client).
type DownloadRequest struct {
	URL       string
	Title     string
	Protocol  Protocol
	IndexerID string
	Category  string
	Content   []byte
}

type DownloadClient interface {
	ID() string
	Protocol() Protocol
	Add(ctx context.Context, d DownloadRequest) (string, error) // returns client item id
	Items(ctx context.Context) ([]DownloadItem, error)
	Remove(ctx context.Context, id string, deleteData bool) error
	Test(ctx context.Context) error
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/provider/...`
Expected: PASS (new tests + existing `TestRegistryRegisterAndGet`, `TestQueryAndReleaseExtensions`).

- [ ] **Step 5: Commit**

```bash
git add internal/core/provider
git commit -m "feat: extend provider DownloadClient with items, remove, test, protocol"
```

---

## Task 2: Store — download_clients table + CRUD

**Files:**
- Create: `internal/core/database/migrations/0003_download_clients.sql`
- Modify: `internal/core/database/database_test.go` (bump migration count)
- Create: `internal/core/store/downloadclient_store.go`
- Test: `internal/core/store/downloadclient_store_test.go`

**Interfaces:**
- Consumes: `*store.Store`, `store.ErrNotFound`, existing `boolToInt`, `rowScanner` (from `indexer_store.go`).
- Produces:
  - `type store.DownloadClient struct { ... }` (see Step 4).
  - `CreateDownloadClient(ctx, DownloadClient) (int64, error)`, `GetDownloadClient(ctx, int64) (*DownloadClient, error)`, `ListDownloadClients(ctx, enabledOnly bool) ([]DownloadClient, error)`, `UpdateDownloadClient(ctx, DownloadClient) error`, `DeleteDownloadClient(ctx, int64) error`, `SetDownloadClientStatus(ctx, id int64, status, failMessage string) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/store/downloadclient_store_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestDownloadClientCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateDownloadClient(ctx, DownloadClient{
		Name: "sab", Implementation: "sabnzbd", Protocol: "usenet",
		Host: "localhost", Port: 8080, UseSSL: false, URLBase: "",
		APIKey: "k", Category: "tv", Enabled: true, Priority: 25,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetDownloadClient(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "sab" || got.Implementation != "sabnzbd" || got.Protocol != "usenet" ||
		got.Port != 8080 || got.Category != "tv" || got.Priority != 25 {
		t.Fatalf("unexpected client: %+v", got)
	}

	got.Enabled = false
	got.Name = "renamed"
	if err := s.UpdateDownloadClient(ctx, *got); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListDownloadClients(ctx, false)
	if err != nil || len(all) != 1 || all[0].Name != "renamed" {
		t.Fatalf("list all: %+v err=%v", all, err)
	}
	enabled, err := s.ListDownloadClients(ctx, true)
	if err != nil || len(enabled) != 0 {
		t.Fatalf("list enabled: %+v err=%v", enabled, err)
	}

	if err := s.SetDownloadClientStatus(ctx, id, "failed", "boom"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDownloadClient(ctx, id)
	if got.Status != "failed" || got.FailMessage != "boom" {
		t.Fatalf("status not persisted: %+v", got)
	}

	if err := s.DeleteDownloadClient(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDownloadClient(ctx, id); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/store/ -run TestDownloadClientCRUD`
Expected: FAIL — undefined `DownloadClient`, `CreateDownloadClient`, etc.

- [ ] **Step 3: Create the migration**

Create `internal/core/database/migrations/0003_download_clients.sql`:
```sql
CREATE TABLE download_clients (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    implementation TEXT NOT NULL,
    protocol       TEXT NOT NULL,
    host           TEXT NOT NULL DEFAULT '',
    port           INTEGER NOT NULL DEFAULT 0,
    use_ssl        INTEGER NOT NULL DEFAULT 0,
    url_base       TEXT NOT NULL DEFAULT '',
    username       TEXT NOT NULL DEFAULT '',
    api_key        TEXT NOT NULL DEFAULT '',
    category       TEXT NOT NULL DEFAULT '',
    enabled        INTEGER NOT NULL DEFAULT 1,
    priority       INTEGER NOT NULL DEFAULT 25,
    settings       TEXT NOT NULL DEFAULT '{}',
    status         TEXT NOT NULL DEFAULT 'unknown',
    last_check     DATETIME,
    fail_message   TEXT NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 4: Create the store methods**

Create `internal/core/store/downloadclient_store.go`:
```go
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
```

- [ ] **Step 5: Bump the migration-count assertion**

In `internal/core/database/database_test.go`, the test asserts 2 applied migrations. Adding `0003` makes it 3. Change:
```go
	if applied != 2 {
		t.Fatalf("expected 2 applied migrations, got %d", applied)
	}
```
to:
```go
	if applied != 3 {
		t.Fatalf("expected 3 applied migrations, got %d", applied)
	}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/core/store/... ./internal/core/database/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/core/store/downloadclient_store.go internal/core/store/downloadclient_store_test.go internal/core/database/migrations/0003_download_clients.sql internal/core/database/database_test.go
git commit -m "feat: add download_clients table and store CRUD"
```

---

## Task 3: Typed errors + server-side grab fetch

**Files:**
- Create: `internal/downloadclient/errors.go`
- Create: `internal/downloadclient/grab.go`
- Test: `internal/downloadclient/grab_test.go`

**Interfaces:**
- Produces:
  - Error vars: `ErrClientUnavailable`, `ErrAuthFailed`, `ErrInvalidResponse`, `ErrUnsupportedProtocol`, `ErrReleaseUnavailable`.
  - `func fetchContent(ctx context.Context, hc *http.Client, rawURL string) ([]byte, error)` — returns `(nil, nil)` for `magnet:` URLs; otherwise GETs the URL and returns the body bytes.

- [ ] **Step 1: Write the failing test**

Create `internal/downloadclient/grab_test.go`:
```go
package downloadclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchContentMagnetPassthrough(t *testing.T) {
	body, err := fetchContent(context.Background(), http.DefaultClient, "magnet:?xt=urn:btih:abc")
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		t.Fatalf("magnet should yield nil content, got %d bytes", len(body))
	}
}

func TestFetchContentDownloadsBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("NZBDATA"))
	}))
	defer srv.Close()

	body, err := fetchContent(context.Background(), srv.Client(), srv.URL+"/get.nzb")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "NZBDATA" {
		t.Fatalf("body = %q", body)
	}
}

func TestFetchContentGoneIsReleaseUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchContent(context.Background(), srv.Client(), srv.URL+"/gone.nzb")
	if !errors.Is(err, ErrReleaseUnavailable) {
		t.Fatalf("want ErrReleaseUnavailable, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/downloadclient/ -run TestFetchContent`
Expected: FAIL — undefined `fetchContent`, `ErrReleaseUnavailable`.

- [ ] **Step 3: Write the errors**

Create `internal/downloadclient/errors.go`:
```go
package downloadclient

import "errors"

var (
	ErrClientUnavailable   = errors.New("downloadclient: unavailable")
	ErrAuthFailed          = errors.New("downloadclient: authentication failed")
	ErrInvalidResponse     = errors.New("downloadclient: invalid response")
	ErrUnsupportedProtocol = errors.New("downloadclient: no client for protocol")
	ErrReleaseUnavailable  = errors.New("downloadclient: release unavailable")
)
```

- [ ] **Step 4: Write the fetch**

Create `internal/downloadclient/grab.go`:
```go
package downloadclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxContentBytes = 32 << 20 // 32 MiB cap on .nzb/.torrent downloads

// fetchContent retrieves the release payload for a grab. Magnet links carry no
// downloadable body, so they pass through as (nil, nil) and the client submits the
// URL directly. Everything else is fetched server-side so the indexer apikey
// embedded in the URL never reaches the download client.
func fetchContent(ctx context.Context, hc *http.Client, rawURL string) ([]byte, error) {
	if strings.HasPrefix(strings.ToLower(rawURL), "magnet:") {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrClientUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("%w: status %d", ErrReleaseUnavailable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: fetch status %d", ErrClientUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContentBytes))
	if err != nil {
		return nil, err
	}
	return body, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/downloadclient/ -run TestFetchContent`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/downloadclient/errors.go internal/downloadclient/grab.go internal/downloadclient/grab_test.go
git commit -m "feat: add downloadclient errors and server-side grab fetch"
```

---

## Task 4: SABnzbdClient (usenet)

**Files:**
- Create: `internal/downloadclient/sabnzbd.go`
- Create: `internal/downloadclient/testdata/sab_queue.json`, `sab_history.json`, `sab_addfile.json`, `sab_version.json`
- Test: `internal/downloadclient/sabnzbd_test.go`

**Interfaces:**
- Consumes: `provider.DownloadClient`, `provider.DownloadRequest`, `provider.DownloadItem`, `provider.ProtocolUsenet`, error vars.
- Produces:
  - `type SABnzbdClient struct { ... }`.
  - `func newSABnzbd(id, base, apiKey, category string, hc *http.Client) *SABnzbdClient` — `base` is the fully-built origin+urlBase (e.g. `http://host:8080`).
  - Methods satisfying `provider.DownloadClient`: `ID`, `Protocol`, `Add`, `Items`, `Remove`, `Test`.

- [ ] **Step 1: Create the fixtures**

Create `internal/downloadclient/testdata/sab_queue.json`:
```json
{"queue":{"slots":[
  {"nzo_id":"SABnzbd_nzo_aaa","filename":"The.Movie.2026.1080p.WEB-DL","status":"Downloading","percentage":"42","mb":"1024.0","mbleft":"593.92"},
  {"nzo_id":"SABnzbd_nzo_bbb","filename":"The.Show.S01E01.720p","status":"Queued","percentage":"0","mb":"512.0","mbleft":"512.0"}
]}}
```

Create `internal/downloadclient/testdata/sab_history.json`:
```json
{"history":{"slots":[
  {"nzo_id":"SABnzbd_nzo_ccc","name":"Old.Movie.2020.1080p","status":"Completed","bytes":2147483648,"fail_message":""},
  {"nzo_id":"SABnzbd_nzo_ddd","name":"Bad.Release.2019","status":"Failed","bytes":0,"fail_message":"Unpacking failed"}
]}}
```

Create `internal/downloadclient/testdata/sab_addfile.json`:
```json
{"status":true,"nzo_ids":["SABnzbd_nzo_new"]}
```

Create `internal/downloadclient/testdata/sab_version.json`:
```json
{"version":"4.2.0"}
```

- [ ] **Step 2: Write the failing test**

Create `internal/downloadclient/sabnzbd_test.go`:
```go
package downloadclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func newSABServer(t *testing.T) *httptest.Server {
	t.Helper()
	queue, _ := os.ReadFile("testdata/sab_queue.json")
	history, _ := os.ReadFile("testdata/sab_history.json")
	addfile, _ := os.ReadFile("testdata/sab_addfile.json")
	version, _ := os.ReadFile("testdata/sab_version.json")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "KEY" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("mode") {
		case "version":
			_, _ = w.Write(version)
		case "queue":
			// A delete action carries name=delete; return a simple ok.
			if r.URL.Query().Get("name") == "delete" {
				_, _ = w.Write([]byte(`{"status":true}`))
				return
			}
			_, _ = w.Write(queue)
		case "history":
			if r.URL.Query().Get("name") == "delete" {
				_, _ = w.Write([]byte(`{"status":true}`))
				return
			}
			_, _ = w.Write(history)
		case "addfile":
			_, _ = w.Write(addfile)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func TestSABnzbdItems(t *testing.T) {
	srv := newSABServer(t)
	defer srv.Close()
	c := newSABnzbd("1", srv.URL, "KEY", "tv", srv.Client())

	items, err := c.Items(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("want 4 items (2 queue + 2 history), got %d: %+v", len(items), items)
	}
	byID := map[string]provider.DownloadItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if got := byID["SABnzbd_nzo_aaa"]; got.Status != provider.StatusDownloading || got.Progress != 42 || got.Protocol != provider.ProtocolUsenet || got.DownloadClientID != "1" {
		t.Fatalf("downloading item wrong: %+v", got)
	}
	if got := byID["SABnzbd_nzo_bbb"]; got.Status != provider.StatusQueued {
		t.Fatalf("queued item wrong: %+v", got)
	}
	if got := byID["SABnzbd_nzo_ccc"]; got.Status != provider.StatusCompleted || got.Size != 2147483648 {
		t.Fatalf("completed item wrong: %+v", got)
	}
	if got := byID["SABnzbd_nzo_ddd"]; got.Status != provider.StatusFailed || got.ErrorMessage != "Unpacking failed" {
		t.Fatalf("failed item wrong: %+v", got)
	}
}

func TestSABnzbdAddAndTestAndRemove(t *testing.T) {
	srv := newSABServer(t)
	defer srv.Close()
	c := newSABnzbd("1", srv.URL, "KEY", "tv", srv.Client())
	ctx := context.Background()

	if err := c.Test(ctx); err != nil {
		t.Fatalf("test: %v", err)
	}
	id, err := c.Add(ctx, provider.DownloadRequest{
		Title: "New.Movie", Protocol: provider.ProtocolUsenet, Content: []byte("NZBDATA"),
	})
	if err != nil || id != "SABnzbd_nzo_new" {
		t.Fatalf("add: id=%q err=%v", id, err)
	}
	if err := c.Remove(ctx, "SABnzbd_nzo_aaa", true); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestSABnzbdAuthFailure(t *testing.T) {
	srv := newSABServer(t)
	defer srv.Close()
	c := newSABnzbd("1", srv.URL, "WRONG", "tv", srv.Client())
	if err := c.Test(context.Background()); err == nil {
		t.Fatal("expected auth failure")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/downloadclient/ -run TestSABnzbd`
Expected: FAIL — undefined `newSABnzbd`.

- [ ] **Step 4: Write the implementation**

Create `internal/downloadclient/sabnzbd.go`:
```go
package downloadclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// SABnzbdClient talks to a SABnzbd usenet download client over its JSON API.
type SABnzbdClient struct {
	id       string
	base     string // origin + url base, e.g. http://host:8080
	apiKey   string
	category string
	http     *http.Client
}

func newSABnzbd(id, base, apiKey, category string, hc *http.Client) *SABnzbdClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &SABnzbdClient{id: id, base: strings.TrimRight(base, "/"), apiKey: apiKey, category: category, http: hc}
}

func (c *SABnzbdClient) ID() string                { return c.id }
func (c *SABnzbdClient) Protocol() provider.Protocol { return provider.ProtocolUsenet }

func (c *SABnzbdClient) apiURL(v url.Values) string {
	v.Set("apikey", c.apiKey)
	v.Set("output", "json")
	return c.base + "/api?" + v.Encode()
}

// get issues a GET against the SAB API and returns the decoded body.
func (c *SABnzbdClient) get(ctx context.Context, v url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL(v), nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *SABnzbdClient) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrClientUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return ErrAuthFailed
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrClientUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContentBytes))
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}

func (c *SABnzbdClient) Test(ctx context.Context) error {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, url.Values{"mode": {"version"}}, &v); err != nil {
		return err
	}
	if v.Version == "" {
		return ErrInvalidResponse
	}
	return nil
}

func (c *SABnzbdClient) Add(ctx context.Context, d provider.DownloadRequest) (string, error) {
	category := d.Category
	if category == "" {
		category = c.category
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("nzbfile", d.Title+".nzb")
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(d.Content); err != nil {
		return "", err
	}
	mw.Close()

	v := url.Values{"mode": {"addfile"}}
	if category != "" {
		v.Set("cat", category)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL(v), &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	var res struct {
		Status bool     `json:"status"`
		NzoIDs []string `json:"nzo_ids"`
	}
	if err := c.do(req, &res); err != nil {
		return "", err
	}
	if !res.Status || len(res.NzoIDs) == 0 {
		return "", fmt.Errorf("%w: addfile rejected", ErrInvalidResponse)
	}
	return res.NzoIDs[0], nil
}

func (c *SABnzbdClient) Remove(ctx context.Context, id string, deleteData bool) error {
	del := "0"
	if deleteData {
		del = "1"
	}
	// Try the queue first, then history; SAB returns ok for a missing id.
	for _, mode := range []string{"queue", "history"} {
		v := url.Values{"mode": {mode}, "name": {"delete"}, "value": {id}, "del_files": {del}}
		if err := c.get(ctx, v, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *SABnzbdClient) Items(ctx context.Context) ([]provider.DownloadItem, error) {
	var q struct {
		Queue struct {
			Slots []struct {
				NzoID      string `json:"nzo_id"`
				Filename   string `json:"filename"`
				Status     string `json:"status"`
				Percentage string `json:"percentage"`
				MB         string `json:"mb"`
				MBLeft     string `json:"mbleft"`
			} `json:"slots"`
		} `json:"queue"`
	}
	if err := c.get(ctx, url.Values{"mode": {"queue"}}, &q); err != nil {
		return nil, err
	}
	var h struct {
		History struct {
			Slots []struct {
				NzoID       string `json:"nzo_id"`
				Name        string `json:"name"`
				Status      string `json:"status"`
				Bytes       int64  `json:"bytes"`
				FailMessage string `json:"fail_message"`
			} `json:"slots"`
		} `json:"history"`
	}
	if err := c.get(ctx, url.Values{"mode": {"history"}}, &h); err != nil {
		return nil, err
	}

	items := make([]provider.DownloadItem, 0, len(q.Queue.Slots)+len(h.History.Slots))
	for _, s := range q.Queue.Slots {
		mb := parseFloat(s.MB)
		mbleft := parseFloat(s.MBLeft)
		size := int64(mb * 1024 * 1024)
		items = append(items, provider.DownloadItem{
			ID:               s.NzoID,
			Title:            s.Filename,
			Status:           sabQueueStatus(s.Status),
			Progress:         parseFloat(s.Percentage),
			Size:             size,
			Downloaded:       int64((mb - mbleft) * 1024 * 1024),
			DownloadClientID: c.id,
			Protocol:         provider.ProtocolUsenet,
		})
	}
	for _, s := range h.History.Slots {
		status := sabHistoryStatus(s.Status)
		it := provider.DownloadItem{
			ID:               s.NzoID,
			Title:            s.Name,
			Status:           status,
			Size:             s.Bytes,
			Downloaded:       s.Bytes,
			DownloadClientID: c.id,
			Protocol:         provider.ProtocolUsenet,
			ErrorMessage:     s.FailMessage,
		}
		if status == provider.StatusCompleted {
			it.Progress = 100
		}
		items = append(items, it)
	}
	return items, nil
}

func sabQueueStatus(s string) provider.DownloadStatus {
	switch strings.ToLower(s) {
	case "queued":
		return provider.StatusQueued
	case "paused":
		return provider.StatusPaused
	case "downloading", "fetching", "checking", "grabbing":
		return provider.StatusDownloading
	default:
		return provider.StatusDownloading
	}
}

func sabHistoryStatus(s string) provider.DownloadStatus {
	switch strings.ToLower(s) {
	case "completed":
		return provider.StatusCompleted
	case "failed":
		return provider.StatusFailed
	default:
		return provider.StatusDownloading // verifying/extracting/repairing
	}
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/downloadclient/ -run TestSABnzbd`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/downloadclient/sabnzbd.go internal/downloadclient/sabnzbd_test.go internal/downloadclient/testdata/sab_*.json
git commit -m "feat: add SABnzbd usenet download client"
```

---

## Task 5: QBittorrentClient (torrent)

**Files:**
- Create: `internal/downloadclient/qbittorrent.go`
- Create: `internal/downloadclient/testdata/qbit_info.json`
- Test: `internal/downloadclient/qbittorrent_test.go`

**Interfaces:**
- Consumes: `provider.DownloadClient`, `provider.DownloadRequest`, `provider.DownloadItem`, `provider.ProtocolTorrent`, error vars.
- Produces:
  - `type QBittorrentClient struct { ... }`.
  - `func newQBittorrent(id, base, username, password, category string, hc *http.Client) *QBittorrentClient` — `base` is origin+urlBase (e.g. `http://host:8080`).
  - Methods satisfying `provider.DownloadClient`: `ID`, `Protocol`, `Add`, `Items`, `Remove`, `Test`.

- [ ] **Step 1: Create the fixture**

Create `internal/downloadclient/testdata/qbit_info.json`:
```json
[
  {"hash":"aaa111","name":"The.Movie.2026.1080p.BluRay","size":2147483648,"progress":0.42,"state":"downloading","completed":901943132,"amount_left":1245540516},
  {"hash":"bbb222","name":"Old.Show.S01.COMPLETE","size":1073741824,"progress":1.0,"state":"uploading","completed":1073741824,"amount_left":0},
  {"hash":"ccc333","name":"Stuck.Release","size":500,"progress":0.0,"state":"error","completed":0,"amount_left":500}
]
```

- [ ] **Step 2: Write the failing test**

Create `internal/downloadclient/qbittorrent_test.go`:
```go
package downloadclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func newQBitServer(t *testing.T) *httptest.Server {
	t.Helper()
	info, _ := os.ReadFile("testdata/qbit_info.json")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("username") != "admin" || r.PostForm.Get("password") != "pw" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Fails."))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session123"})
		_, _ = w.Write([]byte("Ok."))
	})
	requireSID := func(r *http.Request) bool {
		c, err := r.Cookie("SID")
		return err == nil && c.Value == "session123"
	}
	mux.HandleFunc("/api/v2/app/version", func(w http.ResponseWriter, r *http.Request) {
		if !requireSID(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("v4.6.0"))
	})
	mux.HandleFunc("/api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
		if !requireSID(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(info)
	})
	mux.HandleFunc("/api/v2/torrents/add", func(w http.ResponseWriter, r *http.Request) {
		if !requireSID(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("Ok."))
	})
	mux.HandleFunc("/api/v2/torrents/delete", func(w http.ResponseWriter, r *http.Request) {
		if !requireSID(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = r.ParseForm()
		if !strings.Contains(r.PostForm.Get("hashes"), "aaa111") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("Ok."))
	})
	return httptest.NewServer(mux)
}

func TestQBittorrentItems(t *testing.T) {
	srv := newQBitServer(t)
	defer srv.Close()
	c := newQBittorrent("2", srv.URL, "admin", "pw", "movies", srv.Client())

	items, err := c.Items(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	byID := map[string]provider.DownloadItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if got := byID["aaa111"]; got.Status != provider.StatusDownloading || got.Progress != 42 || got.Protocol != provider.ProtocolTorrent || got.DownloadClientID != "2" {
		t.Fatalf("downloading item wrong: %+v", got)
	}
	if got := byID["bbb222"]; got.Status != provider.StatusCompleted || got.Progress != 100 {
		t.Fatalf("completed item wrong: %+v", got)
	}
	if got := byID["ccc333"]; got.Status != provider.StatusFailed {
		t.Fatalf("errored item wrong: %+v", got)
	}
}

func TestQBittorrentAddMagnetTestRemove(t *testing.T) {
	srv := newQBitServer(t)
	defer srv.Close()
	c := newQBittorrent("2", srv.URL, "admin", "pw", "movies", srv.Client())
	ctx := context.Background()

	if err := c.Test(ctx); err != nil {
		t.Fatalf("test: %v", err)
	}
	// Magnet grab: Content is nil, URL carries the magnet. qBit derives the id from
	// the btih hash in the magnet URL.
	// A real BitTorrent v1 infohash is SHA-1 = 40 hex chars; upper-case here to
	// exercise the (?i) regex + ToLower normalization in Add.
	id, err := c.Add(ctx, provider.DownloadRequest{
		URL:      "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=x",
		Protocol: provider.ProtocolTorrent,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if id != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("add id = %q want lowercased 40-hex btih", id)
	}
	if err := c.Remove(ctx, "aaa111", true); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestQBittorrentAuthFailure(t *testing.T) {
	srv := newQBitServer(t)
	defer srv.Close()
	c := newQBittorrent("2", srv.URL, "admin", "WRONG", "movies", srv.Client())
	if err := c.Test(context.Background()); err == nil {
		t.Fatal("expected auth failure")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/downloadclient/ -run TestQBittorrent`
Expected: FAIL — undefined `newQBittorrent`.

- [ ] **Step 4: Write the implementation**

Create `internal/downloadclient/qbittorrent.go`:
```go
package downloadclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// QBittorrentClient talks to a qBittorrent client over its WebUI API v2.
type QBittorrentClient struct {
	id       string
	base     string
	username string
	password string
	category string
	http     *http.Client

	mu  sync.Mutex
	sid string
}

func newQBittorrent(id, base, username, password, category string, hc *http.Client) *QBittorrentClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &QBittorrentClient{
		id: id, base: strings.TrimRight(base, "/"), username: username,
		password: password, category: category, http: hc,
	}
}

func (c *QBittorrentClient) ID() string                  { return c.id }
func (c *QBittorrentClient) Protocol() provider.Protocol { return provider.ProtocolTorrent }

// login authenticates and stores the SID cookie value.
func (c *QBittorrentClient) login(ctx context.Context) error {
	form := url.Values{"username": {c.username}, "password": {c.password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrClientUnavailable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Ok.") {
		return ErrAuthFailed
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == "SID" {
			c.mu.Lock()
			c.sid = ck.Value
			c.mu.Unlock()
			return nil
		}
	}
	return ErrAuthFailed
}

func (c *QBittorrentClient) currentSID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sid
}

// send builds a request with the SID cookie, logging in first if needed, and
// retrying once on a 403 (expired session). Returns the response body bytes.
func (c *QBittorrentClient) send(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, error) {
	if c.currentSID() == "" {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
	}
	do := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.AddCookie(&http.Cookie{Name: "SID", Value: c.currentSID()})
		return c.http.Do(req)
	}
	resp, err := do()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrClientUnavailable, err)
	}
	if resp.StatusCode == http.StatusForbidden && body == nil {
		// Session likely expired; re-auth and retry once (only safe when body is
		// nil/replayable — callers with bodies pass non-nil and skip retry).
		resp.Body.Close()
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		if resp, err = do(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrClientUnavailable, err)
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, ErrAuthFailed
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrClientUnavailable, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxContentBytes))
}

func (c *QBittorrentClient) Test(ctx context.Context) error {
	// login is exercised inside send(); a successful version call confirms auth.
	body, err := c.send(ctx, http.MethodGet, "/api/v2/app/version", nil, "")
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return ErrInvalidResponse
	}
	return nil
}

var btihRe = regexp.MustCompile(`(?i)btih:([0-9a-f]{40})`)

func (c *QBittorrentClient) Add(ctx context.Context, d provider.DownloadRequest) (string, error) {
	category := d.Category
	if category == "" {
		category = c.category
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if len(d.Content) > 0 {
		fw, err := mw.CreateFormFile("torrents", d.Title+".torrent")
		if err != nil {
			return "", err
		}
		if _, err := fw.Write(d.Content); err != nil {
			return "", err
		}
	} else {
		_ = mw.WriteField("urls", d.URL)
	}
	if category != "" {
		_ = mw.WriteField("category", category)
	}
	mw.Close()

	// Ensure auth before sending a body (send() does not retry body requests).
	if c.currentSID() == "" {
		if err := c.login(ctx); err != nil {
			return "", err
		}
	}
	if _, err := c.send(ctx, http.MethodPost, "/api/v2/torrents/add", &buf, mw.FormDataContentType()); err != nil {
		return "", err
	}
	// qBittorrent's add endpoint returns no id; the torrent hash is the identity.
	// For magnets, derive it from the btih; for .torrent files it is not known here
	// (the queue monitor will surface the item by name/hash).
	if m := btihRe.FindStringSubmatch(d.URL); m != nil {
		return strings.ToLower(m[1]), nil
	}
	return "", nil
}

func (c *QBittorrentClient) Remove(ctx context.Context, id string, deleteData bool) error {
	del := "false"
	if deleteData {
		del = "true"
	}
	form := url.Values{"hashes": {id}, "deleteFiles": {del}}
	// Pre-auth so the POST body is sent with a valid cookie.
	if c.currentSID() == "" {
		if err := c.login(ctx); err != nil {
			return err
		}
	}
	_, err := c.send(ctx, http.MethodPost, "/api/v2/torrents/delete",
		strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	return err
}

func (c *QBittorrentClient) Items(ctx context.Context) ([]provider.DownloadItem, error) {
	body, err := c.send(ctx, http.MethodGet, "/api/v2/torrents/info", nil, "")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Hash       string  `json:"hash"`
		Name       string  `json:"name"`
		Size       int64   `json:"size"`
		Progress   float64 `json:"progress"`
		State      string  `json:"state"`
		Completed  int64   `json:"completed"`
		AmountLeft int64   `json:"amount_left"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	items := make([]provider.DownloadItem, 0, len(raw))
	for _, r := range raw {
		items = append(items, provider.DownloadItem{
			ID:               r.Hash,
			Title:            r.Name,
			Status:           qbitStatus(r.State),
			Progress:         r.Progress * 100,
			Size:             r.Size,
			Downloaded:       r.Completed,
			DownloadClientID: c.id,
			Protocol:         provider.ProtocolTorrent,
		})
	}
	return items, nil
}

func qbitStatus(state string) provider.DownloadStatus {
	switch state {
	case "error", "missingFiles":
		return provider.StatusFailed
	case "pausedDL":
		return provider.StatusPaused
	case "queuedDL":
		return provider.StatusQueued
	case "uploading", "stalledUP", "forcedUP", "queuedUP", "checkingUP", "pausedUP":
		return provider.StatusCompleted
	default: // downloading, stalledDL, metaDL, forcedDL, checkingDL, allocating
		return provider.StatusDownloading
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/downloadclient/ -run TestQBittorrent`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/downloadclient/qbittorrent.go internal/downloadclient/qbittorrent_test.go internal/downloadclient/testdata/qbit_info.json
git commit -m "feat: add qBittorrent torrent download client"
```

---

## Task 6: Service — reload, routing, grab, queue, remove

**Files:**
- Modify: `internal/downloadclient/downloadclient.go` (replace the Foundation stub)
- Test: `internal/downloadclient/service_test.go`

**Interfaces:**
- Consumes: `*store.Store`, `store.DownloadClient`, `provider.DownloadClient`, `provider.DownloadRequest`, `provider.DownloadItem`, `provider.Protocol`, `newSABnzbd`, `newQBittorrent`, `fetchContent`.
- Produces:
  - `type ClientError struct { ClientID, Message string }`.
  - `type QueueResult struct { Items []provider.DownloadItem; ClientErrors []ClientError }`.
  - `func NewService(st *store.Store) *Service`.
  - `func (s *Service) WithHTTPClient(hc *http.Client) *Service`.
  - `func (s *Service) Reload(ctx) error` — rebuilds live clients from enabled configs (priority order preserved).
  - `func (s *Service) Grab(ctx, req provider.DownloadRequest, clientID string) (string, error)`.
  - `func (s *Service) Queue(ctx) QueueResult`.
  - `func (s *Service) Remove(ctx, clientID, itemID string, deleteData bool) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/downloadclient/service_test.go`:
```go
package downloadclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

// hostPort splits an httptest URL like http://127.0.0.1:port into host and port.
func hostPort(t *testing.T, raw string) (string, int) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := strconv.Atoi(u.Port())
	return u.Hostname(), p
}

func TestServiceReloadGrabQueue(t *testing.T) {
	sab := newSABServer(t)
	defer sab.Close()
	// A separate indexer-like server that serves the .nzb bytes for the grab fetch.
	nzb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("NZBDATA"))
	}))
	defer nzb.Close()

	st := newTestStore(t)
	ctx := context.Background()
	sabHost, sabPort := hostPort(t, sab.URL)
	if _, err := st.CreateDownloadClient(ctx, store.DownloadClient{
		Name: "sab", Implementation: "sabnzbd", Protocol: "usenet",
		Host: sabHost, Port: sabPort, APIKey: "KEY", Category: "tv", Enabled: true, Priority: 10,
	}); err != nil {
		t.Fatal(err)
	}
	// Disabled client must be ignored by Reload.
	if _, err := st.CreateDownloadClient(ctx, store.DownloadClient{
		Name: "off", Implementation: "qbittorrent", Protocol: "torrent",
		Host: "127.0.0.1", Port: 1, Enabled: false, Priority: 1,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(st).WithHTTPClient(sab.Client())
	if err := svc.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	// Grab a usenet release: fetched server-side, uploaded to SAB.
	id, err := svc.Grab(ctx, provider.DownloadRequest{
		URL: nzb.URL + "/get.nzb", Title: "New.Movie", Protocol: provider.ProtocolUsenet,
	}, "")
	if err != nil || id != "SABnzbd_nzo_new" {
		t.Fatalf("grab: id=%q err=%v", id, err)
	}

	// No torrent client is enabled → routing error.
	if _, err := svc.Grab(ctx, provider.DownloadRequest{
		URL: "magnet:?xt=urn:btih:x", Protocol: provider.ProtocolTorrent,
	}, ""); err != ErrUnsupportedProtocol {
		t.Fatalf("expected ErrUnsupportedProtocol, got %v", err)
	}

	// Queue aggregates the SAB client's items.
	res := svc.Queue(ctx)
	if len(res.Items) != 4 || len(res.ClientErrors) != 0 {
		t.Fatalf("queue: items=%d errors=%+v", len(res.Items), res.ClientErrors)
	}

	if err := svc.Remove(ctx, "1", "SABnzbd_nzo_aaa", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
}
```

Note: `hostPort` (defined here) is reused by the monitor and API tests in later tasks.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/downloadclient/ -run TestServiceReloadGrabQueue`
Expected: FAIL — undefined `NewService` (and missing imports).

- [ ] **Step 3: Write the implementation**

Replace the entire contents of `internal/downloadclient/downloadclient.go` with:
```go
// Package downloadclient integrates usenet (SABnzbd) and torrent (qBittorrent)
// download clients. It fills the provider.DownloadClient contract declared in
// core/provider and imports only internal/core/*. Releases are fetched
// server-side so the indexer apikey never leaves Nexus; magnets pass through.
package downloadclient

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// ClientError is a per-client failure captured during queue fan-out.
type ClientError struct {
	ClientID string `json:"clientId"`
	Message  string `json:"message"`
}

// QueueResult is the aggregated live queue across enabled clients.
type QueueResult struct {
	Items        []provider.DownloadItem `json:"items"`
	ClientErrors []ClientError           `json:"clientErrors"`
}

// Service owns the live set of configured download clients.
type Service struct {
	store *store.Store
	http  *http.Client

	mu      sync.RWMutex
	clients []provider.DownloadClient // priority order preserved from store
}

func NewService(st *store.Store) *Service {
	return &Service{store: st, http: &http.Client{Timeout: 30 * time.Second}}
}

func (s *Service) WithHTTPClient(hc *http.Client) *Service {
	s.http = hc
	return s
}

// Reload rebuilds the live client set from enabled clients in the store,
// preserving the store's priority ordering.
func (s *Service) Reload(ctx context.Context) error {
	rows, err := s.store.ListDownloadClients(ctx, true)
	if err != nil {
		return err
	}
	clients := make([]provider.DownloadClient, 0, len(rows))
	for _, dc := range rows {
		id := strconv.FormatInt(dc.ID, 10)
		base := buildBase(dc)
		switch dc.Implementation {
		case "sabnzbd":
			clients = append(clients, newSABnzbd(id, base, dc.APIKey, dc.Category, s.http))
		case "qbittorrent":
			clients = append(clients, newQBittorrent(id, base, dc.Username, dc.APIKey, dc.Category, s.http))
		}
	}
	s.mu.Lock()
	s.clients = clients
	s.mu.Unlock()
	return nil
}

// buildBase composes scheme://host:port + url base from a stored config.
func buildBase(dc store.DownloadClient) string {
	scheme := "http"
	if dc.UseSSL {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s", scheme, dc.Host)
	if dc.Port != 0 {
		base = fmt.Sprintf("%s:%d", base, dc.Port)
	}
	if dc.URLBase != "" {
		if dc.URLBase[0] != '/' {
			base += "/"
		}
		base += dc.URLBase
	}
	return base
}

func (s *Service) snapshot() []provider.DownloadClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]provider.DownloadClient, len(s.clients))
	copy(out, s.clients)
	return out
}

// route selects the target client: an explicit clientID wins; otherwise the
// highest-priority enabled client whose protocol matches.
func (s *Service) route(protocol provider.Protocol, clientID string) (provider.DownloadClient, error) {
	clients := s.snapshot()
	if clientID != "" {
		for _, c := range clients {
			if c.ID() == clientID {
				return c, nil
			}
		}
		return nil, fmt.Errorf("%w: client %q not found or disabled", ErrClientUnavailable, clientID)
	}
	for _, c := range clients { // snapshot is priority-ordered
		if c.Protocol() == protocol {
			return c, nil
		}
	}
	return nil, ErrUnsupportedProtocol
}

// Grab routes a release, fetches its content server-side, and submits it.
func (s *Service) Grab(ctx context.Context, req provider.DownloadRequest, clientID string) (string, error) {
	c, err := s.route(req.Protocol, clientID)
	if err != nil {
		return "", err
	}
	content, err := fetchContent(ctx, s.http, req.URL)
	if err != nil {
		return "", err
	}
	req.Content = content
	return c.Add(ctx, req)
}

// Queue fans out Items() across enabled clients with partial-success semantics.
func (s *Service) Queue(ctx context.Context) QueueResult {
	clients := s.snapshot()
	type outcome struct {
		id    string
		items []provider.DownloadItem
		err   error
	}
	var wg sync.WaitGroup
	results := make([]outcome, len(clients))
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c provider.DownloadClient) {
			defer wg.Done()
			items, err := c.Items(ctx)
			results[i] = outcome{id: c.ID(), items: items, err: err}
		}(i, c)
	}
	wg.Wait()

	var out QueueResult
	for _, o := range results {
		if o.err != nil {
			out.ClientErrors = append(out.ClientErrors, ClientError{ClientID: o.id, Message: o.err.Error()})
			continue
		}
		out.Items = append(out.Items, o.items...)
	}
	return out
}

// Remove deletes a queue item from a named client.
func (s *Service) Remove(ctx context.Context, clientID, itemID string, deleteData bool) error {
	for _, c := range s.snapshot() {
		if c.ID() == clientID {
			return c.Remove(ctx, itemID, deleteData)
		}
	}
	return fmt.Errorf("%w: client %q not found or disabled", ErrClientUnavailable, clientID)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/downloadclient/ -run TestServiceReloadGrabQueue`
Expected: PASS.

- [ ] **Step 5: Full package test + vet**

Run: `go test ./internal/downloadclient/... && go vet ./internal/downloadclient/...`
Expected: PASS, no vet errors.

- [ ] **Step 6: Commit**

```bash
git add internal/downloadclient/downloadclient.go internal/downloadclient/service_test.go
git commit -m "feat: add download-client Service with reload, routing, grab, queue"
```

---

## Task 7: Queue monitor command + event

**Files:**
- Create: `internal/downloadclient/monitor.go`
- Test: `internal/downloadclient/monitor_test.go`

**Interfaces:**
- Consumes: `*Service`, `*events.Bus`, `command.Reporter`, `provider.DownloadItem`.
- Produces:
  - `type DownloadStatusChanged struct { ClientID string; Item provider.DownloadItem; Removed bool }` with `Name() string == "download.status"`.
  - `func NewMonitor(svc *Service, bus *events.Bus) *Monitor`.
  - Methods: `Name() string` (`"DownloadQueueMonitor"`), `Run(ctx, command.Reporter) error` — satisfies `command.Command`.

- [ ] **Step 1: Write the failing test**

Create `internal/downloadclient/monitor_test.go`:
```go
package downloadclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestMonitorEmitsChanges(t *testing.T) {
	sab := newSABServer(t)
	defer sab.Close()

	st := newTestStore(t)
	ctx := context.Background()
	sabHost, sabPort := hostPort(t, sab.URL)
	if _, err := st.CreateDownloadClient(ctx, store.DownloadClient{
		Name: "sab", Implementation: "sabnzbd", Protocol: "usenet",
		Host: sabHost, Port: sabPort, APIKey: "KEY", Enabled: true, Priority: 10,
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st).WithHTTPClient(sab.Client())
	if err := svc.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	bus := events.New()
	var emitted []DownloadStatusChanged
	bus.Subscribe("download.status", func(_ context.Context, e events.Event) {
		if sc, ok := e.(DownloadStatusChanged); ok {
			emitted = append(emitted, sc)
		}
	})

	mon := NewMonitor(svc, bus)
	if err := mon.Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}
	// First run: all 4 SAB items are new → 4 events.
	if len(emitted) != 4 {
		t.Fatalf("first run: want 4 events, got %d", len(emitted))
	}

	// Second run with identical queue: nothing changed → no new events.
	emitted = nil
	if err := mon.Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 0 {
		t.Fatalf("second run: want 0 events, got %d", len(emitted))
	}
}

// A second server with an empty queue lets us assert "removed" events fire.
func TestMonitorEmitsRemovals(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "queue":
			_, _ = w.Write([]byte(`{"queue":{"slots":[]}}`))
		case "history":
			_, _ = w.Write([]byte(`{"history":{"slots":[]}}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer empty.Close()

	c := newSABnzbd("9", empty.URL, "", "", empty.Client())
	bus := events.New()
	var removed int
	bus.Subscribe("download.status", func(_ context.Context, e events.Event) {
		if sc, ok := e.(DownloadStatusChanged); ok && sc.Removed {
			removed++
		}
	})
	mon := NewMonitor(newServiceWithClients(c), bus)
	// Prime the monitor with a synthetic prior item the now-empty queue no longer has.
	mon.last = map[string]lastItem{"9|old": {clientID: "9"}}
	if err := mon.Run(context.Background(), nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("want 1 removal event, got %d", removed)
	}
}
```

Note: this test uses two unexported helpers added in Step 3 — `newServiceWithClients(...provider.DownloadClient) *Service` and the monitor's `last map[string]lastItem` field — so it will not compile until Step 3 lands (expected under TDD). It shares `nopReporter` and `hostPort` with the other tests in this package.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/downloadclient/ -run TestMonitor`
Expected: FAIL — undefined `NewMonitor`, `newServiceWithClients`, `lastItem`.

- [ ] **Step 3: Write the implementation**

First add the test helper to `downloadclient.go` (append at end):
```go
// newServiceWithClients builds a Service around an explicit client set. Used by
// tests that do not go through Reload.
func newServiceWithClients(clients ...provider.DownloadClient) *Service {
	return &Service{http: http.DefaultClient, clients: clients}
}
```

Then create `internal/downloadclient/monitor.go`:
```go
package downloadclient

import (
	"context"
	"sync"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/provider"
)

// DownloadStatusChanged is published for each new, changed, or removed queue item.
type DownloadStatusChanged struct {
	ClientID string                `json:"clientId"`
	Item     provider.DownloadItem `json:"item"`
	Removed  bool                  `json:"removed"`
}

func (DownloadStatusChanged) Name() string { return "download.status" }

// lastItem records the fields the monitor diffs on between runs.
type lastItem struct {
	clientID string
	status   provider.DownloadStatus
	progress float64
	item     provider.DownloadItem
}

// Monitor polls the aggregated queue and emits change events. It is stateful
// across runs, so the scheduler factory must return the SAME instance each tick.
type Monitor struct {
	svc *Service
	bus *events.Bus

	mu   sync.Mutex
	last map[string]lastItem
}

func NewMonitor(svc *Service, bus *events.Bus) *Monitor {
	return &Monitor{svc: svc, bus: bus, last: map[string]lastItem{}}
}

func (m *Monitor) Name() string { return "DownloadQueueMonitor" }

func (m *Monitor) Run(ctx context.Context, r command.Reporter) error {
	res := m.svc.Queue(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	current := make(map[string]lastItem, len(res.Items))
	for _, it := range res.Items {
		key := it.DownloadClientID + "|" + it.ID
		li := lastItem{clientID: it.DownloadClientID, status: it.Status, progress: it.Progress, item: it}
		current[key] = li
		prev, ok := m.last[key]
		if !ok || prev.status != it.Status || prev.progress != it.Progress {
			m.emit(ctx, DownloadStatusChanged{ClientID: it.DownloadClientID, Item: it})
		}
	}
	for key, prev := range m.last {
		if _, ok := current[key]; !ok {
			m.emit(ctx, DownloadStatusChanged{ClientID: prev.clientID, Item: prev.item, Removed: true})
		}
	}
	m.last = current
	r.Progress(100, "")
	return nil
}

func (m *Monitor) emit(ctx context.Context, e DownloadStatusChanged) {
	if m.bus != nil {
		m.bus.Publish(ctx, e)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/downloadclient/ -run TestMonitor -count=1`
Expected: PASS (both change and removal tests).

- [ ] **Step 5: Commit**

```bash
git add internal/downloadclient/monitor.go internal/downloadclient/monitor_test.go internal/downloadclient/downloadclient.go
git commit -m "feat: add download queue monitor emitting download.status events"
```

---

## Task 8: Download-client API sub-router

**Files:**
- Create: `internal/downloadclient/api.go`
- Test: `internal/downloadclient/api_test.go`

**Interfaces:**
- Consumes: `*store.Store`, `*Service`, `store.DownloadClient`, `provider.DownloadRequest`, `provider.Protocol`, `api.WriteJSON`, `api.WriteError`, chi.
- Produces:
  - `func NewAPI(st *store.Store, svc *Service) *API`.
  - `func (a *API) Mount(r chi.Router)` — registers `/downloadclient*`, `/download`, and `/queue*` on the given (already-authed) router.

- [ ] **Step 1: Write the failing test**

Create `internal/downloadclient/api_test.go`:
```go
package downloadclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func mountedRouter(t *testing.T, a *API) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { a.Mount(r) })
	return r
}

func TestDownloadClientAPICreateListGrabQueue(t *testing.T) {
	sab := newSABServer(t)
	defer sab.Close()
	nzb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("NZBDATA"))
	}))
	defer nzb.Close()

	st := newTestStore(t)
	svc := NewService(st).WithHTTPClient(sab.Client())
	a := NewAPI(st, svc)
	router := mountedRouter(t, a)

	sabHost, sabPort := hostPort(t, sab.URL)
	payload, _ := json.Marshal(map[string]any{
		"name": "sab", "implementation": "sabnzbd",
		"host": sabHost, "port": sabPort, "apiKey": "KEY", "category": "tv",
		"enabled": true, "priority": 10,
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/downloadclient", bytes.NewReader(payload)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}

	// List (credential must not leak).
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/downloadclient", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("KEY")) {
		t.Fatal("api key leaked in list response")
	}

	// Grab.
	grab, _ := json.Marshal(map[string]any{
		"downloadUrl": nzb.URL + "/get.nzb", "title": "New.Movie", "protocol": "usenet",
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/download", bytes.NewReader(grab)))
	if rec.Code != http.StatusOK {
		t.Fatalf("grab: %d body=%s", rec.Code, rec.Body.String())
	}
	var gres struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &gres)
	if gres.ID != "SABnzbd_nzo_new" {
		t.Fatalf("grab id = %q", gres.ID)
	}

	// Queue.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/queue", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("queue: %d", rec.Code)
	}
	var qres QueueResult
	if err := json.Unmarshal(rec.Body.Bytes(), &qres); err != nil {
		t.Fatal(err)
	}
	if len(qres.Items) != 4 {
		t.Fatalf("queue items = %d want 4", len(qres.Items))
	}

	// Remove.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/queue/1/SABnzbd_nzo_aaa", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGrabUnsupportedProtocol(t *testing.T) {
	st := newTestStore(t)
	svc := NewService(st)
	_ = svc.Reload(context.Background())
	a := NewAPI(st, svc)
	router := mountedRouter(t, a)

	grab, _ := json.Marshal(map[string]any{
		"downloadUrl": "magnet:?xt=urn:btih:x", "title": "x", "protocol": "torrent",
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/download", bytes.NewReader(grab)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for no matching client, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/downloadclient/ -run TestDownloadClientAPI`
Expected: FAIL — undefined `NewAPI`.

- [ ] **Step 3: Write the implementation**

Create `internal/downloadclient/api.go`:
```go
package downloadclient

import (
	"encoding/json"
	"errors"
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
}

func NewAPI(st *store.Store, svc *Service) *API {
	return &API{store: st, svc: svc}
}

// Mount registers routes on an already-authenticated router (the /api/v1 group).
func (a *API) Mount(r chi.Router) {
	r.Route("/downloadclient", func(r chi.Router) {
		r.Get("/", a.list)
		r.Post("/", a.create)
		r.Get("/schema", a.schema)
		r.Post("/test", a.testUnsaved)
		r.Get("/{id}", a.get)
		r.Put("/{id}", a.update)
		r.Delete("/{id}", a.delete)
		r.Post("/{id}/test", a.testSaved)
	})
	r.Post("/download", a.grab)
	r.Get("/queue", a.queue)
	r.Delete("/queue/{clientId}/{itemId}", a.removeItem)
}

type clientPayload struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	UseSSL         bool   `json:"useSsl"`
	URLBase        string `json:"urlBase"`
	Username       string `json:"username"`
	APIKey         string `json:"apiKey"`
	Category       string `json:"category"`
	Enabled        bool   `json:"enabled"`
	Priority       int    `json:"priority"`
}

func protocolFor(impl string) string {
	switch impl {
	case "sabnzbd":
		return "usenet"
	case "qbittorrent":
		return "torrent"
	default:
		return ""
	}
}

func (p clientPayload) valid() (string, bool) {
	if strings.TrimSpace(p.Name) == "" {
		return "name is required", false
	}
	if protocolFor(p.Implementation) == "" {
		return "implementation must be sabnzbd or qbittorrent", false
	}
	if strings.TrimSpace(p.Host) == "" {
		return "host is required", false
	}
	return "", true
}

func (p clientPayload) toStore() store.DownloadClient {
	pri := p.Priority
	if pri == 0 {
		pri = 25
	}
	return store.DownloadClient{
		Name: p.Name, Implementation: p.Implementation, Protocol: protocolFor(p.Implementation),
		Host: p.Host, Port: p.Port, UseSSL: p.UseSSL, URLBase: p.URLBase,
		Username: p.Username, APIKey: p.APIKey, Category: p.Category,
		Enabled: p.Enabled, Priority: pri,
	}
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListDownloadClients(r.Context(), false)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list download clients")
		return
	}
	if rows == nil {
		rows = []store.DownloadClient{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var p clientPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	id, err := a.store.CreateDownloadClient(r.Context(), p.toStore())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to create download client")
		return
	}
	_ = a.svc.Reload(r.Context())
	dc, _ := a.store.GetDownloadClient(r.Context(), id)
	api.WriteJSON(w, http.StatusCreated, dc)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	dc, err := a.store.GetDownloadClient(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "download client not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load download client")
		return
	}
	api.WriteJSON(w, http.StatusOK, dc)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	var p clientPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	dc := p.toStore()
	dc.ID = id
	if err := a.store.UpdateDownloadClient(r.Context(), dc); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to update download client")
		return
	}
	_ = a.svc.Reload(r.Context())
	updated, _ := a.store.GetDownloadClient(r.Context(), id)
	api.WriteJSON(w, http.StatusOK, updated)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	if err := a.store.DeleteDownloadClient(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete download client")
		return
	}
	_ = a.svc.Reload(r.Context())
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) testSaved(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	dc, err := a.store.GetDownloadClient(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "download client not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load download client")
		return
	}
	// Single test run: persist the outcome, then report it.
	terr := a.testClient(r, *dc)
	status, msg := "ok", ""
	if terr != nil {
		status, msg = "failed", terr.Error()
	}
	_ = a.store.SetDownloadClientStatus(r.Context(), id, status, msg)
	if terr != nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": terr.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) testUnsaved(w http.ResponseWriter, r *http.Request) {
	var p clientPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	a.runTest(w, r, p.toStore())
}

// testClient builds a transient client from a config and runs its Test().
func (a *API) testClient(r *http.Request, dc store.DownloadClient) error {
	base := buildBase(dc)
	var c provider.DownloadClient
	switch dc.Implementation {
	case "sabnzbd":
		c = newSABnzbd("test", base, dc.APIKey, dc.Category, a.svc.http)
	case "qbittorrent":
		c = newQBittorrent("test", base, dc.Username, dc.APIKey, dc.Category, a.svc.http)
	default:
		return ErrUnsupportedProtocol
	}
	return c.Test(r.Context())
}

func (a *API) runTest(w http.ResponseWriter, r *http.Request, dc store.DownloadClient) {
	if err := a.testClient(r, dc); err != nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) schema(w http.ResponseWriter, r *http.Request) {
	fields := func(credLabel string) []map[string]any {
		return []map[string]any{
			{"name": "name", "type": "string", "required": true},
			{"name": "host", "type": "string", "required": true},
			{"name": "port", "type": "int", "required": false},
			{"name": "useSsl", "type": "bool", "required": false, "default": false},
			{"name": "urlBase", "type": "string", "required": false},
			{"name": "username", "type": "string", "required": false},
			{"name": "apiKey", "type": "string", "required": false, "label": credLabel},
			{"name": "category", "type": "string", "required": false},
			{"name": "priority", "type": "int", "required": false, "default": 25},
			{"name": "enabled", "type": "bool", "required": false, "default": true},
		}
	}
	api.WriteJSON(w, http.StatusOK, []map[string]any{
		{"implementation": "sabnzbd", "protocol": "usenet", "fields": fields("API Key")},
		{"implementation": "qbittorrent", "protocol": "torrent", "fields": fields("Password")},
	})
}

type grabPayload struct {
	DownloadURL string `json:"downloadUrl"`
	Title       string `json:"title"`
	Protocol    string `json:"protocol"`
	IndexerID   string `json:"indexerId"`
	Category    string `json:"category"`
	ClientID    string `json:"clientId"`
}

func (a *API) grab(w http.ResponseWriter, r *http.Request) {
	var p grabPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if strings.TrimSpace(p.DownloadURL) == "" {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "downloadUrl is required")
		return
	}
	req := provider.DownloadRequest{
		URL: p.DownloadURL, Title: p.Title, Protocol: provider.Protocol(p.Protocol),
		IndexerID: p.IndexerID, Category: p.Category,
	}
	id, err := a.svc.Grab(r.Context(), req, p.ClientID)
	if err != nil {
		writeGrabError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"id": id})
}

func writeGrabError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnsupportedProtocol), errors.Is(err, ErrClientUnavailable), errors.Is(err, ErrReleaseUnavailable):
		api.WriteError(w, http.StatusBadRequest, "grab_failed", err.Error())
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

func (a *API) queue(w http.ResponseWriter, r *http.Request) {
	res := a.svc.Queue(r.Context())
	if res.Items == nil {
		res.Items = []provider.DownloadItem{}
	}
	if res.ClientErrors == nil {
		res.ClientErrors = []ClientError{}
	}
	api.WriteJSON(w, http.StatusOK, res)
}

func (a *API) removeItem(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	itemID := chi.URLParam(r, "itemId")
	deleteData := r.URL.Query().Get("deleteData") == "true"
	if err := a.svc.Remove(r.Context(), clientID, itemID, deleteData); err != nil {
		api.WriteError(w, http.StatusBadRequest, "remove_failed", err.Error())
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func parseID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/downloadclient/ -run 'TestDownloadClientAPI|TestGrab'`
Expected: PASS.

- [ ] **Step 5: Full package sweep**

Run: `go test ./internal/downloadclient/... -count=1 && go vet ./internal/downloadclient/...`
Expected: PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/downloadclient/api.go internal/downloadclient/api_test.go
git commit -m "feat: add download-client REST API (CRUD, test, schema, grab, queue)"
```

---

## Task 9: Composition wiring + full sweep

**Files:**
- Modify: `cmd/nexus/main.go`
- Test: `cmd/nexus/main_test.go` (extend)

**Interfaces:**
- Consumes: `downloadclient.NewService`, `downloadclient.NewAPI`, `downloadclient.NewMonitor`, `api.Deps.WSForward`, `scheduler.Every`.
- Produces: a running server that mounts `/api/v1/downloadclient*`, `/api/v1/download`, `/api/v1/queue*`, reloads clients at startup, and schedules the queue monitor; `download.status` is added to `WSForward`.

- [ ] **Step 1: Extend the run test**

Add to `cmd/nexus/main_test.go` (append this test to the file):
```go
func TestRunMountsDownloadClientRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9599")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9599/api/v1/downloadclient", nil)
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
		t.Fatalf("GET /api/v1/downloadclient status = %d want 200", status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/nexus/ -run TestRunMountsDownloadClientRoutes`
Expected: FAIL — route returns 404 (download client not yet wired).

- [ ] **Step 3: Wire the module into `main.go`**

In `cmd/nexus/main.go`, add the import:
```go
	"github.com/hellboundg/nexus/internal/downloadclient"
```
After the indexer wiring block (locate the existing `idxAPI := indexer.NewAPI(...)` line), add the download-client construction immediately below it:
```go
	dlSvc := downloadclient.NewService(st)
	if err := dlSvc.Reload(ctx); err != nil {
		return err
	}
	dlAPI := downloadclient.NewAPI(st, dlSvc)
	dlMonitor := downloadclient.NewMonitor(dlSvc, bus)
```
Then, in the scheduler block (locate `sch.Every(15*time.Minute, func() command.Command {` for the indexer health check), add a second registration after it — the factory returns the SAME monitor instance each tick so its diff state persists:
```go
	sch.Every(1*time.Minute, func() command.Command { return dlMonitor })
```
Then update the router construction. Locate:
```go
	router := api.NewRouter(api.Deps{
		Auth: authSvc, Store: st, Version: version.Version(), Bus: bus,
		WSForward: []string{"indexer.status"},
	}, web.Handler(), idxAPI.Mount)
```
Replace with:
```go
	router := api.NewRouter(api.Deps{
		Auth: authSvc, Store: st, Version: version.Version(), Bus: bus,
		WSForward: []string{"indexer.status", "download.status"},
	}, web.Handler(), idxAPI.Mount, dlAPI.Mount)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/nexus/ -run TestRunMountsDownloadClientRoutes`
Expected: PASS.

- [ ] **Step 5: Full build + test sweep**

Run:
```bash
CGO_ENABLED=0 go build ./cmd/nexus
go vet ./...
go test ./... -count=1
```
Expected: build succeeds; vet clean; all packages PASS. Remove the built binary if produced (`rm -f nexus nexus.exe`).

- [ ] **Step 6: Verify module boundaries**

Run: `go list -deps ./internal/downloadclient | grep hellboundg`
Expected: only `internal/core/*` packages appear — no `internal/indexer`, `internal/media`, or `internal/automation`.

- [ ] **Step 7: Commit**

```bash
git add cmd/nexus/main.go cmd/nexus/main_test.go
git commit -m "feat: wire download-client engine into composition root with queue monitor"
```

---

## Self-Review Notes (author)

- **Spec coverage:** two clients SAB+qBit (Tasks 4–5), server-side grab / grab-proxy (Task 3 fetch + Task 6 Grab), configs-only persistence (Task 2), protocol+priority routing with explicit override (Task 6 `route`), interface extension grab+list+remove+test (Task 1), queue monitor → `download.status` → WS (Tasks 7, 9), CRUD/test/schema/grab/queue/remove API (Task 8), partial-success queue aggregation (Task 6 `Queue`), composition wiring (Task 9). Acceptance criteria §8.1–8.8 map to Tasks 8, 3/6, 6, 6/8, 7/9, 8, 9, 9.
- **Deviations from spec:** (1) grab fetch uses a plain 30s-timeout GET, not the indexer rate limiter (spec §10) — the shared `s.http` client carries the 30s timeout. (2) qBittorrent `.torrent`-file grabs return an empty id (the add endpoint returns no hash); magnets return the btih-derived hash. The item still surfaces in the queue monitor by hash — acceptable, noted. (3) The credential column is the single `api_key` (SAB key or qBit password), `json:"-"` write-only, matching indexer §10.1.
- **Type consistency:** `store.DownloadClient`, `provider.DownloadItem`/`DownloadStatus`/`DownloadRequest`/`DownloadClient`, `Service`/`NewService`/`Reload`/`Grab`/`Queue`/`Remove`, `ClientError`/`QueueResult`, `newSABnzbd`/`newQBittorrent`, `Monitor`/`NewMonitor`/`DownloadStatusChanged` (`"download.status"`), `API`/`NewAPI`/`Mount`, `buildBase`, `newServiceWithClients`, `fetchContent`, `maxContentBytes` verified consistent across Tasks 1–9.
- **Reused store helpers:** `boolToInt`, `rowScanner` already exist in `core/store` (from `indexer_store.go`) and are NOT redefined in Task 2.
- **Ordering note:** Task 3 introduces `errors.go` (needed by grab.go and both clients) before the clients in Tasks 4–5. `maxContentBytes` is defined once in `grab.go` (Task 3) and reused by the SAB/qBit `io.LimitReader` caps.
- **Module path** `github.com/hellboundg/nexus` used throughout.
```
