# Nexus Web UI — Activity Slice 4 (Queue + History) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `/activity` placeholder with a real Activity page — Queue and History tabs — built entirely over Nexus's existing `/queue` and `/history` REST endpoints, with client-side title/quality resolution and WS-driven live refresh.

**Architecture:** A new `web/src/features/activity/` module mirroring `features/settings/`: pure resolution helpers (`resolve.ts`) unit-tested in isolation, thin TanStack Query hooks (`api.ts`), a tabbed `ActivityLayout` with nested routes, and two table components (`QueueSection`, `HistorySection`). No backend changes. Live refresh reuses the existing `useActivity()` WS ring buffer to invalidate queries.

**Tech Stack:** React 19 + TypeScript + Tailwind v4 (CSS-first tokens) + TanStack Query v5 + react-router v6 + Vitest + Testing Library. Served by the Go binary via the committed `web/dist` embed.

## Global Constraints

- **No backend changes.** UI only, over existing endpoints. No new Go files, routes, DTOs, or migrations.
- **TypeScript wire shape is numeric.** `QueueItem.qualityId: number`, `HistoryEvent.qualityId: number | null`, `QualityDefinition.id: number`. Test fixtures MUST use the real numeric shape (repeated 3b lesson — string fixtures hid a real bug).
- **`mediaKind` wire values are `"movie"` and `"tv"`** — NOT `"series"`. The backend enum is `provider.KindTV = "tv"` / `KindMovie = "movie"` (`internal/core/provider/provider.go`); `store.QueueItem`/`HistoryEvent` marshal it verbatim. All `mediaKind` comparisons and test fixtures MUST use `"tv"` for series/TV rows.
- **Styling:** existing dark CSS tokens only (`var(--color-border)`, `--color-muted`, `--color-panel`, `--color-fg`, `--color-ok`, `--color-warn`, `--color-brand`). No new global CSS.
- **User feedback:** existing hand-written toast (`useToast()` → `toast(msg, { variant: "error" })`), inside `ToastProvider` (already wraps the app).
- **Query keys:** `["queue"]` and `["history"]` (distinct from the library module's `["library", ...]` keys — no collision).
- **Verification env:** `export PATH="/c/Program Files/Go/bin:$PATH"` for any Go command. Node available for `npm run test` / `npm run build`. Run all frontend commands from `web/`.
- **Model:** implementers and reviewers run on **sonnet**, never haiku (repo lesson).
- Final task rebuilds committed `web/dist`; the drift guard `git diff --exit-code web/dist` must be clean.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `web/src/features/activity/types.ts` | TS mirrors of `QueueItem`, `HistoryEvent` (numeric wire shape) | T1 |
| `web/src/features/activity/resolve.ts` | Pure helpers: title maps, `resolveTitle`, `qualityName`, `eventLabel`, `statusLabel`, `statusTone`, `shouldRefresh` | T1 |
| `web/src/features/activity/resolve.test.ts` | Unit tests for `resolve.ts` | T1 |
| `web/src/features/activity/api.ts` | Hooks: `useQueue`, `useHistory`, `useImportItem`, `useRemoveQueueItem`, `useActivityInvalidation` | T2 |
| `web/src/features/activity/QueueSection.tsx` | Queue table + row actions | T3 |
| `web/src/features/activity/QueueSection.test.tsx` | QueueSection tests | T3 |
| `web/src/features/activity/HistorySection.tsx` | History table | T4 |
| `web/src/features/activity/HistorySection.test.tsx` | HistorySection tests | T4 |
| `web/src/features/activity/ActivityLayout.tsx` | Tab shell + WS-invalidation mount | T5 |
| `web/src/features/activity/ActivityLayout.test.tsx` | Tab link test | T5 |
| `web/src/app/routes.tsx` (modify) | Replace `/activity` placeholder with nested routes | T5 |
| `web/dist/**` (rebuild) | Committed embed bundle | T6 |

---

### Task 1: Types + pure resolution helpers

**Files:**
- Create: `web/src/features/activity/types.ts`
- Create: `web/src/features/activity/resolve.ts`
- Test: `web/src/features/activity/resolve.test.ts`

**Interfaces:**
- Consumes: `Movie`, `Series` from `@/features/library/types`; `QualityDefinition` from `@/features/settings/qualityTypes`.
- Produces:
  - `QueueItem`, `HistoryEvent` (types).
  - `movieTitleMap(movies?: Movie[]): Map<number, string>`
  - `seriesTitleMap(series?: Series[]): Map<number, string>`
  - `resolveTitle(row: TitleRow, movieMap: Map<number,string>, seriesMap: Map<number,string>): string` where `TitleRow = { mediaKind: string; movieId?: number; seriesId?: number; sourceTitle?: string }`
  - `qualityName(id: number | null | undefined, defs?: QualityDefinition[]): string`
  - `eventLabel(t: string): string`
  - `statusLabel(s: string): string`
  - `statusTone(s: string): "neutral" | "info" | "ok" | "error"`
  - `shouldRefresh(type: string): boolean`

- [ ] **Step 1: Write `types.ts`**

```ts
// web/src/features/activity/types.ts
export type QueueItem = {
  id: number
  downloadClientId: string
  clientItemId: string
  protocol: string
  sourceTitle: string
  mediaKind: string
  seriesId?: number
  movieId?: number
  episodeIds: number[]
  qualityId: number
  status: string
  error?: string
  createdAt: string
  updatedAt: string
}

export type HistoryEvent = {
  id: number
  eventType: string
  mediaKind: string
  seriesId?: number
  episodeId?: number
  movieId?: number
  sourceTitle?: string
  qualityId?: number | null
  message?: string
  createdAt: string
}
```

- [ ] **Step 2: Write the failing test `resolve.test.ts`**

```ts
// web/src/features/activity/resolve.test.ts
import { describe, it, expect } from "vitest"
import type { Movie, Series } from "@/features/library/types"
import type { QualityDefinition } from "@/features/settings/qualityTypes"
import {
  movieTitleMap, seriesTitleMap, resolveTitle, qualityName,
  eventLabel, statusLabel, statusTone, shouldRefresh,
} from "./resolve"

const movies = [
  { id: 1, title: "The Matrix", year: 1999 },
  { id: 2, title: "No Year", year: 0 },
] as Movie[]
const series = [{ id: 10, title: "The Show", year: 2015 }] as Series[]
const defs: QualityDefinition[] = [
  { id: 0, name: "Unknown", source: 0, resolution: 0, rank: 0 },
  { id: 3, name: "WEBDL-1080p", source: 1, resolution: 3, rank: 3 },
]

describe("title maps", () => {
  it("formats movie title with year, bare title when year is 0", () => {
    const m = movieTitleMap(movies)
    expect(m.get(1)).toBe("The Matrix (1999)")
    expect(m.get(2)).toBe("No Year")
  })
  it("maps series id to plain title", () => {
    expect(seriesTitleMap(series).get(10)).toBe("The Show")
  })
  it("returns an empty map for undefined input", () => {
    expect(movieTitleMap(undefined).size).toBe(0)
    expect(seriesTitleMap(undefined).size).toBe(0)
  })
})

describe("resolveTitle", () => {
  const mm = movieTitleMap(movies)
  const sm = seriesTitleMap(series)
  it("resolves a movie row to the clean title", () => {
    expect(resolveTitle({ mediaKind: "movie", movieId: 1, sourceTitle: "raw" }, mm, sm)).toBe("The Matrix (1999)")
  })
  it("resolves a series row to the clean title", () => {
    expect(resolveTitle({ mediaKind: "tv", seriesId: 10, sourceTitle: "raw" }, mm, sm)).toBe("The Show")
  })
  it("falls back to sourceTitle when the id is missing (deleted media)", () => {
    expect(resolveTitle({ mediaKind: "movie", movieId: 999, sourceTitle: "Some.Release" }, mm, sm)).toBe("Some.Release")
  })
  it("falls back to sourceTitle when maps are empty (still loading)", () => {
    expect(resolveTitle({ mediaKind: "movie", movieId: 1, sourceTitle: "Some.Release" }, new Map(), new Map())).toBe("Some.Release")
  })
  it("falls back to sourceTitle when there is no media id", () => {
    expect(resolveTitle({ mediaKind: "movie", sourceTitle: "Untracked.Release" }, mm, sm)).toBe("Untracked.Release")
  })
  it("returns em dash when sourceTitle is also empty", () => {
    expect(resolveTitle({ mediaKind: "movie", sourceTitle: "" }, mm, sm)).toBe("—")
  })
})

describe("qualityName", () => {
  it("resolves a numeric id to its name", () => {
    expect(qualityName(3, defs)).toBe("WEBDL-1080p")
  })
  it("returns em dash for null, 0, or unknown id", () => {
    expect(qualityName(null, defs)).toBe("—")
    expect(qualityName(0, defs)).toBe("—")
    expect(qualityName(99, defs)).toBe("—")
    expect(qualityName(3, undefined)).toBe("—")
  })
})

describe("labels and tones", () => {
  it("maps event types to labels", () => {
    expect(eventLabel("grabbed")).toBe("Grabbed")
    expect(eventLabel("imported")).toBe("Imported")
    expect(eventLabel("upgraded")).toBe("Upgraded")
    expect(eventLabel("import_failed")).toBe("Import failed")
    expect(eventLabel("weird")).toBe("weird")
  })
  it("maps queue statuses to labels", () => {
    expect(statusLabel("grabbed")).toBe("Grabbed")
    expect(statusLabel("importing")).toBe("Importing")
    expect(statusLabel("imported")).toBe("Imported")
    expect(statusLabel("failed")).toBe("Failed")
  })
  it("maps statuses to tones", () => {
    expect(statusTone("imported")).toBe("ok")
    expect(statusTone("importing")).toBe("info")
    expect(statusTone("failed")).toBe("error")
    expect(statusTone("grabbed")).toBe("neutral")
  })
})

describe("shouldRefresh", () => {
  it("is true for queue/import/download events", () => {
    expect(shouldRefresh("queue.updated")).toBe(true)
    expect(shouldRefresh("import.completed")).toBe(true)
    expect(shouldRefresh("download.status")).toBe(true)
  })
  it("is false for unrelated events", () => {
    expect(shouldRefresh("indexer.status")).toBe(false)
    expect(shouldRefresh("")).toBe(false)
  })
})
```

- [ ] **Step 3: Run the test to verify it fails**

Run (from `web/`): `npm run test -- resolve`
Expected: FAIL — cannot resolve `./resolve` / functions not exported.

- [ ] **Step 4: Write `resolve.ts` to make it pass**

```ts
// web/src/features/activity/resolve.ts
import type { Movie, Series } from "@/features/library/types"
import type { QualityDefinition } from "@/features/settings/qualityTypes"

export type TitleRow = {
  mediaKind: string
  movieId?: number
  seriesId?: number
  sourceTitle?: string
}

export function movieTitleMap(movies?: Movie[]): Map<number, string> {
  const m = new Map<number, string>()
  for (const mv of movies ?? []) {
    m.set(mv.id, mv.year > 0 ? `${mv.title} (${mv.year})` : mv.title)
  }
  return m
}

export function seriesTitleMap(series?: Series[]): Map<number, string> {
  const m = new Map<number, string>()
  for (const s of series ?? []) m.set(s.id, s.title)
  return m
}

export function resolveTitle(
  row: TitleRow,
  movieMap: Map<number, string>,
  seriesMap: Map<number, string>,
): string {
  if (row.mediaKind === "movie" && row.movieId != null) {
    const t = movieMap.get(row.movieId)
    if (t) return t
  }
  if (row.mediaKind === "tv" && row.seriesId != null) {
    const t = seriesMap.get(row.seriesId)
    if (t) return t
  }
  return row.sourceTitle && row.sourceTitle.length > 0 ? row.sourceTitle : "—"
}

export function qualityName(
  id: number | null | undefined,
  defs?: QualityDefinition[],
): string {
  if (id == null || id === 0) return "—"
  const d = (defs ?? []).find((q) => q.id === id)
  return d ? d.name : "—"
}

const EVENT_LABELS: Record<string, string> = {
  grabbed: "Grabbed",
  imported: "Imported",
  upgraded: "Upgraded",
  import_failed: "Import failed",
}
export function eventLabel(t: string): string {
  return EVENT_LABELS[t] ?? t
}

const STATUS_LABELS: Record<string, string> = {
  grabbed: "Grabbed",
  importing: "Importing",
  imported: "Imported",
  failed: "Failed",
}
export function statusLabel(s: string): string {
  return STATUS_LABELS[s] ?? s
}

export type Tone = "neutral" | "info" | "ok" | "error"
export function statusTone(s: string): Tone {
  switch (s) {
    case "imported":
      return "ok"
    case "importing":
      return "info"
    case "failed":
      return "error"
    default:
      return "neutral"
  }
}

const REFRESH_EVENTS = new Set(["queue.updated", "import.completed", "download.status"])
export function shouldRefresh(type: string): boolean {
  return REFRESH_EVENTS.has(type)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run (from `web/`): `npm run test -- resolve`
Expected: PASS (all `resolve.test.ts` cases green).

- [ ] **Step 6: Typecheck**

Run (from `web/`): `npm run build` (or `npx tsc -b`)
Expected: exit 0, no type errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/activity/types.ts web/src/features/activity/resolve.ts web/src/features/activity/resolve.test.ts
git commit -m "feat(6-4): activity types + pure resolution helpers"
```

---

### Task 2: TanStack Query hooks

**Files:**
- Create: `web/src/features/activity/api.ts`

**Interfaces:**
- Consumes: `apiGet`, `apiPost`, `apiDelete` from `@/lib/api`; `useActivity` from `@/lib/activity`; `shouldRefresh` from `./resolve`; `QueueItem`, `HistoryEvent` from `./types`.
- Produces:
  - `activityKeys = { queue: ["queue"], history: ["history"] }`
  - `useQueue()` → query of `QueueItem[]`
  - `useHistory()` → query of `HistoryEvent[]`
  - `useImportItem()` → mutation `(id: number) => Promise<{ ok: boolean }>`, invalidates queue + history on success
  - `useRemoveQueueItem()` → mutation `(id: number) => Promise<{ ok: boolean }>`, invalidates queue on success
  - `useActivityInvalidation(): void` — mount-once hook; invalidates queue + history when a refresh-worthy WS event arrives

Note: these hooks follow the repo convention (`features/settings/configApi.ts`) and are verified by `tsc` here and exercised via mocks in the T3/T4 section tests and the T6 live browser check — they are not unit-tested in isolation (matches the existing settings/library api hooks).

- [ ] **Step 1: Write `api.ts`**

```ts
// web/src/features/activity/api.ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useEffect } from "react"
import { apiGet, apiPost, apiDelete } from "@/lib/api"
import { useActivity } from "@/lib/activity"
import { shouldRefresh } from "./resolve"
import type { QueueItem, HistoryEvent } from "./types"

export const activityKeys = {
  queue: ["queue"] as const,
  history: ["history"] as const,
}

export function useQueue() {
  return useQuery({ queryKey: activityKeys.queue, queryFn: () => apiGet<QueueItem[]>("/queue") })
}

export function useHistory() {
  return useQuery({
    queryKey: activityKeys.history,
    queryFn: () => apiGet<HistoryEvent[]>("/history?limit=100"),
  })
}

export function useImportItem() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiPost<{ ok: boolean }>(`/queue/${id}/import`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: activityKeys.queue })
      qc.invalidateQueries({ queryKey: activityKeys.history })
    },
  })
}

export function useRemoveQueueItem() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/queue/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.queue }),
  })
}

export function useActivityInvalidation(): void {
  const events = useActivity()
  const qc = useQueryClient()
  const latest = events[0]
  useEffect(() => {
    if (latest && shouldRefresh(latest.type)) {
      qc.invalidateQueries({ queryKey: activityKeys.queue })
      qc.invalidateQueries({ queryKey: activityKeys.history })
    }
    // keyed on the latest event id so it fires once per new event
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [latest?.id])
}
```

- [ ] **Step 2: Typecheck**

Run (from `web/`): `npm run build` (or `npx tsc -b`)
Expected: exit 0, no type errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/features/activity/api.ts
git commit -m "feat(6-4): activity query hooks (queue, history, import, remove, ws-invalidate)"
```

---

### Task 3: Queue tab component

**Files:**
- Create: `web/src/features/activity/QueueSection.tsx`
- Test: `web/src/features/activity/QueueSection.test.tsx`

**Interfaces:**
- Consumes: `useQueue`, `useImportItem`, `useRemoveQueueItem` from `./api`; `useMovies`, `useSeries` from `@/features/library/api`; `useQualityDefinitions` from `@/features/settings/qualityApi`; `useToast` from `@/lib/toast`; `ApiError` from `@/lib/api`; `relativeTime` from `@/lib/time`; helpers from `./resolve`.
- Produces: `QueueSection` component (default export style: named `export function QueueSection()`).

- [ ] **Step 1: Write the failing test `QueueSection.test.tsx`**

```tsx
// web/src/features/activity/QueueSection.test.tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { QueueSection } from "./QueueSection"
import * as api from "./api"
import * as libApi from "@/features/library/api"
import * as qualityApi from "@/features/settings/qualityApi"
import type { QueueItem } from "./types"

vi.mock("./api")
vi.mock("@/features/library/api")
vi.mock("@/features/settings/qualityApi")

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

function row(over: Partial<QueueItem>): QueueItem {
  return {
    id: 1, downloadClientId: "", clientItemId: "x", protocol: "usenet",
    sourceTitle: "The.Matrix.1999.1080p", mediaKind: "movie", movieId: 1,
    episodeIds: [], qualityId: 3, status: "grabbed", createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(), ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(libApi.useMovies).mockReturnValue({ data: [{ id: 1, title: "The Matrix", year: 1999 }] } as never)
  vi.mocked(libApi.useSeries).mockReturnValue({ data: [] } as never)
  vi.mocked(qualityApi.useQualityDefinitions).mockReturnValue({ data: [{ id: 3, name: "WEBDL-1080p", source: 1, resolution: 3, rank: 3 }] } as never)
  vi.mocked(api.useImportItem).mockReturnValue(mut())
  vi.mocked(api.useRemoveQueueItem).mockReturnValue(mut())
})

function renderQueue() {
  render(<ToastProvider><QueueSection /></ToastProvider>)
}

describe("QueueSection", () => {
  it("shows an empty state when the queue is empty", () => {
    vi.mocked(api.useQueue).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    renderQueue()
    expect(screen.getByText(/queue is empty/i)).toBeInTheDocument()
  })

  it("renders resolved title, sourceTitle subtext and quality", () => {
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({})], isLoading: false, isError: false } as never)
    renderQueue()
    expect(screen.getByText("The Matrix (1999)")).toBeInTheDocument()
    expect(screen.getByText("The.Matrix.1999.1080p")).toBeInTheDocument()
    expect(screen.getByText("WEBDL-1080p")).toBeInTheDocument()
  })

  it("shows the Import button on grabbed and failed rows only", () => {
    vi.mocked(api.useQueue).mockReturnValue({
      data: [
        row({ id: 1, status: "grabbed" }),
        row({ id: 2, status: "failed", error: "no space" }),
        row({ id: 3, status: "importing" }),
        row({ id: 4, status: "imported" }),
      ],
      isLoading: false, isError: false,
    } as never)
    renderQueue()
    expect(screen.getAllByRole("button", { name: /import/i })).toHaveLength(2)
  })

  it("shows the error text on a failed row", () => {
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ status: "failed", error: "disk full" })], isLoading: false, isError: false } as never)
    renderQueue()
    expect(screen.getByText("disk full")).toBeInTheDocument()
  })

  it("removes a row after confirm", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useRemoveQueueItem).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ id: 7 })], isLoading: false, isError: false } as never)
    vi.spyOn(window, "confirm").mockReturnValue(true)
    renderQueue()
    await userEvent.click(screen.getByRole("button", { name: /remove/i }))
    expect(mutate).toHaveBeenCalledWith(7, expect.anything())
  })

  it("does not remove when confirm is cancelled", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useRemoveQueueItem).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ id: 7 })], isLoading: false, isError: false } as never)
    vi.spyOn(window, "confirm").mockReturnValue(false)
    renderQueue()
    await userEvent.click(screen.getByRole("button", { name: /remove/i }))
    expect(mutate).not.toHaveBeenCalled()
  })

  it("surfaces an import error as a toast", async () => {
    const mutate = vi.fn((_id, opts) => opts.onError(new ApiError(400, "rejected", "quality not in profile")))
    vi.mocked(api.useImportItem).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ id: 5, status: "failed", error: "x" })], isLoading: false, isError: false } as never)
    renderQueue()
    await userEvent.click(screen.getByRole("button", { name: /import/i }))
    expect(await screen.findByText(/quality not in profile/i)).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run (from `web/`): `npm run test -- QueueSection`
Expected: FAIL — cannot resolve `./QueueSection`.

- [ ] **Step 3: Write `QueueSection.tsx`**

```tsx
// web/src/features/activity/QueueSection.tsx
import { ApiError } from "@/lib/api"
import { useToast } from "@/lib/toast"
import { relativeTime } from "@/lib/time"
import { useMovies, useSeries } from "@/features/library/api"
import { useQualityDefinitions } from "@/features/settings/qualityApi"
import { useQueue, useImportItem, useRemoveQueueItem } from "./api"
import {
  movieTitleMap, seriesTitleMap, resolveTitle, qualityName, statusLabel, statusTone, type Tone,
} from "./resolve"

const toneClass: Record<Tone, string> = {
  ok: "text-[var(--color-ok)]",
  info: "text-[var(--color-brand)]",
  error: "text-[var(--color-warn)]",
  neutral: "text-[var(--color-muted)]",
}

export function QueueSection() {
  const queue = useQueue()
  const movies = useMovies()
  const series = useSeries()
  const defs = useQualityDefinitions()
  const importItem = useImportItem()
  const removeItem = useRemoveQueueItem()
  const { toast } = useToast()

  if (queue.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading queue…</div>
  if (queue.isError) return <div className="p-6 text-sm text-[var(--color-warn)]">Failed to load queue.</div>

  const rows = queue.data ?? []
  if (rows.length === 0) return <div className="p-6 text-sm text-[var(--color-muted)]">Queue is empty.</div>

  const movieMap = movieTitleMap(movies.data)
  const seriesMap = seriesTitleMap(series.data)

  const onImport = (id: number) =>
    importItem.mutate(id, {
      onSuccess: () => toast("Import started"),
      onError: (e) => toast(e instanceof ApiError ? e.message : "Import failed", { variant: "error" }),
    })

  const onRemove = (id: number) => {
    if (!window.confirm("Remove this item from the queue?")) return
    removeItem.mutate(id, {
      onSuccess: () => toast("Removed from queue"),
      onError: (e) => toast(e instanceof ApiError ? e.message : "Remove failed", { variant: "error" }),
    })
  }

  return (
    <div className="p-6">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-[var(--color-border)] text-left text-xs uppercase tracking-wide text-[var(--color-muted)]">
            <th className="py-2 pr-4">Media</th>
            <th className="py-2 pr-4">Kind</th>
            <th className="py-2 pr-4">Quality</th>
            <th className="py-2 pr-4">Protocol</th>
            <th className="py-2 pr-4">Status</th>
            <th className="py-2 pr-4">Added</th>
            <th className="py-2 pr-4 text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.id} className="border-b border-[var(--color-border)] align-top last:border-b-0">
              <td className="py-2.5 pr-4">
                <div className="font-medium">{resolveTitle(r, movieMap, seriesMap)}</div>
                <div className="truncate text-xs text-[var(--color-muted)]">{r.sourceTitle}</div>
                {r.status === "failed" && r.error ? (
                  <div className="text-xs text-[var(--color-warn)]">{r.error}</div>
                ) : null}
              </td>
              <td className="py-2.5 pr-4 text-[var(--color-muted)]">{r.mediaKind}</td>
              <td className="py-2.5 pr-4">{qualityName(r.qualityId, defs.data)}</td>
              <td className="py-2.5 pr-4 text-[var(--color-muted)]">{r.protocol}</td>
              <td className={`py-2.5 pr-4 font-semibold ${toneClass[statusTone(r.status)]}`}>{statusLabel(r.status)}</td>
              <td className="whitespace-nowrap py-2.5 pr-4 text-[var(--color-muted)]">
                {relativeTime(new Date(r.createdAt).getTime())}
              </td>
              <td className="whitespace-nowrap py-2.5 pr-4 text-right">
                {(r.status === "failed" || r.status === "grabbed") && (
                  <button
                    onClick={() => onImport(r.id)}
                    className="mr-2 rounded border border-[var(--color-border)] px-2 py-1 text-xs hover:border-[var(--color-brand)]"
                  >
                    Import
                  </button>
                )}
                <button
                  onClick={() => onRemove(r.id)}
                  className="rounded border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-warn)] hover:border-[var(--color-warn)]"
                >
                  Remove
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run (from `web/`): `npm run test -- QueueSection`
Expected: PASS (all cases green).

- [ ] **Step 5: Typecheck**

Run (from `web/`): `npm run build` (or `npx tsc -b`)
Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/activity/QueueSection.tsx web/src/features/activity/QueueSection.test.tsx
git commit -m "feat(6-4): Queue tab with resolved titles, status, import/remove actions"
```

---

### Task 4: History tab component

**Files:**
- Create: `web/src/features/activity/HistorySection.tsx`
- Test: `web/src/features/activity/HistorySection.test.tsx`

**Interfaces:**
- Consumes: `useHistory` from `./api`; `useMovies`, `useSeries` from `@/features/library/api`; `useQualityDefinitions` from `@/features/settings/qualityApi`; `relativeTime` from `@/lib/time`; helpers from `./resolve`.
- Produces: `HistorySection` component.

- [ ] **Step 1: Write the failing test `HistorySection.test.tsx`**

```tsx
// web/src/features/activity/HistorySection.test.tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { HistorySection } from "./HistorySection"
import * as api from "./api"
import * as libApi from "@/features/library/api"
import * as qualityApi from "@/features/settings/qualityApi"
import type { HistoryEvent } from "./types"

vi.mock("./api")
vi.mock("@/features/library/api")
vi.mock("@/features/settings/qualityApi")

function ev(over: Partial<HistoryEvent>): HistoryEvent {
  return {
    id: 1, eventType: "grabbed", mediaKind: "movie", movieId: 1,
    sourceTitle: "The.Matrix.1999", qualityId: 3, message: "grabbed from nzb",
    createdAt: new Date().toISOString(), ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(libApi.useMovies).mockReturnValue({ data: [{ id: 1, title: "The Matrix", year: 1999 }] } as never)
  vi.mocked(libApi.useSeries).mockReturnValue({ data: [] } as never)
  vi.mocked(qualityApi.useQualityDefinitions).mockReturnValue({ data: [{ id: 3, name: "WEBDL-1080p", source: 1, resolution: 3, rank: 3 }] } as never)
})

describe("HistorySection", () => {
  it("shows an empty state when there is no history", () => {
    vi.mocked(api.useHistory).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    render(<HistorySection />)
    expect(screen.getByText(/no history yet/i)).toBeInTheDocument()
  })

  it("renders event label, resolved title and quality", () => {
    vi.mocked(api.useHistory).mockReturnValue({
      data: [ev({ eventType: "imported", qualityId: 3 }), ev({ id: 2, eventType: "import_failed", qualityId: null, message: "rejected" })],
      isLoading: false, isError: false,
    } as never)
    render(<HistorySection />)
    expect(screen.getByText("Imported")).toBeInTheDocument()
    expect(screen.getByText("Import failed")).toBeInTheDocument()
    expect(screen.getAllByText("The Matrix (1999)").length).toBeGreaterThan(0)
    expect(screen.getByText("WEBDL-1080p")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run (from `web/`): `npm run test -- HistorySection`
Expected: FAIL — cannot resolve `./HistorySection`.

- [ ] **Step 3: Write `HistorySection.tsx`**

```tsx
// web/src/features/activity/HistorySection.tsx
import { relativeTime } from "@/lib/time"
import { useMovies, useSeries } from "@/features/library/api"
import { useQualityDefinitions } from "@/features/settings/qualityApi"
import { useHistory } from "./api"
import { movieTitleMap, seriesTitleMap, resolveTitle, qualityName, eventLabel } from "./resolve"

export function HistorySection() {
  const history = useHistory()
  const movies = useMovies()
  const series = useSeries()
  const defs = useQualityDefinitions()

  if (history.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading history…</div>
  if (history.isError) return <div className="p-6 text-sm text-[var(--color-warn)]">Failed to load history.</div>

  const rows = history.data ?? []
  if (rows.length === 0) return <div className="p-6 text-sm text-[var(--color-muted)]">No history yet.</div>

  const movieMap = movieTitleMap(movies.data)
  const seriesMap = seriesTitleMap(series.data)

  return (
    <div className="p-6">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-[var(--color-border)] text-left text-xs uppercase tracking-wide text-[var(--color-muted)]">
            <th className="py-2 pr-4">Event</th>
            <th className="py-2 pr-4">Media</th>
            <th className="py-2 pr-4">Quality</th>
            <th className="py-2 pr-4">Message</th>
            <th className="py-2 pr-4">Time</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((h) => (
            <tr key={h.id} className="border-b border-[var(--color-border)] align-top last:border-b-0">
              <td className={`py-2.5 pr-4 font-semibold ${h.eventType === "import_failed" ? "text-[var(--color-warn)]" : "text-[var(--color-fg)]"}`}>
                {eventLabel(h.eventType)}
              </td>
              <td className="py-2.5 pr-4">
                <div className="font-medium">{resolveTitle(h, movieMap, seriesMap)}</div>
                {h.sourceTitle ? <div className="truncate text-xs text-[var(--color-muted)]">{h.sourceTitle}</div> : null}
              </td>
              <td className="py-2.5 pr-4">{qualityName(h.qualityId, defs.data)}</td>
              <td className="py-2.5 pr-4 text-[var(--color-muted)]">{h.message || "—"}</td>
              <td className="whitespace-nowrap py-2.5 pr-4 text-[var(--color-muted)]">
                {relativeTime(new Date(h.createdAt).getTime())}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run (from `web/`): `npm run test -- HistorySection`
Expected: PASS.

- [ ] **Step 5: Typecheck**

Run (from `web/`): `npm run build` (or `npx tsc -b`)
Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/activity/HistorySection.tsx web/src/features/activity/HistorySection.test.tsx
git commit -m "feat(6-4): History tab listing recent events"
```

---

### Task 5: ActivityLayout + routes wiring

**Files:**
- Create: `web/src/features/activity/ActivityLayout.tsx`
- Test: `web/src/features/activity/ActivityLayout.test.tsx`
- Modify: `web/src/app/routes.tsx`

**Interfaces:**
- Consumes: `NavLink`, `Outlet` from `react-router-dom`; `cn` from `@/lib/utils`; `useActivityInvalidation` from `./api`.
- Produces: `ActivityLayout` component; nested `/activity` routes.

- [ ] **Step 1: Write the failing test `ActivityLayout.test.tsx`**

```tsx
// web/src/features/activity/ActivityLayout.test.tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { ActivityLayout } from "./ActivityLayout"

vi.mock("./api", () => ({ useActivityInvalidation: vi.fn() }))

describe("ActivityLayout", () => {
  it("renders Queue and History tab links", () => {
    render(
      <MemoryRouter initialEntries={["/activity/queue"]}>
        <ActivityLayout />
      </MemoryRouter>,
    )
    const queue = screen.getByRole("link", { name: /queue/i })
    const history = screen.getByRole("link", { name: /history/i })
    expect(queue).toHaveAttribute("href", "/activity/queue")
    expect(history).toHaveAttribute("href", "/activity/history")
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run (from `web/`): `npm run test -- ActivityLayout`
Expected: FAIL — cannot resolve `./ActivityLayout`.

- [ ] **Step 3: Write `ActivityLayout.tsx`**

```tsx
// web/src/features/activity/ActivityLayout.tsx
import { NavLink, Outlet } from "react-router-dom"
import { cn } from "@/lib/utils"
import { useActivityInvalidation } from "./api"

const TABS: { to: string; label: string }[] = [
  { to: "/activity/queue", label: "Queue" },
  { to: "/activity/history", label: "History" },
]

export function ActivityLayout() {
  useActivityInvalidation()
  return (
    <div>
      <div className="border-b border-[var(--color-border)] px-6 pt-6">
        <h1 className="mb-3 text-2xl font-bold">Activity</h1>
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

- [ ] **Step 4: Run the test to verify it passes**

Run (from `web/`): `npm run test -- ActivityLayout`
Expected: PASS.

- [ ] **Step 5: Wire the routes — modify `web/src/app/routes.tsx`**

Add imports near the other feature imports (after the settings imports):

```tsx
import { ActivityLayout } from "@/features/activity/ActivityLayout"
import { QueueSection } from "@/features/activity/QueueSection"
import { HistorySection } from "@/features/activity/HistorySection"
```

Replace this line:

```tsx
      { path: "activity", element: <Placeholder title="Activity" /> },
```

with:

```tsx
      {
        path: "activity",
        element: <ActivityLayout />,
        children: [
          { index: true, element: <Navigate to="/activity/queue" replace /> },
          { path: "queue", element: <QueueSection /> },
          { path: "history", element: <HistorySection /> },
        ],
      },
```

(`Navigate` is already imported in `routes.tsx`. Leave the `Placeholder` import — it is still used by `/calendar` and `/system`.)

- [ ] **Step 6: Run the full frontend test suite + typecheck**

Run (from `web/`): `npm run test` then `npm run build`
Expected: all vitest suites PASS; `tsc -b`/build exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/activity/ActivityLayout.tsx web/src/features/activity/ActivityLayout.test.tsx web/src/app/routes.tsx
git commit -m "feat(6-4): ActivityLayout tabs + /activity nested routes"
```

---

### Task 6: Rebuild embed bundle + full verify + live check

**Files:**
- Modify: `web/dist/**` (regenerated build artifacts)

- [ ] **Step 1: Rebuild the committed web bundle**

Run (from `web/`): `npm run build`
Expected: exit 0; `web/dist` regenerated.

- [ ] **Step 2: Full backend + embed verify**

Run (from repo root):
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./...
```
Expected: all packages build/vet/test green (incl. `web/spa_test.go`).

- [ ] **Step 3: Confirm no drift after commit of dist**

```bash
git add web/dist
git diff --cached --stat web/dist   # review that only expected bundle files changed
```
Then after committing (Step 5) the guard must be clean.

- [ ] **Step 4: Live browser check**

Build and run a fresh instance:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build -o nexus.exe ./cmd/nexus
NEXUS_DATA_DIR=$(mktemp -d) NEXUS_ADMIN_PASSWORD=admin ./nexus.exe
```
Open `http://localhost:9494/`, log in (`admin`/`admin`), click **Activity** in the sidebar. Verify:
- `/activity` redirects to `/activity/queue`; both Queue and History tabs render and the sidebar "Activity" link is highlighted on both.
- Empty states render on a fresh DB ("Queue is empty." / "No history yet.").
- No console errors. (Seeding real queue/history rows requires a configured TMDb key + indexer/download client; the empty-state + tab-switch + no-error check is the required live gate. Row-level behaviour is covered by the T3/T4 component tests.)

- [ ] **Step 5: Commit the rebuilt bundle**

```bash
git add web/dist
git commit -m "build(6-4): rebuild embedded web bundle for Activity slice"
```

- [ ] **Step 6: Final drift-guard confirmation**

Run: `git diff --exit-code web/dist`
Expected: exit 0 (clean).

---

## Self-review notes (author)

- **Spec coverage:** §3 module layout → T1–T5 files; §4 resolution + fallbacks → T1 (`resolve.ts` + tests); §5 Queue columns/actions/error → T3; §6 History → T4; §7 WS invalidation → T2 (`useActivityInvalidation`) + mounted in T5; §8 testing → per-task tests + T6 verify; AC 1–9 → T5 routes (AC1), T3 (AC2–5), T4 (AC6), T2/T5 (AC7), T3/T4 empty states (AC8), T6 (AC9).
- **Placeholder scan:** no TBD/TODO; every code step contains full code.
- **Type consistency:** `QueueItem`/`HistoryEvent` (T1) used verbatim in T2/T3/T4; helper names (`movieTitleMap`, `seriesTitleMap`, `resolveTitle`, `qualityName`, `eventLabel`, `statusLabel`, `statusTone`, `shouldRefresh`) consistent across tasks; `activityKeys` used in T2 only; `ApiError.message` (verified field), `relativeTime(from: number)` (verified signature) used correctly.
