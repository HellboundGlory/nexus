# Nexus — Queue / History / Blocklist management (SP-A)

Date: 2026-07-19
Status: approved (design), pending implementation plan

## 1. Context

This is sub-project **A** of a four-item batch the user raised while testing production.
The batch was decomposed as:

| Sub-project | Scope | State |
|---|---|---|
| **SP-A** (this spec) | Clear on all three Activity tabs; pagination on History/Blocklist; queue removal also removes from the download client | designing |
| SP-B | Sequential per-series grabbing + season-pack exhaustion | not started |
| SP-C | TMDB id in library folder names + a Radarr/Sonarr-style rename modal | not started, needs screenshots |

An original item — "library card count should show downloaded rather than monitored
episodes" — was **dropped** after inspection: `seriesBadge` already renders
`episodeFileCount / episodeCount` (downloaded-monitored over monitored), which the
user confirmed is the desired behaviour.

### The problems being solved

1. **Unbounded History and Blocklist.** Both fetch their whole list and render every
   row. `useHistory` requests `/history?limit=100`, so history older than the most
   recent 100 events is *unreachable from the UI entirely*. The user's report ("a
   massive scroll bar") is the visible half of this; the unreachable-history half is
   the more serious one.
2. **No bulk clear.** Rows can only be removed one at a time.
3. **Queue removal is DB-only.** `deleteQueue` (`internal/importing/api.go:114`)
   calls `store.DeleteQueueItem` and nothing else. The download continues in the
   download client with nothing in Nexus tracking it — an orphan.

## 2. Goals / non-goals

**Goals**

- Server-side pagination on History and Blocklist.
- A Clear button on each of the three tabs.
- Removing a queue item (singly or via Clear) also cancels the download in the
  download client and deletes its data.
- Optionally blocklist a release at the moment it is removed from the queue.
- Refuse to clear the queue when a download client is unreachable, rather than
  silently orphaning downloads — with an explicit, opt-in force override.

**Non-goals**

- **Pagination on the Queue tab.** Dropped at the user's direction: the queue is
  naturally self-limiting (rows are deleted on import) and SP-B will shrink it
  further by grabbing one item per series at a time. `GET /queue` keeps its current
  bare-array wire shape.
- Filtering, sorting or searching within the tabs.
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

Queue pagination is out of scope (§2), so this is not at risk in SP-A — but it is
recorded here because it is the single most dangerous thing to get wrong in this
area of the code, and **SP-B will be working directly in it**. If queue pagination
is ever revisited, it must be added as a new method, never by changing `ListQueue`.
The same reasoning applies to `QueueByStatus`, which `ImportCompleted`
(`internal/importing/command.go:15`) uses to find rows to import.

### 3.2 The live client item id is only reliably known from the live queue

To remove a download from its client we need a `(clientID, itemID)` pair.
`store.QueueItem` carries `ClientItemID` and `DownloadClientID`, but
`DownloadClientID` is `""` for any row enqueued without an explicit client
override — `Grab` routes by protocol/priority and the landing client is only known
from the live item. This is documented on `matchItem`
(`internal/importing/importer.go:71-75`) and is why `ImportItem` resolves the
client id from the live item at `importer.go:63` rather than from the row.

**Therefore:** removal resolves the live item via `matchItem` and uses
`item.DownloadClientID` / `item.ID`.

### 3.3 Removal without blocklisting invites an immediate re-grab

Once a queue row is gone, `activeQueue` no longer reports the item as in flight
and `MediaFileForEpisode`/`MediaFileForMovie` still report no file. The next
`MissingSearch` sweep therefore treats it as missing and re-grabs — very possibly
the same release, since nothing recorded that it was unwanted.

This is not a bug to fix here; it is why Radarr and Sonarr put a "Blocklist
release" checkbox on their remove dialogs. SP-A adopts the same escape hatch and
states the consequence inline in the dialog.

### 3.4 The download-client reachability signal exists but is currently discarded

`downloadclient.Service.Queue` returns `QueueResult{Items, ClientErrors}` with
explicit partial-success semantics — a client whose `Items()` call fails is
recorded in `ClientErrors` rather than failing the whole fan-out
(`internal/downloadclient/downloadclient.go:137-165`).

`importing.QueueReader.Queue` is declared as `Queue(ctx) []provider.DownloadItem`,
and `dlQueueAdapter` (`cmd/nexus/main.go:245`) satisfies it by returning
`.Items` and **dropping `ClientErrors` on the floor**. So today the importing layer
genuinely cannot tell "the client says this download is gone" apart from "the
client did not answer".

That distinction is precisely what "refuse to clear when unreachable" needs, so
this flattening must be undone (§4.2). This is the same class of defect, and the
same fix, as C3's `Searcher` → `SearchDetailed` widening, where an adapter
collapsed per-indexer errors that the UI needed.

## 4. Design

### 4.1 Store

Paged reads for the two lists that need them:

```go
func (s *Store) ListHistoryPage(ctx context.Context, offset, limit int) ([]HistoryEvent, int, error)
func (s *Store) ListBlocklistPage(ctx context.Context, offset, limit int) ([]Blocklist, int, error)
```

Each runs a `SELECT COUNT(*)` and a `SELECT … LIMIT ? OFFSET ?`. Ordering matches
the existing unpaged methods exactly — both `id DESC`. `limit <= 0` falls back to
50; `offset < 0` clamps to 0.

Bulk deletes:

```go
func (s *Store) ClearHistory(ctx context.Context) (int64, error)
func (s *Store) ClearBlocklist(ctx context.Context) (int64, error)
```

Both are `DELETE FROM <table>` returning `RowsAffected`. There is deliberately no
`ClearQueue` store method: clearing the queue is a service-level operation because
each row needs client-side removal first (§4.4).

### 4.2 Undoing the QueueReader flattening

`importing.QueueReader.Queue` is **changed** (not widened with a second method) to
carry the client errors:

```go
// in package importing
type ClientError struct {
    ClientID string `json:"clientId"`
    Message  string `json:"message"`
}

type QueueSnapshot struct {
    Items        []provider.DownloadItem
    ClientErrors []ClientError
}

type QueueReader interface {
    Queue(ctx context.Context) QueueSnapshot
    Remove(ctx context.Context, clientID, itemID string, deleteData bool) error
}
```

`dlQueueAdapter.Queue` maps `downloadclient.QueueResult` straight across.

**Why replace rather than add a `QueueDetailed` alongside it** (which is what C3
did for `Searcher`): C3 had multiple implementers and a widely-used method, so
additive was the low-risk choice. Here there is exactly one production implementer
(`dlQueueAdapter`), one test fake (`fakeQueue`,
`internal/importing/enqueue_test.go:30`), and three call sites (`listQueue`,
`ImportCompleted`, plus the new clear path). Keeping both would leave a method
whose only distinguishing feature is that it *loses* the errors — and using it by
mistake reintroduces exactly the bug being fixed. The compiler catches every call
site, so the blast radius is fully enumerable.

Existing callers that do not care about errors read `.Items` and are otherwise
unchanged.

### 4.3 API — paged list endpoints

`GET /history` and `GET /blocklist` accept `?page=` (1-based) and `?pageSize=`,
and return an envelope:

```json
{ "items": [ … ], "page": 1, "pageSize": 50, "total": 1234 }
```

`pageSize` is clamped to `[1, 100]`, defaulting to 50. `page` defaults to 1 and
clamps to `>= 1`. A page past the end returns an empty `items` array with the
correct `total`, so the UI can recover by clamping to the last page.

**This is a breaking wire change for these two endpoints** — both currently return
a bare array. The only consumers are `HistorySection` and `BlocklistSection` in
`web/src/features/activity/`, updated in the same change. `GET /history`'s existing
`?limit=` is dropped in favour of `pageSize`.

`GET /queue` is **unchanged** — still a bare array, still enriched per row with
live `progress` / `downloadStatus` via `matchItem`.

### 4.4 API — clear endpoints

**`DELETE /queue[?force=]` → `{ "removed": N }`, or 503 and no deletions.**

Preflight: call `queue.Queue(ctx)` once. If `len(ClientErrors) > 0` and `force` is
not set, refuse immediately with 503 `client_unavailable` and a message naming the
failing clients — **nothing is deleted**. This is the user-chosen default: better
to block the clear than to orphan downloads that Nexus can no longer see.

Otherwise, for each row: resolve the live item from the snapshot via `matchItem`;
when matched call `queue.Remove(clientID, itemID, true)` (`deleteData: true`, so
partial files go too). A row with no live match is a download the client has
already finished with — its row is simply deleted. Then `store.DeleteQueueItem`
per row, emitting `QueueUpdated{ID: row.ID}` per removed row — the same per-row
emission `ImportItem` makes (`internal/importing/importer.go:67`), so the WS-driven
UI refresh path stays uniform rather than needing a new bulk event type.

A `Remove` call that fails *after* the preflight passed (a client that dropped
mid-sweep) aborts the loop and returns 503; rows already removed stay removed.
This is a genuinely partial outcome, but every row it removed was removed
correctly from both sides, and re-pressing Clear resumes from where it stopped.

**`?force=true`** changes exactly two things and nothing else:

1. The preflight refusal is skipped.
2. A failing `Remove` is `slog.Warn`-ed and the loop continues, rather than
   aborting with 503.

The DB row is deleted either way, so a forced clear always empties the queue. It
never changes *what* is attempted — `Remove` is still called for every matched row,
with `deleteData: true`. Force is about tolerating failure, not skipping the work;
a client that comes back mid-force still gets its downloads cancelled properly.

The response reports what actually happened so a forced clear is not silent:

```json
{ "removed": 12, "clientErrors": [ { "clientId": "sab", "message": "…" } ] }
```

`clientErrors` is omitted when empty, so the non-forced happy path keeps the
simple `{ "removed": N }` shape.

**Force is deliberately not offered on single-item removal** (§4.5). The
`removeFromClient=false` path already produces the identical outcome there — row
deleted, client untouched — and a second flag meaning the same thing would be
redundant surface area.

**`DELETE /history` → `{ "removed": N }`** via `store.ClearHistory`.
**`DELETE /blocklist` → `{ "removed": N }`** via `store.ClearBlocklist`.

Neither touches a download client, so neither can fail this way. Clearing the
blocklist makes previously-rejected releases eligible again; that is the point of
the button, and the confirm dialog says so.

### 4.5 API — single queue-item removal

`DELETE /queue/{id}` gains two query parameters:

| Param | Default | Effect |
|---|---|---|
| `removeFromClient` | `true` | Resolve the live item and `Remove(…, deleteData: true)` before deleting the row |
| `blocklist` | `false` | `store.AddBlocklist` the release, scoped to its movie/series, before deleting the row |

Defaults are chosen so that a plain `DELETE /queue/{id}` does the safe, expected
thing (no orphaned download).

Consistent with §4.4: when `removeFromClient` is true and the client call fails,
the request returns 503 and **the row is not deleted**. The escape hatch is
unchecking "Remove from download client", which deletes the row unconditionally —
so the user is never trapped with an undeletable row, but has to opt into
orphaning it.

A row with no live match is not a failure — the client has nothing to remove, so
the row is deleted.

The blocklist entry mirrors the shape `handleFailed` writes
(`internal/importing/command.go:51-56`), with `Reason: "removed from queue"`.

This logic moves out of the API layer into `Service.RemoveQueueItem(ctx, id, opts)`
so it is testable without HTTP and so `DELETE /queue` can reuse it.

### 4.6 Frontend

New shared component `web/src/components/ui/Pagination.tsx`:

```tsx
<Pagination page={page} pageSize={pageSize} total={total}
            onPageChange={…} onPageSizeChange={…} />
```

Renders `Showing X–Y of Z`, Previous/Next buttons (disabled at the ends), and a
page-size `<select>` offering 25 / 50 / 100. Presentational — page state lives in
each section via `useState`. Used by `HistorySection` and `BlocklistSection` only.

`activity/api.ts` changes:

- `useHistory(page, pageSize)` and `useBlocklist(page, pageSize)` take page state,
  include it in their query keys, and unwrap the envelope. `useQueue()` is
  unchanged.
- New `useClearQueue()`, `useClearHistory()`, `useClearBlocklist()` mutations, each
  invalidating its own key.
- `useRemoveQueueItem()` takes `{ id, removeFromClient, blocklist }`.

`QueueSection` replaces its `window.confirm` with a new `RemoveQueueItemDialog` —
same construction as `DeleteConfirmDialog`
(`web/src/features/library/DeleteConfirmDialog.tsx`), two checkboxes:

- **Remove from download client** (default **on**) — "Also cancel the download and
  delete its files."
- **Blocklist this release** (default off) — "Stop this release being grabbed again.
  Without this, automation may re-grab the same file."

Each section gains a Clear button in a small header row above its table, behind a
confirm dialog naming the row count (e.g. "Clear all 1,234 history events?"). The
Clear button is hidden when there is nothing to clear.

A 503 from single-item removal surfaces as an error toast carrying the server's
message (which names the unreachable client), via the existing `ApiError` handling
already used in `QueueSection`.

**Force is surfaced only where and when it is needed.** There is no force control
in the normal Clear queue dialog. When a clear is refused with 503, the dialog
stays open and switches to a warning state showing the server's message and a
single **Clear anyway** button, which re-issues the call with `force=true`. So the
override is undiscoverable until the exact moment it is the right answer, and
using it is a deliberate second action rather than a checkbox someone leaves
ticked.

After a forced clear that returned `clientErrors`, the success toast says so —
e.g. "Queue cleared (12 items). 1 download client could not be reached; its
downloads may still be running." Silently reporting plain success would hide the
orphans the refusal existed to prevent.

`useClearQueue()` therefore takes `{ force }`.

`useActivityInvalidation` is unchanged — it already invalidates all three keys, and
query keys that now include page state still match by prefix.

### 4.7 Error handling summary

| Situation | Outcome |
|---|---|
| Store error on a paged read | 500, as today |
| `DELETE /queue` with any client unreachable | 503, nothing deleted |
| Client drops mid-clear | 503; rows already removed stay removed |
| `DELETE /queue?force=true` with a client unreachable | Queue emptied; failures logged and reported in `clientErrors` |
| `DELETE /queue/{id}`, client call fails, `removeFromClient=true` | 503, row kept |
| `DELETE /queue/{id}`, `removeFromClient=false` | Row deleted unconditionally |
| Row has no live client match | Not an error; row deleted |
| `store.ErrNotFound` on single removal | 404 (existing behaviour preserved) |
| Blocklist insert fails | Abort before deleting the row, so a retry loses nothing |

## 5. Testing

**Go**

- `ListHistoryPage` / `ListBlocklistPage`: correct slice, correct total, stable
  ordering, out-of-range offset → empty page with real total, `limit <= 0` → default.
- `ClearHistory` / `ClearBlocklist`: right count; clearing an empty table returns 0,
  not an error.
- `ListQueue` still returns every row (regression guard for §3.1, cheap to keep).
- `QueueSnapshot` plumbing: a fake whose snapshot carries `ClientErrors` makes
  `DELETE /queue` return 503 and leaves **every** row present — the central test for
  the user's chosen behaviour.
- `ClearQueue` happy path: removes from client with `deleteData: true`, empties the
  table, returns the count.
- `ClearQueue` where a row has no live match: row deleted, no `Remove` call.
- `ClearQueue` with `force`: unreachable client no longer refuses — queue emptied,
  and the returned `clientErrors` is non-empty (asserting force *reports* rather
  than hides).
- `ClearQueue` with `force` where `Remove` itself errors: loop continues, every row
  deleted — the mid-sweep-drop case, which is distinct from the preflight case and
  is the one that would regress if force were implemented as preflight-skip only.
- `ClearQueue` with `force` and a *healthy* client: `Remove` is still called for
  every matched row with `deleteData: true` (force must not become "skip the work").
- `RemoveQueueItem`: removes from client when `removeFromClient`, does not when not;
  writes a blocklist row when `blocklist`; **row kept** when the client call fails
  with `removeFromClient=true`; row deleted when the client call fails but
  `removeFromClient=false`.
- API: envelope shape asserted via `json.RawMessage` (page/pageSize/total present
  and numeric, `items` an array); `pageSize` clamping; `GET /queue` still a bare
  array (guards against accidentally enveloping it).

The existing `fakeQueue` (`internal/importing/enqueue_test.go:30`) already records
`Remove` calls; it needs its `Queue` method updated to the new signature and a
settable `ClientErrors` field.

**Frontend (vitest)**

- `Pagination`: disabled Previous on page 1, disabled Next on the last page, correct
  `Showing X–Y of Z`, page-size change fires the callback.
- History/Blocklist sections: render a page, Clear hidden when empty, Clear fires
  the mutation after confirmation, page change refetches.
- `RemoveQueueItemDialog`: defaults (client on, blocklist off), resets on reopen,
  passes both flags to `onConfirm`.
- `useRemoveQueueItem` builds the right query string for each flag combination.
- A 503 clear surfaces an error toast rather than silently doing nothing.

## 6. Risks

| Risk | Mitigation |
|---|---|
| Changing `QueueReader.Queue`'s signature misses a call site | Compiler-enforced; three call sites and two implementers, all in-repo and enumerated in §4.2 |
| Wire-shape change breaks a consumer | Only `HistorySection`/`BlocklistSection` consume the two changed endpoints; `GET /queue` deliberately left alone; verified by grep for the endpoint strings |
| Refusing to clear leaves the user stuck when a client is permanently gone (e.g. deleted from config) | `route`/`Remove` return `ErrClientUnavailable` for an unknown-or-disabled client, so a removed client would otherwise block the clear forever. Solved by `?force=true` (§4.4), surfaced in the UI only after a refusal |
| Force becomes the habitual path and silently orphans downloads | No force control in the normal dialog — it appears only after a refusal, as a distinct second action; and a forced clear that hit errors reports them in both the response and the toast (§4.6) |
| Remove-without-blocklist causes an immediate re-grab | Checkbox plus inline explanation (§3.3) |
| Partial clear on a mid-sweep client drop | Every row removed was removed correctly from both sides; re-pressing Clear resumes (§4.4) |

## 7. Open questions

None. Design and all three amendments (no queue pagination; refuse-to-clear on
unreachable client; `?force=true` override built now rather than deferred)
approved by the user 2026-07-19.
