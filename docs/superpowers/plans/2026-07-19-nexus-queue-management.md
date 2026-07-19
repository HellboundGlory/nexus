# SP-A: Queue / History / Blocklist Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Clear buttons to all three Activity tabs, server-side pagination to History and Blocklist, and make queue removal cancel the actual download in the download client.

**Architecture:** Backend-first. The store gains paged reads and bulk deletes. `importing.QueueReader.Queue` changes signature to stop discarding the download client's per-client errors, which is what lets the clear endpoint refuse when a client is unreachable. Removal logic moves from the API handler into `Service.RemoveQueueItem` / `Service.ClearQueue` so it is testable without HTTP. The frontend then consumes the new envelopes and endpoints.

**Tech Stack:** Go 1.x, chi router, SQLite (`database/sql`), React + TypeScript, TanStack Query, Radix UI, vitest, Tailwind (CSS custom properties for theming).

**Spec:** `docs/superpowers/specs/2026-07-19-nexus-queue-management-design.md`

## Global Constraints

- **`store.ListQueue` and `store.QueueByStatus` must NOT be paginated or otherwise changed.** `automation.activeQueue` (`internal/automation/search.go:87`) uses `ListQueue`'s full result as the only guard against duplicate grabs. Spec §3.1.
- **`GET /queue` keeps its bare-array wire shape.** Only `/history` and `/blocklist` become envelopes. Spec §2 non-goals.
- JSON field names are camelCase.
- Go errors from the store surface as 500; `store.ErrNotFound` as 404; download-client unavailability as **503** with code `client_unavailable`.
- Frontend UI components live in `web/src/components/ui/` with **lowercase filenames** (`button.tsx`, `dialog.tsx`) — match that convention.
- Colors come from CSS custom properties (`var(--color-brand)`, `--color-warn`, `--color-muted`, `--color-border`, `--color-panel`, `--color-ok`). Never hardcode hex.
- Run `npm run build` in `web/` and commit `web/dist` only in the final task.

## Verified Test Harness

Do not invent helpers. These exist and are what you use:

**`internal/core/store` (package `store`)**
- `newTestStore(t *testing.T) *Store` — `store_test.go:12`
- `st.CreateMovie(ctx, Movie{TMDBID: 1, Title: "Dune"}) (int64, error)`
- `st.AddBlocklist(ctx, Blocklist{MediaKind, MovieID, SourceTitle, Reason}) (int64, error)`
- `st.AddHistory(ctx, HistoryEvent{...}) error`
- Tests are **inside** package `store` — reference types unqualified (`Movie{}`, not `store.Movie{}`).

**`internal/importing` (package `importing`)**
- `newTestStore(t) *store.Store` — `enqueue_test.go:43` (importing has its own)
- `newSvc(t) (*Service, *store.Store)` — `enqueue_test.go:38`
- `newSvcWithQueue(t, q QueueReader) (*Service, *store.Store)` — `enqueue_test.go:56`
- `newTestAPI(t) (http.Handler, *store.Store)` — `api_test.go:17`
- `fakeQueue{items []provider.DownloadItem; removed map[string]bool}` — `enqueue_test.go:25`
- `seedSeriesWithProfile(t, st) (seriesID, episodeID int64)`, `itoa(int64) string`
- Tests are **inside** package `importing` — `Service`, `QueueReader` unqualified; `store.X` qualified.

**Frontend (`web/`)**
- No shared `renderWithProviders`. Each test file defines a local `wrapper()` returning a `QueryClientProvider` — see `web/src/features/activity/api.test.tsx:13-19`.
- Hook tests mock the api client: `vi.mock("@/lib/api", async (orig) => {...})` then `vi.mocked(apiClient.apiGet).mockResolvedValue(...)`.
- Component tests that use toasts must wrap in `ToastProvider` (`useToast` throws outside it).
- Run tests: `cd web && npx vitest run <path>`. Typecheck: `cd web && npx tsc -p tsconfig.app.json --noEmit`.
- **The `-p tsconfig.app.json` is required, not optional.** `web/tsconfig.json` is a solution-style
  config with `"files": []` and project references, so a bare `npx tsc --noEmit` typechecks nothing
  and exits 0 no matter how broken the code is. Note also that vitest is not a substitute: the
  section tests mock their hooks, so a hook signature change breaks types while every test still
  passes. The real typecheck is the only gate that catches it.

## Known Breakages To Fix (not optional)

`TestAPIQueueListAndHistory` (`internal/importing/api_test.go:24`) loops over `{"/queue", "/history"}` asserting **both** bodies start with `[`. Task 4 turns `/history` into an envelope, so this test must be split there — `/queue` keeps the array assertion, `/history` gets an envelope assertion. Do not delete the test.

---

### Task 1: Store paged reads and bulk deletes

**Files:**
- Modify: `internal/core/store/import_store.go` (add after `ListHistory`, ~line 258)
- Modify: `internal/core/store/blocklist_store.go` (add after `ListBlocklist`, ~line 85)
- Test: `internal/core/store/import_store_test.go`, `internal/core/store/blocklist_store_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces:
  - `func (s *Store) ListHistoryPage(ctx context.Context, offset, limit int) ([]HistoryEvent, int, error)`
  - `func (s *Store) ListBlocklistPage(ctx context.Context, offset, limit int) ([]Blocklist, int, error)`
  - `func (s *Store) ClearHistory(ctx context.Context) (int64, error)`
  - `func (s *Store) ClearBlocklist(ctx context.Context) (int64, error)`
  - All return `(rows, total, err)` where `total` is the **unpaged** row count.

- [ ] **Step 1: Write the failing store tests**

Add to `internal/core/store/import_store_test.go`:

```go
func TestListHistoryPageSlicesAndCountsTotal(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := st.AddHistory(ctx, HistoryEvent{
			EventType: "grabbed", MediaKind: "movie",
			SourceTitle: fmt.Sprintf("Rel.%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Page 1 of 2: newest first (id DESC), matching ListHistory's ordering.
	rows, total, err := st.ListHistoryPage(ctx, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].SourceTitle != "Rel.4" {
		t.Fatalf("rows[0] = %q, want newest Rel.4", rows[0].SourceTitle)
	}

	// Offset past the end: empty page, but the real total.
	rows, total, err = st.ListHistoryPage(ctx, 99, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 || total != 5 {
		t.Fatalf("out-of-range page = %d rows, total %d; want 0 rows, total 5", len(rows), total)
	}

	// limit <= 0 falls back to the 50 default rather than returning nothing.
	rows, _, err = st.ListHistoryPage(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("limit=0 returned %d rows, want all 5 via the default", len(rows))
	}
}

func TestClearHistoryReturnsCountAndEmptiesTable(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := st.AddHistory(ctx, HistoryEvent{EventType: "grabbed", MediaKind: "movie"}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := st.ClearHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("ClearHistory = %d, want 3", n)
	}
	rows, total, err := st.ListHistoryPage(ctx, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 || total != 0 {
		t.Fatalf("after clear: %d rows, total %d; want empty", len(rows), total)
	}
	// Clearing an empty table is a no-op, not an error.
	if n, err := st.ClearHistory(ctx); err != nil || n != 0 {
		t.Fatalf("second ClearHistory = (%d, %v), want (0, nil)", n, err)
	}
}

// Regression guard for spec §3.1: automation.activeQueue depends on ListQueue
// returning EVERY row. If it is ever paginated, items past the first page look
// un-queued and get grabbed a second time on the next sweep.
func TestListQueueReturnsAllRowsUnpaged(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 60; i++ {
		if _, err := st.EnqueueGrab(ctx, QueueItem{
			ClientItemID: fmt.Sprintf("h%d", i), Protocol: "usenet",
			SourceTitle: fmt.Sprintf("Rel.%d", i), MediaKind: "movie",
			Status: QueueGrabbed,
		}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := st.ListQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 60 {
		t.Fatalf("ListQueue returned %d rows, want all 60 (see spec §3.1)", len(rows))
	}
}
```

Add to `internal/core/store/blocklist_store_test.go`:

```go
func TestListBlocklistPageAndClear(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	movieID, err := st.CreateMovie(ctx, Movie{TMDBID: 1, Title: "Dune"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := st.AddBlocklist(ctx, Blocklist{
			MediaKind: "movie", MovieID: &movieID,
			SourceTitle: fmt.Sprintf("Dune.2021.%d-GRP", i), Reason: "boom",
		}); err != nil {
			t.Fatal(err)
		}
	}

	rows, total, err := st.ListBlocklistPage(ctx, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(rows) != 3 {
		t.Fatalf("page = %d rows, total %d; want 3 rows, total 4", len(rows), total)
	}
	if rows[0].SourceTitle != "Dune.2021.3-GRP" {
		t.Fatalf("rows[0] = %q, want newest (id DESC)", rows[0].SourceTitle)
	}

	n, err := st.ClearBlocklist(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("ClearBlocklist = %d, want 4", n)
	}
	if rows, _, _ := st.ListBlocklistPage(ctx, 0, 50); len(rows) != 0 {
		t.Fatalf("after clear: %d rows, want 0", len(rows))
	}
}
```

`import_store_test.go` needs `"fmt"` in its import block; `blocklist_store_test.go` needs `"fmt"` too.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/store/ -run 'HistoryPage|ClearHistory|ListQueueReturnsAll|BlocklistPageAndClear' -v`
Expected: FAIL — `st.ListHistoryPage undefined`, `st.ClearHistory undefined`, `st.ListBlocklistPage undefined`, `st.ClearBlocklist undefined`. (`TestListQueueReturnsAllRowsUnpaged` should already PASS — it guards existing behaviour.)

- [ ] **Step 3: Implement the history methods**

Add to `internal/core/store/import_store.go`, directly after `ListHistory`:

```go
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
```

- [ ] **Step 4: Implement the blocklist methods**

Add to `internal/core/store/blocklist_store.go`, directly after `ListBlocklist`:

```go
// ListBlocklistPage returns one page of blocklist entries newest-first plus the
// total row count across all pages. Ordering matches ListBlocklist.
func (s *Store) ListBlocklistPage(ctx context.Context, offset, limit int) ([]Blocklist, int, error) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blocklist`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+blocklistCols+` FROM blocklist ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Blocklist{}
	for rows.Next() {
		bl, err := scanBlocklistRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, bl)
	}
	return out, total, rows.Err()
}

// ClearBlocklist deletes every blocklist row, returning how many were removed.
// Previously-rejected releases become eligible for grabbing again.
func (s *Store) ClearBlocklist(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM blocklist`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/core/store/ -v`
Expected: PASS, including the pre-existing store tests.

- [ ] **Step 6: Commit**

```bash
git add internal/core/store/
git commit -m "feat(sp-a): store paged history/blocklist reads and bulk clears"
```

---

### Task 2: Stop discarding the download client's per-client errors

**Files:**
- Modify: `internal/importing/importing.go:26-31` (the `QueueReader` interface)
- Modify: `internal/importing/api.go:60` (`listQueue` call site)
- Modify: `internal/importing/command.go:22` (`ImportCompleted` call site)
- Modify: `internal/importing/importer.go` (`ImportItem` call site, if it calls `Queue`)
- Modify: `cmd/nexus/main.go:243-251` (`dlQueueAdapter`)
- Test: `internal/importing/enqueue_test.go` (update `fakeQueue`)

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces:
  - `type ClientError struct { ClientID string \`json:"clientId"\`; Message string \`json:"message"\` }`
  - `type QueueSnapshot struct { Items []provider.DownloadItem; ClientErrors []ClientError }`
  - `QueueReader.Queue(ctx context.Context) QueueSnapshot` (**changed** from `[]provider.DownloadItem`)
  - `fakeQueue` gains a settable `clientErrors []ClientError` field.

**Why this task exists:** `downloadclient.Service.Queue` already returns `QueueResult{Items, ClientErrors}`, but `dlQueueAdapter` returns `.Items` and drops the errors, so the importing layer cannot tell "the client says this download is gone" from "the client did not answer". Task 3's refuse-to-clear depends on that distinction. Spec §3.4 / §4.2.

- [ ] **Step 1: Change the interface and add the types**

In `internal/importing/importing.go`, replace the `QueueReader` block (lines ~26-31):

```go
// ClientError reports one download client that could not be reached during a
// queue read. Mirrors downloadclient.ClientError without importing that package.
type ClientError struct {
	ClientID string `json:"clientId"`
	Message  string `json:"message"`
}

// QueueSnapshot is one read of the aggregated download-client queue. Items and
// ClientErrors are both partial: a client that failed contributes an entry to
// ClientErrors and no items. Callers that only need the items read .Items;
// callers that must not act on an incomplete picture check .ClientErrors.
type QueueSnapshot struct {
	Items        []provider.DownloadItem
	ClientErrors []ClientError
}

// QueueReader reads the aggregated download-client queue and removes items.
// Satisfied by a thin adapter over downloadclient.Service at the composition root.
type QueueReader interface {
	Queue(ctx context.Context) QueueSnapshot
	Remove(ctx context.Context, clientID, itemID string, deleteData bool) error
}
```

- [ ] **Step 2: Run the build to enumerate every call site**

Run: `go build ./...`
Expected: FAIL. The compiler now lists every place that must change — this is the point of changing the signature rather than adding a second method. Expect errors in `internal/importing/api.go`, `internal/importing/command.go`, and `cmd/nexus/main.go`.

- [ ] **Step 3: Update the production call sites**

`internal/importing/api.go`, in `listQueue` (~line 60) — change:

```go
	live := a.svc.queue.Queue(r.Context())
```

to:

```go
	live := a.svc.queue.Queue(r.Context()).Items
```

`internal/importing/command.go`, in `ImportCompleted` (~line 22) — change:

```go
	items := s.queue.Queue(ctx)
```

to:

```go
	items := s.queue.Queue(ctx).Items
```

`cmd/nexus/main.go`, replace `dlQueueAdapter.Queue` (~lines 240-247):

```go
// dlQueueAdapter maps downloadclient.Service.Queue()'s result onto importing's
// QueueReader so importing does not import the downloadclient package. The
// per-client errors are carried across, not dropped: clearing the queue refuses
// to run against an incomplete picture (spec §3.4).
type dlQueueAdapter struct{ svc *downloadclient.Service }

func (a dlQueueAdapter) Queue(ctx context.Context) importing.QueueSnapshot {
	res := a.svc.Queue(ctx)
	out := importing.QueueSnapshot{Items: res.Items}
	for _, e := range res.ClientErrors {
		out.ClientErrors = append(out.ClientErrors, importing.ClientError{
			ClientID: e.ClientID, Message: e.Message,
		})
	}
	return out
}
```

Leave `dlQueueAdapter.Remove` unchanged.

If `go build` reports any other call site not listed here, apply the same `.Items` treatment — it is a caller that only needs the items.

- [ ] **Step 4: Update the test fake**

In `internal/importing/enqueue_test.go`, replace the `fakeQueue` type and its `Queue` method:

```go
type fakeQueue struct {
	items        []provider.DownloadItem
	clientErrors []ClientError
	removed      map[string]bool
	removeErr    error // when non-nil, every Remove call fails with it
}

func (f *fakeQueue) Queue(context.Context) QueueSnapshot {
	return QueueSnapshot{Items: f.items, ClientErrors: f.clientErrors}
}

func (f *fakeQueue) Remove(_ context.Context, _, itemID string, _ bool) error {
	if f.removed == nil {
		f.removed = map[string]bool{}
	}
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed[itemID] = true
	return nil
}
```

`removeErr` is unused this task; Task 3 needs it.

- [ ] **Step 5: Verify the build and the full suite**

Run: `go build ./... && go test ./...`
Expected: PASS across all packages. This task changes plumbing only — no behaviour changes, so every existing test must still pass unmodified. If a test fails, the `.Items` mapping was applied wrongly somewhere.

- [ ] **Step 6: Commit**

```bash
git add internal/importing/ cmd/nexus/main.go
git commit -m "refactor(sp-a): carry download-client errors through QueueReader

dlQueueAdapter dropped QueueResult.ClientErrors, so importing could not
distinguish a finished download from an unreachable client. Clearing the
queue needs that distinction."
```

---

### Task 3: Service-level RemoveQueueItem and ClearQueue

**Files:**
- Create: `internal/importing/queueops.go`
- Test: Create `internal/importing/queueops_test.go`

**Interfaces:**
- Consumes: `QueueSnapshot`, `ClientError`, `fakeQueue{items, clientErrors, removed, removeErr}` from Task 2.
- Produces:
  - `var ErrClientUnavailable = errors.New("download client unavailable")`
  - `type RemoveOptions struct { RemoveFromClient bool; Blocklist bool }`
  - `func (s *Service) RemoveQueueItem(ctx context.Context, id int64, opts RemoveOptions) error`
  - `type ClearResult struct { Removed int; ClientErrors []ClientError }`
  - `func (s *Service) ClearQueue(ctx context.Context, force bool) (ClearResult, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/importing/queueops_test.go`:

```go
package importing

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// seedQueueRow inserts a grabbed queue row whose client item id is itemID.
func seedQueueRow(t *testing.T, st *store.Store, itemID, title string) store.QueueItem {
	t.Helper()
	row, err := st.EnqueueGrab(context.Background(), store.QueueItem{
		DownloadClientID: "sab", ClientItemID: itemID, Protocol: "usenet",
		SourceTitle: title, MediaKind: "movie", Status: store.QueueGrabbed,
	})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func liveItem(itemID string) provider.DownloadItem {
	return provider.DownloadItem{ID: itemID, DownloadClientID: "sab", Status: provider.StatusDownloading}
}

func TestRemoveQueueItemRemovesFromClientByDefault(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: true}); err != nil {
		t.Fatal(err)
	}
	if !q.removed["h1"] {
		t.Fatal("expected the download to be removed from the client")
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("queue row still present, err = %v", err)
	}
}

func TestRemoveQueueItemSkipsClientWhenNotRequested(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: false}); err != nil {
		t.Fatal(err)
	}
	if q.removed["h1"] {
		t.Fatal("client removal should not happen when RemoveFromClient is false")
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("row should still be deleted")
	}
}

// The escape hatch from spec §4.5: a failing client must not trap the user with
// an undeletable row, but opting out of client removal is how you force it.
func TestRemoveQueueItemKeepsRowWhenClientFails(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}, removeErr: errors.New("connection refused")}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: true})
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("err = %v, want ErrClientUnavailable", err)
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); err != nil {
		t.Fatal("row must be KEPT when the client removal failed")
	}

	// Unchecking "remove from client" deletes it regardless.
	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("escape hatch failed: row should be gone")
	}
}

// No live match means the client already finished with it — not a failure.
func TestRemoveQueueItemWithNoLiveMatchDeletesRow(t *testing.T) {
	q := &fakeQueue{} // no live items
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: true}); err != nil {
		t.Fatal(err)
	}
	if len(q.removed) != 0 {
		t.Fatal("no live match: Remove should not have been called")
	}
	if _, err := st.GetQueueItem(context.Background(), row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("row should be deleted")
	}
}

func TestRemoveQueueItemBlocklistsWhenRequested(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	row := seedQueueRow(t, st, "h1", "Dune.2021-GRP")

	if err := svc.RemoveQueueItem(context.Background(), row.ID, RemoveOptions{RemoveFromClient: true, Blocklist: true}); err != nil {
		t.Fatal(err)
	}
	bl, err := st.ListBlocklist(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 1 || bl[0].SourceTitle != "Dune.2021-GRP" {
		t.Fatalf("blocklist = %+v, want one entry for the removed release", bl)
	}
}

func TestRemoveQueueItemUnknownIDIsNotFound(t *testing.T) {
	svc, _ := newSvc(t)
	err := svc.RemoveQueueItem(context.Background(), 4242, RemoveOptions{})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}

// Spec §4.4: an unreachable client means we cannot see the whole picture, so a
// non-forced clear refuses and deletes NOTHING.
func TestClearQueueRefusesWhenAClientIsUnreachable(t *testing.T) {
	q := &fakeQueue{
		items:        []provider.DownloadItem{liveItem("h1")},
		clientErrors: []ClientError{{ClientID: "sab", Message: "connection refused"}},
	}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")
	seedQueueRow(t, st, "h2", "B-GRP")

	_, err := svc.ClearQueue(context.Background(), false)
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("err = %v, want ErrClientUnavailable", err)
	}
	rows, _ := st.ListQueue(context.Background())
	if len(rows) != 2 {
		t.Fatalf("%d rows left, want both kept — a refused clear deletes nothing", len(rows))
	}
}

func TestClearQueueRemovesEveryRowAndItsDownload(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1"), liveItem("h2")}}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")
	seedQueueRow(t, st, "h2", "B-GRP")

	res, err := svc.ClearQueue(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("Removed = %d, want 2", res.Removed)
	}
	if !q.removed["h1"] || !q.removed["h2"] {
		t.Fatalf("both downloads should be removed from the client, got %v", q.removed)
	}
	if rows, _ := st.ListQueue(context.Background()); len(rows) != 0 {
		t.Fatalf("%d rows left, want 0", len(rows))
	}
}

// Force tolerates failure — it does NOT skip the work. A healthy client still
// gets every Remove call.
func TestClearQueueForceStillRemovesFromHealthyClient(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")

	res, err := svc.ClearQueue(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !q.removed["h1"] {
		t.Fatal("force must still attempt the client removal, not skip it")
	}
	if len(res.ClientErrors) != 0 {
		t.Fatalf("ClientErrors = %v, want none for a healthy client", res.ClientErrors)
	}
}

func TestClearQueueForceProceedsAndReportsClientErrors(t *testing.T) {
	q := &fakeQueue{
		items:        []provider.DownloadItem{liveItem("h1")},
		clientErrors: []ClientError{{ClientID: "sab", Message: "connection refused"}},
	}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")
	seedQueueRow(t, st, "h2", "B-GRP")

	res, err := svc.ClearQueue(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("Removed = %d, want 2 — force empties the queue", res.Removed)
	}
	if len(res.ClientErrors) == 0 {
		t.Fatal("a forced clear must REPORT the errors it tolerated, not hide them")
	}
	if rows, _ := st.ListQueue(context.Background()); len(rows) != 0 {
		t.Fatalf("%d rows left, want 0", len(rows))
	}
}

// The mid-sweep-drop case: preflight is clean but Remove itself fails. Force
// must continue rather than abort. This is distinct from the preflight case and
// regresses if force is implemented as preflight-skip only.
func TestClearQueueForceContinuesWhenRemoveErrors(t *testing.T) {
	q := &fakeQueue{
		items:     []provider.DownloadItem{liveItem("h1"), liveItem("h2")},
		removeErr: errors.New("connection refused"),
	}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")
	seedQueueRow(t, st, "h2", "B-GRP")

	res, err := svc.ClearQueue(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("Removed = %d, want 2 — force continues past Remove failures", res.Removed)
	}
	if len(res.ClientErrors) != 2 {
		t.Fatalf("ClientErrors = %d, want one per failed removal", len(res.ClientErrors))
	}
}

// Without force, the same mid-sweep failure aborts — rows already removed stay
// removed, and the caller learns the client is unavailable.
func TestClearQueueWithoutForceAbortsOnRemoveError(t *testing.T) {
	q := &fakeQueue{
		items:     []provider.DownloadItem{liveItem("h1")},
		removeErr: errors.New("connection refused"),
	}
	svc, st := newSvcWithQueue(t, q)
	seedQueueRow(t, st, "h1", "A-GRP")

	_, err := svc.ClearQueue(context.Background(), false)
	if !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("err = %v, want ErrClientUnavailable", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/importing/ -run 'RemoveQueueItem|ClearQueue' -v`
Expected: FAIL to compile — `RemoveOptions`, `ErrClientUnavailable`, `RemoveQueueItem`, `ClearResult`, `ClearQueue` all undefined.

- [ ] **Step 3: Implement**

Create `internal/importing/queueops.go`:

```go
package importing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hellboundg/nexus/internal/core/store"
)

// ErrClientUnavailable reports that a download client could not be reached, so
// an operation that must not act on an incomplete picture was refused.
var ErrClientUnavailable = errors.New("download client unavailable")

// RemoveOptions controls what happens alongside deleting a queue row.
type RemoveOptions struct {
	// RemoveFromClient cancels the download in its download client and deletes
	// its data. When the client call fails the row is KEPT, so the user can
	// retry; clearing this flag deletes the row unconditionally.
	RemoveFromClient bool
	// Blocklist records the release so automation will not grab it again.
	// Without it the next missing-search sweep may re-grab the same file.
	Blocklist bool
}

// RemoveQueueItem deletes one queue row, optionally cancelling its download and
// blocklisting the release first.
func (s *Service) RemoveQueueItem(ctx context.Context, id int64, opts RemoveOptions) error {
	row, err := s.store.GetQueueItem(ctx, id)
	if err != nil {
		return err // store.ErrNotFound surfaces as 404
	}
	if opts.RemoveFromClient {
		snap := s.queue.Queue(ctx)
		if it, ok := matchItem(snap.Items, row); ok {
			if err := s.queue.Remove(ctx, it.DownloadClientID, it.ID, true); err != nil {
				// Keep the row: the download is still running, and deleting the
				// row now would orphan it with nothing tracking it.
				return fmt.Errorf("%w: %s", ErrClientUnavailable, err)
			}
		}
		// No live match: the client has already finished with this download,
		// so there is nothing to cancel. Deleting the row is correct.
	}
	if opts.Blocklist {
		if _, err := s.store.AddBlocklist(ctx, store.Blocklist{
			MediaKind: row.MediaKind, MovieID: row.MovieID, SeriesID: row.SeriesID,
			SourceTitle: row.SourceTitle, Protocol: row.Protocol,
			QualityID: row.QualityID, Reason: "removed from queue",
		}); err != nil {
			// Abort before deleting the row so a retry loses nothing.
			return err
		}
	}
	if err := s.store.DeleteQueueItem(ctx, id); err != nil {
		return err
	}
	s.emit(ctx, QueueUpdated{ID: id})
	return nil
}

// ClearResult reports what a ClearQueue call actually did. ClientErrors is
// non-empty only for a forced clear that tolerated failures.
type ClearResult struct {
	Removed      int           `json:"removed"`
	ClientErrors []ClientError `json:"clientErrors,omitempty"`
}

// ClearQueue removes every queue row, cancelling each download in its client.
//
// When force is false an unreachable client refuses the whole operation before
// anything is deleted — clearing against an incomplete picture would orphan
// downloads Nexus can no longer see.
//
// force does NOT skip the client removals; it only tolerates their failure, so
// a client that is merely flaky still gets its downloads cancelled properly.
func (s *Service) ClearQueue(ctx context.Context, force bool) (ClearResult, error) {
	snap := s.queue.Queue(ctx)
	var res ClearResult
	if len(snap.ClientErrors) > 0 {
		if !force {
			return ClearResult{}, fmt.Errorf("%w: %s", ErrClientUnavailable, describeClientErrors(snap.ClientErrors))
		}
		res.ClientErrors = append(res.ClientErrors, snap.ClientErrors...)
	}
	rows, err := s.store.ListQueue(ctx)
	if err != nil {
		return ClearResult{}, err
	}
	for _, row := range rows {
		if it, ok := matchItem(snap.Items, row); ok {
			if err := s.queue.Remove(ctx, it.DownloadClientID, it.ID, true); err != nil {
				if !force {
					return res, fmt.Errorf("%w: %s", ErrClientUnavailable, err)
				}
				slog.Warn("importing: clear queue could not remove download from client",
					"queueId", row.ID, "clientId", it.DownloadClientID, "err", err)
				res.ClientErrors = append(res.ClientErrors, ClientError{
					ClientID: it.DownloadClientID, Message: err.Error(),
				})
			}
		}
		if err := s.store.DeleteQueueItem(ctx, row.ID); err != nil {
			return res, err
		}
		res.Removed++
		s.emit(ctx, QueueUpdated{ID: row.ID})
	}
	return res, nil
}

func describeClientErrors(errs []ClientError) string {
	if len(errs) == 1 {
		return fmt.Sprintf("%s: %s", errs[0].ClientID, errs[0].Message)
	}
	return fmt.Sprintf("%d download clients could not be reached", len(errs))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/importing/ -v`
Expected: PASS, including the pre-existing importing tests.

- [ ] **Step 5: Commit**

```bash
git add internal/importing/
git commit -m "feat(sp-a): RemoveQueueItem and ClearQueue with client-aware removal"
```

---

### Task 4: API endpoints

**Files:**
- Modify: `internal/importing/api.go` (`Mount`, `deleteQueue`, `history`, `listBlocklist`; add `clearQueue`, `clearHistory`, `clearBlocklist`, `writeErr`)
- Test: `internal/importing/api_test.go`

**Interfaces:**
- Consumes: Task 1's store methods; Task 3's `RemoveQueueItem`, `ClearQueue`, `RemoveOptions`, `ClearResult`, `ErrClientUnavailable`.
- Produces (wire contract the frontend depends on):
  - `GET /history?page=&pageSize=` → `{items, page, pageSize, total}`
  - `GET /blocklist?page=&pageSize=` → `{items, page, pageSize, total}`
  - `GET /queue` → **unchanged bare array**
  - `DELETE /queue?force=` → `{removed, clientErrors?}` or 503 `client_unavailable`
  - `DELETE /queue/{id}?removeFromClient=&blocklist=` → `{ok:true}`; 503 / 404
  - `DELETE /history` → `{removed}`
  - `DELETE /blocklist` → `{removed}`

- [ ] **Step 1: Write the failing API tests**

In `internal/importing/api_test.go`, **replace** `TestAPIQueueListAndHistory` (it currently asserts `/history` returns an array, which this task changes):

```go
// /queue keeps its bare-array wire shape; only /history and /blocklist became
// paged envelopes (spec §2 non-goals, §4.3).
func TestAPIQueueStaysABareArray(t *testing.T) {
	r, _ := newTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/queue", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(w.Body.String()), "[") {
		t.Fatalf("GET /queue status=%d body=%s, want a JSON array", w.Code, w.Body.String())
	}
}

func TestAPIHistoryAndBlocklistArePagedEnvelopes(t *testing.T) {
	r, st := newTestAPI(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := st.AddHistory(ctx, store.HistoryEvent{EventType: "grabbed", MediaKind: "movie"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/history?page=1&pageSize=2", "/blocklist"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d", path, w.Code)
		}
		var env struct {
			Items    json.RawMessage `json:"items"`
			Page     int             `json:"page"`
			PageSize int             `json:"pageSize"`
			Total    int             `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("GET %s not an envelope: %v body=%s", path, err, w.Body.String())
		}
		if !strings.HasPrefix(strings.TrimSpace(string(env.Items)), "[") {
			t.Fatalf("GET %s items = %s, want an array", path, env.Items)
		}
		if env.Page < 1 || env.PageSize < 1 {
			t.Fatalf("GET %s page=%d pageSize=%d, want both >= 1", path, env.Page, env.PageSize)
		}
	}

	// The page slice and total are honoured.
	req := httptest.NewRequest(http.MethodGet, "/history?page=1&pageSize=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env struct {
		Items []store.HistoryEvent `json:"items"`
		Total int                  `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Items) != 2 || env.Total != 3 {
		t.Fatalf("items=%d total=%d, want 2 and 3", len(env.Items), env.Total)
	}
}

func TestAPIPageSizeIsClamped(t *testing.T) {
	r, _ := newTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/history?pageSize=9999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env struct {
		PageSize int `json:"pageSize"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.PageSize != 100 {
		t.Fatalf("pageSize = %d, want clamped to 100", env.PageSize)
	}
}

func TestAPIClearHistoryAndBlocklist(t *testing.T) {
	r, st := newTestAPI(t)
	ctx := context.Background()
	if err := st.AddHistory(ctx, store.HistoryEvent{EventType: "grabbed", MediaKind: "movie"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/history", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /history = %d body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Removed int `json:"removed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Removed != 1 {
		t.Fatalf("removed = %d (err %v), want 1", got.Removed, err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/blocklist", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /blocklist = %d", w.Code)
	}
}

func TestAPIClearQueueRefusesWithUnreachableClient(t *testing.T) {
	q := &fakeQueue{clientErrors: []ClientError{{ClientID: "sab", Message: "connection refused"}}}
	svc, st := newSvcWithQueue(t, q)
	r := chi.NewRouter()
	NewAPI(svc).Mount(r)
	seedQueueRow(t, st, "h1", "A-GRP")

	req := httptest.NewRequest(http.MethodDelete, "/queue", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 body=%s", w.Code, w.Body.String())
	}
	if rows, _ := st.ListQueue(context.Background()); len(rows) != 1 {
		t.Fatal("a refused clear must delete nothing")
	}

	// force=true goes through.
	req = httptest.NewRequest(http.MethodDelete, "/queue?force=true", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("forced clear status = %d body=%s", w.Code, w.Body.String())
	}
	if rows, _ := st.ListQueue(context.Background()); len(rows) != 0 {
		t.Fatal("forced clear should have emptied the queue")
	}
}

func TestAPIDeleteQueueItemDefaultsToRemovingFromClient(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	r := chi.NewRouter()
	NewAPI(svc).Mount(r)
	row := seedQueueRow(t, st, "h1", "A-GRP")

	req := httptest.NewRequest(http.MethodDelete, "/queue/"+itoa(row.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !q.removed["h1"] {
		t.Fatal("removeFromClient must default to true")
	}
}

func TestAPIDeleteQueueItemHonoursFlags(t *testing.T) {
	q := &fakeQueue{items: []provider.DownloadItem{liveItem("h1")}}
	svc, st := newSvcWithQueue(t, q)
	r := chi.NewRouter()
	NewAPI(svc).Mount(r)
	row := seedQueueRow(t, st, "h1", "A-GRP")

	req := httptest.NewRequest(http.MethodDelete,
		"/queue/"+itoa(row.ID)+"?removeFromClient=false&blocklist=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if q.removed["h1"] {
		t.Fatal("removeFromClient=false must skip the client call")
	}
	bl, _ := st.ListBlocklist(context.Background())
	if len(bl) != 1 {
		t.Fatalf("blocklist has %d entries, want 1", len(bl))
	}
}
```

`api_test.go` needs `"github.com/go-chi/chi/v5"` and `"encoding/json"` in its imports (chi may already be absent — add it).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/importing/ -run 'TestAPI' -v`
Expected: FAIL — envelope unmarshal failures on `/history`, 405/404 on the new `DELETE` routes, `removeFromClient` ignored.

- [ ] **Step 3: Add the paging helper and envelope type**

Add near the top of `internal/importing/api.go`, after `idParam`:

```go
const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// pageParams reads 1-based ?page= and ?pageSize=, clamping both into range, and
// returns the page, the size, and the corresponding SQL offset.
func pageParams(r *http.Request) (page, size, offset int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ = strconv.Atoi(r.URL.Query().Get("pageSize"))
	if size < 1 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return page, size, (page - 1) * size
}

// pagedResponse is the envelope for the paged list endpoints. Items is always a
// JSON array, never null.
type pagedResponse struct {
	Items    any `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

// boolParam reads a query flag, returning def when absent or unparseable.
func boolParam(r *http.Request, name string, def bool) bool {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}
```

- [ ] **Step 4: Update Mount and the handlers**

In `internal/importing/api.go`, replace `Mount`:

```go
func (a *API) Mount(r chi.Router) {
	r.Route("/queue", func(r chi.Router) {
		r.Get("/", a.listQueue)
		r.Post("/", a.enqueue)
		r.Delete("/", a.clearQueue)
		r.Delete("/{id}", a.deleteQueue)
		r.Post("/{id}/import", a.importItem)
	})
	r.Route("/history", func(r chi.Router) {
		r.Get("/", a.history)
		r.Delete("/", a.clearHistory)
	})
	r.Route("/blocklist", func(r chi.Router) {
		r.Get("/", a.listBlocklist)
		r.Delete("/", a.clearBlocklist)
		r.Delete("/{id}", a.removeBlocklist)
	})
	r.Route("/config/naming", func(r chi.Router) {
		r.Get("/", a.getNaming)
		r.Put("/", a.putNaming)
	})
}
```

Replace `deleteQueue`:

```go
func (a *API) deleteQueue(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	// removeFromClient defaults to true so a bare DELETE does the safe thing
	// (no orphaned download); unchecking it is the escape hatch when the client
	// is unreachable.
	opts := RemoveOptions{
		RemoveFromClient: boolParam(r, "removeFromClient", true),
		Blocklist:        boolParam(r, "blocklist", false),
	}
	if err := a.svc.RemoveQueueItem(r.Context(), id, opts); err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) clearQueue(w http.ResponseWriter, r *http.Request) {
	res, err := a.svc.ClearQueue(r.Context(), boolParam(r, "force", false))
	if err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, res)
}

func (a *API) clearHistory(w http.ResponseWriter, r *http.Request) {
	n, err := a.svc.store.ClearHistory(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to clear history")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]int64{"removed": n})
}

func (a *API) clearBlocklist(w http.ResponseWriter, r *http.Request) {
	n, err := a.svc.store.ClearBlocklist(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to clear blocklist")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]int64{"removed": n})
}
```

Replace `history`:

```go
func (a *API) history(w http.ResponseWriter, r *http.Request) {
	page, size, offset := pageParams(r)
	rows, total, err := a.svc.store.ListHistoryPage(r.Context(), offset, size)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list history")
		return
	}
	api.WriteJSON(w, http.StatusOK, pagedResponse{Items: rows, Page: page, PageSize: size, Total: total})
}
```

Replace `listBlocklist` (keeping the title enrichment):

```go
func (a *API) listBlocklist(w http.ResponseWriter, r *http.Request) {
	page, size, offset := pageParams(r)
	rows, total, err := a.svc.store.ListBlocklistPage(r.Context(), offset, size)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list blocklist")
		return
	}
	out := make([]blocklistDTO, 0, len(rows))
	for _, bl := range rows {
		title := ""
		if bl.MovieID != nil {
			if m, err := a.svc.store.GetMovie(r.Context(), *bl.MovieID); err == nil {
				title = m.Title
			}
		} else if bl.SeriesID != nil {
			if se, err := a.svc.store.GetSeries(r.Context(), *bl.SeriesID); err == nil {
				title = se.Title
			}
		}
		out = append(out, blocklistDTO{Blocklist: bl, Title: title})
	}
	api.WriteJSON(w, http.StatusOK, pagedResponse{Items: out, Page: page, PageSize: size, Total: total})
}
```

Add the 503 case to `writeErr`, **before** the `default`:

```go
	case errors.Is(err, ErrClientUnavailable):
		api.WriteError(w, http.StatusServiceUnavailable, "client_unavailable", err.Error())
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/importing/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Run the whole Go suite**

Run: `go test ./... && go vet ./...`
Expected: PASS across all packages. Watch for any other test that assumed `/history` returned an array.

- [ ] **Step 7: Commit**

```bash
git add internal/importing/
git commit -m "feat(sp-a): paged history/blocklist endpoints and clear routes"
```

---

### Task 5: Pagination component

**Files:**
- Create: `web/src/components/ui/pagination.tsx`
- Test: Create `web/src/components/ui/pagination.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: `export function Pagination(props: { page: number; pageSize: number; total: number; onPageChange: (p: number) => void; onPageSizeChange: (s: number) => void }): JSX.Element`
  - Renders nothing at all when `total === 0`.
  - Page-size options are `[25, 50, 100]`.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/ui/pagination.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { Pagination } from "./pagination"

function setup(over: Partial<React.ComponentProps<typeof Pagination>> = {}) {
  const onPageChange = vi.fn()
  const onPageSizeChange = vi.fn()
  render(
    <Pagination
      page={1}
      pageSize={50}
      total={120}
      onPageChange={onPageChange}
      onPageSizeChange={onPageSizeChange}
      {...over}
    />,
  )
  return { onPageChange, onPageSizeChange }
}

describe("Pagination", () => {
  it("summarises the visible slice", () => {
    setup({ page: 2, pageSize: 50, total: 120 })
    expect(screen.getByText(/51.*100.*120/)).toBeTruthy()
  })

  it("clamps the summary to the total on the last page", () => {
    setup({ page: 3, pageSize: 50, total: 120 })
    expect(screen.getByText(/101.*120.*120/)).toBeTruthy()
  })

  it("disables Previous on the first page", () => {
    setup({ page: 1 })
    expect(screen.getByRole("button", { name: /previous/i })).toHaveProperty("disabled", true)
  })

  it("disables Next on the last page", () => {
    setup({ page: 3, pageSize: 50, total: 120 })
    expect(screen.getByRole("button", { name: /next/i })).toHaveProperty("disabled", true)
  })

  it("advances the page", async () => {
    const { onPageChange } = setup({ page: 1 })
    await userEvent.click(screen.getByRole("button", { name: /next/i }))
    expect(onPageChange).toHaveBeenCalledWith(2)
  })

  it("reports a page-size change", async () => {
    const { onPageSizeChange } = setup()
    await userEvent.selectOptions(screen.getByLabelText(/per page/i), "25")
    expect(onPageSizeChange).toHaveBeenCalledWith(25)
  })

  it("renders nothing when there is nothing to page", () => {
    const { container } = render(
      <Pagination page={1} pageSize={50} total={0} onPageChange={vi.fn()} onPageSizeChange={vi.fn()} />,
    )
    expect(container.textContent).toBe("")
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/ui/pagination.test.tsx`
Expected: FAIL — cannot resolve `./pagination`.

- [ ] **Step 3: Implement**

Create `web/src/components/ui/pagination.tsx`:

```tsx
const PAGE_SIZES = [25, 50, 100]

export function Pagination({
  page, pageSize, total, onPageChange, onPageSizeChange,
}: {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
  onPageSizeChange: (size: number) => void
}) {
  if (total === 0) return null

  const lastPage = Math.max(1, Math.ceil(total / pageSize))
  const first = (page - 1) * pageSize + 1
  const last = Math.min(page * pageSize, total)

  const btn =
    "rounded border border-[var(--color-border)] px-2 py-1 text-xs hover:border-[var(--color-brand)] disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:border-[var(--color-border)]"

  return (
    <div className="mt-4 flex items-center justify-between gap-4 text-xs text-[var(--color-muted)]">
      <span>
        Showing {first}–{last} of {total}
      </span>
      <div className="flex items-center gap-2">
        <label className="flex items-center gap-1">
          <span>Per page</span>
          <select
            aria-label="Rows per page"
            value={pageSize}
            onChange={(e) => onPageSizeChange(Number(e.target.value))}
            className="rounded border border-[var(--color-border)] bg-[var(--color-panel)] px-1 py-1 text-xs"
          >
            {PAGE_SIZES.map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </select>
        </label>
        <button className={btn} disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
          Previous
        </button>
        <span className="tabular-nums">
          {page} / {lastPage}
        </span>
        <button className={btn} disabled={page >= lastPage} onClick={() => onPageChange(page + 1)}>
          Next
        </button>
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/components/ui/pagination.test.tsx`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ui/pagination.tsx web/src/components/ui/pagination.test.tsx
git commit -m "feat(sp-a): shared Pagination component"
```

---

### Task 6: Frontend API hooks

**Files:**
- Modify: `web/src/features/activity/api.ts`
- Modify: `web/src/features/activity/types.ts` (add the envelope type)
- Test: `web/src/features/activity/api.test.tsx`

**Interfaces:**
- Consumes: Task 4's wire contract.
- Produces:
  - `type Paged<T> = { items: T[]; page: number; pageSize: number; total: number }`
  - `useHistory(page: number, pageSize: number)` → query of `Paged<HistoryEvent>`
  - `useBlocklist(page: number, pageSize: number)` → query of `Paged<BlocklistEntry>`
  - `useQueue()` — **unchanged**, still `QueueItem[]`
  - `useClearQueue()` → mutation taking `{ force?: boolean }`, returning `{ removed: number; clientErrors?: { clientId: string; message: string }[] }`
  - `useClearHistory()`, `useClearBlocklist()` → mutations taking no argument
  - `useRemoveQueueItem()` → mutation taking `{ id: number; removeFromClient: boolean; blocklist: boolean }`

- [ ] **Step 1: Write the failing tests**

Add to `web/src/features/activity/api.test.tsx` (it already mocks `apiGet`; extend the mock factory to add `apiDelete`):

```tsx
// Extend the existing vi.mock factory at the top of the file to:
//   return { ...actual, apiGet: vi.fn(), apiDelete: vi.fn() }
// and add these imports:
//   import { useHistory, useRemoveQueueItem, useClearQueue } from "@/features/activity/api"

describe("useHistory", () => {
  it("requests the asked-for page and unwraps the envelope", async () => {
    vi.mocked(apiClient.apiGet).mockResolvedValue({ items: [{ id: 1 }], page: 2, pageSize: 25, total: 60 })
    const { result } = renderHook(() => useHistory(2, 25), { wrapper: wrapper() })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(apiClient.apiGet).toHaveBeenCalledWith("/history?page=2&pageSize=25")
    expect(result.current.data?.total).toBe(60)
  })

  it("keys the cache by page so changing page refetches", async () => {
    vi.mocked(apiClient.apiGet).mockResolvedValue({ items: [], page: 1, pageSize: 50, total: 0 })
    const w = wrapper()
    const { result, rerender } = renderHook(({ p }: { p: number }) => useHistory(p, 50), {
      wrapper: w,
      initialProps: { p: 1 },
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    rerender({ p: 2 })
    await waitFor(() => expect(apiClient.apiGet).toHaveBeenCalledWith("/history?page=2&pageSize=50"))
  })
})

describe("useRemoveQueueItem", () => {
  it("defaults nothing — it sends exactly the flags it is given", async () => {
    vi.mocked(apiClient.apiDelete).mockResolvedValue({ ok: true })
    const { result } = renderHook(() => useRemoveQueueItem(), { wrapper: wrapper() })
    result.current.mutate({ id: 7, removeFromClient: true, blocklist: false })
    await waitFor(() =>
      expect(apiClient.apiDelete).toHaveBeenCalledWith("/queue/7?removeFromClient=true&blocklist=false"),
    )
  })

  it("passes both flags through when set", async () => {
    vi.mocked(apiClient.apiDelete).mockResolvedValue({ ok: true })
    const { result } = renderHook(() => useRemoveQueueItem(), { wrapper: wrapper() })
    result.current.mutate({ id: 7, removeFromClient: false, blocklist: true })
    await waitFor(() =>
      expect(apiClient.apiDelete).toHaveBeenCalledWith("/queue/7?removeFromClient=false&blocklist=true"),
    )
  })
})

describe("useClearQueue", () => {
  it("omits force by default", async () => {
    vi.mocked(apiClient.apiDelete).mockResolvedValue({ removed: 3 })
    const { result } = renderHook(() => useClearQueue(), { wrapper: wrapper() })
    result.current.mutate({})
    await waitFor(() => expect(apiClient.apiDelete).toHaveBeenCalledWith("/queue"))
  })

  it("sends force=true when forcing", async () => {
    vi.mocked(apiClient.apiDelete).mockResolvedValue({ removed: 3 })
    const { result } = renderHook(() => useClearQueue(), { wrapper: wrapper() })
    result.current.mutate({ force: true })
    await waitFor(() => expect(apiClient.apiDelete).toHaveBeenCalledWith("/queue?force=true"))
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/activity/api.test.tsx`
Expected: FAIL — `useHistory` takes no arguments yet; `useRemoveQueueItem` takes a number; `useClearQueue` does not exist.

- [ ] **Step 3: Add the envelope type**

Add to `web/src/features/activity/types.ts`:

```ts
export type Paged<T> = {
  items: T[]
  page: number
  pageSize: number
  total: number
}

export type ClientError = {
  clientId: string
  message: string
}

export type ClearResult = {
  removed: number
  clientErrors?: ClientError[]
}
```

- [ ] **Step 4: Implement the hooks**

In `web/src/features/activity/api.ts`, update the imports:

```ts
import type { QueueItem, HistoryEvent, BlocklistEntry, Paged, ClearResult } from "./types"
```

Replace `activityKeys` so paged keys nest under their list key (prefix invalidation in `useActivityInvalidation` keeps working):

```ts
export const activityKeys = {
  queue: ["queue"] as const,
  history: ["history"] as const,
  blocklist: ["blocklist"] as const,
  historyPage: (page: number, pageSize: number) => ["history", page, pageSize] as const,
  blocklistPage: (page: number, pageSize: number) => ["blocklist", page, pageSize] as const,
}
```

Replace `useHistory` and `useBlocklist`:

```ts
export function useHistory(page: number, pageSize: number) {
  return useQuery({
    queryKey: activityKeys.historyPage(page, pageSize),
    queryFn: () => apiGet<Paged<HistoryEvent>>(`/history?page=${page}&pageSize=${pageSize}`),
  })
}

export function useBlocklist(page: number, pageSize: number) {
  return useQuery({
    queryKey: activityKeys.blocklistPage(page, pageSize),
    queryFn: () => apiGet<Paged<BlocklistEntry>>(`/blocklist?page=${page}&pageSize=${pageSize}`),
  })
}
```

Replace `useRemoveQueueItem` and add the clear mutations:

```ts
export function useRemoveQueueItem() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, removeFromClient, blocklist }: { id: number; removeFromClient: boolean; blocklist: boolean }) =>
      apiDelete<{ ok: boolean }>(
        `/queue/${id}?removeFromClient=${removeFromClient}&blocklist=${blocklist}`,
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: activityKeys.queue })
      qc.invalidateQueries({ queryKey: activityKeys.blocklist })
    },
  })
}

export function useClearQueue() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ force }: { force?: boolean }) =>
      apiDelete<ClearResult>(force ? "/queue?force=true" : "/queue"),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.queue }),
  })
}

export function useClearHistory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => apiDelete<{ removed: number }>("/history"),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.history }),
  })
}

export function useClearBlocklist() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => apiDelete<{ removed: number }>("/blocklist"),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.blocklist }),
  })
}
```

`useQueue` and `useActivityInvalidation` are unchanged.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/activity/api.test.tsx`
Expected: PASS. The pre-existing `useQueue` poll test must still pass — `useQueue` was not touched.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/activity/api.ts web/src/features/activity/types.ts web/src/features/activity/api.test.tsx
git commit -m "feat(sp-a): paged activity hooks and clear mutations"
```

---

### Task 7: History and Blocklist sections

**Files:**
- Modify: `web/src/features/activity/HistorySection.tsx`
- Modify: `web/src/features/activity/BlocklistSection.tsx`
- Create: `web/src/features/activity/ClearConfirmDialog.tsx`
- Test: `web/src/features/activity/HistorySection.test.tsx`, `web/src/features/activity/BlocklistSection.test.tsx`, Create `web/src/features/activity/ClearConfirmDialog.test.tsx`

**Interfaces:**
- Consumes: Task 5's `Pagination`; Task 6's `useHistory(page, pageSize)`, `useBlocklist(page, pageSize)`, `useClearHistory()`, `useClearBlocklist()`.
- Produces: `export function ClearConfirmDialog(props: { open: boolean; onOpenChange: (o: boolean) => void; title: string; body: string; confirmLabel?: string; warning?: string | null; onConfirm: () => void }): JSX.Element`
  - When `warning` is a non-empty string the dialog renders it in warn colour and the confirm button reads `confirmLabel` (Task 8 uses this for "Clear anyway").

- [ ] **Step 1: Write the failing ClearConfirmDialog test**

Create `web/src/features/activity/ClearConfirmDialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ClearConfirmDialog } from "./ClearConfirmDialog"

describe("ClearConfirmDialog", () => {
  it("shows the body and confirms", async () => {
    const onConfirm = vi.fn()
    render(
      <ClearConfirmDialog
        open
        onOpenChange={vi.fn()}
        title="Clear history?"
        body="This removes all 12 history events."
        onConfirm={onConfirm}
      />,
    )
    expect(screen.getByText(/all 12 history events/i)).toBeTruthy()
    await userEvent.click(screen.getByRole("button", { name: /^clear$/i }))
    expect(onConfirm).toHaveBeenCalled()
  })

  it("surfaces a warning and a custom confirm label", () => {
    render(
      <ClearConfirmDialog
        open
        onOpenChange={vi.fn()}
        title="Clear queue?"
        body="This removes all 3 items."
        warning="sab: connection refused"
        confirmLabel="Clear anyway"
        onConfirm={vi.fn()}
      />,
    )
    expect(screen.getByText(/connection refused/i)).toBeTruthy()
    expect(screen.getByRole("button", { name: /clear anyway/i })).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/features/activity/ClearConfirmDialog.test.tsx`
Expected: FAIL — cannot resolve `./ClearConfirmDialog`.

- [ ] **Step 3: Implement ClearConfirmDialog**

Create `web/src/features/activity/ClearConfirmDialog.tsx`:

```tsx
import { Dialog, DialogTitle } from "@/components/ui/dialog"

export function ClearConfirmDialog({
  open, onOpenChange, title, body, warning, confirmLabel, onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  body: string
  /** When set, the dialog stays open in a warning state explaining why the
   *  first attempt was refused; confirming then retries with force. */
  warning?: string | null
  confirmLabel?: string
  onConfirm: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>{title}</DialogTitle>
      <p className="text-sm text-[var(--color-muted)]">{body}</p>
      {warning ? (
        <p className="mt-2 text-sm text-[var(--color-warn)]">{warning}</p>
      ) : null}
      <div className="mt-4 flex justify-end gap-2">
        <button
          onClick={() => onOpenChange(false)}
          className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
        >
          Cancel
        </button>
        <button
          onClick={onConfirm}
          className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
        >
          {confirmLabel ?? "Clear"}
        </button>
      </div>
    </Dialog>
  )
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd web && npx vitest run src/features/activity/ClearConfirmDialog.test.tsx`
Expected: PASS (2 tests).

- [ ] **Step 5: Update HistorySection**

Read `web/src/features/activity/HistorySection.tsx` first — keep its table markup exactly as-is and change only the data plumbing and the surrounding chrome.

Change the top of the component:

```tsx
import { useState } from "react"
import { relativeTime } from "@/lib/time"
import { Pagination } from "@/components/ui/pagination"
import { useMovies, useSeries } from "@/features/library/api"
import { useQualityDefinitions } from "@/features/settings/qualityApi"
import { useHistory, useClearHistory } from "./api"
import { ClearConfirmDialog } from "./ClearConfirmDialog"
import { movieTitleMap, seriesTitleMap, resolveTitle, qualityName, eventLabel } from "./resolve"

export function HistorySection() {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const history = useHistory(page, pageSize)
  const clearHistory = useClearHistory()
  const movies = useMovies()
  const series = useSeries()
  const defs = useQualityDefinitions()

  if (history.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading history…</div>
  if (history.isError) return <div className="p-6 text-sm text-[var(--color-warn)]">Failed to load history.</div>

  const rows = history.data?.items ?? []
  const total = history.data?.total ?? 0

  const movieMap = movieTitleMap(movies.data)
  const seriesMap = seriesTitleMap(series.data)

  const onClear = () => {
    clearHistory.mutate(undefined, { onSuccess: () => { setConfirmOpen(false); setPage(1) } })
  }
```

Then wrap the render. The `<table>` element and everything inside it is unchanged; only what surrounds it changes:

```tsx
  return (
    <div className="p-6">
      <div className="mb-3 flex items-center justify-between">
        <span className="text-xs text-[var(--color-muted)]">{total} events</span>
        {total > 0 ? (
          <button
            onClick={() => setConfirmOpen(true)}
            className="rounded border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-warn)] hover:border-[var(--color-warn)]"
          >
            Clear history
          </button>
        ) : null}
      </div>

      {rows.length === 0 ? (
        <div className="text-sm text-[var(--color-muted)]">No history yet.</div>
      ) : (
        <>
          <table className="w-full text-sm">
            {/* …unchanged thead/tbody… */}
          </table>
          <Pagination
            page={page}
            pageSize={pageSize}
            total={total}
            onPageChange={setPage}
            onPageSizeChange={(s) => { setPageSize(s); setPage(1) }}
          />
        </>
      )}

      <ClearConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title="Clear history?"
        body={`This permanently removes all ${total} history events.`}
        onConfirm={onClear}
      />
    </div>
  )
}
```

Note `setPage(1)` on page-size change — otherwise growing the page size can strand you past the last page.

- [ ] **Step 6: Update BlocklistSection the same way**

Read `web/src/features/activity/BlocklistSection.tsx` and apply the identical pattern: `useState` for page/pageSize/confirmOpen, `useBlocklist(page, pageSize)`, `useClearBlocklist()`, `rows = data?.items ?? []`, `total = data?.total ?? 0`, a "Clear blocklist" button, `<Pagination>` under the table, and a `ClearConfirmDialog` whose body warns about the consequence:

```tsx
        body={`This removes all ${total} blocklisted releases. They become eligible for automatic grabbing again.`}
```

Keep the existing per-row remove button and the table markup unchanged.

- [ ] **Step 7: Update the section tests**

Both `HistorySection.test.tsx` and `BlocklistSection.test.tsx` currently mock `./api` returning a bare array. Update their mocks to return the envelope shape and add coverage. For `HistorySection.test.tsx`:

```tsx
// The mocked useHistory must now return { data: { items, page, pageSize, total } }.
it("hides Clear when there is nothing to clear", () => {
  // mock useHistory -> { data: { items: [], page: 1, pageSize: 50, total: 0 }, isLoading: false, isError: false }
  // render, then:
  expect(screen.queryByRole("button", { name: /clear history/i })).toBeNull()
})

it("clears after confirming", async () => {
  // mock useHistory with total: 2 and two rows; mock useClearHistory -> { mutate }
  await userEvent.click(screen.getByRole("button", { name: /clear history/i }))
  await userEvent.click(screen.getByRole("button", { name: /^clear$/i }))
  expect(mutate).toHaveBeenCalled()
})
```

Read each existing test file and adapt its established mocking style rather than importing a new one.

- [ ] **Step 8: Run the frontend suite**

Run: `cd web && npx vitest run && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS, 0 type errors.

- [ ] **Step 9: Commit**

```bash
git add web/src/features/activity/ web/src/components/ui/
git commit -m "feat(sp-a): paginate and clear History and Blocklist"
```

---

### Task 8: Queue section — remove dialog, clear, and force

**Files:**
- Modify: `web/src/features/activity/QueueSection.tsx`
- Create: `web/src/features/activity/RemoveQueueItemDialog.tsx`
- Test: Create `web/src/features/activity/RemoveQueueItemDialog.test.tsx`; modify `web/src/features/activity/QueueSection.test.tsx`
- Modify: `web/dist/**` (rebuild)

**Interfaces:**
- Consumes: Task 6's `useRemoveQueueItem()`, `useClearQueue()`; Task 7's `ClearConfirmDialog`.
- Produces: `export function RemoveQueueItemDialog(props: { open: boolean; onOpenChange: (o: boolean) => void; title: string; onConfirm: (opts: { removeFromClient: boolean; blocklist: boolean }) => void }): JSX.Element`
  - Defaults: `removeFromClient` **true**, `blocklist` **false**. Both reset every time the dialog opens.

- [ ] **Step 1: Write the failing RemoveQueueItemDialog test**

Create `web/src/features/activity/RemoveQueueItemDialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { RemoveQueueItemDialog } from "./RemoveQueueItemDialog"

function open(onConfirm = vi.fn()) {
  render(
    <RemoveQueueItemDialog open onOpenChange={vi.fn()} title="Dune.2021-GRP" onConfirm={onConfirm} />,
  )
  return onConfirm
}

describe("RemoveQueueItemDialog", () => {
  it("defaults to removing from the client but not blocklisting", async () => {
    const onConfirm = open()
    await userEvent.click(screen.getByRole("button", { name: /^remove$/i }))
    expect(onConfirm).toHaveBeenCalledWith({ removeFromClient: true, blocklist: false })
  })

  it("passes both flags when the user changes them", async () => {
    const onConfirm = open()
    await userEvent.click(screen.getByLabelText(/remove from download client/i))
    await userEvent.click(screen.getByLabelText(/blocklist/i))
    await userEvent.click(screen.getByRole("button", { name: /^remove$/i }))
    expect(onConfirm).toHaveBeenCalledWith({ removeFromClient: false, blocklist: true })
  })

  it("warns that not blocklisting invites a re-grab", () => {
    open()
    expect(screen.getByText(/re-grab/i)).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/features/activity/RemoveQueueItemDialog.test.tsx`
Expected: FAIL — cannot resolve `./RemoveQueueItemDialog`.

- [ ] **Step 3: Implement RemoveQueueItemDialog**

Create `web/src/features/activity/RemoveQueueItemDialog.tsx`:

```tsx
import { useEffect, useState } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"

export function RemoveQueueItemDialog({
  open, onOpenChange, title, onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  onConfirm: (opts: { removeFromClient: boolean; blocklist: boolean }) => void
}) {
  const [removeFromClient, setRemoveFromClient] = useState(true)
  const [blocklist, setBlocklist] = useState(false)

  // Reset on every open so a previous removal's choices never carry over.
  useEffect(() => {
    if (open) {
      setRemoveFromClient(true)
      setBlocklist(false)
    }
  }, [open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>Remove from queue?</DialogTitle>
      <p className="text-sm text-[var(--color-muted)]">{title}</p>

      <label className="mt-3 flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={removeFromClient}
          onChange={(e) => setRemoveFromClient(e.target.checked)}
        />
        Remove from download client
      </label>
      <p className="mt-1 text-xs text-[var(--color-muted)]">
        Also cancel the download and delete its files.
      </p>

      <label className="mt-3 flex items-center gap-2 text-sm">
        <input type="checkbox" checked={blocklist} onChange={(e) => setBlocklist(e.target.checked)} />
        Blocklist this release
      </label>
      <p className="mt-1 text-xs text-[var(--color-muted)]">
        Stop this release being grabbed again. Without this, automation may re-grab the same file.
      </p>

      <div className="mt-4 flex justify-end gap-2">
        <button
          onClick={() => onOpenChange(false)}
          className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
        >
          Cancel
        </button>
        <button
          onClick={() => onConfirm({ removeFromClient, blocklist })}
          className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
        >
          Remove
        </button>
      </div>
    </Dialog>
  )
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd web && npx vitest run src/features/activity/RemoveQueueItemDialog.test.tsx`
Expected: PASS (3 tests).

- [ ] **Step 5: Wire QueueSection**

In `web/src/features/activity/QueueSection.tsx`, add state and swap the `window.confirm`:

```tsx
import { useState } from "react"
import { ApiError } from "@/lib/api"
// …existing imports…
import { useQueue, useImportItem, useRemoveQueueItem, useClearQueue } from "./api"
import { RemoveQueueItemDialog } from "./RemoveQueueItemDialog"
import { ClearConfirmDialog } from "./ClearConfirmDialog"
```

Inside the component, above the early returns:

```tsx
  const clearQueue = useClearQueue()
  const [removeTarget, setRemoveTarget] = useState<{ id: number; title: string } | null>(null)
  const [clearOpen, setClearOpen] = useState(false)
  // Set when a non-forced clear is refused; showing it turns the dialog into
  // the "Clear anyway" state. Force is never offered until it is the answer.
  const [clearWarning, setClearWarning] = useState<string | null>(null)
```

Replace `onRemove`:

```tsx
  const onRemove = (opts: { removeFromClient: boolean; blocklist: boolean }) => {
    if (!removeTarget) return
    removeItem.mutate(
      { id: removeTarget.id, ...opts },
      {
        onSuccess: () => {
          setRemoveTarget(null)
          toast("Removed from queue")
        },
        onError: (e) => toast(e instanceof ApiError ? e.message : "Remove failed", { variant: "error" }),
      },
    )
  }

  const onClear = (force: boolean) => {
    clearQueue.mutate(
      { force },
      {
        onSuccess: (res) => {
          setClearOpen(false)
          setClearWarning(null)
          const failed = res.clientErrors?.length ?? 0
          toast(
            failed > 0
              ? `Queue cleared (${res.removed} items). ${failed} download client(s) could not be reached; their downloads may still be running.`
              : `Queue cleared (${res.removed} items)`,
            failed > 0 ? { variant: "error" } : undefined,
          )
        },
        onError: (e) => {
          // A refused clear keeps the dialog open and offers the override.
          if (e instanceof ApiError && e.status === 503) {
            setClearWarning(e.message)
            return
          }
          setClearOpen(false)
          toast(e instanceof ApiError ? e.message : "Clear failed", { variant: "error" })
        },
      },
    )
  }
```

Change the per-row Remove button's handler to open the dialog:

```tsx
                  <button
                    onClick={() => setRemoveTarget({ id: r.id, title: r.sourceTitle || title })}
                    className="rounded border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-warn)] hover:border-[var(--color-warn)]"
                  >
                    Remove
                  </button>
```

Add a header row above the table with the Clear button, and both dialogs at the end of the returned tree:

```tsx
      <div className="mb-3 flex items-center justify-between">
        <span className="text-xs text-[var(--color-muted)]">{rows.length} items</span>
        <button
          onClick={() => { setClearWarning(null); setClearOpen(true) }}
          className="rounded border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-warn)] hover:border-[var(--color-warn)]"
        >
          Clear queue
        </button>
      </div>
```

```tsx
      <RemoveQueueItemDialog
        open={removeTarget !== null}
        onOpenChange={(o) => { if (!o) setRemoveTarget(null) }}
        title={removeTarget?.title ?? ""}
        onConfirm={onRemove}
      />
      <ClearConfirmDialog
        open={clearOpen}
        onOpenChange={(o) => { setClearOpen(o); if (!o) setClearWarning(null) }}
        title="Clear queue?"
        body={`This removes all ${rows.length} queued items and cancels their downloads.`}
        warning={clearWarning}
        confirmLabel={clearWarning ? "Clear anyway" : "Clear"}
        onConfirm={() => onClear(clearWarning !== null)}
      />
```

**Important:** the early `if (rows.length === 0) return …` currently short-circuits before any of this renders. Restructure so the empty state renders inside the same wrapper as the header (mirroring Task 7's HistorySection), otherwise the Clear button vanishes exactly when a stuck queue most needs it — and hide the Clear button when `rows.length === 0`.

- [ ] **Step 6: Update QueueSection tests**

Read `web/src/features/activity/QueueSection.test.tsx` and adapt to its existing mocking style. Add:

```tsx
it("removes via the dialog rather than window.confirm", async () => {
  // render with one queued row, then:
  await userEvent.click(screen.getByRole("button", { name: /^remove$/i }))     // row button
  await userEvent.click(screen.getByRole("button", { name: /^remove$/i }))     // dialog confirm
  expect(mutate).toHaveBeenCalledWith(
    { id: 1, removeFromClient: true, blocklist: false },
    expect.anything(),
  )
})

it("offers Clear anyway after a 503 refusal", async () => {
  // mock useClearQueue's mutate to invoke opts.onError(new ApiError(503, "client_unavailable", "sab: connection refused"))
  await userEvent.click(screen.getByRole("button", { name: /clear queue/i }))
  await userEvent.click(screen.getByRole("button", { name: /^clear$/i }))
  expect(await screen.findByText(/connection refused/i)).toBeTruthy()
  expect(screen.getByRole("button", { name: /clear anyway/i })).toBeTruthy()
})
```

Any test that stubbed `window.confirm` for the remove flow must drop that stub — the dialog replaces it.

- [ ] **Step 7: Run the full frontend suite and typecheck**

Run: `cd web && npx vitest run && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS, 0 type errors.

- [ ] **Step 8: Rebuild the bundle**

Run: `cd web && npm run build`
Expected: build succeeds; `web/dist` asset hashes change.

- [ ] **Step 9: Full-stack verification**

Run from the repo root:

```bash
go build ./... && go vet ./... && go test ./...
```

Expected: PASS across all packages.

- [ ] **Step 10: Commit**

```bash
git add web/src/features/activity/ web/dist
git commit -m "feat(sp-a): queue remove dialog, clear queue, and force override"
```

---

## Verification Checklist

After Task 8, confirm each spec requirement:

- [ ] `GET /history` and `GET /blocklist` return `{items, page, pageSize, total}`; `GET /queue` still returns a bare array (spec §4.3)
- [ ] `store.ListQueue` is unpaged and its regression test passes (spec §3.1)
- [ ] `DELETE /queue` refuses with 503 and deletes nothing when a client is unreachable (spec §4.4)
- [ ] `DELETE /queue?force=true` empties the queue, still attempts every `Remove`, and reports `clientErrors` (spec §4.4)
- [ ] `DELETE /queue/{id}` defaults `removeFromClient=true`, keeps the row when the client call fails, and deletes it when `removeFromClient=false` (spec §4.5)
- [ ] Blocklist-on-remove writes an entry scoped to the movie/series (spec §4.5)
- [ ] History and Blocklist tabs paginate with a 25/50/100 selector; Queue does not (spec §2, §4.6)
- [ ] Clear buttons on all three tabs, hidden when empty, behind confirmation (spec §4.6)
- [ ] "Clear anyway" appears only after a refusal (spec §4.6)
- [ ] `go build ./... && go vet ./... && go test ./...` all pass
- [ ] `cd web && npx vitest run && npx tsc -p tsconfig.app.json --noEmit` pass
- [ ] `web/dist` rebuilt and committed
