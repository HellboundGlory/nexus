# Nexus Web UI — Activity Slice 4 (Queue + History)

Date: 2026-07-12
Status: Approved (brainstorm)
Sub-project: 6 (Web UI) — Slice 4 (Activity)

## 1. Goal & scope

Replace the `/activity` placeholder with a real **Activity** page: two tabs —
**Queue** and **History** — built entirely as a UI over Nexus's *existing* REST
endpoints. There are **no backend changes** in this slice.

It mirrors the conventions established by Slices 3a/3b: everything lives under a
new `web/src/features/activity/` module, data flows through TanStack Query v5
hooks, user feedback uses the existing hand-written toast, styling uses the
existing dark CSS tokens, and pure logic is extracted into unit-testable helpers.

### In scope
- New `/activity` parent route rendering an `ActivityLayout` with two nested,
  deep-linkable tabs: `/activity/queue` (index redirect) and `/activity/history`.
- **Queue tab:** list `GET /api/v1/queue`; per-row **Remove** (`DELETE /queue/{id}`)
  and conditional **Import** (`POST /queue/{id}/import`); failed rows surface their
  error; live refresh on WS events.
- **History tab:** read-only list of the last 100 events via `GET /api/v1/history?limit=100`.
- **Title resolution:** resolve numeric `seriesId`/`movieId` to clean library
  titles client-side by reusing `useMovies()`/`useSeries()`; `sourceTitle` shown as
  subtext; robust fallbacks (see §4).
- **Quality resolution:** map numeric `qualityId` to a name by reusing
  `useQualityDefinitions()`.
- **Live refresh:** WS-driven query invalidation reusing the existing
  `useActivity()` ring buffer (no polling).

### Explicitly out of scope (YAGNI / not in Nexus)
- **Blocklist** — Nexus has no blocklist backend.
- **Server-side history pagination / filtering** — the `/history` endpoint takes
  only `?limit=N` (no offset, no type filter); real paging would need backend work.
- **Per-episode `SxxExx` title resolution** — episodes are not in the movie/series
  list payloads; series rows show the series title + `sourceTitle` subtext.
- **Clickable title → detail-page links** — deferred; can be added later without
  reshaping the data layer.
- **A separate live event-feed tab** — the Dashboard already renders the raw WS feed.
- No new database migration; no new/changed backend routes or DTOs.

## 2. Existing backend surface (verified)

| Domain | Endpoints | Shape |
|---|---|---|
| Queue | `GET /api/v1/queue`, `DELETE /api/v1/queue/{id}`, `POST /api/v1/queue/{id}/import` | `store.QueueItem` (see below) |
| History | `GET /api/v1/history?limit=N` | `store.HistoryEvent` (see below) |
| Movies list | `GET /api/v1/movies` | reused via `useMovies()` — provides id→title |
| Series list | `GET /api/v1/series` | reused via `useSeries()` — provides id→title |
| Quality ladder | `GET /api/v1/quality/definitions` | reused via `useQualityDefinitions()` → `QualityDefinition[]{ id, name, ... }` |

### `QueueItem` (wire shape — `internal/core/store/import_store.go`)
```
id: number
downloadClientId: string
clientItemId: string
protocol: string
sourceTitle: string
mediaKind: string            // "movie" | "series"
seriesId?: number            // omitempty
movieId?: number             // omitempty
episodeIds: number[]
qualityId: number            // NON-nullable int
status: string               // "grabbed" | "importing" | "imported" | "failed"
error?: string               // omitempty — populated on failed rows
createdAt: string            // RFC3339
updatedAt: string            // RFC3339
```

### `HistoryEvent` (wire shape — `internal/core/store/import_store.go`)
```
id: number
eventType: string            // "grabbed" | "imported" | "upgraded" | "import_failed"
mediaKind: string            // "movie" | "series"
seriesId?: number            // omitempty
episodeId?: number           // omitempty
movieId?: number             // omitempty
sourceTitle?: string         // omitempty
qualityId?: number | null    // omitempty + nullable (*int) — may be absent
message?: string             // omitempty
createdAt: string            // RFC3339
```

Behaviours to respect:
- **`POST /queue/{id}/import`** runs the full import pipeline synchronously and can
  return `200 {ok:true}` on success, or the standard error envelope:
  `400 rejected`, `400 no_profile`, `404 not_found`, `500 internal`
  (`internal/importing/api.go` `writeErr`). It must never fail silently in the UI.
- **`DELETE /queue/{id}`** returns `200 {ok:true}` or `404 not_found`.
- Queue is returned ordered by `id` ascending; History ordered by `id` **descending**
  (newest first), capped at `limit` (default 100 when `limit<=0`).
- **`qualityId` wire mismatch caution (repeated 3b lesson):** `QueueItem.qualityId`
  is a non-nullable `int`; `HistoryEvent.qualityId` is a nullable `*int`;
  `/quality/definitions` serializes ids as **numeric** enums. All TS types and test
  fixtures MUST use the real numeric shape. String-typed fixtures previously hid a
  real bug in 3b.

## 3. Frontend architecture

New module `web/src/features/activity/`, mirroring `features/settings/`:

| File | Responsibility |
|---|---|
| `types.ts` | TS mirrors of `QueueItem` and `HistoryEvent` using the real numeric wire shape (`qualityId: number` on queue, `qualityId: number \| null` on history). |
| `resolve.ts` | **Pure helpers**, no React: `resolveTitle(row, movieMap, seriesMap)`, `qualityName(id, defs)`, `eventLabel(eventType)`, `statusLabel(status)`, `statusTone(status)`. Independently unit-testable. |
| `api.ts` | Hooks: `useQueue()`, `useHistory()`, `useImportItem()`, `useRemoveQueueItem()`, and `useActivityInvalidation()` (WS → invalidate). Query keys `["queue"]`, `["history"]`. |
| `ActivityLayout.tsx` | Tab shell (Queue / History) using the same `NavLink` pattern as `SettingsLayout`. Mounts `useActivityInvalidation()` once so both tabs stay fresh. Renders `<Outlet/>`. |
| `QueueSection.tsx` | Queue table + row actions. |
| `HistorySection.tsx` | History table. |

**Routing** (`web/src/app/routes.tsx`): replace
`{ path: "activity", element: <Placeholder title="Activity" /> }` with a parent
route rendering `ActivityLayout` and children:
- index → `<Navigate to="/activity/queue" replace />`
- `queue` → `<QueueSection />`
- `history` → `<HistorySection />`

The Sidebar already links `/activity` (`web/src/app/Sidebar.tsx`), so no nav change
is needed. `react-router` matches `/activity/queue` under the `/activity` NavLink.

## 4. Data flow & resolution helpers

- `useQueue()` → `apiGet<QueueItem[]>("/queue")`, key `["queue"]`.
- `useHistory()` → `apiGet<HistoryEvent[]>("/history?limit=100")`, key `["history"]`.
- `useMovies()`, `useSeries()` (reused from `features/library/api.ts`) → build
  `Map<number, string>` of id→display title. Both `Movie` and `Series` expose
  `title` and `year` (verified in `features/library/types.ts`). Series display
  title is the series `title`; movie display title is `"<title> (<year>)"` when
  `year > 0`, else just `title` (a compact single-line formatting choice for table
  rows — this is deliberately denser than the library grid, which shows year as a
  separate subtitle).
- `useQualityDefinitions()` (reused from `features/settings/qualityApi.ts`) → build
  `Map<number, string>` id→quality name.

**`resolveTitle(row, movieMap, seriesMap)` fallback chain:**
1. If `row.mediaKind === "movie"` and `row.movieId` present and in `movieMap` →
   clean movie title.
2. If `row.mediaKind === "series"` and `row.seriesId` present and in `seriesMap` →
   clean series title.
3. Otherwise (no media id, id not in map because media was deleted, **or** the list
   query has not resolved yet) → fall back to `row.sourceTitle`.
4. If `sourceTitle` is also empty → `"—"`.

Never renders blank. `sourceTitle` is always shown as secondary subtext beneath the
resolved title (so the raw release name is visible even when the title resolves).

**`qualityName(id, defs)`:** numeric lookup against `QualityDefinition.id`; returns
`""` (rendered as `—`) when `id` is `null`, `0`, or not found. History rows with an
absent `qualityId` show `—`.

Episode-level series queue/history rows: show the **series title** + `sourceTitle`
subtext (no per-episode resolution).

## 5. Queue tab (`QueueSection`)

Table columns:
- **Media** — resolved title (bold) + `sourceTitle` subtext (muted). On `failed`
  rows, `QueueItem.error` is shown as red subtext so the failure reason is visible.
- **Kind** — `movie` / `series`.
- **Quality** — resolved quality name (or `—`).
- **Protocol** — `protocol` string.
- **Status** — colored badge via `statusTone`: `grabbed` (neutral), `importing`
  (info/accent), `imported` (ok/green), `failed` (red).
- **Added** — `relativeTime(createdAt)` (reuse `lib/time.ts`).
- **Actions** — see below.

Row actions:
- **Remove** — present on every row. Click → confirm dialog → `DELETE /queue/{id}`
  → on success invalidate `["queue"]` + success toast; on error, error toast.
- **Import** — shown **only** when `status === "failed"` (retry) or
  `status === "grabbed"` (force import). **Hidden** on `importing` (racy) and
  `imported` (no-op). Click → `POST /queue/{id}/import`. On `200` → success toast +
  invalidate `["queue"]` and `["history"]`. On error, the error-envelope `message`
  (`rejected` / `no_profile` / `not_found`) surfaces as an **error toast** — never
  silent.

Empty state: "Queue is empty." Loading: a simple loading line consistent with the
existing sections. Error: an error line.

## 6. History tab (`HistorySection`)

Read-only table, last 100 events, newest-first (as returned). Columns:
- **Event** — `eventLabel(eventType)`: Grabbed / Imported / Upgraded / Import failed.
  `import_failed` gets a red tone; others neutral/ok.
- **Media** — resolved title + `sourceTitle` subtext (same `resolveTitle`).
- **Quality** — resolved quality name (or `—`).
- **Message** — `message` (or `—`).
- **Time** — `relativeTime(createdAt)`.

Empty state: "No history yet." No filtering or pagination.

## 7. Live refresh (WS-driven invalidation)

`useActivityInvalidation()` (in `api.ts`), mounted once in `ActivityLayout`:
- Reads the existing `useActivity()` buffer (newest event at index 0).
- In a `useEffect` keyed on the **latest event `id`** (fires once per new event):
  if `latest.type` is one of `queue.updated`, `import.completed`, `download.status`,
  call `queryClient.invalidateQueries({ queryKey: ["queue"] })` and
  `invalidateQueries({ queryKey: ["history"] })`.
- No polling; no `refetchInterval`. `ActivityProvider` already wraps the whole
  `Layout`, so the buffer is available on the Activity page.

## 8. Testing

Vitest, following the `features/settings` test style:
- **`resolve.test.ts` (pure):** `resolveTitle` — movie hit, series hit, missing id
  (deleted media) → `sourceTitle`, empty maps (loading) → `sourceTitle`, empty
  `sourceTitle` → `—`; `qualityName` — numeric hit, `null`/`0`/absent → `—` (fixtures
  in real numeric shape); `eventLabel`/`statusLabel`/`statusTone` mappings.
- **`QueueSection.test.tsx`:** Import button visible only on `failed`/`grabbed`,
  hidden on `importing`/`imported`; `error` subtext rendered on failed rows; Remove
  triggers confirm→delete→invalidate; Import error surfaces a toast; empty state.
- **`HistorySection.test.tsx`:** rows render with resolved titles/labels; empty state.

Full verify before merge: `npm run test` (vitest) green, `tsc -b` exit 0,
`CGO_ENABLED=0 go build/vet/test ./...` (incl. `web/spa_test.go`), and rebuild the
committed `web/dist` with the drift guard clean (`git diff --exit-code web/dist`).
Final task also does a live browser check (both tabs render; a failed row shows its
error; Remove works; Import on a grabbed/failed row toasts a result; History lists
events; WS event triggers a refresh).

## 9. Acceptance criteria

1. `/activity` renders an `ActivityLayout` with Queue and History tabs; `/activity`
   redirects to `/activity/queue`; both subroutes are deep-linkable and the sidebar
   "Activity" link highlights for both.
2. Queue tab lists queue rows with resolved media titles (+ `sourceTitle` subtext),
   quality name, protocol, colored status badge, and relative added-time.
3. Failed queue rows display their `error` text.
4. Remove removes a row (after confirm) and the list updates; errors toast.
5. Import appears only on `failed`/`grabbed` rows; a successful import toasts and
   refreshes queue + history; a rejected/no-profile/not-found import toasts the error.
6. History tab lists the last 100 events newest-first with event label, resolved
   media title, quality, message, and relative time.
7. A `queue.updated` / `import.completed` / `download.status` WS event refreshes the
   open tab without a manual reload.
8. Empty states render for an empty queue and empty history.
9. No backend changes; `web/dist` rebuilt and drift-guard clean; all verify steps green.
