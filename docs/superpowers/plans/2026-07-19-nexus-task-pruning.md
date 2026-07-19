# Task-table Pruning + Honest Active-Count Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the `tasks` table growing without bound (hourly per-name prune), make the "Active Tasks" stat a real in-flight count, and show the last 10 in the Queue (Radarr/Sonarr parity).

**Architecture:** Two new `*store.Store` query methods (`PruneTasksPerName`, `CountActiveTasks`); a new core `command.NewPruneTasks` ("Housekeeping") that calls the prune method; wire Housekeeping into the scheduler (`sch.Every(time.Hour, …)`) and swap the status handler + Queue limit. Pure backend — no frontend, no `web/dist`, no migration.

**Tech Stack:** Go, `modernc.org/sqlite v1.53.0` (SQLite ≥3.25 → window functions available), chi router.

## Global Constraints

- **No new migration / no schema change.** Both new methods are queries. The applied-migration-count assertion in `internal/core/database/database_test.go` must stay untouched (still 8).
- **No frontend change and no `web/dist` rebuild.** The wire field stays `taskCount`; the FE is untouched.
- **Retention constants:** keep **50** terminal rows **per task name**; Housekeeping runs **hourly**; Queue shows the last **10**.
- **Never delete queued/running rows** — pruning targets `status IN ('completed','failed')` only.
- Package `command` already imports `context`, `store`, `events`; the new prune command file adds its own `fmt` import.
- Task names that reach the `tasks` table are a closed static set (verified in spec §4); per-name retention depends on this.

---

### Task 1: `PruneTasksPerName` store method

**Files:**
- Modify: `internal/core/store/store.go` (add method beside the other `tasks` methods, ~after `ListTasks`)
- Test: `internal/core/store/store_test.go`

**Interfaces:**
- Consumes: existing `Store.UpsertTask`, `Store.ListTasks`, `newTestStore(t)`.
- Produces: `func (s *Store) PruneTasksPerName(ctx context.Context, keep int) (int64, error)` — deletes terminal (`completed`/`failed`) rows beyond the newest `keep` **per task name**; never touches queued/running; returns rows deleted.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/store/store_test.go`:

```go
func TestPruneTasksPerName(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// A frequent task with 60 terminal rows, plus a queued and a running row.
	for i := 0; i < 60; i++ {
		if err := st.UpsertTask(ctx, Task{ID: fmt.Sprintf("f%d", i), Name: "Frequent", Status: "completed"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpsertTask(ctx, Task{ID: "fq", Name: "Frequent", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertTask(ctx, Task{ID: "fr", Name: "Frequent", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	// An infrequent task with a SINGLE old terminal row — must survive.
	if err := st.UpsertTask(ctx, Task{ID: "r0", Name: "Rare", Status: "completed"}); err != nil {
		t.Fatal(err)
	}

	deleted, err := st.PruneTasksPerName(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	// 60 Frequent completed - 50 kept = 10 deleted; Rare/queued/running untouched.
	if deleted != 10 {
		t.Fatalf("want 10 deleted, got %d", deleted)
	}

	rows, err := st.ListTasks(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{} // "name/status" -> count
	for _, r := range rows {
		counts[r.Name+"/"+r.Status]++
	}
	if counts["Frequent/completed"] != 50 {
		t.Fatalf("Frequent completed should be capped at 50, got %d", counts["Frequent/completed"])
	}
	if counts["Rare/completed"] != 1 {
		t.Fatalf("Rare's lone terminal row must survive, got %d", counts["Rare/completed"])
	}
	if counts["Frequent/queued"] != 1 || counts["Frequent/running"] != 1 {
		t.Fatalf("queued/running must never be pruned, got queued=%d running=%d",
			counts["Frequent/queued"], counts["Frequent/running"])
	}
}

func TestPruneTasksPerNameBelowThreshold(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = st.UpsertTask(ctx, Task{ID: fmt.Sprintf("x%d", i), Name: "X", Status: "completed"})
	}
	deleted, err := st.PruneTasksPerName(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("below threshold should delete nothing, got %d", deleted)
	}
}
```

Add `"fmt"` to the `store_test.go` import block if not already present (it currently imports `context`, `testing`, `time`, and the `database` package — add `"fmt"`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/store/ -run TestPruneTasksPerName -v`
Expected: FAIL — `st.PruneTasksPerName undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/core/store/store.go` after `ListTasks`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/store/ -run TestPruneTasksPerName -v`
Expected: PASS (both `TestPruneTasksPerName` and `TestPruneTasksPerNameBelowThreshold`).

- [ ] **Step 5: Commit**

```bash
git add internal/core/store/store.go internal/core/store/store_test.go
git commit -m "feat(sp5): store.PruneTasksPerName keeps newest N terminal rows per task name"
```

---

### Task 2: `CountActiveTasks` store method

**Files:**
- Modify: `internal/core/store/store.go` (add beside the other `tasks` methods)
- Test: `internal/core/store/store_test.go`

**Interfaces:**
- Consumes: existing `Store.UpsertTask`, `newTestStore(t)`.
- Produces: `func (s *Store) CountActiveTasks(ctx context.Context) (int, error)` — `COUNT(*)` of rows with `status IN ('queued','running')`.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/store/store_test.go`:

```go
func TestCountActiveTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_ = st.UpsertTask(ctx, Task{ID: "a", Name: "N", Status: "queued"})
	_ = st.UpsertTask(ctx, Task{ID: "b", Name: "N", Status: "running"})
	_ = st.UpsertTask(ctx, Task{ID: "c", Name: "N", Status: "completed"})
	_ = st.UpsertTask(ctx, Task{ID: "d", Name: "N", Status: "failed"})

	n, err := st.CountActiveTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 active (queued+running), got %d", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/store/ -run TestCountActiveTasks -v`
Expected: FAIL — `st.CountActiveTasks undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/core/store/store.go`:

```go
// CountActiveTasks returns the number of queued or running task rows.
func (s *Store) CountActiveTasks(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status IN ('queued','running')`).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/store/ -run TestCountActiveTasks -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/store/store.go internal/core/store/store_test.go
git commit -m "feat(sp5): store.CountActiveTasks counts queued+running rows"
```

---

### Task 3: `NewPruneTasks` Housekeeping command

**Files:**
- Create: `internal/core/command/prune.go`
- Test: `internal/core/command/prune_test.go`

**Interfaces:**
- Consumes: `Store.PruneTasksPerName` (Task 1); the `Command`/`Reporter` types and `Store.UpsertTask` from package `command`/`store`.
- Produces: `func NewPruneTasks(s *store.Store, keep int) Command` — `Name() == "Housekeeping"`; `Run` prunes to `keep` per name and reports `"<n> pruned"`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/command/prune_test.go`:

```go
package command

import (
	"context"
	"fmt"
	"testing"
)

type capReporter struct {
	pct int
	msg string
}

func (c *capReporter) Progress(pct int, msg string) { c.pct = pct; c.msg = msg }

func TestPruneCommandName(t *testing.T) {
	// newMgr (command_test.go) gives us a migrated *store.Store.
	_, s := newMgr(t)
	if got := NewPruneTasks(s, 50).Name(); got != "Housekeeping" {
		t.Fatalf("want Housekeeping, got %q", got)
	}
}

func TestPruneCommandRunPrunesAndReports(t *testing.T) {
	_, s := newMgr(t)
	ctx := context.Background()
	for i := 0; i < 55; i++ {
		if err := s.UpsertTask(ctx, store.Task{ID: fmt.Sprintf("c%d", i), Name: "Job", Status: "completed"}); err != nil {
			t.Fatal(err)
		}
	}

	rep := &capReporter{}
	if err := NewPruneTasks(s, 50).Run(ctx, rep); err != nil {
		t.Fatal(err)
	}
	// 55 - 50 = 5 pruned.
	if rep.msg != "5 pruned" {
		t.Fatalf("want report %q, got %q", "5 pruned", rep.msg)
	}
	rows, err := s.ListTasks(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 50 {
		t.Fatalf("want 50 rows kept, got %d", len(rows))
	}
}
```

Note: `prune_test.go` is in package `command`, which already uses `store` via `command_test.go`'s imports — but each file needs its own imports, so this file imports `context`, `fmt`, `testing`. It references `store.Task`, so also add `"github.com/hellboundg/nexus/internal/core/store"` to this file's import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/command/ -run TestPruneCommand -v`
Expected: FAIL — `NewPruneTasks undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/command/prune.go`:

```go
package command

import (
	"context"
	"fmt"

	"github.com/hellboundg/nexus/internal/core/store"
)

// pruneTasks is the scheduled Housekeeping command: it prunes the tasks table
// to the newest `keep` terminal rows per task name.
type pruneTasks struct {
	store *store.Store
	keep  int
}

// NewPruneTasks returns the Housekeeping command.
func NewPruneTasks(s *store.Store, keep int) Command {
	return &pruneTasks{store: s, keep: keep}
}

func (p *pruneTasks) Name() string { return "Housekeeping" }

func (p *pruneTasks) Run(ctx context.Context, r Reporter) error {
	n, err := p.store.PruneTasksPerName(ctx, p.keep)
	if err != nil {
		return err
	}
	r.Progress(100, fmt.Sprintf("%d pruned", n))
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/command/ -run TestPruneCommand -v`
Expected: PASS (both `TestPruneCommandName` and `TestPruneCommandRunPrunesAndReports`).

- [ ] **Step 5: Commit**

```bash
git add internal/core/command/prune.go internal/core/command/prune_test.go
git commit -m "feat(sp5): command.NewPruneTasks Housekeeping command"
```

---

### Task 4: Wire status count + Queue limit in the API

**Files:**
- Modify: `internal/core/api/system.go` (`handleStatus` count source; `handleTasks` Queue limit 50→10)
- Test: `internal/core/api/system_test.go`

**Interfaces:**
- Consumes: `Store.CountActiveTasks` (Task 2); existing `tasksTestRouter`, `keyed`, `fakeTasks`.
- Produces: `GET /api/v1/system/status` returns `taskCount` = active (queued+running) count; `GET /api/v1/system/tasks` Queue is bounded to 10.

- [ ] **Step 1: Write the failing tests**

Add to `internal/core/api/system_test.go`:

```go
func TestStatusTaskCountIsActiveOnly(t *testing.T) {
	ft := &fakeTasks{}
	h, st := tasksTestRouter(t, ft)
	ctx := context.Background()
	_ = st.UpsertTask(ctx, store.Task{ID: "a", Name: "N", Status: "queued"})
	_ = st.UpsertTask(ctx, store.Task{ID: "b", Name: "N", Status: "running"})
	_ = st.UpsertTask(ctx, store.Task{ID: "c", Name: "N", Status: "completed"})
	_ = st.UpsertTask(ctx, store.Task{ID: "d", Name: "N", Status: "failed"})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, keyed(http.MethodGet, "/api/v1/system/status"))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		TaskCount int `json:"taskCount"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TaskCount != 2 {
		t.Fatalf("taskCount should count active (queued+running) only, want 2 got %d", body.TaskCount)
	}
}

func TestSystemTasksQueueCappedAtTen(t *testing.T) {
	ft := &fakeTasks{}
	h, st := tasksTestRouter(t, ft)
	ctx := context.Background()
	for i := 0; i < 15; i++ {
		_ = st.UpsertTask(ctx, store.Task{ID: fmt.Sprintf("t%d", i), Name: "Job", Status: "completed"})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, keyed(http.MethodGet, "/api/v1/system/tasks"))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Queue []map[string]json.RawMessage `json:"queue"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Queue) != 10 {
		t.Fatalf("Queue should be capped at 10, got %d", len(body.Queue))
	}
}
```

(`system_test.go` already imports `context`, `encoding/json`, `fmt`, `net/http`, `net/http/httptest`, `testing`, and `store` — no import changes needed.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/api/ -run 'TestStatusTaskCountIsActiveOnly|TestSystemTasksQueueCappedAtTen' -v`
Expected: FAIL — `TestStatusTaskCountIsActiveOnly` gets 4 (current total-rows count), `TestSystemTasksQueueCappedAtTen` gets 15.

- [ ] **Step 3: Write the implementation**

In `internal/core/api/system.go`, replace the body of `handleStatus` (lines ~54-67) so it uses the active count:

```go
func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.deps.Store.CountActiveTasks(r.Context())
	if err != nil {
		slog.Default().Error("status failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, statusResponse{
		Version:   s.deps.Version,
		AppName:   "Nexus",
		Healthy:   true,
		TaskCount: count,
	})
}
```

In `handleTasks`, change the Queue read from 50 to 10:

```go
	rows, err := s.deps.Store.ListTasks(r.Context(), 10)
```

- [ ] **Step 4: Run the full api package tests**

Run: `go test ./internal/core/api/ -v`
Expected: PASS — the two new tests pass, and the existing `TestGetSystemTasks` / `TestSystemStatusRequiresAuth` still pass (neither asserts a count that changed).

- [ ] **Step 5: Commit**

```bash
git add internal/core/api/system.go internal/core/api/system_test.go
git commit -m "feat(sp5): status taskCount = active only; Queue shows last 10"
```

---

### Task 5: Wire Housekeeping into the scheduler

**Files:**
- Modify: `cmd/nexus/main.go` (register the hourly Housekeeping task beside the other `sch.Every` calls)
- Test: `cmd/nexus/main_test.go`

**Interfaces:**
- Consumes: `command.NewPruneTasks` (Task 3); existing `sch`, `st`, `command`, `time` in `main.go`.
- Produces: a running instance registers a `Housekeeping` scheduled task visible at `GET /api/v1/system/tasks`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/nexus/main_test.go`:

```go
func TestRunRegistersHousekeepingTask(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9596")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	found := false
	for time.Now().Before(deadline) && !found {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9596/api/v1/system/tasks", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var body struct {
			Scheduled []struct {
				Name string `json:"name"`
			} `json:"scheduled"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		for _, s := range body.Scheduled {
			if s.Name == "Housekeeping" {
				found = true
			}
		}
		if !found {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !found {
		t.Fatal("Housekeeping scheduled task not registered")
	}
}
```

Add `"encoding/json"` to `main_test.go`'s import block (it currently imports `context`, `net/http`, `testing`, `time`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/nexus/ -run TestRunRegistersHousekeepingTask -v`
Expected: FAIL — "Housekeeping scheduled task not registered".

- [ ] **Step 3: Write the implementation**

In `cmd/nexus/main.go`, add after the last `sch.Every(...)` registration (after the `UpgradeSearchEnabled` block, before `sch.Start()`):

```go
	const taskRetention = 50 // newest terminal rows kept per task name
	sch.Every(time.Hour, func() command.Command {
		return command.NewPruneTasks(st, taskRetention)
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/nexus/ -run TestRunRegistersHousekeepingTask -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/nexus/main.go cmd/nexus/main_test.go
git commit -m "feat(sp5): schedule hourly Housekeeping task-table prune"
```

---

### Final verification (after all tasks)

- [ ] **Full Go suite + build + vet**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all packages PASS, build clean, vet clean.

- [ ] **Confirm no FE / dist / migration drift**

Run: `git status --porcelain` and confirm only Go files under `internal/` and `cmd/` (+ this plan/spec) changed — no `web/` changes, no new file under `internal/core/database/migrations/`.

## Self-Review Notes

- **Spec coverage:** §3.1 PruneTasksPerName → T1; §3.2 Housekeeping command → T3; §3.3 wiring → T5; §3.4 CountActiveTasks + handleStatus → T2+T4; §3.5 Queue 10 → T4. §4 per-name rationale → T1's infrequent-survives assertion. §5 test list → each task's tests (T1 covers the frequent-capped + infrequent-survives + below-threshold cases). All covered.
- **Type consistency:** `PruneTasksPerName(ctx, keep int) (int64, error)` used identically in T1 (def), T3 (call). `CountActiveTasks(ctx) (int, error)` in T2 (def), T4 (call). `NewPruneTasks(s *store.Store, keep int) Command` in T3 (def), T5 (call). `Name()=="Housekeeping"` consistent across T3/T5.
- **No new migration** — reaffirmed in Global Constraints and final verification.
