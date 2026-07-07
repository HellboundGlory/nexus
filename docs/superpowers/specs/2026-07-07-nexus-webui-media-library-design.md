# Nexus Web UI — Sub-project 6, Slice 2: Media Library

**Date:** 2026-07-07
**Status:** Design approved, ready for implementation planning
**Depends on:** Slice 1 (Shell/Foundation, merge `00fbeb8`); backend sub-projects 4a (media), 4b (quality), 5a (automation search)

## 1. Purpose

Replace the `Movies` and `TV Shows` nav placeholders with a real media library: browse
what's in the library, see at a glance whether each item's files are present, add new
movies/series from TMDb, and perform per-item actions (monitor, assign quality profile,
refresh metadata, trigger a search, delete). Series get a season/episode detail view.

This is the second slice of the Web UI (Sub-project 6). It follows Slice 1's established
patterns (Vite + React 19 + TS + Tailwind v4 + shadcn/ui, TanStack Query v5, cookie auth via
the typed `web/src/lib/api.ts` client, committed `web/dist` with drift-guard).

## 2. Scope

**In scope**
- Movies **and** TV Shows, each with list + detail + add flow (both nav items become real).
- A thin, read-only backend enrichment exposing file presence (`hasFile` / file counts) on
  existing media endpoints — the only backend change.
- Add flow that consumes existing root folders + quality profiles (management deferred to the
  Settings slice), with graceful empty-states.
- Manual "Search" as a fire-and-forget trigger (toast feedback), matching the async
  `POST /automation/search/...` endpoints.

**Out of scope (deferred)**
- Interactive release picker (needs new backend work).
- Server-side search / sort / pagination (libraries are small; list endpoints return all).
- Root-folder and quality-profile **management** (Slice 3 / Settings).
- Per-item history and download-queue views (Activity slice).
- Calendar (Slice 5).
- Bulk actions, drag-and-drop.

## 3. Backend changes (the one thin touch)

All additive, read-only. No migration. Module boundary preserved: `internal/media` →
`internal/core/*` only.

### 3.1 Store count helpers (`internal/core/store/media_store.go`)
Two batch queries over the existing `media_files` table (`GROUP BY`, no N+1):
- `CountMovieFiles(ctx) (map[int64]int, error)` — movieID → file count.
- `CountEpisodeFiles(ctx) (map[int64]int, error)` — episodeID → file count.

`media_files` is the table created in migration `0006_import.sql`; these helpers only read it.
The exact join/columns follow the existing `MediaFile` model (keyed by media kind + owning id).

### 3.2 DTO enrichment (existing endpoints, no new routes)
Computed fields populated in the API handler layer (`internal/media/api.go`), **not** stored on
the row structs — the handler builds a response DTO wrapping the store row plus computed fields.

- `GET /movies` — each item gains `hasFile bool`.
- `GET /movies/{id}` — gains `hasFile bool`.
- `GET /series` — each item gains `episodeCount int` and `episodeFileCount int`. The denominator
  (`episodeCount`) counts **monitored** episodes only (Sonarr "monitored progress" semantics);
  `episodeFileCount` counts monitored episodes that have a file.
- `GET /series/{id}` — each episode in the existing `episodes[]` gains `hasFile bool`. Season-level
  counts are derived **client-side** from the episode list (no extra backend field).

Wire compatibility: fields are additive; existing consumers ignore unknown fields. Existing Go
tests remain green; new handler tests assert the computed values.

## 4. Frontend architecture (Approach A)

New feature folder co-locating all Slice-2 UI:

```
web/src/features/library/
  api.ts            // typed calls + TanStack Query hooks + query keys
  types.ts          // Movie, Series, SeriesDetail, Episode, RootFolder, QualityProfile, MetadataResult
  MediaGrid.tsx     // shared responsive poster grid + loading/empty/error states
  MediaCard.tsx     // poster + title + status badge (movies & series)
  StatusBadge.tsx   // Downloaded / Missing / Unmonitored / progress pill
  AddMediaDialog.tsx// TMDb search -> pick root folder + profile + monitor -> POST
  MovieDetail.tsx
  SeriesDetail.tsx
  SeasonTable.tsx   // per-season groups: season monitor toggle + episode rows
```

Thin route pages compose feature components:
- `web/src/pages/Movies.tsx`, `web/src/pages/TvShows.tsx`, `web/src/pages/MediaDetail.tsx`.
- `web/src/app/routes.tsx`: swap the `movies` / `tv` placeholders for the real pages; add
  `movies/:id` and `tv/:id` detail routes.

Shared UI primitive added this slice: a toast (shadcn `sonner`) mounted in the app shell.

## 5. Data flow

- **Reads** via TanStack Query hooks: `useMovies`, `useSeries`, `useMovieDetail(id)`,
  `useSeriesDetail(id)`, `useRootFolders`, `useQualityProfiles`, `useLookup(term, kind)`.
  Query keys namespaced, e.g. `["library","movies"]`, `["library","series",id]`.
- **Mutations** via hooks that call the typed client then `invalidateQueries` on affected keys:
  `useAddMovie`/`useAddSeries`, `useSetMonitored` (series/season/episode/movie), `useRefresh`,
  `useDelete`, `useAssignProfile`, `useSearch`. `useSearch` is fire-and-forget → success toast.
- `useLookup` is debounced (~300ms) and disabled while the term is empty (avoids hammering TMDb).
- All calls go through the existing `api.ts` `request()` (cookie auth, global 401 handler, error
  envelope). Extend `api.ts` with `apiPut` and `apiDelete` helpers (only `apiGet`/`apiPost` exist
  today).

### 5.1 Endpoint map (all authed `/api/v1`, cookie auth)
| UI action | Method + path |
|---|---|
| List movies / series | `GET /movies`, `GET /series` |
| Detail | `GET /movies/{id}`, `GET /series/{id}` |
| TMDb lookup | `GET /media/lookup?term=&kind=movie\|tv` |
| Add | `POST /movies`, `POST /series` |
| Monitor toggles | `PUT /movies/{id}/monitor`, `/series/{id}/monitor`, `/season/{id}/monitor`, `/episode/{id}/monitor` |
| Assign profile | `PUT /movies/{id}/qualityprofile`, `/series/{id}/qualityprofile` |
| Refresh | `POST /movies/{id}/refresh`, `/series/{id}/refresh` |
| Delete | `DELETE /movies/{id}`, `/series/{id}` |
| Search (fire-and-forget) | `POST /automation/search/movie/{id}`, `/series/{id}`, `/series/{id}/season/{n}`, `/episode/{id}` |
| Dropdown sources | `GET /rootfolder`, `GET /qualityprofile` |

## 6. Per-screen UX

### 6.1 Movies / TV list (`/movies`, `/tv`)
- Responsive poster grid of `MediaCard`s (poster from `posterUrl`, placeholder tile when empty;
  title; year/network line; status badge).
- Top bar: client-side title-filter text input + "+ Add" button (opens `AddMediaDialog`).
- No server-side search/sort/paging this slice.
- Badges:
  - Movies: `Downloaded` (green, `hasFile`) / `Missing` (amber, monitored & no file) /
    `Unmonitored` (grey).
  - Series: `episodeFileCount / episodeCount` progress (e.g. "7 / 10"); complete = green,
    partial-or-zero-while-monitored = amber, unmonitored = grey.

### 6.2 Add dialog (shared)
- Search field → debounced `GET /media/lookup` → results (poster thumb, title, year, overview
  snippet). Select one → form:
  - Root folder dropdown (from `useRootFolders`).
  - Quality profile dropdown (optional; empty allowed).
  - Monitor option: series `all` / `future` / `none`; movie monitored checkbox.
- Submit → `POST` → invalidate list query → success toast → close.
- **Empty-state guards:**
  - No root folders (`[]`): form shows "No root folder configured — add one in Settings" and
    disables submit. (The API allows a null root folder, but adding without one is a foot-gun;
    the UI requires it.)
  - No quality profiles: dropdown shows "None configured yet (optional)"; submit still allowed
    (`quality_profile_id` is nullable, and no default profile is seeded on a fresh install).

### 6.3 Movie detail (`/movies/:id`)
- Header: fanart/poster, title, year, overview, status, file badge.
- Action row: Monitor toggle, Quality-profile select (inline `PUT`), Search (fire-and-forget →
  toast), Refresh (→ toast), Delete (confirm dialog → navigate back to list).

### 6.4 Series detail (`/tv/:id`)
- Same header/actions as movie (minus movie-specific fields), plus `SeasonTable`:
  - Seasons as collapsible groups; each has a season-level monitor toggle and a "x / y" count
    (derived client-side from episodes).
  - Episode rows: number, title, air date, `hasFile` badge, per-episode monitor toggle,
    per-episode search button.
  - Season-level search → `POST /automation/search/series/{id}/season/{n}`.

## 7. Error & empty states
- Loading: skeleton grid / detail skeleton (shadcn skeleton).
- Query error: inline error card with Retry (re-runs query) — mirrors Slice 1 Dashboard error
  handling (the IMP-1 precedent).
- Empty library: "No movies yet — click Add to get started."
- Mutation errors: `ApiError.message` surfaced via toast.
- 404 detail (bad id): "Not found" panel with a back link.
- 401: already handled globally by `api.ts` → redirect to login.

## 8. Testing (Vitest + React Testing Library + Go)
- **Backend (Go):** handler tests asserting `hasFile` / counts appear in list + detail JSON;
  store tests for `CountMovieFiles` / `CountEpisodeFiles` (no files, single, multi-file,
  monitored-only denominator).
- **Frontend unit:** `StatusBadge` state logic (all badge variants); `MediaCard` render; series
  progress math.
- **Hooks/integration (mocked fetch):** add flow (lookup → submit → invalidation); monitor
  toggle invalidation; search toast; empty-root-folder guard; query-error retry.
- **Router:** `movies` / `tv` / detail routes resolve; placeholders removed.
- **Merge gates (same as Slice 1):** Vitest green, `tsc -b` exit 0,
  `CGO_ENABLED=0 go build/vet/test ./...` green, `web/dist` drift-guard clean (rebuild committed
  bundle).

## 9. Acceptance criteria
1. `/movies` and `/tv` render a poster grid of library items with correct status badges;
   placeholders are gone.
2. Clicking an item opens its detail page (`/movies/:id`, `/tv/:id`) with metadata and actions.
3. "+ Add" performs a TMDb lookup and adds a movie/series with chosen root folder, optional
   profile, and monitor option; the new item appears in the grid without a manual reload.
4. Monitor toggles (item/season/episode), quality-profile assignment, refresh, delete, and
   search all call the correct endpoints and reflect their result (invalidation or toast).
5. Series detail shows seasons/episodes with per-episode file badges and monitor toggles.
6. File-presence badges are driven by the new backend `hasFile` / count fields.
7. Empty-states (no library items, no root folders, no profiles) render gracefully.
8. All merge gates pass; module boundary unchanged (`internal/media` → `core/*` only).

## 10. Notes / decisions carried from brainstorming
- Both media types built together (shared grid/card/add-dialog; TV adds `SeasonTable`).
- One thin read-only backend touch for file presence (Approach A) rather than a separate status
  endpoint (Approach B) or per-item N+1 (Approach C).
- Root-folder / quality-profile management stays in Settings (Slice 3); the Add dialog only
  consumes existing lists with graceful empty-states.
- No default quality profile is seeded (verified: `0005_quality.sql` creates an empty table) —
  profile selection is optional in the Add dialog.
- Manual search is a fire-and-forget trigger with toast feedback; interactive release-picking is
  a future feature requiring new backend work.

## 11. Slice-1 deferred backlog to fold in (fix before the relevant screen renders)
From Slice 1 final review (see project-status memory "SLICE-2 KICKOFF BACKLOG"):
1. `web/src/lib/api.ts` global 401 handler fires on any 401 incl. `POST /auth/login` — gate the
   login path (or only fire when previously-authed) before an authed endpoint legitimately 401s.
2. `web/src/styles/index.css`: add `@layer base { * { @apply border-border outline-ring/50; } }`
   **before** any slice renders a bare `<Card>` (else near-white border on dark). Slice 2 renders
   shared Cards, so this must land here.
3. `web/src/lib/ws.ts`: store + clear the reconnect timer id (cancellable) — lower priority
   (matters for Activity slice), fix if touched.
