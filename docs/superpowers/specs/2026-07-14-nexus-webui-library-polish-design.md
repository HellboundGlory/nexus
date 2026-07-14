# Nexus Web UI — Library Polish (Sub-project A) Design

**Date:** 2026-07-14
**Status:** Approved (design)
**Scope:** Pure frontend. No backend endpoints, no DTO changes, no migration.

## 1. Context

This is sub-project **A** of a four-part Web UI improvement effort the user requested
after test-driving the app locally. The full set:

- **A — Library UI polish** (this doc): items 1, 2, 4 below.
- B — Interactive ("manual") search modal (item 3). *Separate spec.*
- C — Discover page with TMDb filters (item 5). *Separate spec.*
- D — System page UI (item 6). *Separate spec.*

Ordering agreed with the user: **A → B → C → D**, each its own design → plan → build cycle.

The three user requests folded into A:

1. TV/movie library cards are too large; make the default ~half size and add a
   slider to change card scale dynamically.
2. On a show's detail page the backdrop is rendered squished down the left side
   (the `w-40` poster in a flex row). Move the backdrop to a full-width banner
   across the top. Rename the "Season 0" section to **Specials**, move it to the
   bottom, and make each season section **collapsible**.
4. The Add dialog shows only title + year. Add poster art per result and a sort
   control (by year).

All data these need already exists server-side:
- `MetadataResult.posterUrl` is already returned by `GET /media/lookup`
  (`web/src/features/library/types.ts`).
- `Series.fanartUrl` / `Movie.fanartUrl` are populated from TMDb `backdrop_path`
  (`internal/media/tmdb.go:174,213`) and written back on refresh
  (`internal/media/media.go:233,284`).

Rows added before the backdrop mapping existed may have an empty `fanartUrl`; the
banner falls back gracefully and a **Refresh** backfills it. No data migration.

## 2. Design decisions (user-confirmed)

- Scale slider applies to **both** Movies and TV grids, sharing one persisted value.
- Slider is a **continuous** range control; default sits at ~half the current card
  size. Persisted in `localStorage`.
- Detail banner redesign applies to **both** TV and movie detail pages.
- Season sections default state: **regular seasons expanded, Specials collapsed**.
- Add-dialog results render as a **poster grid**; sort options **Relevance
  (default) / Newest / Oldest**.

## 3. Components & changes

### 3.1 Grid scaling (item 1)

**New hook — `web/src/features/library/useGridScale.ts`**
- Returns `[scale, setScale]` where `scale` is card min-width in px.
- Backed by `localStorage` key `nexus.grid.scale`. Clamped to `[MIN, MAX]`
  (MIN = 110, MAX = 230). Default `DEFAULT = 120` (≈ half the current ~
  `lg:grid-cols-6` card width on a typical viewport).
- Pure clamp/parse logic is unit-testable without React (extract
  `clampScale(n)` + `readScale()` helpers).

**`MediaGrid.tsx`**
- Add optional `scale?: number` prop (default `DEFAULT` when omitted, so existing
  callers/tests that don't pass it still render).
- Replace the fixed `grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6`
  with a single `grid` whose `style={{ gridTemplateColumns:
  \`repeat(auto-fill, minmax(${scale}px, 1fr))\` }}`. Applies to both the loaded
  grid and the loading-skeleton grid (same column template).
- `gap-4 p-6` retained.

**New component — `web/src/features/library/ScaleSlider.tsx`**
- Controlled: `{ value: number; onChange: (n: number) => void }`.
- Renders a small-poster icon, `<input type="range" min={MIN} max={MAX}>`
  (value = `scale`, so dragging right raises the min-width → larger cards), a
  large-poster icon. `aria-label="Card size"`.

**`pages/Movies.tsx` and `pages/TvShows.tsx`**
- Call `useGridScale()`, render `<ScaleSlider>` in the existing toolbar row
  (next to Filter / +Add), pass `scale` into `<MediaGrid>`.

### 3.2 Detail page redesign (item 2)

**New component — `web/src/features/library/DetailBanner.tsx`**
- Props: `{ fanartUrl: string; posterUrl: string; title: string; children:
  ReactNode }` (children = the header meta + action buttons block).
- Layout: full-bleed container (breaks the page `p-6` padding to span edge-to-edge
  at the top). Background = `fanartUrl` via `background-image`, `bg-cover`,
  `bg-center`, height ~ `h-[320px]`. A gradient overlay
  (`from-transparent to-[var(--color-bg)]`, bottom-heavy) keeps text legible and
  fades into the page. When `fanartUrl` is empty, render the flat
  `var(--color-panel-2)` background instead (no broken image).
- Content sits in the lower portion: small poster inset (optional), then
  `children`.
- Used by both `SeriesDetail` and `MovieDetail`; the existing `← Back` link sits
  above/over the banner.

**New component — `web/src/features/library/SeasonSection.tsx`**
- One collapsible season. Props: `{ title: string; withFile: number; total:
  number; monitored: boolean; defaultOpen: boolean; onToggleMonitor; onSearch;
  children }` (children = the episode list `<ul>`).
- Header (always visible): disclosure chevron + `title` + progress
  `StatusBadge` + Search + monitor checkbox. Clicking the header row (not the
  checkbox/search controls) toggles open/closed. Internal `open` state seeded
  from `defaultOpen`.
- Body (episode list) shown only when open.

**`SeasonTable.tsx`**
- Compute season display + ordering:
  - Sort regular seasons (number > 0) ascending.
  - Season 0 (if present) is titled **"Specials"** and appended **last**.
  - Regular seasons titled `Season {n}`.
- Render each via `<SeasonSection>` with `defaultOpen = seasonNumber !== 0`
  (Specials closed, regular seasons open).
- The episode `<ul>` (unchanged row markup) becomes the `SeasonSection` child.
- Pure ordering/labeling helper extracted for unit test:
  `seasonSections(seasons, episodes)` → array of `{ id, seasonNumber, title,
  defaultOpen, eps, withFile }`.

**`SeriesDetail.tsx`**
- Replace the `flex gap-6` poster-beside-content layout with:
  `<DetailBanner …>` wrapping the current title/year/status + action buttons +
  quality-profile select, then `<SeasonTable …>` below in a normal padded
  container.

**`MovieDetail.tsx`**
- Same `<DetailBanner>` treatment wrapping the existing movie header + actions
  (no season list). (Read the current file during implementation; keep all
  existing actions/behavior — only the layout wrapper changes.)

### 3.3 Add dialog (item 4)

**`AddMediaDialog.tsx`** (search/pick step only; the picked/confirm step is
unchanged except it may show the chosen poster)
- Add local `sort` state: `"relevance" | "newest" | "oldest"`, default
  `"relevance"`. A small `<Select>` (or segmented buttons) above the results.
- Derive displayed results: `relevance` = TMDb order as-is; `newest` = by `year`
  desc; `oldest` = by `year` asc. Results with `year === 0`/missing sort last in
  newest/oldest. Pure helper `sortResults(results, sort)` for unit test.
- Replace the `<ul>` text list with a **poster grid** (`grid grid-cols-3 gap-3`,
  scrollable, `max-h-*`): each result a button tile with poster image
  (`aspect-[2/3]`, `object-cover`), placeholder ("No poster") when `posterUrl`
  empty, title (truncated) + year below. Clicking a tile sets `picked` (existing
  behavior).
- Error / loading / empty / not-configured states unchanged.

## 4. Testing

Vitest (jsdom), matching the repo's existing test style:
- `useGridScale`: `clampScale` clamps below MIN / above MAX / passes through;
  `readScale` returns default on missing/garbage localStorage, persisted value
  otherwise.
- `MediaGrid`: renders with a passed `scale` (assert the inline
  `gridTemplateColumns` reflects the value); still renders when `scale` omitted.
- `SeasonTable` / `seasonSections`: Season 0 labeled "Specials" and ordered last;
  regular seasons ascending; `defaultOpen` false for specials, true otherwise;
  `withFile`/`total` counts correct.
- `SeasonSection`: body hidden when `defaultOpen=false`, visible after header
  click; toggling monitor/search does not collapse.
- `AddMediaDialog` / `sortResults`: newest/oldest ordering incl. missing-year
  handling; poster tile renders image when `posterUrl` present, placeholder when
  empty.
- Update existing `MediaGrid.test.tsx`, `SeriesDetail.test.tsx`,
  `AddMediaDialog.test.tsx` for the new markup.

## 5. Build & verify

- Follows the project's established flow: this spec → implementation plan
  (writing-plans) → build (SDD subagent loop or direct TDD, decided at plan time).
- Every touched slice ends with `web/dist` rebuilt; the `git diff --exit-code
  web/dist` drift guard must be clean.
- Full verify: FE `vitest` green, `tsc -b` exit 0, `CGO_ENABLED=0 go
  build/vet/test ./...` (the Go `web/spa_test.go` embeds dist).
- Live browser AC on a seeded instance: (a) slider resizes cards on both Movies
  and TV and the size survives a reload; (b) TV detail shows a full-width top
  banner, Specials labeled + last + collapsed, seasons collapsible; (c) movie
  detail shows the same banner; (d) Add dialog shows a poster grid and the sort
  control reorders results.
- **ASK before pushing `master`** (standing rule).

## 6. Out of scope

- Any backend change (no new fanart source, no lookup changes).
- Filtering the Add results by year (only *sorting* was requested).
- Persisting per-page (Movies vs TV) independent scales — one shared value.
- Interactive search, Discover, System (sub-projects B/C/D).
