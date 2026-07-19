# System › Tasks tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the empty `/system` route into a tabbed Status + Tasks page — a Radarr/Sonarr-style Scheduled table (with per-task Run-now) and a live Queue table — and remove the endless Activity stream from the Dashboard.

**Architecture:** The scheduler (anonymous today) exposes its registered entries (name, interval, next-run) and a `RunNow`. Task rows gain `started_at`/`ended_at` (derived in SQL from the status transition, so the command Manager is untouched) to feed the Queue's Queued/Started/Ended/Duration. The core API serves `GET /system/tasks` + `POST /system/tasks/{name}/run`. The frontend adds a `SystemLayout` (Status/Tasks tabs), moves System Info out of Settings, and renders the two tables live via the existing `task.updated` WS event.

**Tech Stack:** Go (chi, database/sql over SQLite), React + TypeScript, TanStack Query, Vitest + Testing Library, Tailwind.

## Global Constraints

- Build/test with `CGO_ENABLED=0`.
- Intervals and all durations cross the wire as **integer seconds** (`intervalSeconds`, `lastDurationSeconds`, `durationSeconds`); timestamps are RFC3339; the frontend humanizes them ("5 seconds", "in 9 minutes", `HH:MM:SS`). Nullable time/duration fields serialise as JSON `null` when unset (never `0`), asserted via `json.RawMessage`.
- Task timestamps are derived in the `UpsertTask` SQL from the status transition (`started_at` on first `running`, `ended_at` on `completed`/`failed`) — the `command.Manager` is NOT modified.
- Disk/DB writes stay best-effort where they already are; no behavior change to existing scheduled commands.
- Deferred (out of scope, do not add): pruning the `tasks` table; changing the "Active Tasks" stat to a real count.
- `web/dist` is committed and CI drift-checks it — the final frontend task rebuilds it.
- Follow existing patterns: core API handlers in `internal/core/api`, `chi` routes in `NewRouter`; FE tabbed layouts like `SettingsLayout`/`ActivityLayout`; theme tokens (`--color-brand` #7c5cff, `--color-ok` #3fb950, `--color-warn` #d29922).

---

### Task 1: Backend — task timestamps + `LastTaskByName`

**Files:**
- Create: `internal/core/database/migrations/0008_task_times.sql`
- Modify: `internal/core/store/store.go` (`Task` struct, `UpsertTask`, `GetTask`, `ListTasks`; add `LastTaskByName`)
- Test: `internal/core/store/store_test.go`

**Interfaces:**
- Produces: `store.Task` gains `StartedAt *time.Time` / `EndedAt *time.Time`; `store.LastTaskByName(ctx, name string) (*Task, error)`. `UpsertTask` now stamps started/ended from status. Consumed by Tasks 2/3.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/store/store_test.go` (it is `package store`; use the store package's existing test-store helper — check the top of the file for its name, e.g. `newTestStore(t)`; `time`/`errors` may need importing):

```go
func TestTaskTimestampsLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// queued: no started/ended yet
	if err := st.UpsertTask(ctx, Task{ID: "t1", Name: "Job", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	q, _ := st.GetTask(ctx, "t1")
	if q.StartedAt != nil || q.EndedAt != nil {
		t.Fatalf("queued task should have nil started/ended, got %+v", q)
	}

	// running: started set, ended still nil
	if err := st.UpsertTask(ctx, Task{ID: "t1", Name: "Job", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	r, _ := st.GetTask(ctx, "t1")
	if r.StartedAt == nil || r.EndedAt != nil {
		t.Fatalf("running task should have started set, ended nil, got %+v", r)
	}

	// completed: ended set, started preserved
	if err := st.UpsertTask(ctx, Task{ID: "t1", Name: "Job", Status: "completed", Progress: 100}); err != nil {
		t.Fatal(err)
	}
	c, _ := st.GetTask(ctx, "t1")
	if c.StartedAt == nil || c.EndedAt == nil {
		t.Fatalf("completed task should have both timestamps, got %+v", c)
	}
	if !c.StartedAt.Equal(*r.StartedAt) {
		t.Fatal("started_at must be preserved across the terminal transition")
	}
}

func TestLastTaskByName(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if got, err := st.LastTaskByName(ctx, "Nope"); err != nil || got != nil {
		t.Fatalf("want nil,nil for unknown name, got %v,%v", got, err)
	}
	_ = st.UpsertTask(ctx, Task{ID: "a", Name: "Job", Status: "completed"})
	_ = st.UpsertTask(ctx, Task{ID: "b", Name: "Job", Status: "completed"})
	last, err := st.LastTaskByName(ctx, "Job")
	if err != nil {
		t.Fatal(err)
	}
	if last == nil || last.Name != "Job" {
		t.Fatalf("want a Job task, got %+v", last)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/store/ -run 'TestTaskTimestampsLifecycle|TestLastTaskByName' -v`
Expected: FAIL — `Task` has no `StartedAt`/`EndedAt`; `LastTaskByName` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/core/database/migrations/0008_task_times.sql`:

```sql
ALTER TABLE tasks ADD COLUMN started_at DATETIME;
ALTER TABLE tasks ADD COLUMN ended_at DATETIME;
```

In `internal/core/store/store.go`, add the two fields to `Task` (after `UpdatedAt`), and ensure `database/sql` + `time` are imported:

```go
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
```

Replace `UpsertTask` so the timestamps are derived from the status transition (Manager unchanged):

```go
func (s *Store) UpsertTask(ctx context.Context, t Task) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (id, name, status, progress, message)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   status = excluded.status,
		   progress = excluded.progress,
		   message = excluded.message,
		   updated_at = CURRENT_TIMESTAMP,
		   started_at = CASE WHEN excluded.status = 'running' AND started_at IS NULL
		                     THEN CURRENT_TIMESTAMP ELSE started_at END,
		   ended_at   = CASE WHEN excluded.status IN ('completed','failed')
		                     THEN CURRENT_TIMESTAMP ELSE ended_at END`,
		t.ID, t.Name, t.Status, t.Progress, t.Message)
	return err
}
```

Add a scan helper and use it in `GetTask`/`ListTasks`, and add `LastTaskByName`:

```go
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
		 FROM tasks WHERE name = ? ORDER BY created_at DESC LIMIT 1`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
```

Update `GetTask` to select the two new columns and scan via the helper (return `ErrNotFound` on `sql.ErrNoRows` as before):

```go
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
```

Update `ListTasks` to select + scan the new columns via the helper:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/store/ -run 'TestTaskTimestampsLifecycle|TestLastTaskByName' -v`
Expected: PASS.

- [ ] **Step 5: Full check + commit**

Run: `CGO_ENABLED=0 go build ./... && go vet ./internal/core/store/ && go test ./internal/core/store/`
Expected: all pass.

```bash
git add internal/core/database/migrations/0008_task_times.sql internal/core/store/store.go internal/core/store/store_test.go
git commit -m "feat(store): task started_at/ended_at + LastTaskByName"
```

---

### Task 2: Backend — scheduler exposes entries + Run-now

**Files:**
- Modify: `internal/core/scheduler/scheduler.go`
- Test: `internal/core/scheduler/scheduler_test.go`

**Interfaces:**
- Produces: `scheduler.ScheduledTask{Name string; Interval time.Duration; NextRun time.Time}`; `(*Scheduler).Scheduled() []ScheduledTask`; `(*Scheduler).RunNow(name string) (string, error)`. Consumed by Task 3.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/scheduler/scheduler_test.go` (reuses the file's existing `countCmd` + `command.NewManager`):

```go
func TestScheduledSnapshot(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/t.db")
	defer db.Close()
	database.Migrate(db)
	m := command.NewManager(store.New(db), events.New(), 1)
	m.Start()
	defer m.Stop()

	var n int32
	sch := New(m)
	sch.Every(time.Hour, func() command.Command { return countCmd{n: &n} })
	sch.Start()
	defer sch.Stop()

	got := sch.Scheduled()
	if len(got) != 1 {
		t.Fatalf("want 1 scheduled, got %d", len(got))
	}
	if got[0].Name != "Count" || got[0].Interval != time.Hour {
		t.Fatalf("bad entry: %+v", got[0])
	}
	if !got[0].NextRun.After(time.Now()) {
		t.Fatalf("next run should be in the future, got %v", got[0].NextRun)
	}
}

func TestRunNowEnqueues(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/t.db")
	defer db.Close()
	database.Migrate(db)
	st := store.New(db)
	m := command.NewManager(st, events.New(), 1)
	m.Start()
	defer m.Stop()

	var n int32
	sch := New(m)
	sch.Every(time.Hour, func() command.Command { return countCmd{n: &n} })
	// not started — RunNow should still enqueue immediately

	id, err := sch.RunNow("Count")
	if err != nil || id == "" {
		t.Fatalf("RunNow: id=%q err=%v", id, err)
	}
	if _, err := sch.RunNow("Nope"); err == nil {
		t.Fatal("unknown task should error")
	}
	// the enqueued command should run and increment n
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&n) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&n) == 0 {
		t.Fatal("RunNow command did not execute")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/scheduler/ -run 'TestScheduledSnapshot|TestRunNowEnqueues' -v`
Expected: FAIL — `Scheduled`/`RunNow` undefined.

- [ ] **Step 3: Write minimal implementation**

Rewrite `internal/core/scheduler/scheduler.go` (add `fmt`, `time` already imported; entries become pointers, add a mutex, capture name at registration, track next-run):

```go
package scheduler

import (
	"fmt"
	"sync"
	"time"

	"github.com/hellboundg/nexus/internal/core/command"
)

type entry struct {
	name     string
	interval time.Duration
	factory  func() command.Command
	nextRun  time.Time
}

// ScheduledTask is a read-only snapshot of a registered recurring task.
type ScheduledTask struct {
	Name     string
	Interval time.Duration
	NextRun  time.Time
}

type Scheduler struct {
	mgr      *command.Manager
	mu       sync.Mutex
	entries  []*entry
	stop     chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
}

func New(m *command.Manager) *Scheduler {
	return &Scheduler{mgr: m, stop: make(chan struct{})}
}

// Every registers a recurring command produced by factory at each interval.
// The task's name is captured once via factory().Name().
func (s *Scheduler) Every(d time.Duration, factory func() command.Command) {
	s.entries = append(s.entries, &entry{name: factory().Name(), interval: d, factory: factory})
}

func (s *Scheduler) Start() {
	now := time.Now()
	s.mu.Lock()
	for _, e := range s.entries {
		e.nextRun = now.Add(e.interval)
	}
	s.mu.Unlock()

	for _, e := range s.entries {
		s.wg.Add(1)
		e := e
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(e.interval)
			defer ticker.Stop()
			for {
				select {
				case <-s.stop:
					return
				case <-ticker.C:
					_, _ = s.mgr.Enqueue(e.factory())
					s.mu.Lock()
					e.nextRun = time.Now().Add(e.interval)
					s.mu.Unlock()
				}
			}
		}()
	}
}

// Scheduled returns a snapshot of the registered tasks.
func (s *Scheduler) Scheduled() []ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ScheduledTask, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, ScheduledTask{Name: e.name, Interval: e.interval, NextRun: e.nextRun})
	}
	return out
}

// RunNow enqueues the named task immediately and returns its task id.
func (s *Scheduler) RunNow(name string) (string, error) {
	for _, e := range s.entries {
		if e.name == name {
			return s.mgr.Enqueue(e.factory())
		}
	}
	return "", fmt.Errorf("scheduler: no task named %q", name)
}

// Stop is safe to call multiple times.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.wg.Wait()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/scheduler/ -v`
Expected: PASS (new tests + the pre-existing `TestEveryFiresRepeatedly` / double-stop).

- [ ] **Step 5: Full check + commit**

Run: `CGO_ENABLED=0 go build ./... && go vet ./internal/core/scheduler/ && go test ./internal/core/scheduler/`
Expected: all pass.

```bash
git add internal/core/scheduler/scheduler.go internal/core/scheduler/scheduler_test.go
git commit -m "feat(scheduler): expose Scheduled() snapshot + RunNow"
```

---

### Task 3: Backend — `/system/tasks` endpoints + wiring

**Files:**
- Modify: `internal/core/api/api.go` (`Deps` gains `Tasks`; register the two routes)
- Modify: `internal/core/api/system.go` (add `handleTasks` + `handleRunTask` + DTOs)
- Modify: `cmd/nexus/main.go` (pass `Tasks: sch`; add `"task.updated"` to `WSForward`)
- Test: `internal/core/api/system_test.go` (create if absent, else add to the api test file)

**Interfaces:**
- Consumes: `scheduler.ScheduledTask`, `(*Scheduler).Scheduled()`, `(*Scheduler).RunNow` (Task 2); `store.LastTaskByName`, `store.ListTasks`, `Task.StartedAt`/`EndedAt` (Task 1).
- Produces: `GET /api/v1/system/tasks` → `{scheduled, queue}`; `POST /api/v1/system/tasks/{name}/run` → `202 {taskId}`.

- [ ] **Step 1: Write the failing test**

Add `internal/core/api/system_test.go` (package `api`). Auth matches the existing `api_test.go` harness exactly: `Deps.Auth = auth.NewService(st, "k")` and requests carry `req.Header.Set(auth.APIKeyHeader, "k")` (see `api_test.go:31,53`). `NewRouter(d, spa)` — spa is any `http.Handler` (use `http.NotFoundHandler()`), no mounts needed.

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/scheduler"
	"github.com/hellboundg/nexus/internal/core/store"
)

type fakeTasks struct{ ran string }

func (f *fakeTasks) Scheduled() []scheduler.ScheduledTask {
	return []scheduler.ScheduledTask{{Name: "Job", Interval: 5 * time.Second, NextRun: time.Now().Add(time.Minute)}}
}
func (f *fakeTasks) RunNow(name string) (string, error) {
	if name != "Job" {
		return "", fmt.Errorf("unknown task %q", name)
	}
	f.ran = name
	return "tid", nil
}

func tasksTestRouter(t *testing.T, ft *fakeTasks) (http.Handler, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	h := NewRouter(Deps{
		Auth: auth.NewService(st, "k"), Store: st, Version: "v", Bus: events.New(), Tasks: ft,
	}, http.NotFoundHandler())
	return h, st
}

func keyed(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set(auth.APIKeyHeader, "k")
	return req
}

func TestGetSystemTasks(t *testing.T) {
	ft := &fakeTasks{}
	h, st := tasksTestRouter(t, ft)
	ctx := context.Background()
	// a completed run of "Job" so lastExecution is populated
	_ = st.UpsertTask(ctx, store.Task{ID: "j1", Name: "Job", Status: "running"})
	_ = st.UpsertTask(ctx, store.Task{ID: "j1", Name: "Job", Status: "completed", Progress: 100})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, keyed(http.MethodGet, "/api/v1/system/tasks"))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Scheduled []map[string]json.RawMessage `json:"scheduled"`
		Queue     []map[string]json.RawMessage `json:"queue"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Scheduled) != 1 {
		t.Fatalf("want 1 scheduled, got %d", len(body.Scheduled))
	}
	// a never-run scheduled task would have null lastExecution; this one ran, so non-null
	if string(body.Scheduled[0]["lastExecution"]) == "null" {
		t.Fatal("lastExecution should be populated for a run task")
	}
	if len(body.Queue) != 1 {
		t.Fatalf("want 1 queue row, got %d", len(body.Queue))
	}
}

func TestRunSystemTask(t *testing.T) {
	ft := &fakeTasks{}
	h, _ := tasksTestRouter(t, ft)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, keyed(http.MethodPost, "/api/v1/system/tasks/Job/run"))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if ft.ran != "Job" {
		t.Fatal("RunNow was not called for Job")
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, keyed(http.MethodPost, "/api/v1/system/tasks/Nope/run"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown task want 404, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/api/ -run 'TestGetSystemTasks|TestRunSystemTask' -v`
Expected: FAIL — `Deps.Tasks` field missing / routes 404.

- [ ] **Step 3: Write minimal implementation**

In `internal/core/api/api.go`, add the interface + `Deps` field, and register the routes:

```go
// TaskScheduler is the scheduler surface the tasks endpoints need.
type TaskScheduler interface {
	Scheduled() []scheduler.ScheduledTask
	RunNow(name string) (string, error)
}
```

Add `Tasks TaskScheduler` to `Deps` and import `"github.com/hellboundg/nexus/internal/core/scheduler"`. In the authed group (beside `r.Get("/system/status", ...)`):

```go
			r.Get("/system/tasks", s.handleTasks)
			r.Post("/system/tasks/{name}/run", s.handleRunTask)
```

In `internal/core/api/system.go`, add (imports: `time`, `github.com/go-chi/chi/v5`, plus existing):

```go
type scheduledDTO struct {
	Name                string     `json:"name"`
	IntervalSeconds     int        `json:"intervalSeconds"`
	LastExecution       *time.Time `json:"lastExecution"`
	LastDurationSeconds *int       `json:"lastDurationSeconds"`
	NextExecution       time.Time  `json:"nextExecution"`
}

type queueDTO struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	QueuedAt        time.Time  `json:"queuedAt"`
	StartedAt       *time.Time `json:"startedAt"`
	EndedAt         *time.Time `json:"endedAt"`
	DurationSeconds *int       `json:"durationSeconds"`
}

type tasksResponse struct {
	Scheduled []scheduledDTO `json:"scheduled"`
	Queue     []queueDTO     `json:"queue"`
}

func durationSeconds(start, end *time.Time) *int {
	if start == nil || end == nil {
		return nil
	}
	d := int(end.Sub(*start).Seconds())
	if d < 0 {
		d = 0
	}
	return &d
}

func (s *server) handleTasks(w http.ResponseWriter, r *http.Request) {
	sched := s.deps.Tasks.Scheduled()
	out := tasksResponse{Scheduled: make([]scheduledDTO, 0, len(sched))}
	for _, t := range sched {
		dto := scheduledDTO{
			Name:            t.Name,
			IntervalSeconds: int(t.Interval.Seconds()),
			NextExecution:   t.NextRun,
		}
		last, err := s.deps.Store.LastTaskByName(r.Context(), t.Name)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		if last != nil {
			when := last.CreatedAt
			if last.EndedAt != nil {
				when = *last.EndedAt
			}
			dto.LastExecution = &when
			dto.LastDurationSeconds = durationSeconds(last.StartedAt, last.EndedAt)
		}
		out.Scheduled = append(out.Scheduled, dto)
	}

	rows, err := s.deps.Store.ListTasks(r.Context(), 50)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	out.Queue = make([]queueDTO, 0, len(rows))
	for _, t := range rows {
		out.Queue = append(out.Queue, queueDTO{
			ID: t.ID, Name: t.Name, Status: t.Status,
			QueuedAt: t.CreatedAt, StartedAt: t.StartedAt, EndedAt: t.EndedAt,
			DurationSeconds: durationSeconds(t.StartedAt, t.EndedAt),
		})
	}
	WriteJSON(w, http.StatusOK, out)
}

func (s *server) handleRunTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id, err := s.deps.Tasks.RunNow(name)
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "no such scheduled task")
		return
	}
	WriteJSON(w, http.StatusAccepted, map[string]string{"taskId": id})
}
```

In `cmd/nexus/main.go`, pass the scheduler into `Deps` and forward the event. Change the `api.NewRouter(api.Deps{…})` literal to include `Tasks: sch,` and append `"task.updated"` to the `WSForward` slice:

```go
		WSForward: []string{"indexer.status", "download.status", "media.series.updated", "media.movie.updated", "import.completed", "queue.updated", "automation.search.completed", "automation.rss.completed", "automation.upgrade.completed", "download.failed", "task.updated"},
```
and add `Tasks: sch,` to the `api.Deps{…}` fields.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/api/ -run 'TestGetSystemTasks|TestRunSystemTask' -v`
Expected: PASS.

- [ ] **Step 5: Full check + commit**

Run: `CGO_ENABLED=0 go build ./... && go vet ./internal/core/api/ ./cmd/nexus/ && go test ./internal/core/api/`
Expected: all pass.

```bash
git add internal/core/api/api.go internal/core/api/system.go internal/core/api/system_test.go cmd/nexus/main.go
git commit -m "feat(api): GET /system/tasks + POST /system/tasks/{name}/run"
```

---

### Task 4: Frontend — System tabbed layout + Status tab + Dashboard cleanup

**Files:**
- Create: `web/src/features/system/SystemLayout.tsx`, `web/src/features/system/StatusSection.tsx` (+ `StatusSection.test.tsx`)
- Modify: `web/src/app/routes.tsx` (nest `/system`), `web/src/features/settings/GeneralSection.tsx` (remove the System Info block), `web/src/features/settings/GeneralSection.test.tsx` (drop any System-Info assertion), `web/src/pages/Dashboard.tsx` (remove the Activity stream), `web/src/pages/Dashboard.test.tsx` (drop any stream assertion)
- Test: `StatusSection.test.tsx`, updated `Dashboard.test.tsx`

**Interfaces:**
- Consumes: `useSystemStatus` (`features/settings/configApi`). Produces: `SystemLayout` (Status/Tasks tabs), `StatusSection`. Task 5 fills the Tasks route.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/system/StatusSection.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import { StatusSection } from "@/features/system/StatusSection"
import * as cfg from "@/features/settings/configApi"

vi.mock("@/features/settings/configApi", async (orig) => ({
  ...(await orig<typeof import("@/features/settings/configApi")>()),
  useSystemStatus: vi.fn(),
}))

describe("StatusSection", () => {
  it("renders system info", () => {
    vi.mocked(cfg.useSystemStatus).mockReturnValue({
      data: { version: "1.2.3", appName: "Nexus", healthy: true, taskCount: 4 },
      isLoading: false,
    } as unknown as ReturnType<typeof cfg.useSystemStatus>)
    render(<StatusSection />)
    expect(screen.getByText("1.2.3")).toBeInTheDocument()
    expect(screen.getByText("4")).toBeInTheDocument()
  })
})
```

Update `web/src/pages/Dashboard.test.tsx`: remove any assertion that the "Activity" stream renders; keep/─add an assertion that a stat card (e.g. text "Active Tasks") still renders. (Inspect the current test and adjust — the stream is being removed.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/system/StatusSection.test.tsx`
Expected: FAIL — module `StatusSection` not found.

- [ ] **Step 3: Write minimal implementation**

Create `web/src/features/system/StatusSection.tsx` (moves the System Info block out of GeneralSection):

```tsx
import { useSystemStatus } from "@/features/settings/configApi"

export function StatusSection() {
  const statusQ = useSystemStatus()
  const s = statusQ.data
  return (
    <div className="p-6">
      <section className="rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] p-4">
        <h3 className="mb-2 text-sm font-medium">System Info</h3>
        {statusQ.isLoading || !s ? (
          <p className="text-sm text-[var(--color-muted)]">Loading…</p>
        ) : (
          <dl className="grid max-w-md grid-cols-2 gap-x-4 gap-y-1 text-sm">
            <dt className="text-[var(--color-muted)]">Version</dt><dd>{s.version}</dd>
            <dt className="text-[var(--color-muted)]">App</dt><dd>{s.appName}</dd>
            <dt className="text-[var(--color-muted)]">Healthy</dt><dd>{s.healthy ? "Yes" : "No"}</dd>
            <dt className="text-[var(--color-muted)]">Active tasks</dt><dd>{s.taskCount}</dd>
          </dl>
        )}
      </section>
    </div>
  )
}
```

Create `web/src/features/system/SystemLayout.tsx` (mirrors `SettingsLayout`):

```tsx
import { NavLink, Outlet } from "react-router-dom"
import { cn } from "@/lib/utils"

const TABS = [
  { to: "/system/status", label: "Status" },
  { to: "/system/tasks", label: "Tasks" },
]

export function SystemLayout() {
  return (
    <div>
      <div className="border-b border-[var(--color-border)] px-6 pt-6">
        <h1 className="mb-3 text-2xl font-bold">System</h1>
        <nav className="flex gap-1">
          {TABS.map((t) => (
            <NavLink
              key={t.to}
              to={t.to}
              className={({ isActive }) =>
                cn(
                  "rounded-t-md px-4 py-2 text-sm text-[var(--color-muted)]",
                  isActive && "bg-[rgba(124,92,255,0.16)] font-semibold text-[var(--color-fg)]",
                )
              }
            >
              {t.label}
            </NavLink>
          ))}
        </nav>
      </div>
      <Outlet />
    </div>
  )
}
```

In `web/src/app/routes.tsx`, replace `{ path: "system", element: <Placeholder title="System" /> }` with a nested route (import `SystemLayout`, `StatusSection`, and `Placeholder` stays imported for the temporary Tasks child):

```tsx
      {
        path: "system",
        element: <SystemLayout />,
        children: [
          { index: true, element: <Navigate to="/system/status" replace /> },
          { path: "status", element: <StatusSection /> },
          { path: "tasks", element: <Placeholder title="Tasks" /> },
        ],
      },
```

In `web/src/features/settings/GeneralSection.tsx`, remove the **System Info** `<section>` (lines ~57-69) and the now-unused `useSystemStatus` import + `statusQ`/`s`. Keep the Task Scheduling section. Update `GeneralSection.test.tsx` if it referenced System Info.

In `web/src/pages/Dashboard.tsx`, remove the Activity stream: delete the `useActivity()` call, the `events` panel `<div className="overflow-hidden …">…</div>`, the `describe()` helper, and the now-unused imports (`useActivity`, `relativeTime`). Keep the stat-cards grid.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: tsc clean; full suite green (StatusSection passes; Dashboard/GeneralSection updated tests pass).

- [ ] **Step 5: Commit**

```bash
git add web/src/features/system/ web/src/app/routes.tsx web/src/features/settings/GeneralSection.tsx web/src/features/settings/GeneralSection.test.tsx web/src/pages/Dashboard.tsx web/src/pages/Dashboard.test.tsx
git commit -m "feat(webui): System tabbed layout + Status tab; drop Dashboard stream"
```

---

### Task 5: Frontend — Tasks tab (Scheduled + Queue tables, live, Run-now)

**Files:**
- Create: `web/src/features/system/systemApi.ts`, `web/src/features/system/format.ts`, `web/src/features/system/TasksSection.tsx` (+ `TasksSection.test.tsx`)
- Modify: `web/src/app/routes.tsx` (point `/system/tasks` at `TasksSection`)
- Modify: `web/dist/**` (rebuild)

**Interfaces:**
- Consumes: `GET /system/tasks`, `POST /system/tasks/{name}/run` (Task 3); `useActivity` (`lib/activity`) for live refresh; `useToast`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/system/TasksSection.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { TasksSection } from "@/features/system/TasksSection"
import * as api from "@/features/system/systemApi"

vi.mock("@/features/system/systemApi", async (orig) => ({
  ...(await orig<typeof import("@/features/system/systemApi")>()),
  useTasks: vi.fn(),
  useRunTask: vi.fn(),
}))
vi.mock("@/lib/activity", () => ({ useActivity: () => [] }))

function stub(run = vi.fn()) {
  vi.mocked(api.useTasks).mockReturnValue({
    data: {
      scheduled: [
        { name: "ImportCompletedDownloads", intervalSeconds: 5, lastExecution: "2026-07-18T19:00:00Z", lastDurationSeconds: 0, nextExecution: "2999-01-01T00:00:00Z" },
      ],
      queue: [
        { id: "q1", name: "DownloadQueueMonitor", status: "completed", queuedAt: "2026-07-18T19:00:00Z", startedAt: "2026-07-18T19:00:00Z", endedAt: "2026-07-18T19:00:01Z", durationSeconds: 1 },
        { id: "q2", name: "ImportCompletedDownloads", status: "running", queuedAt: "2026-07-18T19:00:02Z", startedAt: "2026-07-18T19:00:02Z", endedAt: null, durationSeconds: null },
      ],
    },
    isLoading: false,
  } as unknown as ReturnType<typeof api.useTasks>)
  vi.mocked(api.useRunTask).mockReturnValue({ mutate: run, isPending: false } as unknown as ReturnType<typeof api.useRunTask>)
  return run
}

function renderTasks() {
  render(<ToastProvider><TasksSection /></ToastProvider>)
}

describe("TasksSection", () => {
  it("renders humanized scheduled + queue rows", () => {
    stub()
    renderTasks()
    expect(screen.getByText("Import Completed Downloads")).toBeInTheDocument()
    expect(screen.getByText("Download Queue Monitor")).toBeInTheDocument()
    expect(screen.getByText(/running/i)).toBeInTheDocument() // the running queue row
  })

  it("runs a scheduled task", async () => {
    const run = stub()
    renderTasks()
    await userEvent.click(screen.getByRole("button", { name: /run import completed downloads/i }))
    expect(run).toHaveBeenCalledWith("ImportCompletedDownloads")
  })
})
```

Create `web/src/features/system/format.test.ts`:

```ts
import { describe, it, expect } from "vitest"
import { formatDuration, humanizeInterval, humanizeName } from "@/features/system/format"

describe("format", () => {
  it("formats duration HH:MM:SS", () => expect(formatDuration(65)).toBe("00:01:05"))
  it("humanizes interval", () => {
    expect(humanizeInterval(5)).toBe("5 seconds")
    expect(humanizeInterval(900)).toBe("15 minutes")
    expect(humanizeInterval(43200)).toBe("12 hours")
  })
  it("humanizes camelCase names", () => {
    expect(humanizeName("ImportCompletedDownloads")).toBe("Import Completed Downloads")
    expect(humanizeName("RSSSync")).toBe("RSS Sync")
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/system/TasksSection.test.tsx src/features/system/format.test.ts`
Expected: FAIL — modules not found.

- [ ] **Step 3: Write minimal implementation**

Create `web/src/features/system/format.ts`:

```ts
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  return [h, m, sec].map((n) => String(n).padStart(2, "0")).join(":")
}

export function humanizeInterval(seconds: number): string {
  const plural = (n: number, u: string) => `${n} ${u}${n === 1 ? "" : "s"}`
  if (seconds % 86400 === 0) return plural(seconds / 86400, "day")
  if (seconds % 3600 === 0) return plural(seconds / 3600, "hour")
  if (seconds % 60 === 0) return plural(seconds / 60, "minute")
  return plural(seconds, "second")
}

export function humanizeName(name: string): string {
  return name
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
}

export function relativePast(iso: string, now = Date.now()): string {
  const s = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000))
  if (s < 60) return "a few seconds ago"
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} minute${m === 1 ? "" : "s"} ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} hour${h === 1 ? "" : "s"} ago`
  const d = Math.floor(h / 24)
  return `${d} day${d === 1 ? "" : "s"} ago`
}

export function relativeFuture(iso: string, now = Date.now()): string {
  const diff = new Date(iso).getTime() - now
  if (diff <= 0) return "now"
  const s = Math.floor(diff / 1000)
  if (s < 60) return `in ${s} second${s === 1 ? "" : "s"}`
  const m = Math.floor(s / 60)
  if (m < 60) return `in ${m} minute${m === 1 ? "" : "s"}`
  const h = Math.floor(m / 60)
  if (h < 24) return `in ${h} hour${h === 1 ? "" : "s"}`
  const d = Math.floor(h / 24)
  return `in ${d} day${d === 1 ? "" : "s"}`
}
```

Create `web/src/features/system/systemApi.ts`:

```ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost } from "@/lib/api"

export type ScheduledTask = {
  name: string
  intervalSeconds: number
  lastExecution: string | null
  lastDurationSeconds: number | null
  nextExecution: string
}
export type QueueTask = {
  id: string
  name: string
  status: string
  queuedAt: string
  startedAt: string | null
  endedAt: string | null
  durationSeconds: number | null
}
export type TasksData = { scheduled: ScheduledTask[]; queue: QueueTask[] }

export const systemKeys = { tasks: ["system", "tasks"] as const }

export function useTasks() {
  return useQuery({ queryKey: systemKeys.tasks, queryFn: () => apiGet<TasksData>("/system/tasks") })
}

export function useRunTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => apiPost<{ taskId: string }>(`/system/tasks/${encodeURIComponent(name)}/run`),
    onSuccess: () => qc.invalidateQueries({ queryKey: systemKeys.tasks }),
  })
}
```

Create `web/src/features/system/TasksSection.tsx`:

```tsx
import { useEffect } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useActivity } from "@/lib/activity"
import { useToast } from "@/lib/toast"
import { useTasks, useRunTask, systemKeys, type QueueTask } from "./systemApi"
import { formatDuration, humanizeInterval, humanizeName, relativePast, relativeFuture } from "./format"

export function TasksSection() {
  const q = useTasks()
  const run = useRunTask()
  const { toast } = useToast()
  const qc = useQueryClient()
  const events = useActivity()

  // Live: refetch when a task.updated event arrives.
  useEffect(() => {
    if (events.some((e) => e.type === "task.updated")) {
      qc.invalidateQueries({ queryKey: systemKeys.tasks })
    }
  }, [events, qc])

  if (q.isLoading || !q.data) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading…</div>
  const { scheduled, queue } = q.data

  return (
    <div className="space-y-6 p-6">
      <section>
        <h2 className="mb-2 text-sm font-semibold">Scheduled</h2>
        <div className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-panel)]">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide text-[var(--color-muted)]">
                  <th className="px-4 py-2.5 font-semibold">Name</th>
                  <th className="px-4 py-2.5 font-semibold">Interval</th>
                  <th className="px-4 py-2.5 font-semibold">Last Execution</th>
                  <th className="px-4 py-2.5 font-semibold">Last Duration</th>
                  <th className="px-4 py-2.5 font-semibold">Next Execution</th>
                  <th className="px-4 py-2.5 text-right font-semibold">Run</th>
                </tr>
              </thead>
              <tbody>
                {scheduled.map((t) => {
                  const next = relativeFuture(t.nextExecution)
                  return (
                    <tr key={t.name} className="border-t border-[var(--color-border)]">
                      <td className="px-4 py-2.5">{humanizeName(t.name)}</td>
                      <td className="px-4 py-2.5 text-[var(--color-muted)]">{humanizeInterval(t.intervalSeconds)}</td>
                      <td className="px-4 py-2.5 text-[var(--color-muted)]">{t.lastExecution ? relativePast(t.lastExecution) : "—"}</td>
                      <td className="px-4 py-2.5 tabular-nums text-[var(--color-muted)]">{t.lastDurationSeconds != null ? formatDuration(t.lastDurationSeconds) : "—"}</td>
                      <td className={`px-4 py-2.5 tabular-nums ${next === "now" ? "text-[var(--color-brand)]" : "text-[var(--color-muted)]"}`}>{next}</td>
                      <td className="px-4 py-2.5 text-right">
                        <button
                          aria-label={`Run ${humanizeName(t.name)} now`}
                          title="Run now"
                          onClick={() => run.mutate(t.name, { onSuccess: () => toast(`Started ${humanizeName(t.name)}`) })}
                          className="rounded-md border border-transparent px-2 py-1 text-[var(--color-muted)] hover:border-[var(--color-border)] hover:text-[var(--color-brand)]"
                        >
                          ↻
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <section>
        <div className="mb-2 flex items-center gap-2">
          <h2 className="text-sm font-semibold">Queue</h2>
          <span className="text-xs font-semibold text-[var(--color-ok)]">LIVE</span>
        </div>
        <div className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-panel)]">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide text-[var(--color-muted)]">
                  <th className="px-4 py-2.5 font-semibold">Name</th>
                  <th className="px-4 py-2.5 font-semibold">Queued</th>
                  <th className="px-4 py-2.5 font-semibold">Started</th>
                  <th className="px-4 py-2.5 font-semibold">Ended</th>
                  <th className="px-4 py-2.5 text-right font-semibold">Duration</th>
                </tr>
              </thead>
              <tbody>
                {queue.length === 0 ? (
                  <tr><td colSpan={5} className="px-4 py-8 text-center text-[var(--color-muted)]">No recent tasks.</td></tr>
                ) : queue.map((t) => <QueueRow key={t.id} t={t} />)}
              </tbody>
            </table>
          </div>
        </div>
      </section>
    </div>
  )
}

function QueueRow({ t }: { t: QueueTask }) {
  const running = t.status === "running"
  const failed = t.status === "failed"
  const glyph = running ? "◐" : failed ? "✕" : "✓"
  const glyphColor = running ? "text-[var(--color-brand)]" : failed ? "text-[var(--color-warn)]" : "text-[var(--color-ok)]"
  return (
    <tr className="border-t border-[var(--color-border)]">
      <td className="px-4 py-2.5">
        <span className="flex items-center gap-2">
          <span className={glyphColor} aria-label={t.status}>{glyph}</span>
          {humanizeName(t.name)}
        </span>
      </td>
      <td className="px-4 py-2.5 text-[var(--color-muted)]">{relativePast(t.queuedAt)}</td>
      <td className="px-4 py-2.5 text-[var(--color-muted)]">{t.startedAt ? relativePast(t.startedAt) : "—"}</td>
      <td className="px-4 py-2.5 text-[var(--color-muted)]">{t.endedAt ? relativePast(t.endedAt) : "—"}</td>
      <td className="px-4 py-2.5 text-right tabular-nums">
        {running ? <span className="text-[var(--color-brand)]">Running…</span>
          : <span className={failed ? "text-[var(--color-warn)]" : "text-[var(--color-muted)]"}>{t.durationSeconds != null ? formatDuration(t.durationSeconds) : "—"}</span>}
      </td>
    </tr>
  )
}
```

In `web/src/app/routes.tsx`, point the tasks child at `TasksSection` (import it; drop the temporary `Placeholder` for tasks — keep the `Placeholder` import only if still used elsewhere):

```tsx
          { path: "tasks", element: <TasksSection /> },
```

> Implementer note: confirm `useActivity` returns events with a `type` field (see `Dashboard`'s old `describe(e.data)` usage and `lib/ws` `ActivityEvent`). If the field differs, filter on the correct property.

- [ ] **Step 4: Run tests + typecheck**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: tsc clean; full suite green.

- [ ] **Step 5: Rebuild the bundle + confirm no stray drift**

Run: `cd web && npm run build && git status --short web/dist`
Expected: build succeeds; only expected `web/dist` asset changes.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/system/ web/src/app/routes.tsx web/dist
git commit -m "feat(webui): System Tasks tab — Scheduled + Queue tables, live, Run-now"
```

---

## Self-Review

**Spec coverage:**
- §3.1 scheduler `Scheduled()` + `RunNow` → T2. ✓
- §3.2 `started_at`/`ended_at` migration + Manager-free SQL derivation → T1. ✓
- §3.3 `LastTaskByName` → T1. ✓
- §3.4 `GET /system/tasks` (scheduled+queue, seconds, null-when-unset) + `POST …/run` (202/404) on core API, `TaskScheduler` in Deps → T3. ✓
- §3.5 add `task.updated` to WSForward + live Queue → T3 (WSForward) + T5 (useActivity invalidate). ✓
- §4.1 SystemLayout tabs → T4. ✓
- §4.2 Status tab (System Info moved from GeneralSection) → T4. ✓
- §4.3 Tasks tab (Scheduled + Queue tables, humanized, run-now, status glyphs) → T5. ✓
- §4.4 Dashboard drops the stream, keeps stat cards → T4. ✓
- §6 tests: store timestamps/LastTaskByName (T1); scheduler snapshot/run-now (T2); GET/POST tasks + null last-fields via RawMessage (T3); StatusSection + Dashboard (T4); TasksSection + format helpers (T5); dist rebuild (T5). ✓

**Placeholder scan:** No TBDs. Test harnesses verified against source: T1 uses `newTestStore(t)` (store_test.go:11); T3 uses the `auth.APIKeyHeader` + `auth.NewService(st,"k")` pattern (api_test.go:31,53) with a concrete `keyed()` helper. One remaining implementer verification (cites the exact source to match, not invent): T5's `useActivity` event `type` field — confirm against `lib/ws` `ActivityEvent` before filtering.

**Type consistency:** `store.Task.StartedAt/EndedAt *time.Time` (T1) consumed by T3's `queueDTO`/`durationSeconds`. `scheduler.ScheduledTask{Name,Interval,NextRun}` (T2) consumed by T3's `TaskScheduler` interface + `handleTasks`. `TasksData`/`ScheduledTask`/`QueueTask` FE types (T5) mirror T3's `scheduledDTO`/`queueDTO` field names (`intervalSeconds`, `lastExecution`, `lastDurationSeconds`, `nextExecution`; `queuedAt`, `startedAt`, `endedAt`, `durationSeconds`). `useRunTask` mutate arg is the raw command `name` (T5), matching `POST /system/tasks/{name}/run` (T3) and `RunNow(name)` (T2).
