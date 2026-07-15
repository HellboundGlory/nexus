# Nexus Web UI — Live Download Progress in the Activity Queue (Wave C2)

Date: 2026-07-15
Status: Approved (design)

## 1. Goal

Surface live download progress (a percentage + progress bar) on each in-flight row of
the Activity → Queue page, and reflect the release's live download sub-status
(Downloading / Queued / Paused / Warning) while it is still in the client. Also tighten
the download-pipeline polling cadence from 60s to 30s so progress and imports react
roughly twice as fast.

The download monitor already tracks `provider.DownloadItem.Progress` (0..100) and emits
it over WebSocket (`download.status`); the Activity Queue currently shows only the
grab-tracking status (grabbed / importing / imported / failed) with no progress.

## 2. Scope

In scope:
- Backend: enrich `GET /api/v1/queue` rows with live `progress` + `downloadStatus` by
  joining the grab-tracked queue rows to the live download-client queue snapshot.
- Frontend: render a progress bar + % + live sub-status label on `grabbed` rows that
  have a live match, per an explicit precedence table.
- Scheduler: change the two pipeline tasks (download monitor, import reconcile) from
  `1*time.Minute` to `30*time.Second` in `cmd/nexus/main.go`.

Out of scope (YAGNI):
- ETA / download speed (the monitor provides no rate — only Size / Downloaded).
- Size / downloaded byte readout on the row (a display option that was declined).
- Per-task configurable intervals / a settings UI for the 30s cadence (hardcoded change).
- Any change to the other scheduled tasks (health check 15m, media refresh 12h,
  automation searches on hourly cadences) — deliberately left as-is.
- Any migration, new route, new dependency, or new wiring.

## 3. Architecture

### 3.1 Data flow (unchanged plumbing)

The download `Monitor` (`internal/downloadclient/monitor.go`) polls the aggregated
download-client queue on a schedule and emits `download.status` events (full
`provider.DownloadItem`, incl. `Progress` and live `Status`) on any change. The web
client already forwards these over WS and the Activity feature already treats
`download.status` as a refetch trigger (`REFRESH_EVENTS` in
`web/src/features/activity/resolve.ts`). So the update-trigger path is already wired —
this slice only adds the progress *data* to the queue response and renders it.

### 3.2 Backend enrichment (`internal/importing/api.go`)

`importing.Service` already holds the `QueueReader` dependency
(`Queue(ctx) []provider.DownloadItem`), used by the importer — so no new dependency,
wiring, or import cycle is introduced. `listQueue` gains a live join:

New response DTO (mirrors the C1 `blocklistDTO` embed pattern):

```go
type queueItemDTO struct {
    store.QueueItem
    Progress       *float64 `json:"progress,omitempty"`       // 0..100, nil when no live match
    DownloadStatus string   `json:"downloadStatus,omitempty"` // provider.DownloadStatus, "" when no live match
}
```

`listQueue`:
1. `rows := store.ListQueue(ctx)` (unchanged).
2. `items := svc.queue.Queue(ctx)` — one live snapshot.
3. For each row, reuse the existing package-private `matchItem(items, row)` to find its
   live `DownloadItem`. On a match, set `Progress` (pointer to `it.Progress`) and
   `DownloadStatus = string(it.Status)`. On no match, both stay nil/"".
4. Marshal `[]queueItemDTO` (empty slice, not null, when no rows — preserve current
   behavior).

The backend is a **pure join**: it enriches *every* row that has a live match regardless
of grab status, and applies **no display policy**. Display precedence lives entirely in
the frontend (where it is unit-tested).

### 3.3 Wire-shape guard (the recurring trap)

This is the same class of bug as 3b (qualityId int-vs-string), 6-4 (mediaKind "tv"),
and 6-5 (season-0 omitempty). The discriminator for "has live data" MUST be **field
presence, never a progress value**:

- `Progress` is a **pointer** (`*float64`) so `omitempty` drops it entirely when there is
  no live match. A genuinely-just-started 0% row (which HAS a `downloadStatus`) is thus
  distinguishable from a no-live-data row (both fields absent). A non-pointer float would
  serialize `progress: 0` on a matchless row and render a misleading empty "0%" bar.
- `provider.DownloadStatus` is a Go `string` type, so it serializes as a JSON string
  ("downloading", "queued", "paused", "warning", "completed", "failed") — no int-enum
  trap.
- The frontend renders the bar **iff `downloadStatus != null`**, never keyed off the
  numeric progress.

### 3.4 Frontend (`web/src/features/activity/`)

`types.ts` — `QueueItem` gains:
```ts
progress?: number
downloadStatus?: string
```

`resolve.ts` — new pure helpers:
- `liveStatusLabel(downloadStatus: string): string` — maps the live status to a display
  label (downloading→"Downloading", queued→"Queued", paused→"Paused", warning→"Warning",
  completed→"Completed"; unknown → passthrough).
- A precedence resolver (e.g. `queueRowStatus(row)`) returning what to render: whether to
  show the bar, the percent, and the status label + tone. Rule below.

`QueueSection.tsx` — renders a slim progress bar (existing `--color-brand` CSS var) plus
the percent and the live sub-status label in the Status cell, per the resolver.

### 3.5 Display precedence (every row state enumerated)

| Grab status (`row.status`) | Live match (`downloadStatus`) | Display |
| --- | --- | --- |
| `grabbed` | downloading / queued / paused / warning | progress bar + `NN%` + live sub-status label (info tone) |
| `grabbed` | completed (100%) | bar at 100% + "Completed" (transient, before the next import tick removes/imports it) |
| `grabbed` | **none** (absent) | plain "Grabbed" label, **no bar** |
| `importing` | (ignored even if present) | existing "Importing" label, no bar |
| `imported` | (ignored) | existing "Imported" label, no bar |
| `failed` (rejected import) | (ignored) | existing "Failed" label + error text, no bar |

Rule: the live `downloadStatus` overrides the grab-status label **only** for `grabbed`
rows that have a match. For every other grab status the existing `statusLabel` /
`statusTone` output is used unchanged, and no bar is shown. A `grabbed` row with no live
match falls back to the plain "Grabbed" label with no bar.

### 3.6 Scheduler cadence (`cmd/nexus/main.go`)

Change two lines from `1*time.Minute` to `30*time.Second`:
- `sch.Every(1*time.Minute, func() command.Command { return dlMonitor })`
- `sch.Every(1*time.Minute, func() command.Command { return importCmd })`

Left unchanged: indexer health check (15m), media refresh (12h), missing/RSS/upgrade
searches (hours).

## 4. Cadence & degradation

- Effective progress-update granularity becomes ~30s (the monitor is the only thing that
  polls the download clients; the enriched endpoint reflects whatever the last poll
  produced, and WS `download.status` events — now ~30s — trigger the refetch).
- `listQueue` now calls `svc.queue.Queue(ctx)` on every request (mount + WS-triggered
  refetches), where before it was a pure DB read. This is acceptable for a self-hosted
  app with one or two local download clients.
- Graceful degradation is guaranteed by `downloadclient.Service.Queue`: it fans out per
  client, routes a failing client's error into `ClientErrors`, and still returns the
  items that succeeded — no error propagation. A dead/slow SAB/qBit therefore yields
  fewer live items → those rows simply render unenriched (plain grab status). The queue
  page can never hang or error because of a bad client. (Build-time check: confirm the
  `QueueReader` adapter passed at the composition root surfaces `.Items` and swallows
  `ClientErrors`, which is the existing importer behavior.)

## 5. Testing

- Backend (`internal/importing/api_test.go`): `listQueue` enrichment — a `grabbed` row
  matched to a live downloading item gets `progress` + `downloadStatus`; a row with no
  live match omits both fields (assert the JSON keys are absent, not zero); empty queue
  returns `[]`.
- Frontend `resolve.test.ts`: precedence resolver + `liveStatusLabel`. Explicit cases:
  (a) grabbed + downloading @ 0% with `downloadStatus` present → bar shown at 0% (the
  presence-not-value guard); (b) grabbed + **no** `downloadStatus` → no bar, "Grabbed"
  label; (c) importing/imported/failed → existing label, no bar even if `downloadStatus`
  present.
- Frontend `QueueSection.test.tsx`: renders a progress bar + percent for a grabbed row
  with live progress; renders no bar for a matchless grabbed row and for
  importing/imported/failed rows.
- Rebuild `web/dist`; full verify: `CGO_ENABLED=0 go build/vet/test ./...`, vitest,
  `tsc -b`, web/dist drift guard.
- Live browser AC (throwaway seeded instance): a grabbed row shows a progress bar + %
  and "Downloading"; a matchless grabbed row shows plain "Grabbed"; no console errors.

## 6. Build approach

Small slice — built via the subagent-driven-development loop per project convention.
Expected ~4 TDD tasks:
1. Backend: `queueItemDTO` + `listQueue` enrichment + api_test.
2. Frontend: `types.ts` fields + `resolve.ts` helpers/precedence + resolve.test.ts.
3. Frontend: `QueueSection.tsx` progress bar + QueueSection.test.tsx.
4. Scheduler 30s change + rebuild `web/dist` + full verify + live browser AC.

Opus whole-branch review before merge. ASK before pushing master.
