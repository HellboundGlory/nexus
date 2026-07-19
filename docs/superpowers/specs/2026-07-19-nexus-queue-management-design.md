# Nexus — Queue / History / Blocklist management (SP-A)

Date: 2026-07-19
Status: approved (design), pending implementation plan

## 1. Context

This is sub-project **A** of a four-item batch the user raised while testing production.
The batch was decomposed as:

| Sub-project | Scope | State |
|---|---|---|
| **SP-A** (this spec) | Clear + pagination on Queue/History/Blocklist; queue removal also removes from the download client | designing |
| SP-B | Sequential per-series grabbing + season-pack exhaustion | not started |
| SP-C | TMDB id in library folder names + a Radarr/Sonarr-style rename modal | not started, needs screenshots |

An original item — "library card count should show downloaded rather than monitored
episodes" — was **dropped** after inspection: `seriesBadge` already renders
`episodeFileCount / episodeCount` (downloaded-monitored over monitored), which the
user confirmed is the desired behaviour.

### The problems being solved

1. **Unbounded lists.** All three Activity tabs fetch their whole list and render
   every row. `useHistory` requests `/history?limit=100`, so history older than the
   most recent 100 events is *unreachable from the UI entirely*. The user's report
   ("a massive scroll bar") is the visible half of this; the unreachable-history
   half is the more serious one.
2. **No bulk clear.** Rows can only be removed one at a time.
3. **Queue removal is DB-only.** `deleteQueue` (`internal/importing/api.go:114`)
   calls `store.DeleteQueueItem` and nothing else. The download continues in the
   download client with nothing in Nexus tracking it — an orphan.

## 2. Goals / non-goals

**Goals**

- Server-side pagination on Queue, History and Blocklist.
- A Clear button on each of the three tabs.
- Removing a queue item (singly or via Clear) also cancels the download in the
  download client and deletes its data.
- Optionally blocklist a release at the moment it is removed from the queue.

**Non-goals**

- Filtering, sorting or searching within the tabs. Pagination only.
- Changing what History records, or adding history retention/pruning. (Task-table
  pruning shipped in SP5; history pruning is a separate question, not raised.)
- Any change to automation's grab behaviour — that is SP-B.

## 3. Load-bearing constraints

These are facts verified against source, not assumptions. Violating any of them
breaks existing behaviour silently.

### 3.1 `store.ListQueue` must keep returning the *whole* queue

`automation.activeQueue` (`internal/automation/search.go:87`) calls
`store.ListQueue(ctx)` and folds the result into the sets of movie ids and episode
ids that currently have an in-flight download. Those sets are the **only** guard
against grabbing the same item twice — `searchMovie` (`search.go:39`),
`searchSeason` (`search.go:230`) and `searchEpisode` (`search.go:316`) all consult
them.

If `ListQueue` were changed to return a page, items beyond the first page would
look un-queued and be re-grabbed on every sweep.

**Therefore:** pagination is added as a *new* method (`ListQueuePage`). `ListQueue`
is not touched. The same reasoning applies to `QueueByStatus`, which
`ImportCompleted` (`internal/importing/command.go:15`) uses to find rows to import.

### 3.2 The live client item id is only reliably known from the live queue

To remove a download from its client we need a `(clientID, itemID)` pair.
`store.QueueItem` carries `ClientItemID` and `DownloadClientID`, but
`DownloadClientID` is `""` for any row enqueued without an explicit client
override — `Grab` routes by protocol/priority and the landing client is only known
from the live item. This is documented on `matchItem`
(`internal/importing/importer.go:71-75`) and is why `ImportItem` resolves the
client id from the live item at `importer.go:63` rather than from the row.

**Therefore:** removal resolves the live item via `matchItem` and uses
`item.DownloadClientID` / `item.ID`. When there is no live match (already
finished, already removed, client offline) the DB row is still deleted — a missing
client item must never block the user from clearing their queue.

### 3.3 Removal without blocklisting invites an immediate re-grab

Once a queue row is gone, `activeQueue` no longer reports the item as in flight
and `MediaFileForEpisode`/`MediaFileForMovie` still report no file. The next
`MissingSearch` sweep therefore treats it as missing and re-grabs — very possibly
the same release, since nothing recorded that it was unwanted.

This is not a bug to fix here; it is why Radarr and Sonarr put a "Blocklist
release" checkbox on their remove dialogs. SP-A adopts the same escape hatch and
states the consequence inline in the dialog.

## 4. Design

### 4.1 Store — paged reads

Three new methods, each returning the page plus the unfiltered total:

```go
func (s *Store) ListQueuePage(ctx context.Context, offset, limit int) ([]QueueItem, int, error)
func (s *Store) ListHistoryPage(ctx context.Context, offset, limit int) ([]HistoryEvent, int, error)
func (s *Store) ListBlocklistPage(ctx context.Context, offset, limit int) ([]Blocklist, int, error)
```

Each runs a `SELECT COUNT(*)` and a `SELECT … LIMIT ? OFFSET ?` against its table.
Ordering matches the existing unpaged methods exactly — `download_queue` by `id`
ascending, `history` and `blocklist` by `id` descending — so a page boundary is
stable across the queue's 5-second poll.

`limit <= 0` falls back to 50; `offset < 0` clamps to 0.

### 4.2 Store — bulk deletes

```go
func (s *Store) ClearHistory(ctx context.Context) (int64, error)
func (s *Store) ClearBlocklist(ctx context.Context) (int64, error)
```

Both are `DELETE FROM <table>` returning `RowsAffected`. There is deliberately no
`ClearQueue` store method: clearing the queue is a service-level operation because
each row needs client-side removal first (§4.4).

### 4.3 API — paged list endpoints

`GET /queue`, `GET /history` and `GET /blocklist` accept `?page=` (1-based) and
`?pageSize=`, and return an envelope:

```json
{ "items": [ … ], "page": 1, "pageSize": 50, "total": 1234 }
```

`pageSize` is clamped to `[1, 100]`, defaulting to 50. `page` defaults to 1 and
clamps to `>= 1`. A page past the end returns an empty `items` array with the
correct `total`, so the UI can recover by clamping to the last page.

**This is a breaking wire change** — all three endpoints currently return a bare
array. The only consumers are the three FE sections in `web/src/features/activity/`,
all updated in the same change. `GET /history`'s existing `?limit=` is dropped in
favour of `pageSize`; nothing outside the FE uses it.

`GET /queue` keeps enriching each row with live `progress` / `downloadStatus` via
`matchItem`, exactly as today — enrichment now applies to the page's rows only.

### 4.4 API — clear endpoints

- `DELETE /queue` → `{ "removed": N }`. Iterates every row: resolve the live item
  via `matchItem`, and when matched call `queue.Remove(clientID, itemID, true)` —
  `deleteData: true`, so partial files go too. Client-removal errors are
  `slog.Warn`-ed and never abort the sweep. Then `store.DeleteQueueItem` per row,
  emitting `QueueUpdated{ID: row.ID}` per removed row — the same per-row emission
  `ImportItem` makes (`internal/importing/importer.go:67`), so the WS-driven UI
  refresh path stays uniform rather than needing a new bulk event type.
- `DELETE /history` → `{ "removed": N }` via `store.ClearHistory`.
- `DELETE /blocklist` → `{ "removed": N }` via `store.ClearBlocklist`.

Clearing the blocklist makes previously-rejected releases eligible again; that is
the point of the button, and the confirm dialog says so.

### 4.5 API — single queue-item removal

`DELETE /queue/{id}` gains two query parameters:

| Param | Default | Effect |
|---|---|---|
| `removeFromClient` | `true` | Resolve the live item and `Remove(…, deleteData: true)` before deleting the row |
| `blocklist` | `false` | `store.AddBlocklist` the release, scoped to its movie/series, before deleting the row |

Defaults are chosen so that the plain `DELETE /queue/{id}` a future client might
send does the *safe, expected* thing (no orphaned download). The blocklist entry
mirrors the shape `handleFailed` writes (`internal/importing/command.go:51-56`),
with `Reason: "removed from queue"`.

This moves the handler's logic out of the API layer into a new
`Service.RemoveQueueItem(ctx, id, opts)` so it is testable without HTTP and so
`DELETE /queue` can reuse it.

### 4.6 Frontend

New shared component `web/src/components/ui/Pagination.tsx`:

```tsx
<Pagination page={page} pageSize={pageSize} total={total}
            onPageChange={…} onPageSizeChange={…} />
```

Renders `Showing X–Y of Z`, Previous/Next buttons (disabled at the ends), and a
page-size `<select>` offering 25 / 50 / 100. It is presentational — page state
lives in each section via `useState`.

`activity/api.ts` changes:

- `useQueue(page, pageSize)`, `useHistory(page, pageSize)`, `useBlocklist(page, pageSize)`
  take page state, include it in their query keys, and unwrap the envelope.
- New `useClearQueue()`, `useClearHistory()`, `useClearBlocklist()` mutations,
  each invalidating its own key (plus `queue` → also `history`, since clearing the
  queue does not write history but importing does and the two views sit together).
- `useRemoveQueueItem()` takes `{ id, removeFromClient, blocklist }`.

`QueueSection` replaces its `window.confirm` with a new
`RemoveQueueItemDialog` — same construction as `DeleteConfirmDialog`
(`web/src/features/library/DeleteConfirmDialog.tsx`), two checkboxes:

- **Remove from download client** (default **on**) — "Also cancel the download and
  delete its files."
- **Blocklist this release** (default off) — "Stop this release being grabbed again.
  Without this, automation may re-grab the same file."

Each section gains a Clear button in a small header row above its table, behind a
confirm dialog naming the row count (e.g. "Clear all 1,234 history events?").
The Clear button is hidden when `total === 0`.

`useActivityInvalidation` is unchanged — it already invalidates all three keys, and
query keys that now include page state still match by prefix.

### 4.7 Error handling

- Paged reads: a store error → 500, as today.
- Clear queue: per-row client-removal failure is logged and skipped; a DB delete
  failure aborts and returns 500 with however many were already removed
  uncounted. Partial clears are acceptable and self-healing — the user can press
  Clear again.
- Single removal: `store.ErrNotFound` → 404 (existing behaviour preserved);
  blocklist-insert failure aborts before the row is deleted, so the user can retry
  without having lost the row.

## 5. Testing

**Go**

- `ListQueuePage` / `ListHistoryPage` / `ListBlocklistPage`: correct slice, correct
  total, stable ordering, out-of-range offset → empty page with real total,
  `limit <= 0` → default.
- `ListQueue` still returns every row after the paged methods land — a direct
  regression test for §3.1.
- `ClearHistory` / `ClearBlocklist` return the right count; clearing an empty table
  returns 0, not an error.
- `RemoveQueueItem`: removes from client when `removeFromClient`, does not when
  not; deletes the row either way; writes a blocklist row when `blocklist`; row is
  still deleted when the client removal errors; no live match → row deleted, no
  client call. The existing `fakeQueue` (`internal/importing/enqueue_test.go:30`)
  already records `Remove` calls.
- API: envelope shape asserted via `json.RawMessage` (page/pageSize/total present
  and numeric, `items` an array); `pageSize` clamping; `DELETE /queue` returns the
  count and empties the table.

**Frontend (vitest)**

- `Pagination`: disabled Previous on page 1, disabled Next on the last page,
  correct `Showing X–Y of Z`, page-size change fires the callback.
- Each section: renders a page, Clear button hidden at `total === 0`, Clear fires
  the mutation after confirmation.
- `RemoveQueueItemDialog`: defaults (client on, blocklist off), resets on reopen,
  passes both flags to `onConfirm`.
- `useRemoveQueueItem` builds the right query string for each flag combination.

## 6. Risks

| Risk | Mitigation |
|---|---|
| Paginating the wrong method breaks automation's duplicate-grab guard (§3.1) | New methods only; explicit regression test that `ListQueue` is unpaged |
| Wire-shape change breaks a consumer | Only the three FE sections consume these endpoints; all updated together; verified by grep for the endpoint strings |
| Clear queue leaves client downloads running when the client is offline | Rows are deleted regardless (§3.2); failures logged. User can re-clear once the client is back — but the already-deleted rows will not retry, so this is a genuine gap, accepted as the alternative (blocking clear on client health) is worse |
| Remove-without-blocklist causes an immediate re-grab | Checkbox plus inline explanation (§3.3) |

## 7. Open questions

None. Design approved by the user 2026-07-19.
