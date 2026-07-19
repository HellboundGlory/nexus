# Nexus — Task-table pruning + honest Active-Tasks count (SP5)

Date: 2026-07-19
Status: Approved (design)

## 1. Goal

SP4 shipped the System › Tasks tab but deliberately deferred two items it
depends on nothing for. This feature closes both:

1. **The `tasks` table grows without bound.** `Store.UpsertTask` only ever
   `INSERT`s (via `command.Manager.Enqueue`/`update`); nothing deletes. With the
   import tick scheduled every 5s, the table gains ~17k rows/day, forever.
2. **The "Active Tasks" stat lies.** `handleStatus` reports
   `TaskCount: len(ListTasks(100))` — capped at 100 **and** counting terminal
   (completed/failed) rows, not active ones. The Dashboard card and Status tab
   both label it "Active Tasks", so it reads as "≥100 in-flight" when the real
   in-flight count is almost always 0–1.

Also aligns the Queue table with Radarr/Sonarr, which show the **last 10** task
runs (the SP4 Queue shows 50).

Pure backend. No frontend source change, no `web/dist` rebuild, no new
migration (both are queries, not schema).

## 2. Scope

**In scope:**
- A scheduled **Housekeeping** command that prunes the `tasks` table, keeping
  the newest **50 terminal rows per task name**, run **hourly**.
- `Store.PruneTasksPerName(ctx, keep int) (int64, error)` — the prune query.
- `Store.CountActiveTasks(ctx) (int, error)` — a real `COUNT(*)` of
  queued+running rows; wired into `handleStatus`.
- Queue table: `handleTasks` reads the last **10** instead of 50.

**Out of scope (explicit non-goals):**
- **Any FE change.** The wire field stays `taskCount`; the "Active Tasks" label
  simply becomes truthful. No component, test, or `web/dist` change.
- **A user-facing retention setting.** The keep-count and cadence are code
  constants (YAGNI — no reason to expose them).
- **A new index or schema.** The table stays small (~600 terminal rows max);
  `DELETE`/`COUNT` over a few hundred rows needs no index. No migration file →
  the applied-migration-count assertion in `database_test.go` is untouched.
- **Age-based retention.** Rejected: with the 5s import tick, an age window that
  outlives the 12h `MediaRefresh` task (needed to keep its Last Execution — see
  §4) is still ~120k rows, reintroducing the growth concern. Per-name bounds the
  table regardless of task frequency.
- **A separate last-execution table** (how Sonarr keeps Last Execution safe from
  its age-based Housekeeping). Per-name retention preserves Last Execution from
  the single `tasks` table instead — see §4.

## 3. Backend design

### 3.1 Pruning — `PruneTasksPerName`

New `Store` method (in `internal/core/store/store.go`, beside the other `tasks`
methods):

```go
// PruneTasksPerName deletes terminal (completed/failed) task rows beyond the
// newest `keep` per task name. Queued/running rows are never deleted. Returns
// the number of rows removed.
func (s *Store) PruneTasksPerName(ctx context.Context, keep int) (int64, error)
```

Query:

```sql
DELETE FROM tasks
WHERE status IN ('completed','failed')
  AND id NOT IN (
    SELECT id FROM (
      SELECT id, ROW_NUMBER() OVER (
        PARTITION BY name ORDER BY created_at DESC, rowid DESC) AS rn
      FROM tasks
      WHERE status IN ('completed','failed'))
    WHERE rn <= ?)
```

- `WHERE status IN ('completed','failed')` on the outer delete guarantees
  queued/running rows are untouched (belt-and-braces with the subquery filter).
- `PARTITION BY name` keeps the newest `keep` **per name**, so every task —
  including infrequent ones like the 12h `MediaRefresh` — always retains its
  most recent terminal row. This is what protects the Scheduled table's Last
  Execution / Last Duration columns (§4).
- `ORDER BY created_at DESC, rowid DESC` matches the existing `LastTaskByName`
  determinism (created_at is second-granular; rowid breaks ties).
- Fewer than `keep` terminal rows for a name → that name's rows are all inside
  the window → none deleted.
- Window functions are available: `modernc.org/sqlite v1.53.0` embeds
  SQLite ≥ 3.25.
- Returns `sql.Result.RowsAffected()`.

### 3.2 Housekeeping command

A core maintenance command in `internal/core/command` (which already imports
`store`), following the same shape as the domain command constructors:

```go
// NewPruneTasks returns the scheduled Housekeeping command: it prunes the tasks
// table to the newest `keep` terminal rows per task name.
// Declared in package command, so the return type is the unqualified Command.
func NewPruneTasks(s *store.Store, keep int) Command
```

- `Name() → "Housekeeping"` — appears in the Scheduled table like Sonarr's.
- `Run(ctx, r Reporter)` calls `s.PruneTasksPerName(ctx, keep)`, then
  `r.Progress(100, fmt.Sprintf("%d pruned", n))`; returns the store error if any.
- **Self-delete safety:** during its own `Run` the Housekeeping row is `running`,
  so the prune it performs cannot delete it. After completion its row is
  `completed` and subject to the next hourly run, always inside
  newest-50-for-Housekeeping.
- **Net self-cost:** one task id per run (queued→running→completed is an upsert
  on the same id), always within the retained 50. Negligible.

### 3.3 Wiring (`cmd/nexus/main.go`)

Add beside the other `sch.Every` registrations:

```go
const taskRetention = 50 // newest terminal rows kept per task name

sch.Every(time.Hour, func() command.Command {
    return command.NewPruneTasks(st, taskRetention)
})
```

Hourly (not Sonarr's daily) because the 5s import tick writes far more often
than any Sonarr task; hourly holds the between-prune peak to ~1,100 rows instead
of ~17k/day. `Housekeeping` is then one of the entries `scheduler.Scheduled()`
returns, so it renders in the Scheduled table automatically — no API change.

### 3.4 Honest count — `CountActiveTasks`

New `Store` method:

```go
// CountActiveTasks returns the number of queued or running task rows.
func (s *Store) CountActiveTasks(ctx context.Context) (int, error)
```

```sql
SELECT COUNT(*) FROM tasks WHERE status IN ('queued','running')
```

`handleStatus` (`internal/core/api/system.go`) swaps:

```go
tasks, err := s.deps.Store.ListTasks(r.Context(), 100)
// ...
TaskCount: len(tasks),
```

for:

```go
count, err := s.deps.Store.CountActiveTasks(r.Context())
// ...
TaskCount: count,
```

`Deps.Store` is the concrete `*store.Store`, so no interface change. The
`statusResponse.TaskCount` field and JSON key are unchanged.

### 3.5 Queue shows the last 10

In `handleTasks`, change the Queue read from `ListTasks(r.Context(), 50)` to
`ListTasks(r.Context(), 10)`. Matches Radarr/Sonarr. No other change — the DTO,
ordering (created_at DESC), and live `task.updated` invalidation all stay.

## 4. Why per-name retention (the load-bearing decision)

The Scheduled table's **Last Execution** and **Last Duration** columns come from
`LastTaskByName(name)`, which reads the *same* `tasks` table pruning deletes
from. A global "keep newest N rows" would break this: the 5s import tick means
N=500 rows ≈ 40 minutes of history, so the 12h `MediaRefresh` task's last row
would be deleted long before its next run, leaving its Last Execution
permanently "—".

Per-name retention (`PARTITION BY name`) keeps each task's newest rows
independently, so every task's last terminal row survives regardless of how
often other tasks run. This is the single-table equivalent of what Sonarr gets
from a separate ScheduledTasks table.

The bound holds because **every** command `Name()` that reaches the `tasks`
table is a static string from a closed set — verified against source:

- Scheduled: `IndexerHealthCheck`, `DownloadQueueMonitor`, `MediaRefresh`,
  `ImportCompletedDownloads`, `MissingSearch`, `RSSSync`, `UpgradeSearch`,
  `Housekeeping`.
- On-demand (via the automation API dispatch): `SearchMovie`, `SearchSeries`,
  `SearchSeason`, `SearchEpisode` — all static (`command.go` sets a literal
  `name`, no id/title embedded).

~12 names × 50 ≈ 600 terminal rows worst case. Had any name embedded an id
(e.g. `"Search: The Matrix"`), per-name would instead retain up to 50 rows per
distinct value **and** keep any one-off name's single row forever — the exact
opposite of pruning. It does not, so per-name is correct. If a future feature
introduces a dynamic command name, this invariant must be revisited (a global
cap over non-scheduled names would then be the fix).

## 5. Testing

- **`PruneTasksPerName` (store test):** seed two names — a frequent one with
  >50 terminal rows and an infrequent one with a single old terminal row — plus
  a queued and a running row. Assert: the frequent name is capped at 50, the
  infrequent name's lone row **survives**, queued/running rows are untouched,
  and the returned count equals the rows deleted. Add a below-threshold case
  (all names < keep → deletes nothing, returns 0). The infrequent-survives
  assertion is the whole reason for the more complex per-name query — without it
  the test doesn't exercise the design.
- **`CountActiveTasks` (store test):** seed a mix of queued/running/completed/
  failed → assert only queued+running are counted.
- **`NewPruneTasks` (command test):** `Name()` is `"Housekeeping"`; `Run` over a
  seeded store deletes down to `keep` per name and reports the count.
- **`handleStatus` (api test):** seed in-flight vs terminal rows → assert the
  response `taskCount` reflects active (queued+running) only, not the total.
- **`handleTasks` (api test):** assert the Queue is bounded to 10 (seed >10
  terminal rows, assert `len(queue) == 10`).

## 6. Non-goals recap / future follow-ups

- If a Queue "show more / full history" view is ever wanted, per-name retention
  already keeps ~50 per task; surface it with a larger `ListTasks` limit then.
- A real "total tasks processed" metric (distinct from active count) would need
  its own counter, since pruning makes `COUNT(*)` of the table meaningless as a
  lifetime total. Not needed now.
