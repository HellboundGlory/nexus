# Nexus Web UI — Live Download Progress in the Activity Queue (Wave C2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a live download progress bar + % + sub-status on in-flight Activity Queue rows, and tighten the download-monitor + import-reconcile schedule from 60s to 30s.

**Architecture:** The backend `GET /api/v1/queue` handler enriches each grab-tracked row with live `progress` + `downloadStatus` by joining to the already-injected `QueueReader` live snapshot (reusing the existing `matchItem`), as a pure data join. The frontend applies all display policy via a pure precedence resolver and renders a bar only when live data is present (keyed on field presence, never on the numeric value).

**Tech Stack:** Go 1.26 (chi router, `net/http/httptest`), React + TypeScript (Vite, TanStack Query v5, Vitest + Testing Library), Tailwind CSS vars.

## Global Constraints

- Go is NOT on the session PATH. Prefix every Go command with: `export PATH="/c/Program Files/Go/bin:$PATH"`.
- Go build/test invocation: `CGO_ENABLED=0 go <cmd> ./...` (no CGO; `-race` unavailable — use `-count=N` for concurrency if ever needed).
- Web commands run from the `web/` directory: `npm test` (vitest run), `npm run build` (`tsc -b && vite build`).
- Backend module boundary: `internal/importing` may import `internal/core/*` and `internal/naming` only — do NOT add new external deps or wiring for this slice.
- Wire-shape rule (non-negotiable): the "has live data" discriminator is **field presence**, never a progress value. `Progress` is `*float64` with `omitempty`; the FE renders the bar iff `downloadStatus != null`.
- `provider.DownloadStatus` is a Go `string` type — it serializes as a JSON string (no int enum).
- `GET /queue` MUST always return a JSON array (`[]` when empty), never `null`.
- Commit after every task. ASK before pushing master (do not push in this plan).

---

### Task 1: Backend — enrich `GET /queue` with live progress

**Files:**
- Modify: `internal/importing/api.go` (imports region lines 1-15; `listQueue` lines 48-58)
- Test: `internal/importing/api_test.go`

**Interfaces:**
- Consumes: `store.ListQueue(ctx) []store.QueueItem`; `Service.queue` (field, type `QueueReader` with `Queue(ctx) []provider.DownloadItem`); package-private `matchItem(items []provider.DownloadItem, row store.QueueItem) (provider.DownloadItem, bool)` (already in `importer.go`); `store.QueueItem`; `provider.DownloadItem` (fields `ID`, `DownloadClientID`, `Status provider.DownloadStatus`, `Progress float64`).
- Produces: `type queueItemDTO struct { store.QueueItem; Progress *float64 json:"progress,omitempty"; DownloadStatus string json:"downloadStatus,omitempty" }` — the new JSON shape of each `GET /queue` element.

- [ ] **Step 1: Write the failing test**

Add to `internal/importing/api_test.go` (note: this test file is `package importing`, so it can reference `queueItemDTO` and `fakeQueue` directly). Add `"encoding/json"` and `"github.com/hellboundg/nexus/internal/core/provider"` to the test file's imports if not already present.

```go
func TestAPIQueueEnrichesLiveProgress(t *testing.T) {
	ctx := context.Background()
	prog := 42.5
	fq := &fakeQueue{items: []provider.DownloadItem{
		{ID: "h1", DownloadClientID: "sab", Status: provider.StatusDownloading, Progress: prog},
	}}
	svc, st := newSvcWithQueue(t, fq)
	r := chi.NewRouter()
	NewAPI(svc).Mount(r)

	// matched row (client item "h1" is live)
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "sab", ClientItemID: "h1", Protocol: "usenet",
		SourceTitle: "Matched.Release", MediaKind: "movie", Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	// matchless row (no live item with id "ghost")
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		DownloadClientID: "sab", ClientItemID: "ghost", Protocol: "usenet",
		SourceTitle: "Matchless.Release", MediaKind: "movie", Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/queue", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var got []queueItemDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows got %d", len(got))
	}
	var matched, matchless *queueItemDTO
	for i := range got {
		switch got[i].ClientItemID {
		case "h1":
			matched = &got[i]
		case "ghost":
			matchless = &got[i]
		}
	}
	if matched == nil || matchless == nil {
		t.Fatalf("rows not found: %+v", got)
	}
	if matched.Progress == nil || *matched.Progress != 42.5 || matched.DownloadStatus != "downloading" {
		t.Fatalf("matched enrichment wrong: progress=%v status=%q", matched.Progress, matched.DownloadStatus)
	}
	if matchless.Progress != nil || matchless.DownloadStatus != "" {
		t.Fatalf("matchless row should be unenriched: progress=%v status=%q", matchless.Progress, matchless.DownloadStatus)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/importing/ -run TestAPIQueueEnrichesLiveProgress -v`
Expected: FAIL — `undefined: queueItemDTO` (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/importing/api.go`, replace the `listQueue` function (lines 48-58) with the DTO type plus the enriching handler:

```go
type queueItemDTO struct {
	store.QueueItem
	Progress       *float64 `json:"progress,omitempty"`       // 0..100, nil when no live match
	DownloadStatus string   `json:"downloadStatus,omitempty"` // provider.DownloadStatus, "" when no live match
}

func (a *API) listQueue(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.store.ListQueue(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list queue")
		return
	}
	live := a.svc.queue.Queue(r.Context())
	out := make([]queueItemDTO, 0, len(rows))
	for _, row := range rows {
		dto := queueItemDTO{QueueItem: row}
		if it, ok := matchItem(live, row); ok {
			p := it.Progress
			dto.Progress = &p
			dto.DownloadStatus = string(it.Status)
		}
		out = append(out, dto)
	}
	api.WriteJSON(w, http.StatusOK, out)
}
```

(The `provider` package is already imported in `api.go`; `matchItem` already lives in `importer.go` in the same package.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/importing/ -v`
Expected: PASS — new test passes AND the existing `TestAPIQueueListAndHistory` (which asserts the body still starts with `[`) still passes.

- [ ] **Step 5: Commit**

```bash
git add internal/importing/api.go internal/importing/api_test.go
git commit -m "feat(importing): enrich GET /queue rows with live progress + downloadStatus"
```

---

### Task 2: Frontend — queue types + precedence resolver helpers

**Files:**
- Modify: `web/src/features/activity/types.ts` (the `QueueItem` type)
- Modify: `web/src/features/activity/resolve.ts`
- Test: `web/src/features/activity/resolve.test.ts`

**Interfaces:**
- Consumes: existing `statusLabel(s: string): string`, `statusTone(s: string): Tone`, `type Tone` from `resolve.ts`.
- Produces:
  - `QueueItem` gains optional `progress?: number` and `downloadStatus?: string`.
  - `liveStatusLabel(s: string): string` — maps a live download status to a display label.
  - `type QueueDisplay = { kind: "live"; percent: number; label: string; tone: Tone } | { kind: "status"; label: string; tone: Tone }`.
  - `queueRowDisplay(row: { status: string; progress?: number; downloadStatus?: string }): QueueDisplay` — the display precedence resolver.

- [ ] **Step 1: Add the type fields (no test yet)**

In `web/src/features/activity/types.ts`, add two optional fields to the `QueueItem` type (place them after `updatedAt: string`):

```ts
  progress?: number
  downloadStatus?: string
```

- [ ] **Step 2: Write the failing test**

Append to `web/src/features/activity/resolve.test.ts`. First extend the import from `./resolve` to also pull in `liveStatusLabel` and `queueRowDisplay`:

```ts
import {
  movieTitleMap, seriesTitleMap, resolveTitle, qualityName,
  eventLabel, statusLabel, statusTone, shouldRefresh,
  liveStatusLabel, queueRowDisplay,
} from "./resolve"
```

Then add these describe blocks:

```ts
describe("liveStatusLabel", () => {
  it("maps known live statuses", () => {
    expect(liveStatusLabel("downloading")).toBe("Downloading")
    expect(liveStatusLabel("queued")).toBe("Queued")
    expect(liveStatusLabel("paused")).toBe("Paused")
    expect(liveStatusLabel("warning")).toBe("Warning")
    expect(liveStatusLabel("completed")).toBe("Completed")
  })
  it("passes through an unknown status", () => {
    expect(liveStatusLabel("weird")).toBe("weird")
  })
})

describe("queueRowDisplay", () => {
  it("shows live progress for a grabbed row with a live match at 0% (presence, not value)", () => {
    const d = queueRowDisplay({ status: "grabbed", progress: 0, downloadStatus: "downloading" })
    expect(d).toEqual({ kind: "live", percent: 0, label: "Downloading", tone: "info" })
  })
  it("shows live progress mid-download", () => {
    const d = queueRowDisplay({ status: "grabbed", progress: 42.5, downloadStatus: "downloading" })
    expect(d).toEqual({ kind: "live", percent: 42.5, label: "Downloading", tone: "info" })
  })
  it("shows Completed at 100% for a grabbed row still in the client", () => {
    const d = queueRowDisplay({ status: "grabbed", progress: 100, downloadStatus: "completed" })
    expect(d).toEqual({ kind: "live", percent: 100, label: "Completed", tone: "info" })
  })
  it("falls back to the grab status when a grabbed row has no live match", () => {
    expect(queueRowDisplay({ status: "grabbed" })).toEqual({ kind: "status", label: "Grabbed", tone: "neutral" })
  })
  it("ignores live data on non-grabbed rows (importing keeps its label)", () => {
    expect(queueRowDisplay({ status: "importing", progress: 90, downloadStatus: "downloading" }))
      .toEqual({ kind: "status", label: "Importing", tone: "info" })
  })
  it("keeps the grab label for imported and failed rows", () => {
    expect(queueRowDisplay({ status: "imported" })).toEqual({ kind: "status", label: "Imported", tone: "ok" })
    expect(queueRowDisplay({ status: "failed" })).toEqual({ kind: "status", label: "Failed", tone: "error" })
  })
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/activity/resolve.test.ts`
Expected: FAIL — `liveStatusLabel is not a function` / `queueRowDisplay is not a function`.

- [ ] **Step 4: Write minimal implementation**

Append to `web/src/features/activity/resolve.ts`:

```ts
const LIVE_STATUS_LABELS: Record<string, string> = {
  downloading: "Downloading",
  queued: "Queued",
  paused: "Paused",
  warning: "Warning",
  completed: "Completed",
}
export function liveStatusLabel(s: string): string {
  return LIVE_STATUS_LABELS[s] ?? s
}

export type QueueDisplay =
  | { kind: "live"; percent: number; label: string; tone: Tone }
  | { kind: "status"; label: string; tone: Tone }

export function queueRowDisplay(row: {
  status: string
  progress?: number
  downloadStatus?: string
}): QueueDisplay {
  // Live progress overrides the grab-status label ONLY for grabbed rows that
  // have a live match. Presence of downloadStatus is the discriminator — never
  // the numeric progress (a genuine 0% row still has a downloadStatus).
  if (row.status === "grabbed" && row.downloadStatus != null) {
    return { kind: "live", percent: row.progress ?? 0, label: liveStatusLabel(row.downloadStatus), tone: "info" }
  }
  return { kind: "status", label: statusLabel(row.status), tone: statusTone(row.status) }
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd web && npx vitest run src/features/activity/resolve.test.ts`
Expected: PASS (all resolve tests, old and new).

- [ ] **Step 6: Commit**

```bash
git add web/src/features/activity/types.ts web/src/features/activity/resolve.ts web/src/features/activity/resolve.test.ts
git commit -m "feat(webui): queue progress types + display precedence resolver"
```

---

### Task 3: Frontend — render the progress bar in QueueSection

**Files:**
- Modify: `web/src/features/activity/QueueSection.tsx` (imports from `./resolve`; the Status `<td>` around lines 78-80)
- Test: `web/src/features/activity/QueueSection.test.tsx`

**Interfaces:**
- Consumes: `queueRowDisplay` from `./resolve` (Task 2); the `QueueItem` fields `progress`, `downloadStatus` (Task 2); existing `toneClass` map in `QueueSection.tsx`.
- Produces: a `role="progressbar"` element with `aria-valuenow={Math.round(percent)}` rendered in the Status cell for `live` rows.

- [ ] **Step 1: Write the failing test**

Append to `web/src/features/activity/QueueSection.test.tsx` (inside the `describe("QueueSection", …)` block, before its closing `})`):

```ts
  it("renders a progress bar and percent for a downloading grabbed row", () => {
    vi.mocked(api.useQueue).mockReturnValue({
      data: [row({ status: "grabbed", progress: 42.5, downloadStatus: "downloading" })],
      isLoading: false, isError: false,
    } as never)
    renderQueue()
    const bar = screen.getByRole("progressbar")
    expect(bar).toHaveAttribute("aria-valuenow", "43")
    expect(screen.getByText("43%")).toBeInTheDocument()
    expect(screen.getByText("Downloading")).toBeInTheDocument()
  })

  it("renders no progress bar for a grabbed row with no live match", () => {
    vi.mocked(api.useQueue).mockReturnValue({
      data: [row({ status: "grabbed" })], isLoading: false, isError: false,
    } as never)
    renderQueue()
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument()
    expect(screen.getByText("Grabbed")).toBeInTheDocument()
  })

  it("renders no progress bar for an importing row even if live data is present", () => {
    vi.mocked(api.useQueue).mockReturnValue({
      data: [row({ status: "importing", progress: 90, downloadStatus: "downloading" })],
      isLoading: false, isError: false,
    } as never)
    renderQueue()
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument()
    expect(screen.getByText("Importing")).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/activity/QueueSection.test.tsx`
Expected: FAIL — no `progressbar` role / "43%" not found (the Status cell currently only prints `statusLabel(r.status)`).

- [ ] **Step 3: Write minimal implementation**

In `web/src/features/activity/QueueSection.tsx`:

(a) Extend the import from `./resolve` to include `queueRowDisplay`:

```ts
import {
  movieTitleMap, seriesTitleMap, resolveTitle, qualityName, statusLabel, statusTone, queueRowDisplay, type Tone,
} from "./resolve"
```

(b) Replace the Status `<td>` (currently:
`<td className={\`py-2.5 pr-4 font-semibold ${toneClass[statusTone(r.status)]}\`}>{statusLabel(r.status)}</td>`)
with a block that branches on the resolver. Add, just after `const title = resolveTitle(r, movieMap, seriesMap)` inside the `.map`, a `const disp = queueRowDisplay(r)`, then render:

```tsx
                <td className="py-2.5 pr-4">
                  {disp.kind === "live" ? (
                    <div className="min-w-[7rem]">
                      <div className="mb-1 flex items-center justify-between gap-2">
                        <span className={`text-xs font-semibold ${toneClass[disp.tone]}`}>{disp.label}</span>
                        <span className="text-xs tabular-nums text-[var(--color-muted)]">{Math.round(disp.percent)}%</span>
                      </div>
                      <div className="h-1.5 w-full overflow-hidden rounded bg-[var(--color-border)]">
                        <div
                          role="progressbar"
                          aria-valuenow={Math.round(disp.percent)}
                          aria-valuemin={0}
                          aria-valuemax={100}
                          className="h-full rounded bg-[var(--color-brand)]"
                          style={{ width: `${Math.round(disp.percent)}%` }}
                        />
                      </div>
                    </div>
                  ) : (
                    <span className={`font-semibold ${toneClass[disp.tone]}`}>{disp.label}</span>
                  )}
                </td>
```

`statusLabel` / `statusTone` may now be unused in this file — if `tsc`/eslint flags them, drop them from the `./resolve` import (keep only what remains referenced: `movieTitleMap, seriesTitleMap, resolveTitle, qualityName, queueRowDisplay, type Tone`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run src/features/activity/QueueSection.test.tsx`
Expected: PASS (all QueueSection tests, old and new).

- [ ] **Step 5: Typecheck**

Run: `cd web && npx tsc -b`
Expected: exit 0, no errors (fix any unused-import error as noted in Step 3).

- [ ] **Step 6: Commit**

```bash
git add web/src/features/activity/QueueSection.tsx web/src/features/activity/QueueSection.test.tsx
git commit -m "feat(webui): render live download progress bar in Activity queue"
```

---

### Task 4: Scheduler 30s cadence + dist rebuild + full verify

**Files:**
- Modify: `cmd/nexus/main.go:155` and `cmd/nexus/main.go:157`
- Modify: `web/dist/**` (rebuilt embedded assets)

**Interfaces:**
- Consumes: everything from Tasks 1-3.
- Produces: no new symbols — final wiring + build artifacts.

- [ ] **Step 1: Change the two pipeline intervals to 30s**

In `cmd/nexus/main.go`, change the download-monitor and import-reconcile schedules from `1*time.Minute` to `30*time.Second`:

Line 155 — from:
```go
	sch.Every(1*time.Minute, func() command.Command { return dlMonitor })
```
to:
```go
	sch.Every(30*time.Second, func() command.Command { return dlMonitor })
```

Line 157 — from:
```go
	sch.Every(1*time.Minute, func() command.Command { return importCmd })
```
to:
```go
	sch.Every(30*time.Second, func() command.Command { return importCmd })
```

Leave the health check (15m), media refresh (12h), and automation searches (hours) unchanged.

- [ ] **Step 2: Verify the change and that it compiles**

Run: `grep -n "30\*time.Second" cmd/nexus/main.go && export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go build ./cmd/nexus`
Expected: two matching lines printed (dlMonitor + importCmd), build exits 0.

- [ ] **Step 3: Rebuild the embedded web assets**

Run: `cd web && npm run build`
Expected: `tsc -b` clean + `vite build` writes fresh `web/dist/assets/*` and `web/dist/index.html`.

- [ ] **Step 4: Full verification suite**

Run each and confirm all green:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./...
cd web && npm test && npx tsc -b
```
Expected: Go build/vet/test all pass across every package (incl. `web/spa_test.go` serving the rebuilt `dist`); vitest all pass; `tsc -b` exit 0.

- [ ] **Step 5: Commit**

```bash
git add cmd/nexus/main.go web/dist
git commit -m "feat: 30s download-monitor + import cadence; rebuild web/dist for queue progress"
```

- [ ] **Step 6: Live browser AC (manual, after merge review)**

Seed a throwaway instance (per project convention: a `cmd/acseed` store-seeder that creates a movie + a `grabbed` `download_queue` row whose `client_item_id` matches a fake live download item, served on a spare port with admin/admin), then verify in the browser:
1. A grabbed row that matches a downloading live item shows a progress bar + percent + "Downloading".
2. A grabbed row with no live match shows plain "Grabbed" and no bar.
3. No console errors.

Remove the seeder + throwaway binary/data afterward.

---

## Notes for the executor

- This slice adds **no** migration, route, dependency, or composition-root wiring. If a task seems to require one, stop and re-read the spec §2 (out of scope).
- The backend enrichment reuses `matchItem` verbatim — do not reimplement matching.
- Graceful degradation is inherited from `downloadclient.Service.Queue` (a dead client yields fewer live items → unenriched rows). No error handling is added in `listQueue` for the live snapshot; `Queue(ctx)` has no error return.
