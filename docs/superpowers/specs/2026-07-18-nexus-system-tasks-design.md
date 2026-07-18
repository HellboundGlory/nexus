# Nexus — System › Tasks tab

Date: 2026-07-18
Status: Approved (design)

## 1. Goal

The Dashboard today shows an endless LIVE "Activity" stream, and the `/system`
route is an empty placeholder. This feature builds out `/system` into a tabbed
**Status + Tasks** page — a Radarr/Sonarr-style Tasks view with a **Scheduled**
table (each recurring task, its cadence, and a per-task Run-now trigger) and a
**Queue** table (recent command runs) — and removes the endless stream from the
Dashboard.

Approved against a mockup that matches the Radarr/Sonarr Tasks layout, styled in
Nexus's own theme and populated with Nexus's real scheduled commands. Covers
request #7. This is the last item in the post-C3 web-UI batch (SP1–SP4).

## 2. Scope

**In scope:**
- Build `/system` into a tabbed layout (like Settings/Activity): **Status** and
  **Tasks** tabs.
- **Status tab:** move the read-only System Info (version / health / task count)
  here from Settings › General.
- **Tasks tab:** a **Scheduled** table (Name · Interval · Last Execution · Last
  Duration · Next Execution · Run-now) and a **Queue** table (Name · Queued ·
  Started · Ended · Duration), the Queue updating live.
- A per-scheduled-task **Run now** trigger (`POST /system/tasks/{name}/run`).
- Backend: expose the scheduler's registered entries (name, interval, next run);
  add `started_at` / `ended_at` to task rows so the Queue can show
  Queued/Started/Ended/Duration; a `LastTaskByName` query.
- **Dashboard:** remove the LIVE Activity stream; keep the three stat cards.
- Forward the `task.updated` event over the WebSocket (it isn't today) so the
  Queue is live.

**Out of scope (explicit non-goals):**
- **Pruning the `tasks` table** and **fixing the "Active Tasks" stat to a real
  count.** Deferred by the user's steer toward the fixed two-table model. The
  Queue reads a bounded recent list (LIMIT), so nothing here depends on them.
  Recorded in §8 as a future follow-up.
- **Other System tabs** (Backup, Updates, Events, Log Files from the reference).
  Only Status + Tasks. The tabbed layout leaves room to add them later.
- **Editing schedules / cadences** from the UI. Intervals stay code/config-driven.
- **Per-run detail drill-down, cancel, or history export.** View + Run-now only.

## 3. Backend design

### 3.1 Scheduler exposes its entries

`scheduler.Scheduler` (`internal/core/scheduler/scheduler.go`) is anonymous today
— `Every(interval, factory)` stores only the interval and factory. It gains:

- A **name** per entry, captured once at registration via `factory().Name()`
  (command `Name()` is cheap and pure).
- A tracked **next-run** time per entry, set when `Start()` arms the ticker and
  advanced on each tick. Guarded by a mutex (read by the API, written by the
  ticker goroutines).

New API on the scheduler:

```go
type ScheduledTask struct {
    Name     string
    Interval time.Duration
    NextRun  time.Time
}

func (s *Scheduler) Scheduled() []ScheduledTask         // snapshot, for the GET
func (s *Scheduler) RunNow(name string) (string, error) // enqueue factory(), return task id
```

`RunNow` finds the entry whose name matches, enqueues `factory()` via the
`command.Manager`, and returns the new task id (or an error if the name is
unknown). Scheduled-task names are unique (each is a distinct command), so name
is a valid identifier.

### 3.2 Task timestamps for the Queue columns

The Queue's **Queued / Started / Ended / Duration** columns need more than
today's `created_at` / `updated_at`. Add two nullable columns to the `tasks`
table via a migration:

- `started_at` — set when the task transitions to `running`.
- `ended_at` — set when it transitions to a terminal state (`completed` /
  `failed`).

`store.Task` gains `StartedAt *time.Time` and `EndedAt *time.Time`. The
`command.Manager` (`internal/core/command/command.go`) stamps them at the
existing transition points (`run` sets started on the running update; the
terminal update sets ended). `UpsertTask` is extended to persist them.

- **Queued** = `CreatedAt` (already captured on Enqueue).
- **Started** = `StartedAt`.
- **Ended** = `EndedAt`.
- **Duration** = `EndedAt − StartedAt` (blank / "Running…" while `EndedAt` is
  null and status is `running`).

### 3.3 Last execution per scheduled task

New store query:

```go
func (s *Store) LastTaskByName(ctx context.Context, name string) (*Task, error)
```

Returns the most recent task row for a command name (`ORDER BY created_at DESC
LIMIT 1`; `nil, nil` when none). Used to fill the Scheduled table's **Last
Execution** (the row's `EndedAt`, or `CreatedAt` if never finished) and **Last
Duration** (`EndedAt − StartedAt`).

### 3.4 API endpoints

Served by the **core API** (`internal/core/api/`), beside the existing
`GET /system/status` (`api.go:47`, handler in `system.go`). The core router
gains a handle to the scheduler through a small interface on `api.Deps` (the
scheduler is created in `main.go` before the router, so it can be passed in):

```go
type TaskScheduler interface {
    Scheduled() []scheduler.ScheduledTask
    RunNow(name string) (string, error)
}
```

Endpoints:

```
GET  /api/v1/system/tasks            → { scheduled: [...], queue: [...] }
POST /api/v1/system/tasks/{name}/run → 202 { taskId }
```

`GET /system/tasks` response shape:

```json
{
  "scheduled": [
    { "name": "ImportCompletedDownloads", "intervalSeconds": 5,
      "lastExecution": "2026-07-18T19:00:00Z", "lastDurationSeconds": 0,
      "nextExecution": "2026-07-18T19:00:05Z" }
  ],
  "queue": [
    { "id": "…", "name": "DownloadQueueMonitor", "status": "completed",
      "queuedAt": "…", "startedAt": "…", "endedAt": "…", "durationSeconds": 1 }
  ]
}
```

- `scheduled` is built from `Scheduler.Scheduled()` joined with
  `LastTaskByName(name)` for last-execution/last-duration; a never-run task
  returns null last-execution/last-duration (rendered as "—").
- `queue` is `ListTasks(limit)` (existing) mapped with the new timestamps; a
  reasonable fixed `limit` (e.g. 50) keeps it bounded.
- `POST /system/tasks/{name}/run` calls `RunNow(name)` → `202 {taskId}`; unknown
  name → `404`.

Interval and all durations serialise as **integer seconds** (`intervalSeconds`,
`lastDurationSeconds`, `durationSeconds`); the frontend humanizes them ("5
seconds", "15 minutes", "12 hours", `HH:MM:SS`). Timestamps are RFC3339 and the
frontend renders them as relative times. This keeps the wire numeric and
locale-agnostic. (The example above uses `interval: "5s"` illustratively — the
real field is `intervalSeconds: 5`.)

### 3.5 Live updates

`main.go`'s `WSForward` list does **not** include `task.updated` today — add it.
The `command.Manager` already emits `TaskUpdated` on every state change
(`command.go`); forwarding it lets the Queue (and the Scheduled table's
last/next columns) refresh live without polling, reusing the existing WS +
activity plumbing the Dashboard/Activity pages use.

## 4. Frontend design

### 4.1 System page — tabbed layout

`/system` (currently `<Placeholder title="System" />`, `routes.tsx:64`) becomes a
`SystemLayout` with two tabs, mirroring `SettingsLayout` / `ActivityLayout`:

```
/system            → redirect to /system/status
/system/status     → StatusSection   (System Info, moved from Settings › General)
/system/tasks      → TasksSection     (Scheduled + Queue tables)
```

The sidebar `System` entry (already present, `Sidebar.tsx:12`) gains the same
active-tab treatment the other sections have.

### 4.2 Status tab

`StatusSection` renders the read-only System Info currently in `GeneralSection`
(version, health, task count — whatever `/system/status` already returns).
`GeneralSection` under Settings keeps the **automation config** it also holds;
only the System Info block moves. (If that leaves Settings › General as
automation-only, it stays there — renaming Settings tabs is out of scope.)

### 4.3 Tasks tab

`TasksSection` renders two tables inside the app's standard panel styling
(matching the approved mockup):

- **Scheduled:** columns Name · Interval · Last Execution · Last Duration · Next
  Execution, plus a trailing **Run now** (↻) icon button per row. Names are the
  humanized command names (e.g. `ImportCompletedDownloads` → "Import Completed
  Downloads"). Interval and the relative times ("6 minutes ago", "in 9 minutes",
  "now") are formatted client-side (reuse `relativeTime` / `lib/time`); duration
  as `HH:MM:SS`. "Next Execution" shows "now" in the accent color when due.
- **Queue:** columns Name · Queued · Started · Ended · Duration, newest first,
  with a per-row status glyph — green check (completed), amber ✕ (failed), a
  brand spinner + "Running…" (running, no Ended yet). A "LIVE" indicator on the
  section header. Updates via the `task.updated` WS event.

New FE files under `web/src/features/system/`: `systemApi.ts` (typed hooks —
`useTasks`, `useRunTask`), `SystemLayout.tsx`, `StatusSection.tsx`,
`TasksSection.tsx` (+ a small `formatDuration` helper and humanize-name helper),
plus the route + sidebar wiring.

Run-now: clicking ↻ calls `POST /system/tasks/{name}/run`, shows a toast
("Started {name}"), and lets the live event stream surface the new run in the
Queue.

### 4.4 Dashboard

`Dashboard.tsx` drops the LIVE Activity stream (the `useActivity()` list and its
panel) and keeps the three stat cards (Version / Health / Active Tasks) — room to
add to it later. The `Active Tasks` stat card is unchanged (still `/system/status`
`taskCount`; the count-accuracy fix is deferred, §8).

## 5. Error handling

| Case | Behavior |
|------|----------|
| `GET /system/tasks`, a scheduled task never ran | its last-execution / last-duration are null → "—" |
| `POST /system/tasks/{name}/run`, unknown name | `404` |
| `POST …/run`, manager stopped / enqueue fails | `500` (or the manager's error), surfaced as a toast |
| Queue row still running | Ended = "—", Duration = "Running…" (status `running`) |
| Queue row failed | amber ✕ + the recorded duration |
| A conditional schedule (RSS/upgrade) is disabled | it isn't registered, so it simply doesn't appear (matches reality) |

## 6. Testing

**Go**
- `Scheduler.Scheduled()` returns the registered entries with names + intervals +
  a next-run in the future; `Every` captures the name from `factory().Name()`.
- `Scheduler.RunNow(name)` enqueues the matching factory (a task row appears with
  that name) and returns its id; an unknown name errors.
- `command.Manager` stamps `started_at` on running and `ended_at` on
  completed/failed; `UpsertTask` round-trips both (nullable when unset).
- `store.LastTaskByName` returns the most recent row for a name, `nil` when none.
- `GET /system/tasks` returns scheduled (joined with last-execution) + queue with
  the timestamps; a never-run scheduled task has null last fields (assert via
  `json.RawMessage`). `POST /system/tasks/{name}/run` → 202 + taskId; unknown →
  404.

**Frontend**
- `TasksSection`: renders the Scheduled rows (name/interval/last/next) and the
  Queue rows (queued/started/ended/duration) from a mocked `useTasks`; a running
  row shows "Running…", a failed row shows the failed styling; clicking Run-now
  calls `useRunTask` with the task name.
- `StatusSection`: renders the system info from `/system/status`.
- `Dashboard`: no longer renders the activity stream; the stat cards remain.
- `SystemLayout`: the two tabs route to Status / Tasks.
- `web/dist` rebuild committed (CI drift-checks it).

## 7. Source facts (verified 2026-07-18)

- Scheduler: `Every(d, factory)` + `Start()` tickers, anonymous entries
  (`scheduler.go`). Registered schedules in `main.go:151-176`:
  `IndexerHealthCheck` (15m), `DownloadQueueMonitor` (30s), `MediaRefresh` (12h),
  `ImportCompletedDownloads` via `command.Single` (5s), `MissingSearch`
  (config hrs), `RSSSync` (config min, if enabled), `UpgradeSearch` (config hrs,
  if enabled).
- Command names (`Name()`): `IndexerHealthCheck` (indexer/health.go:36),
  `DownloadQueueMonitor` (downloadclient/monitor.go:43), `MediaRefresh`
  (media/refresh.go:17), `ImportCompletedDownloads` (importing/command.go:95),
  `MissingSearch`/`RSSSync`/`UpgradeSearch` (automation/command.go:53/64/73),
  `SingleFlight.Name()` delegates to the wrapped command (command/singleflight.go:21).
- `command.Manager` emits `TaskUpdated{Task}` ("task.updated") on every state
  change; `run` updates running→completed/failed (command.go:112-138).
- `store.Task{ID,Name,Status,Progress,Message,CreatedAt,UpdatedAt}`
  (store.go:29); `UpsertTask` (store.go:128), `ListTasks(limit)` (store.go:155,
  `ORDER BY created_at DESC`).
- `GET /system/status` served by core API `handleStatus` (api.go:47,
  system.go); `SystemStatus{Healthy,TaskCount,Version…}`. Core router built via
  `api.NewRouter(api.Deps{…}, …)` in `main.go:179`; `sch` exists before it →
  can be passed in. `WSForward` (main.go:181) lists forwarded events —
  `task.updated` NOT among them.
- FE: `/system` = `<Placeholder title="System" />` (routes.tsx:64); `System`
  nav item exists (Sidebar.tsx:12); `SettingsLayout`/`ActivityLayout` are the
  tabbed-layout precedents; `GeneralSection` holds System Info + automation
  config (GeneralSection.tsx); `Dashboard` renders stat cards + `useActivity()`
  stream (Dashboard.tsx); `getStatus` / `useSystemStatus` hit `/system/status`;
  `relativeTime` in `lib/time`.

## 8. Deferred

- **Task-table pruning** — the `tasks` table grows unbounded (a row per
  scheduler tick). The Queue's LIMIT keeps the view bounded, but a retention
  prune (keep last N or last X days) is worth a follow-up.
- **Real "Active Tasks" count** — the Dashboard stat is `ListTasks(100)` length
  (capped), not a true count of queued/running. A small `COUNT` fix would make
  it honest.
- Additional System tabs (Backup / Updates / Events / Log Files).
- UI-editable schedule cadences; per-run drill-down / cancel.
