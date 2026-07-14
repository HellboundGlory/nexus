# Nexus Web UI — Library Polish (Sub-project A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a dynamic card-scale slider to the library grids, redesign the TV/movie detail pages with a full-width backdrop banner + collapsible seasons (Season 0 → "Specials", pinned last), and turn the Add dialog into a sortable poster grid.

**Architecture:** Pure frontend changes to the React SPA under `web/`. Data already exists (`posterUrl`, `fanartUrl`). New pure helpers are extracted into their own files so they unit-test without React; new components (`ScaleSlider`, `DetailBanner`, `SeasonSection`) are small and focused. No backend, no DTO, no migration.

**Tech Stack:** React 19, TypeScript, Tailwind v4 (CSS-var tokens), Vitest + Testing Library, TanStack Query v5 (already wired). Served from Go via committed `web/dist`.

## Global Constraints

- **No backend changes.** No files under `internal/**`, `cmd/**`, or `web/embed.go`. No new endpoints, no migration.
- **Test runner:** Vitest. Run a single test file with `cd web && npx vitest run <path>` from the repo root. Full FE suite: `cd web && npm test`.
- **Type check:** `cd web && npx tsc -b` must exit 0.
- **Design tokens:** use existing CSS vars only — `var(--color-bg)`, `var(--color-panel)`, `var(--color-panel-2)`, `var(--color-border)`, `var(--color-brand)`, `var(--color-muted)`, `var(--color-warn)`. Do not introduce raw hex colors.
- **web/dist drift guard:** the final task rebuilds `web/dist`; `git diff --exit-code web/dist` must be clean at the end.
- **ASK before pushing `master`.** Work stays on branch `feat/webui-library-polish`.
- Commit after every task (steps end with a commit). Conventional-commit style, and end each message with the `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` trailer.

---

### Task 1: Grid-scale hook + pure helpers

**Files:**
- Create: `web/src/features/library/useGridScale.ts`
- Test: `web/src/features/library/useGridScale.test.ts`

**Interfaces:**
- Produces:
  - `MIN_SCALE = 110`, `MAX_SCALE = 230`, `DEFAULT_SCALE = 120` (numbers)
  - `SCALE_KEY = "nexus.grid.scale"` (string)
  - `clampScale(n: number): number` — clamps to `[MIN_SCALE, MAX_SCALE]`; non-finite → `DEFAULT_SCALE`
  - `readScale(): number` — reads+clamps from `localStorage`, else `DEFAULT_SCALE`
  - `useGridScale(): [number, (n: number) => void]` — React hook; setter persists to `localStorage`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/features/library/useGridScale.test.ts
import { describe, it, expect, beforeEach } from "vitest"
import {
  clampScale, readScale, MIN_SCALE, MAX_SCALE, DEFAULT_SCALE, SCALE_KEY,
} from "@/features/library/useGridScale"

describe("clampScale", () => {
  it("passes through an in-range value", () => {
    expect(clampScale(150)).toBe(150)
  })
  it("clamps below MIN and above MAX", () => {
    expect(clampScale(10)).toBe(MIN_SCALE)
    expect(clampScale(9999)).toBe(MAX_SCALE)
  })
  it("falls back to DEFAULT on non-finite input", () => {
    expect(clampScale(NaN)).toBe(DEFAULT_SCALE)
  })
})

describe("readScale", () => {
  beforeEach(() => localStorage.clear())
  it("returns DEFAULT when nothing is stored", () => {
    expect(readScale()).toBe(DEFAULT_SCALE)
  })
  it("returns DEFAULT when the stored value is garbage", () => {
    localStorage.setItem(SCALE_KEY, "not-a-number")
    expect(readScale()).toBe(DEFAULT_SCALE)
  })
  it("returns the clamped stored value", () => {
    localStorage.setItem(SCALE_KEY, "9999")
    expect(readScale()).toBe(MAX_SCALE)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/library/useGridScale.test.ts`
Expected: FAIL — cannot resolve module `@/features/library/useGridScale`.

- [ ] **Step 3: Write minimal implementation**

```ts
// web/src/features/library/useGridScale.ts
import { useState, useCallback } from "react"

export const MIN_SCALE = 110
export const MAX_SCALE = 230
export const DEFAULT_SCALE = 120
export const SCALE_KEY = "nexus.grid.scale"

export function clampScale(n: number): number {
  if (!Number.isFinite(n)) return DEFAULT_SCALE
  return Math.min(MAX_SCALE, Math.max(MIN_SCALE, Math.round(n)))
}

export function readScale(): number {
  try {
    const raw = localStorage.getItem(SCALE_KEY)
    if (raw == null || raw.trim() === "") return DEFAULT_SCALE
    const n = Number(raw)
    if (!Number.isFinite(n)) return DEFAULT_SCALE
    return clampScale(n)
  } catch {
    return DEFAULT_SCALE
  }
}

export function useGridScale(): [number, (n: number) => void] {
  const [scale, setScale] = useState<number>(() => readScale())
  const set = useCallback((n: number) => {
    const c = clampScale(n)
    setScale(c)
    try {
      localStorage.setItem(SCALE_KEY, String(c))
    } catch {
      /* storage unavailable — keep in-memory only */
    }
  }, [])
  return [scale, set]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/library/useGridScale.test.ts`
Expected: PASS (6 assertions).

- [ ] **Step 5: Commit**

```bash
git add web/src/features/library/useGridScale.ts web/src/features/library/useGridScale.test.ts
git commit -m "feat(webui): grid-scale hook + clamp/persist helpers"
```

---

### Task 2: ScaleSlider component

**Files:**
- Create: `web/src/features/library/ScaleSlider.tsx`
- Test: `web/src/features/library/ScaleSlider.test.tsx`

**Interfaces:**
- Consumes: `MIN_SCALE`, `MAX_SCALE` from `useGridScale`.
- Produces: `ScaleSlider(props: { value: number; onChange: (n: number) => void })` — a range input labeled `"Card size"`; dragging right raises `value` (larger cards).

- [ ] **Step 1: Write the failing test**

```tsx
// web/src/features/library/ScaleSlider.test.tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { ScaleSlider } from "@/features/library/ScaleSlider"

describe("ScaleSlider", () => {
  it("renders a range input reflecting the value", () => {
    render(<ScaleSlider value={150} onChange={() => {}} />)
    const range = screen.getByLabelText("Card size") as HTMLInputElement
    expect(range).toHaveAttribute("type", "range")
    expect(range.value).toBe("150")
  })
  it("calls onChange with a number when dragged", () => {
    const onChange = vi.fn()
    render(<ScaleSlider value={150} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText("Card size"), { target: { value: "200" } })
    expect(onChange).toHaveBeenCalledWith(200)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/library/ScaleSlider.test.tsx`
Expected: FAIL — cannot resolve `@/features/library/ScaleSlider`.

- [ ] **Step 3: Write minimal implementation**

```tsx
// web/src/features/library/ScaleSlider.tsx
import { MIN_SCALE, MAX_SCALE } from "./useGridScale"

export function ScaleSlider({
  value, onChange,
}: {
  value: number
  onChange: (n: number) => void
}) {
  return (
    <div className="ml-auto flex items-center gap-2 text-[var(--color-muted)]">
      <span aria-hidden className="text-xs">▪</span>
      <input
        type="range"
        aria-label="Card size"
        min={MIN_SCALE}
        max={MAX_SCALE}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="h-1 w-28 cursor-pointer accent-[var(--color-brand)]"
      />
      <span aria-hidden className="text-base">◼</span>
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/library/ScaleSlider.test.tsx`
Expected: PASS (2 assertions).

- [ ] **Step 5: Commit**

```bash
git add web/src/features/library/ScaleSlider.tsx web/src/features/library/ScaleSlider.test.tsx
git commit -m "feat(webui): ScaleSlider control"
```

---

### Task 3: MediaGrid scale prop + wire into Movies/TV pages

**Files:**
- Modify: `web/src/features/library/MediaGrid.tsx`
- Modify: `web/src/pages/Movies.tsx`
- Modify: `web/src/pages/TvShows.tsx`
- Test: `web/src/features/library/MediaGrid.test.tsx` (add one case)

**Interfaces:**
- Consumes: `DEFAULT_SCALE` from `useGridScale`; `useGridScale`, `ScaleSlider`.
- Produces: `MediaGrid` gains optional prop `scale?: number` (defaults to `DEFAULT_SCALE`). Both grid containers use `style={{ gridTemplateColumns: \`repeat(auto-fill, minmax(${scale}px, 1fr))\` }}`.

- [ ] **Step 1: Write the failing test** (append inside the existing `describe("MediaGrid", …)` in `MediaGrid.test.tsx`)

```tsx
  it("applies the scale to the grid template columns", () => {
    render(
      <MediaGrid
        items={[{ id: 1 }]}
        isLoading={false}
        isError={false}
        onRetry={() => {}}
        empty="none"
        scale={200}
        renderCard={(it: { id: number }) => <div key={it.id}>card-{it.id}</div>}
      />,
    )
    const grid = screen.getByText("card-1").parentElement as HTMLElement
    expect(grid.style.gridTemplateColumns).toContain("200px")
  })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/library/MediaGrid.test.tsx`
Expected: FAIL — `scale` not a valid prop / `gridTemplateColumns` empty.

- [ ] **Step 3: Implement `MediaGrid.tsx`**

Replace the whole file with:

```tsx
import * as React from "react"
import { DEFAULT_SCALE } from "./useGridScale"

export function MediaGrid<T>({
  items, isLoading, isError, onRetry, empty, renderCard, scale = DEFAULT_SCALE,
}: {
  items: T[] | undefined
  isLoading: boolean
  isError: boolean
  onRetry: () => void
  empty: string
  renderCard: (item: T) => React.ReactNode
  scale?: number
}) {
  const gridStyle: React.CSSProperties = {
    gridTemplateColumns: `repeat(auto-fill, minmax(${scale}px, 1fr))`,
  }
  if (isLoading) {
    return (
      <div data-testid="grid-loading" className="grid gap-4 p-6" style={gridStyle}>
        {Array.from({ length: 12 }).map((_, i) => (
          <div key={i} className="aspect-[2/3] animate-pulse rounded-lg bg-[var(--color-panel-2)]" />
        ))}
      </div>
    )
  }
  if (isError) {
    return (
      <div className="m-6 rounded-lg border border-[var(--color-warn)] bg-[var(--color-panel)] p-6 text-center">
        <p className="text-sm text-[var(--color-muted)]">Failed to load. Please try again.</p>
        <button
          onClick={onRetry}
          className="mt-3 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm hover:border-[var(--color-brand)]"
        >
          Retry
        </button>
      </div>
    )
  }
  if (!items || items.length === 0) {
    return <div className="p-10 text-center text-sm text-[var(--color-muted)]">{empty}</div>
  }
  return (
    <div className="grid gap-4 p-6" style={gridStyle}>
      {items.map((it) => renderCard(it))}
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/library/MediaGrid.test.tsx`
Expected: PASS (all cases, including the new one).

- [ ] **Step 5: Wire the slider into `Movies.tsx`**

In `web/src/pages/Movies.tsx`, add imports and the slider. Change the imports block to include:

```tsx
import { useGridScale } from "@/features/library/useGridScale"
import { ScaleSlider } from "@/features/library/ScaleSlider"
```

Inside `Movies()`, after `const [addOpen, setAddOpen] = useState(false)` add:

```tsx
  const [scale, setScale] = useGridScale()
```

In the toolbar `<div className="flex items-center gap-3 p-6 pb-0">`, add the slider as the last child (after the + Add button):

```tsx
        <ScaleSlider value={scale} onChange={setScale} />
```

And pass `scale` to the grid — change `<MediaGrid` opening to include the prop:

```tsx
      <MediaGrid
        scale={scale}
        items={q.data ? items : undefined}
```

- [ ] **Step 6: Wire the slider into `TvShows.tsx`**

Apply the identical four edits to `web/src/pages/TvShows.tsx` (same imports, same `const [scale, setScale] = useGridScale()` after `addOpen`, same `<ScaleSlider value={scale} onChange={setScale} />` as last toolbar child, same `scale={scale}` on `<MediaGrid`).

- [ ] **Step 7: Type-check + run the library test folder**

Run: `cd web && npx tsc -b && npx vitest run src/features/library`
Expected: tsc exit 0; tests PASS.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/library/MediaGrid.tsx web/src/features/library/MediaGrid.test.tsx web/src/pages/Movies.tsx web/src/pages/TvShows.tsx
git commit -m "feat(webui): scalable library grid + slider on Movies/TV pages"
```

---

### Task 4: DetailBanner component

**Files:**
- Create: `web/src/features/library/DetailBanner.tsx`
- Test: `web/src/features/library/DetailBanner.test.tsx`

**Interfaces:**
- Produces: `DetailBanner(props: { fanartUrl: string; posterUrl: string; title: string; children: React.ReactNode })` — full-width backdrop header. When `fanartUrl` is empty, renders no backdrop `<img>` and uses the flat panel background. Always renders `children`.

- [ ] **Step 1: Write the failing test**

```tsx
// web/src/features/library/DetailBanner.test.tsx
import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { DetailBanner } from "@/features/library/DetailBanner"

describe("DetailBanner", () => {
  it("renders a backdrop image when fanartUrl is set", () => {
    render(
      <DetailBanner fanartUrl="http://img/bd.jpg" posterUrl="" title="Breaking Bad">
        <p>meta</p>
      </DetailBanner>,
    )
    const bd = screen.getByTestId("banner-backdrop") as HTMLImageElement
    expect(bd.tagName).toBe("IMG")
    expect(bd.src).toContain("bd.jpg")
    expect(screen.getByText("meta")).toBeInTheDocument()
  })
  it("renders no backdrop image when fanartUrl is empty", () => {
    render(
      <DetailBanner fanartUrl="" posterUrl="" title="Breaking Bad">
        <p>meta</p>
      </DetailBanner>,
    )
    expect(screen.queryByTestId("banner-backdrop")).toBeNull()
    expect(screen.getByText("meta")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/library/DetailBanner.test.tsx`
Expected: FAIL — cannot resolve `@/features/library/DetailBanner`.

- [ ] **Step 3: Write minimal implementation**

```tsx
// web/src/features/library/DetailBanner.tsx
import * as React from "react"

export function DetailBanner({
  fanartUrl, posterUrl, title, children,
}: {
  fanartUrl: string
  posterUrl: string
  title: string
  children: React.ReactNode
}) {
  return (
    <div className="relative isolate -mx-6 -mt-6 mb-6 min-h-[320px] overflow-hidden bg-[var(--color-panel-2)]">
      {fanartUrl ? (
        <img
          data-testid="banner-backdrop"
          src={fanartUrl}
          alt=""
          aria-hidden
          className="absolute inset-0 h-full w-full object-cover"
        />
      ) : null}
      {/* darkening gradient so text stays legible and the banner melts into the page */}
      <div className="absolute inset-0 bg-gradient-to-t from-[var(--color-bg)] via-[var(--color-bg)]/70 to-transparent" />
      <div className="relative z-10 flex min-h-[320px] items-end gap-6 p-6">
        {posterUrl ? (
          <div className="aspect-[2/3] w-32 shrink-0 overflow-hidden rounded-lg shadow-lg sm:w-40">
            <img src={posterUrl} alt={title} className="h-full w-full object-cover" />
          </div>
        ) : null}
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/library/DetailBanner.test.tsx`
Expected: PASS (both cases).

- [ ] **Step 5: Commit**

```bash
git add web/src/features/library/DetailBanner.tsx web/src/features/library/DetailBanner.test.tsx
git commit -m "feat(webui): full-width DetailBanner component"
```

---

### Task 5: Collapsible seasons — `seasonSections` helper + `SeasonSection` + refactor SeasonTable

**Files:**
- Create: `web/src/features/library/seasonSections.ts`
- Create: `web/src/features/library/SeasonSection.tsx`
- Modify: `web/src/features/library/SeasonTable.tsx`
- Test: `web/src/features/library/seasonSections.test.ts`
- Test: `web/src/features/library/SeasonSection.test.tsx`

**Interfaces:**
- Consumes: `Season`, `Episode` from `./types`; `StatusBadge` from `./StatusBadge`.
- Produces:
  - `type SeasonSectionData = { id: number; seasonNumber: number; title: string; defaultOpen: boolean; eps: Episode[]; withFile: number }`
  - `seasonSections(seasons: Season[], episodes: Episode[]): SeasonSectionData[]` — regular seasons (`seasonNumber > 0`) ascending first, then Season 0 (if present) last titled `"Specials"`; regular titled `"Season {n}"`; `defaultOpen = seasonNumber !== 0`; `eps` sorted by episode number; `withFile` = count of eps with `hasFile`.
  - `SeasonSection(props: { title; withFile; total; monitored; defaultOpen; onToggleMonitor: () => void; onSearch: () => void; children: React.ReactNode })` — collapsible; header always visible; body (`children`) shown only when open; toggling monitor/search must not collapse.

- [ ] **Step 1: Write the failing helper test**

```ts
// web/src/features/library/seasonSections.test.ts
import { describe, it, expect } from "vitest"
import { seasonSections } from "@/features/library/seasonSections"
import type { Season, Episode } from "@/features/library/types"

const season = (id: number, n: number): Season => ({ id, seriesId: 1, seasonNumber: n, monitored: true })
const ep = (id: number, n: number, e: number, hasFile: boolean): Episode => ({
  id, seriesId: 1, seasonNumber: n, episodeNumber: e, tmdbId: 0, title: `E${e}`,
  overview: "", airDate: "", monitored: true, hasFile,
})

describe("seasonSections", () => {
  it("titles Season 0 as Specials and orders it last", () => {
    const out = seasonSections(
      [season(10, 0), season(11, 2), season(12, 1)],
      [],
    )
    expect(out.map((s) => s.title)).toEqual(["Season 1", "Season 2", "Specials"])
  })
  it("marks specials closed by default and regular seasons open", () => {
    const out = seasonSections([season(10, 0), season(11, 1)], [])
    const specials = out.find((s) => s.seasonNumber === 0)!
    const s1 = out.find((s) => s.seasonNumber === 1)!
    expect(specials.defaultOpen).toBe(false)
    expect(s1.defaultOpen).toBe(true)
  })
  it("groups + counts episodes with files per season, sorted by episode number", () => {
    const out = seasonSections(
      [season(11, 1)],
      [ep(102, 1, 2, false), ep(101, 1, 1, true)],
    )
    const s1 = out[0]
    expect(s1.eps.map((e) => e.episodeNumber)).toEqual([1, 2])
    expect(s1.withFile).toBe(1)
  })
})
```

- [ ] **Step 2: Run helper test to verify it fails**

Run: `cd web && npx vitest run src/features/library/seasonSections.test.ts`
Expected: FAIL — cannot resolve `@/features/library/seasonSections`.

- [ ] **Step 3: Implement `seasonSections.ts`**

```ts
// web/src/features/library/seasonSections.ts
import type { Season, Episode } from "./types"

export type SeasonSectionData = {
  id: number
  seasonNumber: number
  title: string
  defaultOpen: boolean
  eps: Episode[]
  withFile: number
}

export function seasonSections(seasons: Season[], episodes: Episode[]): SeasonSectionData[] {
  const build = (s: Season): SeasonSectionData => {
    const eps = episodes
      .filter((e) => e.seasonNumber === s.seasonNumber)
      .sort((a, b) => a.episodeNumber - b.episodeNumber)
    return {
      id: s.id,
      seasonNumber: s.seasonNumber,
      title: s.seasonNumber === 0 ? "Specials" : `Season ${s.seasonNumber}`,
      defaultOpen: s.seasonNumber !== 0,
      eps,
      withFile: eps.filter((e) => e.hasFile).length,
    }
  }
  const regular = seasons
    .filter((s) => s.seasonNumber > 0)
    .sort((a, b) => a.seasonNumber - b.seasonNumber)
    .map(build)
  const specials = seasons.filter((s) => s.seasonNumber === 0).map(build)
  return [...regular, ...specials]
}
```

- [ ] **Step 4: Run helper test to verify it passes**

Run: `cd web && npx vitest run src/features/library/seasonSections.test.ts`
Expected: PASS (3 cases).

- [ ] **Step 5: Write the failing `SeasonSection` test**

```tsx
// web/src/features/library/SeasonSection.test.tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { SeasonSection } from "@/features/library/SeasonSection"

function renderOne(defaultOpen: boolean, onSearch = vi.fn(), onToggleMonitor = vi.fn()) {
  render(
    <SeasonSection
      title="Specials" withFile={0} total={3} monitored
      defaultOpen={defaultOpen} onSearch={onSearch} onToggleMonitor={onToggleMonitor}
    >
      <li>episode-body</li>
    </SeasonSection>,
  )
}

describe("SeasonSection", () => {
  it("hides the body when defaultOpen is false and shows it after clicking the header", async () => {
    renderOne(false)
    expect(screen.queryByText("episode-body")).toBeNull()
    await userEvent.click(screen.getByText("Specials"))
    expect(screen.getByText("episode-body")).toBeInTheDocument()
  })
  it("shows the body when defaultOpen is true", () => {
    renderOne(true)
    expect(screen.getByText("episode-body")).toBeInTheDocument()
  })
  it("does not collapse when the search control is used", async () => {
    const onSearch = vi.fn()
    renderOne(true, onSearch)
    await userEvent.click(screen.getByRole("button", { name: /search season/i }))
    expect(onSearch).toHaveBeenCalled()
    expect(screen.getByText("episode-body")).toBeInTheDocument()
  })
})
```

- [ ] **Step 6: Run `SeasonSection` test to verify it fails**

Run: `cd web && npx vitest run src/features/library/SeasonSection.test.tsx`
Expected: FAIL — cannot resolve `@/features/library/SeasonSection`.

- [ ] **Step 7: Implement `SeasonSection.tsx`**

```tsx
// web/src/features/library/SeasonSection.tsx
import * as React from "react"
import { useState } from "react"
import { StatusBadge } from "./StatusBadge"

export function SeasonSection({
  title, withFile, total, monitored, defaultOpen, onToggleMonitor, onSearch, children,
}: {
  title: string
  withFile: number
  total: number
  monitored: boolean
  defaultOpen: boolean
  onToggleMonitor: () => void
  onSearch: () => void
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="overflow-hidden rounded-lg border border-[var(--color-border)]">
      <div className="flex items-center justify-between bg-[var(--color-panel-2)] px-4 py-2">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          className="flex items-center gap-3"
        >
          <span aria-hidden className="text-xs text-[var(--color-muted)]">{open ? "▾" : "▸"}</span>
          <span className="font-semibold">{title}</span>
          <StatusBadge tone={withFile >= total && total > 0 ? "ok" : "warn"} label={`${withFile} / ${total}`} />
        </button>
        <div className="flex items-center gap-2">
          <button onClick={onSearch} className="text-xs text-[var(--color-brand)]">Search season</button>
          <label className="flex items-center gap-1 text-xs text-[var(--color-muted)]">
            <input type="checkbox" checked={monitored} onChange={onToggleMonitor} /> monitor
          </label>
        </div>
      </div>
      {open ? <ul>{children}</ul> : null}
    </div>
  )
}
```

- [ ] **Step 8: Run `SeasonSection` test to verify it passes**

Run: `cd web && npx vitest run src/features/library/SeasonSection.test.tsx`
Expected: PASS (3 cases).

- [ ] **Step 9: Refactor `SeasonTable.tsx` to use the helper + component**

Replace the whole file with:

```tsx
import type { Season, Episode } from "./types"
import { StatusBadge } from "./StatusBadge"
import { SeasonSection } from "./SeasonSection"
import { seasonSections } from "./seasonSections"

export function SeasonTable({
  seasons, episodes, onToggleSeason, onToggleEpisode, onSearchSeason, onSearchEpisode,
}: {
  seasons: Season[]
  episodes: Episode[]
  seriesId: number
  onToggleSeason: (s: Season) => void
  onToggleEpisode: (e: Episode) => void
  onSearchSeason: (seasonNumber: number) => void
  onSearchEpisode: (e: Episode) => void
}) {
  const sections = seasonSections(seasons, episodes)
  const seasonById = new Map(seasons.map((s) => [s.id, s]))
  return (
    <div className="mt-6 flex flex-col gap-4">
      {sections.map((sec) => {
        const season = seasonById.get(sec.id)!
        return (
          <SeasonSection
            key={sec.id}
            title={sec.title}
            withFile={sec.withFile}
            total={sec.eps.length}
            monitored={season.monitored}
            defaultOpen={sec.defaultOpen}
            onToggleMonitor={() => onToggleSeason(season)}
            onSearch={() => onSearchSeason(sec.seasonNumber)}
          >
            {sec.eps.map((e) => (
              <li key={e.id} className="flex items-center gap-3 border-t border-[var(--color-border)] px-4 py-2 text-sm">
                <span className="w-10 text-[var(--color-muted)]">{e.episodeNumber}</span>
                <span className="min-w-0 flex-1 truncate">{e.title}</span>
                <span className="text-xs text-[var(--color-muted)]">{e.airDate}</span>
                <StatusBadge tone={e.hasFile ? "ok" : "muted"} label={e.hasFile ? "File" : "—"} />
                <button aria-label={`Search episode ${e.episodeNumber}`} onClick={() => onSearchEpisode(e)} className="text-xs text-[var(--color-brand)]">Search episode</button>
                <label className="flex items-center gap-1 text-xs text-[var(--color-muted)]">
                  <input type="checkbox" checked={e.monitored} onChange={() => onToggleEpisode(e)} /> mon
                </label>
              </li>
            ))}
          </SeasonSection>
        )
      })}
    </div>
  )
}
```

- [ ] **Step 10: Type-check + run the library test folder**

Run: `cd web && npx tsc -b && npx vitest run src/features/library`
Expected: tsc exit 0; all library tests PASS (the existing `SeriesDetail.test.tsx` season is `seasonNumber: 1` → open by default, so "System"/"Hands" remain visible).

- [ ] **Step 11: Commit**

```bash
git add web/src/features/library/seasonSections.ts web/src/features/library/seasonSections.test.ts web/src/features/library/SeasonSection.tsx web/src/features/library/SeasonSection.test.tsx web/src/features/library/SeasonTable.tsx
git commit -m "feat(webui): collapsible seasons with Specials pinned last"
```

---

### Task 6: Wire DetailBanner into SeriesDetail + MovieDetail

**Files:**
- Modify: `web/src/features/library/SeriesDetail.tsx`
- Modify: `web/src/features/library/MovieDetail.tsx`

**Interfaces:**
- Consumes: `DetailBanner` from `./DetailBanner` (Task 4); `SeasonTable` (Task 5, unchanged call site).

- [ ] **Step 1: Refactor `SeriesDetail.tsx`**

Add `import { DetailBanner } from "./DetailBanner"` to the imports. Replace the returned JSX (the `return ( <div className="p-6"> … </div> )` block, starting at the outer `<div className="p-6">`) with:

```tsx
  return (
    <div className="p-6">
      <button onClick={() => nav("/tv")} className="mb-4 text-sm text-[var(--color-brand)]">← TV Shows</button>
      <DetailBanner fanartUrl={s.fanartUrl} posterUrl={s.posterUrl} title={s.title}>
        <div className="flex items-center gap-3">
          <h2 className="text-2xl font-bold">{s.title}</h2>
          {s.firstAired ? <span className="text-[var(--color-muted)]">{s.firstAired.slice(0, 4)}</span> : null}
          <StatusBadge tone={badge.tone} label={badge.label} />
        </div>
        <p className="mt-3 max-w-2xl text-sm text-[var(--color-muted)]">{s.overview}</p>
        <div className="mt-5 flex flex-wrap items-center gap-2">
          <button onClick={() => setMon.mutate({ target: { kind: "series", id }, monitored: !s.monitored })} className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm">
            {s.monitored ? "Unmonitor" : "Monitor"}
          </button>
          <button onClick={() => { search.mutate({ kind: "series", id }); toast(`Search started for ${s.title}`) }} className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm">Search</button>
          <button onClick={() => { refresh.mutate({ kind: "series", id }); toast("Refresh started") }} className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm">Refresh</button>
          <button
            onClick={() => { if (confirm(`Delete ${s.title}?`)) del.mutate({ kind: "series", id }, { onSuccess: () => { toast("Deleted"); nav("/tv") } }) }}
            className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
          >
            Delete
          </button>
          <div className="w-48">
            <Select
              aria-label="Quality profile"
              value={s.qualityProfileId ? String(s.qualityProfileId) : ""}
              disabled={(profiles.data ?? []).length === 0}
              onChange={(v) => v && assign.mutate({ kind: "series", id, qualityProfileId: Number(v) })}
            >
              <option value="">{(profiles.data ?? []).length === 0 ? "No profiles" : "Quality profile…"}</option>
              {(profiles.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </Select>
          </div>
        </div>
      </DetailBanner>

      <SeasonTable
        seasons={s.seasons}
        episodes={s.episodes}
        seriesId={id}
        onToggleSeason={(sn) => setMon.mutate({ target: { kind: "season", id: sn.id }, monitored: !sn.monitored })}
        onToggleEpisode={(e) => setMon.mutate({ target: { kind: "episode", id: e.id }, monitored: !e.monitored })}
        onSearchSeason={(seasonNumber) => { search.mutate({ kind: "season", seriesId: id, seasonNumber }); toast(`Search started for season ${seasonNumber}`) }}
        onSearchEpisode={(e) => { search.mutate({ kind: "episode", id: e.id }); toast(`Search started for ${e.title}`) }}
      />
    </div>
  )
```

- [ ] **Step 2: Refactor `MovieDetail.tsx`**

Add `import { DetailBanner } from "./DetailBanner"` to the imports. Replace the returned JSX (the final `return ( <div className="p-6"> … </div> )` block that contains the `flex gap-6` poster row) with:

```tsx
  return (
    <div className="p-6">
      <button onClick={() => nav("/movies")} className="mb-4 text-sm text-[var(--color-brand)]">← Movies</button>
      <DetailBanner fanartUrl={m.fanartUrl} posterUrl={m.posterUrl} title={m.title}>
        <div className="flex items-center gap-3">
          <h2 className="text-2xl font-bold">{m.title}</h2>
          {m.year ? <span className="text-[var(--color-muted)]">{m.year}</span> : null}
          <StatusBadge tone={badge.tone} label={badge.label} />
        </div>
        <p className="mt-3 max-w-2xl text-sm text-[var(--color-muted)]">{m.overview}</p>
        <div className="mt-5 flex flex-wrap items-center gap-2">
          <button
            onClick={() => setMon.mutate({ target: { kind: "movie", id }, monitored: !m.monitored })}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            {m.monitored ? "Unmonitor" : "Monitor"}
          </button>
          <button
            onClick={() => { search.mutate({ kind: "movie", id }); toast(`Search started for ${m.title}`) }}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            Search
          </button>
          <button
            onClick={() => { refresh.mutate({ kind: "movie", id }); toast("Refresh started") }}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            Refresh
          </button>
          <button
            onClick={() => {
              if (confirm(`Delete ${m.title}?`)) {
                del.mutate({ kind: "movie", id }, { onSuccess: () => { toast("Deleted"); nav("/movies") } })
              }
            }}
            className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
          >
            Delete
          </button>
          <div className="w-48">
            <Select
              aria-label="Quality profile"
              value={m.qualityProfileId ? String(m.qualityProfileId) : ""}
              disabled={(profiles.data ?? []).length === 0}
              onChange={(v) => v && assign.mutate({ kind: "movie", id, qualityProfileId: Number(v) })}
            >
              <option value="">{(profiles.data ?? []).length === 0 ? "No profiles" : "Quality profile…"}</option>
              {(profiles.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </Select>
          </div>
        </div>
      </DetailBanner>
    </div>
  )
```

- [ ] **Step 3: Type-check + run the library test folder**

Run: `cd web && npx tsc -b && npx vitest run src/features/library`
Expected: tsc exit 0; tests PASS (SeriesDetail test still finds title/episodes; MovieDetail test still finds its actions).

- [ ] **Step 4: Commit**

```bash
git add web/src/features/library/SeriesDetail.tsx web/src/features/library/MovieDetail.tsx
git commit -m "feat(webui): banner-led detail layout for TV + movies"
```

---

### Task 7: Add dialog — poster grid + sort

**Files:**
- Create: `web/src/features/library/addSort.ts`
- Modify: `web/src/features/library/AddMediaDialog.tsx`
- Test: `web/src/features/library/addSort.test.ts`
- Test: `web/src/features/library/AddMediaDialog.test.tsx` (add a poster-grid case)

**Interfaces:**
- Consumes: `MetadataResult` from `./types`.
- Produces:
  - `type AddSort = "relevance" | "newest" | "oldest"`
  - `sortResults(results: MetadataResult[], sort: AddSort): MetadataResult[]` — `relevance` returns input order (new array, unmutated); `newest` = year desc; `oldest` = year asc; results with falsy/`0` year sort **last** in newest/oldest.

- [ ] **Step 1: Write the failing helper test**

```ts
// web/src/features/library/addSort.test.ts
import { describe, it, expect } from "vitest"
import { sortResults } from "@/features/library/addSort"
import type { MetadataResult } from "@/features/library/types"

const r = (tmdbId: number, year: number): MetadataResult => ({
  tmdbId, title: `t${tmdbId}`, year, overview: "", posterUrl: "", kind: "movie",
})

describe("sortResults", () => {
  const input = [r(1, 2001), r(2, 0), r(3, 2020)]
  it("keeps relevance order and does not mutate the input", () => {
    const out = sortResults(input, "relevance")
    expect(out.map((x) => x.tmdbId)).toEqual([1, 2, 3])
    expect(input.map((x) => x.tmdbId)).toEqual([1, 2, 3])
  })
  it("sorts newest first with missing years last", () => {
    expect(sortResults(input, "newest").map((x) => x.tmdbId)).toEqual([3, 1, 2])
  })
  it("sorts oldest first with missing years last", () => {
    expect(sortResults(input, "oldest").map((x) => x.tmdbId)).toEqual([1, 3, 2])
  })
})
```

- [ ] **Step 2: Run helper test to verify it fails**

Run: `cd web && npx vitest run src/features/library/addSort.test.ts`
Expected: FAIL — cannot resolve `@/features/library/addSort`.

- [ ] **Step 3: Implement `addSort.ts`**

```ts
// web/src/features/library/addSort.ts
import type { MetadataResult } from "./types"

export type AddSort = "relevance" | "newest" | "oldest"

export function sortResults(results: MetadataResult[], sort: AddSort): MetadataResult[] {
  const copy = [...results]
  if (sort === "relevance") return copy
  return copy.sort((a, b) => {
    const ay = a.year || 0
    const by = b.year || 0
    // Missing years (0) always sort last regardless of direction.
    if (ay === 0 && by === 0) return 0
    if (ay === 0) return 1
    if (by === 0) return -1
    return sort === "newest" ? by - ay : ay - by
  })
}
```

- [ ] **Step 4: Run helper test to verify it passes**

Run: `cd web && npx vitest run src/features/library/addSort.test.ts`
Expected: PASS (3 cases).

- [ ] **Step 5: Add the poster-grid test to `AddMediaDialog.test.tsx`**

Append this case inside the existing `describe("AddMediaDialog", …)` block. It reuses the file's existing `stub()` + mock setup:

```tsx
  it("renders results as poster tiles and can reorder by sort", async () => {
    stub()
    vi.mocked(lib.useRootFolders).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useRootFolders>)
    render(
      <ToastProvider>
        <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
      </ToastProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
    // the result tile shows the title and the year
    expect(await screen.findByText("Dune")).toBeInTheDocument()
    expect(screen.getByText("2021")).toBeInTheDocument()
    // the sort control is present
    expect(screen.getByLabelText(/sort/i)).toBeInTheDocument()
  })
```

- [ ] **Step 6: Run the dialog test to verify the new case fails**

Run: `cd web && npx vitest run src/features/library/AddMediaDialog.test.tsx`
Expected: FAIL — no element with an accessible name matching `/sort/i`.

- [ ] **Step 7: Implement the poster grid + sort in `AddMediaDialog.tsx`**

Add these imports near the top (with the other imports):

```tsx
import { sortResults, type AddSort } from "./addSort"
```

Add sort state next to the other `useState` calls (after `const [monitored, setMonitored] = useState(true)`):

```tsx
  const [sort, setSort] = useState<AddSort>("relevance")
```

Add `setSort("relevance")` to `reset()` so it reads:

```tsx
  function reset() {
    setTerm(""); setDebounced(""); setPicked(null); setRootFolderId("")
    setMonitorOption("all"); setMonitored(true); setSort("relevance")
  }
```

Replace the results `<ul>…</ul>` block (the one mapping `lookup.data`) with the sort control + poster grid:

```tsx
          {(lookup.data ?? []).length > 0 && (
            <div className="mt-3 flex items-center gap-2">
              <label htmlFor="add-sort" className="text-xs text-[var(--color-muted)]">Sort</label>
              <Select id="add-sort" aria-label="Sort" value={sort} onChange={(v) => setSort(v as AddSort)}>
                <option value="relevance">Relevance</option>
                <option value="newest">Newest</option>
                <option value="oldest">Oldest</option>
              </Select>
            </div>
          )}
          <div className="mt-3 grid max-h-96 grid-cols-3 gap-3 overflow-auto">
            {sortResults(lookup.data ?? [], sort).map((rr) => (
              <button
                key={rr.tmdbId}
                onClick={() => setPicked(rr)}
                className="group flex flex-col overflow-hidden rounded-md border border-[var(--color-border)] text-left hover:border-[var(--color-brand)]"
              >
                <div className="aspect-[2/3] w-full bg-[var(--color-panel-2)]">
                  {rr.posterUrl ? (
                    <img src={rr.posterUrl} alt={rr.title} className="h-full w-full object-cover" loading="lazy" />
                  ) : (
                    <div className="flex h-full w-full items-center justify-center text-xs text-[var(--color-muted)]">No poster</div>
                  )}
                </div>
                <div className="flex flex-col gap-0.5 p-2">
                  <span className="truncate text-sm font-medium" title={rr.title}>{rr.title}</span>
                  {rr.year ? <span className="text-xs text-[var(--color-muted)]">{rr.year}</span> : null}
                </div>
              </button>
            ))}
          </div>
```

Note: the `Select` component is already imported at the top of `AddMediaDialog.tsx`.

- [ ] **Step 8: Run the dialog + addSort tests to verify they pass**

Run: `cd web && npx vitest run src/features/library/AddMediaDialog.test.tsx src/features/library/addSort.test.ts`
Expected: PASS. (The existing "no root folders" test still works: it types "dune" and clicks the "Dune" tile, which still renders the title text.)

- [ ] **Step 9: Type-check + full library folder**

Run: `cd web && npx tsc -b && npx vitest run src/features/library`
Expected: tsc exit 0; all PASS.

- [ ] **Step 10: Commit**

```bash
git add web/src/features/library/addSort.ts web/src/features/library/addSort.test.ts web/src/features/library/AddMediaDialog.tsx web/src/features/library/AddMediaDialog.test.tsx
git commit -m "feat(webui): poster-grid Add dialog with year sort"
```

---

### Task 8: Rebuild dist + full verification + manual AC

**Files:**
- Modify: `web/dist/**` (build output — committed)

- [ ] **Step 1: Full FE suite + type check**

Run: `cd web && npm test && npx tsc -b`
Expected: all vitest files PASS; tsc exit 0.

- [ ] **Step 2: Rebuild the embedded bundle**

Run: `cd web && npm run build`
Expected: `tsc -b && vite build` succeeds; `web/dist` regenerated.

- [ ] **Step 3: Go build + the embed test**

Run (from repo root): `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./web/...`
Expected: build/vet clean; `web/spa_test.go` PASS (dist embeds).

- [ ] **Step 4: Manual browser acceptance (seeded instance)**

Start a throwaway instance on a fresh data dir (admin/admin), open the UI, and confirm:
- (a) Movies **and** TV grids show a **Card size** slider; dragging it resizes cards; the size **survives a page reload** (localStorage).
- (b) A show's detail page shows a **full-width top banner** (backdrop). If the existing Breaking Bad row's banner is blank, click **Refresh** once and reload — the backdrop appears.
- (c) On that detail page **Season 0 is labeled "Specials", sits at the very bottom, and starts collapsed**; regular seasons start expanded; clicking a season header toggles it.
- (d) Movie detail shows the same banner treatment.
- (e) The **Add** dialog shows results as a **poster grid** with a **Sort** control (Relevance/Newest/Oldest) that reorders results.
- Console shows zero errors.

- [ ] **Step 5: Confirm the drift guard is clean, then commit dist**

Run (from repo root): `git add web/dist && git status --porcelain web/dist`

```bash
git commit -m "build(webui): rebuild dist for library polish"
```

Then verify: `git diff --exit-code web/dist` exits 0.

---

## Self-Review

**Spec coverage:**
- Item 1 (card size + slider): Tasks 1–3. ✅
- Item 2 (banner across top): Tasks 4, 6. ✅ (Specials rename + last + collapsible): Task 5. ✅
- Item 4 (poster art + year sort in Add): Task 7. ✅
- Both TV + movie banner (user choice): Task 6. ✅
- Seasons expanded / Specials collapsed (user choice): `defaultOpen` in Task 5 helper + test. ✅
- Poster grid for Add (user choice): Task 7. ✅
- Shared/persisted scale (spec §3.1): Task 1 hook + Task 3 both pages. ✅
- web/dist drift guard + verify (spec §5): Task 8. ✅

**Placeholder scan:** No TBD/TODO; every code step shows complete code and exact commands. ✅

**Type consistency:** `scale?: number` (Task 1 `DEFAULT_SCALE` default) consistent across MediaGrid (Task 3) and pages. `SeasonSectionData`/`seasonSections` names identical in Task 5 helper, test, and SeasonTable refactor. `AddSort`/`sortResults` identical in Task 7 helper, test, and dialog. `DetailBanner` prop shape (`fanartUrl`, `posterUrl`, `title`, `children`) identical in Task 4 and its consumers in Task 6. ✅

**No backend files touched** in any task (Global Constraints). ✅
