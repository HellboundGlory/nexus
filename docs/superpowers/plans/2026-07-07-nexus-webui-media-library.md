# Nexus Web UI — Media Library (Slice 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Movies and TV Shows placeholders with a working media library — poster-grid lists, TMDb add flow, and per-item detail pages with monitor/profile/refresh/delete/search actions — backed by a thin read-only file-presence enrichment on existing media endpoints.

**Architecture:** Frontend feature folder (`web/src/features/library/`) using the Slice-1 stack (React 19 + TS + Tailwind v4 + `radix-ui` primitives + TanStack Query v5 + the typed `web/src/lib/api.ts` client). One backend touch: additive computed JSON fields (`hasFile`, episode counts) on `GET /movies`, `/series`, and their detail routes, sourced from three new read-only store helpers over the existing `media_files` table. No migration; module boundary `internal/media` → `internal/core/*` only, unchanged.

**Tech Stack:** Go 1.x (chi, database/sql, SQLite), React 19, TypeScript 5.7, Tailwind v4, `radix-ui` 1.6, TanStack Query 5, Vitest 2 + React Testing Library.

## Global Constraints

- **Go env:** Go lives at `C:\Program Files\Go\bin`, NOT on the session PATH. Prefix every Go command: `export PATH="/c/Program Files/Go/bin:$PATH"`. Build with `CGO_ENABLED=0`. `go test -race` is unavailable (no C toolchain) — use `-count=N` for concurrency confidence.
- **Module boundary:** `internal/media` may import `internal/core/*` only. No new cross-feature imports.
- **No new npm dependencies.** UI primitives are hand-written wrappers over the already-present `radix-ui` package (matches Slice 1's `components/ui/`). The toast is a small hand-written context — do NOT add `sonner`.
- **Committed `web/dist`:** the Go binary embeds `web/dist`. The merge gate runs `git diff --exit-code web/dist`. Frontend source tasks run `npm test` + `npx tsc -b` only; the final task rebuilds and commits `web/dist`. Do not rebuild dist mid-plan.
- **Error envelope:** backend errors use `api.WriteError(w, status, code, message)` → `{"error":{"code","message"}}`. Frontend surfaces `ApiError.message`.
- **JSON field names are camelCase** (Go struct tags already camelCase). New fields: `hasFile`, `episodeCount`, `episodeFileCount`.
- **All frontend API calls go through `web/src/lib/api.ts`** (`credentials:"include"`, global 401 handler, error-envelope normalization).
- **Commands run from the repo root** `C:\Users\James\Downloads\Projects\Nexus` unless noted. Frontend commands run from `web/`.

---

## File Structure

**Backend (modify):**
- `internal/core/store/import_store.go` — add `MovieFileIDs`, `EpisodeFileIDs`, `SeriesEpisodeStats` + `SeriesEpisodeStats` type.
- `internal/core/store/import_store_test.go` (create if absent) — tests for the three helpers.
- `internal/media/api.go` — DTO enrichment on `listMovies`, `getMovie`, `listSeries`, `getSeries`.
- `internal/media/api_test.go` — assertions for the new fields.

**Frontend foundation (modify):**
- `web/src/lib/api.ts` — add `apiPut`, `apiDelete`; gate the global 401 handler off the login path.
- `web/src/lib/api.test.ts` — cover the new helpers + login-gate.
- `web/src/styles/index.css` — base-layer default border color.
- `web/src/lib/toast.tsx` (create) — `ToastProvider`, `useToast()`.
- `web/src/lib/toast.test.tsx` (create).
- `web/src/app/Layout.tsx` — mount `ToastProvider`.

**Frontend feature (create):**
- `web/src/features/library/types.ts`
- `web/src/features/library/api.ts`
- `web/src/features/library/StatusBadge.tsx` + `.test.tsx`
- `web/src/features/library/MediaCard.tsx`
- `web/src/features/library/MediaGrid.tsx` + `.test.tsx`
- `web/src/features/library/AddMediaDialog.tsx` + `.test.tsx`
- `web/src/features/library/MovieDetail.tsx`
- `web/src/features/library/SeriesDetail.tsx`
- `web/src/features/library/SeasonTable.tsx`
- `web/src/pages/Movies.tsx`, `web/src/pages/TvShows.tsx`, `web/src/pages/MediaDetail.tsx` (+ a route test)
- `web/src/components/ui/` — hand-written `dialog.tsx`, `select.tsx`, `switch.tsx`, `collapsible.tsx` wrappers as needed.
- `web/src/app/routes.tsx` — swap placeholders, add detail routes.

---

## Task 1: Store file-presence helpers

**Files:**
- Modify: `internal/core/store/import_store.go`
- Test: `internal/core/store/import_store_test.go` (create if it does not exist)

**Interfaces:**
- Consumes: existing `media_files` table (cols `movie_id`, `episode_id`, both `UNIQUE`), `episodes` table (col `monitored`, `series_id`), and the test helper that opens a migrated store (see step 1).
- Produces:
  - `func (s *Store) MovieFileIDs(ctx context.Context) (map[int64]bool, error)`
  - `func (s *Store) EpisodeFileIDs(ctx context.Context) (map[int64]bool, error)`
  - `type SeriesEpisodeStats struct { EpisodeCount int; EpisodeFileCount int }`
  - `func (s *Store) SeriesEpisodeStats(ctx context.Context) (map[int64]SeriesEpisodeStats, error)` — keyed by series id; counts **monitored** episodes only; `EpisodeFileCount` = monitored episodes that have a file.

- [ ] **Step 1: Find the store test bootstrap.** Open an existing store test (e.g. `internal/core/store/media_store_test.go`) and note the helper that returns a migrated `*Store` (commonly `newTestStore(t)` or similar). Reuse it verbatim; do NOT invent a new one. If `import_store_test.go` does not exist, create it in `package store` importing the same helper.

- [ ] **Step 2: Write the failing test.**

```go
package store

import (
	"context"
	"testing"
)

func TestFilePresenceHelpers(t *testing.T) {
	st := newTestStore(t) // reuse the existing migrated-store helper
	ctx := context.Background()

	// Root folder + series + two monitored episodes; one gets a file.
	rfID, err := st.CreateRootFolder(ctx, "/data/tv")
	if err != nil {
		t.Fatal(err)
	}
	se, err := st.UpsertSeries(ctx, Series{TMDBID: 1, Title: "Show", RootFolderID: &rfID, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	ep1, err := st.UpsertEpisode(ctx, Episode{SeriesID: se.ID, SeasonNumber: 1, EpisodeNumber: 1, TMDBID: 11, Title: "E1", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertEpisode(ctx, Episode{SeriesID: se.ID, SeasonNumber: 1, EpisodeNumber: 2, TMDBID: 12, Title: "E2", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	// A movie with a file.
	mv, err := st.UpsertMovie(ctx, Movie{TMDBID: 2, Title: "Film", RootFolderID: &rfID, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, MediaFile{MediaKind: "episode", EpisodeID: &ep1.ID, RelativePath: "e1.mkv", Size: 1, QualityID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, MediaFile{MediaKind: "movie", MovieID: &mv.ID, RelativePath: "film.mkv", Size: 1, QualityID: 1}); err != nil {
		t.Fatal(err)
	}

	epFiles, err := st.EpisodeFileIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !epFiles[ep1.ID] || len(epFiles) != 1 {
		t.Fatalf("EpisodeFileIDs = %v, want only ep1", epFiles)
	}
	mvFiles, err := st.MovieFileIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !mvFiles[mv.ID] || len(mvFiles) != 1 {
		t.Fatalf("MovieFileIDs = %v, want only mv", mvFiles)
	}
	stats, err := st.SeriesEpisodeStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := stats[se.ID]
	if got.EpisodeCount != 2 || got.EpisodeFileCount != 1 {
		t.Fatalf("SeriesEpisodeStats[series] = %+v, want {2 1}", got)
	}
}
```

> NOTE: If the real constructor signatures differ (e.g. `UpsertSeries`/`UpsertEpisode`/`UpsertMovie` return `(int64, error)` rather than a struct, or take different fields), adapt the test to the actual signatures in `media_store.go` — read them first. The assertions (counts) stay the same.

- [ ] **Step 3: Run the test to verify it fails.**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/core/store/ -run TestFilePresenceHelpers -count=1`
Expected: FAIL — `MovieFileIDs`/`EpisodeFileIDs`/`SeriesEpisodeStats` undefined.

- [ ] **Step 4: Implement the helpers** in `import_store.go`.

```go
// MovieFileIDs returns the set of movie ids that currently have a file.
func (s *Store) MovieFileIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT movie_id FROM media_files WHERE movie_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// EpisodeFileIDs returns the set of episode ids that currently have a file.
func (s *Store) EpisodeFileIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT episode_id FROM media_files WHERE episode_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SeriesEpisodeStats reports, per series, the count of monitored episodes and
// how many of those have a file. Series with no monitored episodes are absent
// from the map (callers treat a missing key as the zero value).
type SeriesEpisodeStats struct {
	EpisodeCount     int
	EpisodeFileCount int
}

func (s *Store) SeriesEpisodeStats(ctx context.Context) (map[int64]SeriesEpisodeStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.series_id,
		       SUM(CASE WHEN e.monitored = 1 THEN 1 ELSE 0 END) AS monitored_count,
		       SUM(CASE WHEN e.monitored = 1 AND mf.episode_id IS NOT NULL THEN 1 ELSE 0 END) AS monitored_with_file
		FROM episodes e
		LEFT JOIN media_files mf ON mf.episode_id = e.id
		GROUP BY e.series_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]SeriesEpisodeStats)
	for rows.Next() {
		var seriesID int64
		var count, withFile int
		if err := rows.Scan(&seriesID, &count, &withFile); err != nil {
			return nil, err
		}
		out[seriesID] = SeriesEpisodeStats{EpisodeCount: count, EpisodeFileCount: withFile}
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Run the test to verify it passes.**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/core/store/ -run TestFilePresenceHelpers -count=1`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add internal/core/store/import_store.go internal/core/store/import_store_test.go
git commit -m "feat(6-2): store file-presence helpers for library UI"
```

---

## Task 2: Media API DTO enrichment

**Files:**
- Modify: `internal/media/api.go` (`listMovies`, `getMovie`, `listSeries`, `getSeries`, and the `seriesDetail` struct)
- Test: `internal/media/api_test.go`

**Interfaces:**
- Consumes: Task 1 helpers `MovieFileIDs`, `EpisodeFileIDs`, `SeriesEpisodeStats`; existing `a.store.ListMovies/ListSeries/ListSeasons/ListEpisodes/GetMovie/GetSeries`.
- Produces (wire shapes for the frontend):
  - `GET /movies` → array of `store.Movie` fields **plus** `hasFile bool`.
  - `GET /movies/{id}` → `store.Movie` fields **plus** `hasFile bool`.
  - `GET /series` → array of `store.Series` fields **plus** `episodeCount int`, `episodeFileCount int`.
  - `GET /series/{id}` → `store.Series` fields + `seasons: []store.Season` + `episodes: []{...store.Episode, hasFile bool}`.

- [ ] **Step 1: Write the failing test** (append to `api_test.go`). It adds a movie + a series with an episode, attaches a file to each, and asserts the new fields appear.

```go
func TestAPIListEnrichment(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries(), movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()

	// Add a movie and a series through the API so ids exist.
	post(t, r, "/movies", `{"tmdbId":200,"monitored":true}`, http.StatusCreated)
	post(t, r, "/series", `{"tmdbId":100,"monitorOption":"all"}`, http.StatusCreated)

	// Attach a file to the movie directly in the store.
	movies, _ := st.ListMovies(ctx)
	if len(movies) == 0 {
		t.Fatal("no movie added")
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &movies[0].ID, RelativePath: "m.mkv", Size: 1, QualityID: 1}); err != nil {
		t.Fatal(err)
	}

	// Movie list carries hasFile=true.
	body := get(t, r, "/movies", http.StatusOK)
	var ml []map[string]any
	mustJSON(t, body, &ml)
	if len(ml) != 1 || ml[0]["hasFile"] != true {
		t.Fatalf("movie list missing hasFile: %s", body)
	}

	// Series list carries episodeCount / episodeFileCount.
	body = get(t, r, "/series", http.StatusOK)
	var sl []map[string]any
	mustJSON(t, body, &sl)
	if len(sl) != 1 {
		t.Fatalf("series list len: %s", body)
	}
	if _, ok := sl[0]["episodeCount"]; !ok {
		t.Fatalf("series list missing episodeCount: %s", body)
	}
	if _, ok := sl[0]["episodeFileCount"]; !ok {
		t.Fatalf("series list missing episodeFileCount: %s", body)
	}

	// Series detail episodes carry hasFile.
	sid := int64(sl[0]["id"].(float64))
	body = get(t, r, "/series/"+strconv.FormatInt(sid, 10), http.StatusOK)
	var detail map[string]any
	mustJSON(t, body, &detail)
	eps, _ := detail["episodes"].([]any)
	if len(eps) == 0 {
		t.Fatalf("series detail has no episodes: %s", body)
	}
	first := eps[0].(map[string]any)
	if _, ok := first["hasFile"]; !ok {
		t.Fatalf("episode missing hasFile: %s", body)
	}
}

// small helpers local to the test file
func post(t *testing.T, r http.Handler, path, body string, want int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("POST %s = %d want %d body=%s", path, w.Code, want, w.Body.String())
	}
}
func get(t *testing.T, r http.Handler, path string, want int) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("GET %s = %d want %d body=%s", path, w.Code, want, w.Body.String())
	}
	return w.Body.String()
}
func mustJSON(t *testing.T, body string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
}
```

> NOTE: `sampleMovies()` may not exist yet — mirror `sampleSeries()` in the test support file (`service_test.go` / wherever `sampleSeries` lives). If `fakeProvider` has no `movies` field, add one alongside `series` and return it from `SearchMovie`/`GetMovie`. Read the existing fake before editing. Add `"context"` to imports if missing.

- [ ] **Step 2: Run to verify it fails.**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/media/ -run TestAPIListEnrichment -count=1`
Expected: FAIL (fields absent / helpers unused).

- [ ] **Step 3: Implement enrichment** in `api.go`. Add DTO types and update the four handlers.

```go
type movieDTO struct {
	store.Movie
	HasFile bool `json:"hasFile"`
}

type seriesListItem struct {
	store.Series
	EpisodeCount     int `json:"episodeCount"`
	EpisodeFileCount int `json:"episodeFileCount"`
}

type episodeDTO struct {
	store.Episode
	HasFile bool `json:"hasFile"`
}
```

Update `listMovies`:

```go
func (a *API) listMovies(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListMovies(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list movies")
		return
	}
	files, err := a.store.MovieFileIDs(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list movies")
		return
	}
	out := make([]movieDTO, 0, len(rows))
	for _, m := range rows {
		out = append(out, movieDTO{Movie: m, HasFile: files[m.ID]})
	}
	api.WriteJSON(w, http.StatusOK, out)
}
```

Update `getMovie` (after the existing load + not-found handling, replace the final write):

```go
	files, err := a.store.MovieFileIDs(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load movie")
		return
	}
	api.WriteJSON(w, http.StatusOK, movieDTO{Movie: *m, HasFile: files[m.ID]})
```

Update `listSeries`:

```go
func (a *API) listSeries(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListSeries(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list series")
		return
	}
	stats, err := a.store.SeriesEpisodeStats(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list series")
		return
	}
	out := make([]seriesListItem, 0, len(rows))
	for _, s := range rows {
		st := stats[s.ID]
		out = append(out, seriesListItem{Series: s, EpisodeCount: st.EpisodeCount, EpisodeFileCount: st.EpisodeFileCount})
	}
	api.WriteJSON(w, http.StatusOK, out)
}
```

Update `seriesDetail` + `getSeries` to attach per-episode `hasFile`:

```go
type seriesDetail struct {
	store.Series
	Seasons  []store.Season `json:"seasons"`
	Episodes []episodeDTO   `json:"episodes"`
}
```

In `getSeries`, after loading `seasons` and `episodes`, before writing:

```go
	epFiles, err := a.store.EpisodeFileIDs(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load series")
		return
	}
	epDTOs := make([]episodeDTO, 0, len(episodes))
	for _, e := range episodes {
		epDTOs = append(epDTOs, episodeDTO{Episode: e, HasFile: epFiles[e.ID]})
	}
	api.WriteJSON(w, http.StatusOK, seriesDetail{Series: *se, Seasons: seasons, Episodes: epDTOs})
```

- [ ] **Step 4: Run the new test + the whole media package.**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/media/ -count=1`
Expected: PASS (new test + all existing media tests, including `TestAPINoCredentialLeak`).

- [ ] **Step 5: Build + vet the whole module.**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go build ./... && go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 6: Commit.**

```bash
git add internal/media/api.go internal/media/api_test.go internal/media/service_test.go
git commit -m "feat(6-2): enrich media list/detail JSON with file-presence fields"
```

---

## Task 3: Frontend api.ts — PUT/DELETE helpers + login-gated 401

**Files:**
- Modify: `web/src/lib/api.ts`
- Test: `web/src/lib/api.test.ts`

**Interfaces:**
- Consumes: existing `request<T>`, `ApiError`, `unauthorizedHandler`.
- Produces:
  - `export function apiPut<T>(path: string, body?: unknown): Promise<T>`
  - `export function apiDelete<T>(path: string): Promise<T>`
  - Behavior change: the global `unauthorizedHandler` does NOT fire for `POST /auth/login` (a bad-password 401 must not trigger the app-wide logout/redirect). (Slice-1 backlog item 1.)

- [ ] **Step 1: Write the failing tests** (append to `api.test.ts`). Mirror the existing fetch-mock style in that file (read it first for the exact `global.fetch` mock shape).

```ts
it("apiPut sends a PUT with JSON body", async () => {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "Content-Type": "application/json" } }),
  )
  vi.stubGlobal("fetch", fetchMock)
  await apiPut("/series/1/monitor", { monitored: true })
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/series/1/monitor",
    expect.objectContaining({ method: "PUT", credentials: "include" }),
  )
})

it("apiDelete sends a DELETE", async () => {
  const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 200 }))
  vi.stubGlobal("fetch", fetchMock)
  await apiDelete("/movies/1")
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/movies/1",
    expect.objectContaining({ method: "DELETE" }),
  )
})

it("does not fire the unauthorized handler on a login 401", async () => {
  const handler = vi.fn()
  setUnauthorizedHandler(handler)
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ error: { code: "unauthorized", message: "bad" } }), { status: 401 }),
  )
  vi.stubGlobal("fetch", fetchMock)
  await expect(login("admin", "wrong")).rejects.toBeInstanceOf(ApiError)
  expect(handler).not.toHaveBeenCalled()
  setUnauthorizedHandler(null)
})
```

Ensure the import line at the top of the test pulls in the new symbols: `apiPut, apiDelete, setUnauthorizedHandler, login, ApiError`.

- [ ] **Step 2: Run to verify it fails.**

Run (from `web/`): `npm test -- src/lib/api.test.ts`
Expected: FAIL — `apiPut`/`apiDelete` undefined; login-401 fires handler.

- [ ] **Step 3: Implement** in `api.ts`. Thread a flag so `request` can skip the 401 hook for the login call, and add the helpers.

```ts
async function request<T>(method: string, path: string, body?: unknown, skipAuthHandler = false): Promise<T> {
  const init: RequestInit = {
    method,
    credentials: "include",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  }
  const res = await fetch(`${BASE}${path}`, init)
  if (res.status === 401 && !skipAuthHandler && unauthorizedHandler) unauthorizedHandler()
  if (!res.ok) throw await toApiError(res)
  if (res.status === 204) return undefined as T
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

export function apiPut<T>(path: string, body?: unknown): Promise<T> {
  return request<T>("PUT", path, body)
}
export function apiDelete<T>(path: string): Promise<T> {
  return request<T>("DELETE", path)
}
```

Update `login` to skip the handler:

```ts
export function login(username: string, password: string): Promise<void> {
  return request<void>("POST", "/auth/login", { username, password }, true)
}
```

- [ ] **Step 4: Run to verify it passes.**

Run (from `web/`): `npm test -- src/lib/api.test.ts`
Expected: PASS (new + existing api tests).

- [ ] **Step 5: Commit.**

```bash
git add web/src/lib/api.ts web/src/lib/api.test.ts
git commit -m "feat(6-2): add apiPut/apiDelete and gate 401 handler off login"
```

---

## Task 4: Base border layer + toast primitive

**Files:**
- Modify: `web/src/styles/index.css`
- Create: `web/src/lib/toast.tsx`, `web/src/lib/toast.test.tsx`
- Modify: `web/src/app/Layout.tsx`

**Interfaces:**
- Produces:
  - CSS base layer defaulting every element's border color to `--color-border` (Slice-1 backlog item 2 — prevents bare `border` on shared `<Card>` reading as `currentColor`).
  - `export function ToastProvider({ children }: { children: React.ReactNode }): JSX.Element`
  - `export function useToast(): { toast: (msg: string, opts?: { variant?: "ok" | "error" }) => void }`
  - Toasts auto-dismiss after ~4s and render in a fixed bottom-right region.

- [ ] **Step 1: Add the base layer to `index.css`** (append at end of file):

```css
@layer base {
  *, ::before, ::after {
    border-color: var(--color-border);
  }
}
```

- [ ] **Step 2: Write the failing toast test** (`web/src/lib/toast.test.tsx`):

```tsx
import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider, useToast } from "@/lib/toast"

function Trigger() {
  const { toast } = useToast()
  return <button onClick={() => toast("Saved!", { variant: "ok" })}>go</button>
}

describe("toast", () => {
  it("shows a message after toast() is called", async () => {
    render(
      <ToastProvider>
        <Trigger />
      </ToastProvider>,
    )
    expect(screen.queryByText("Saved!")).not.toBeInTheDocument()
    await userEvent.click(screen.getByText("go"))
    expect(await screen.findByText("Saved!")).toBeInTheDocument()
  })
})
```

- [ ] **Step 3: Run to verify it fails.**

Run (from `web/`): `npm test -- src/lib/toast.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 4: Implement `web/src/lib/toast.tsx`.**

```tsx
import * as React from "react"

type Toast = { id: number; msg: string; variant: "ok" | "error" }
type ToastCtx = { toast: (msg: string, opts?: { variant?: "ok" | "error" }) => void }

const Ctx = React.createContext<ToastCtx | null>(null)

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = React.useState<Toast[]>([])
  const nextId = React.useRef(1)

  const toast = React.useCallback((msg: string, opts?: { variant?: "ok" | "error" }) => {
    const id = nextId.current++
    setToasts((t) => [...t, { id, msg, variant: opts?.variant ?? "ok" }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4000)
  }, [])

  return (
    <Ctx.Provider value={{ toast }}>
      {children}
      <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            role="status"
            className={`rounded-md border px-4 py-2 text-sm shadow-lg ${
              t.variant === "error"
                ? "border-[var(--color-warn)] bg-[var(--color-panel-2)] text-[var(--color-warn)]"
                : "border-[var(--color-ok)] bg-[var(--color-panel-2)] text-[var(--color-fg)]"
            }`}
          >
            {t.msg}
          </div>
        ))}
      </div>
    </Ctx.Provider>
  )
}

export function useToast(): ToastCtx {
  const ctx = React.useContext(Ctx)
  if (!ctx) throw new Error("useToast must be used within ToastProvider")
  return ctx
}
```

- [ ] **Step 5: Mount `ToastProvider` in `Layout.tsx`.** Wrap the existing tree inside `ActivityProvider`:

```tsx
import { Outlet, useLocation } from "react-router-dom"
import { Sidebar, NAV_ITEMS } from "@/app/Sidebar"
import { TopBar } from "@/app/TopBar"
import { ActivityProvider } from "@/lib/activity"
import { ToastProvider } from "@/lib/toast"

function titleForPath(pathname: string): string {
  const match = NAV_ITEMS.find((n) => (n.to === "/" ? pathname === "/" : pathname.startsWith(n.to)))
  return match?.label ?? "Nexus"
}

export function Layout() {
  const { pathname } = useLocation()
  return (
    <ActivityProvider>
      <ToastProvider>
        <div className="flex h-screen overflow-hidden">
          <Sidebar />
          <div className="flex min-w-0 flex-1 flex-col">
            <TopBar title={titleForPath(pathname)} />
            <main className="flex-1 overflow-auto">
              <Outlet />
            </main>
          </div>
        </div>
      </ToastProvider>
    </ActivityProvider>
  )
}
```

- [ ] **Step 6: Run the toast test + typecheck.**

Run (from `web/`): `npm test -- src/lib/toast.test.tsx && npx tsc -b`
Expected: PASS, tsc exit 0.

- [ ] **Step 7: Commit.**

```bash
git add web/src/styles/index.css web/src/lib/toast.tsx web/src/lib/toast.test.tsx web/src/app/Layout.tsx
git commit -m "feat(6-2): base border layer + hand-written toast primitive"
```

---

## Task 5: Library types + data layer

**Files:**
- Create: `web/src/features/library/types.ts`, `web/src/features/library/api.ts`, `web/src/features/library/api.test.ts`

**Interfaces:**
- Consumes: `apiGet`, `apiPost`, `apiPut`, `apiDelete` from `@/lib/api`; TanStack Query `useQuery`/`useMutation`/`useQueryClient`.
- Produces (used by Tasks 6–11):
  - Types: `Movie`, `Series`, `Season`, `Episode`, `SeriesDetail`, `RootFolder`, `QualityProfile`, `MetadataResult`, `AddMovieBody`, `AddSeriesBody`.
  - Query keys object `libraryKeys`.
  - Read hooks: `useMovies()`, `useSeries()`, `useMovieDetail(id)`, `useSeriesDetail(id)`, `useRootFolders()`, `useQualityProfiles()`, `useLookup(term, kind)`.
  - Mutation hooks: `useAddMovie()`, `useAddSeries()`, `useSetMonitored()`, `useAssignProfile()`, `useRefresh()`, `useDelete()`, `useSearch()`.

- [ ] **Step 1: Write `types.ts`.**

```ts
export type RootFolder = { id: number; path: string; createdAt: string }

export type QualityProfileItem = { qualityId: number; allowed: boolean }
export type QualityProfile = {
  id: number
  name: string
  cutoffQualityId: number
  upgradeAllowed: boolean
  items: QualityProfileItem[]
  createdAt: string
}

export type MetadataResult = {
  tmdbId: number
  title: string
  year: number
  overview: string
  posterUrl: string
  kind: string
}

export type Movie = {
  id: number
  tmdbId: number
  title: string
  sortTitle: string
  overview: string
  status: string
  year: number
  releaseDate: string
  runtime: number
  imdbId: string
  posterUrl: string
  fanartUrl: string
  rootFolderId: number | null
  qualityProfileId: number | null
  monitored: boolean
  addedAt: string
  lastRefreshedAt: string | null
  hasFile: boolean
}

export type Series = {
  id: number
  tmdbId: number
  title: string
  sortTitle: string
  overview: string
  status: string
  firstAired: string
  posterUrl: string
  fanartUrl: string
  rootFolderId: number | null
  qualityProfileId: number | null
  monitored: boolean
  addedAt: string
  lastRefreshedAt: string | null
  episodeCount: number
  episodeFileCount: number
}

export type Season = { id: number; seriesId: number; seasonNumber: number; monitored: boolean }

export type Episode = {
  id: number
  seriesId: number
  seasonNumber: number
  episodeNumber: number
  tmdbId: number
  title: string
  overview: string
  airDate: string
  monitored: boolean
  hasFile: boolean
}

export type SeriesDetail = Series & { seasons: Season[]; episodes: Episode[] }

export type AddMovieBody = { tmdbId: number; rootFolderId: number | null; monitored: boolean }
export type AddSeriesBody = { tmdbId: number; rootFolderId: number | null; monitorOption: "all" | "future" | "none" }

export type MediaKind = "movie" | "tv"
```

- [ ] **Step 2: Write the failing data-layer test** (`api.test.ts`) — verifies `useLookup` is disabled on an empty term (avoids hammering TMDb) and enabled otherwise.

```ts
import { describe, it, expect, vi, beforeEach } from "vitest"
import type { ReactNode } from "react"
import { renderHook, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import * as apiClient from "@/lib/api"
import { useLookup } from "@/features/library/api"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, apiGet: vi.fn() }
})

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  }
}

beforeEach(() => vi.clearAllMocks())

describe("useLookup", () => {
  it("does not fetch when the term is empty", () => {
    renderHook(() => useLookup("", "movie"), { wrapper: wrapper() })
    expect(apiClient.apiGet).not.toHaveBeenCalled()
  })

  it("fetches when the term is non-empty", async () => {
    vi.mocked(apiClient.apiGet).mockResolvedValue([])
    renderHook(() => useLookup("bear", "tv"), { wrapper: wrapper() })
    await waitFor(() => expect(apiClient.apiGet).toHaveBeenCalledWith("/media/lookup?term=bear&kind=tv"))
  })
})
```

> NOTE: this test file uses JSX, so name it `api.test.tsx` (not `.ts`). Adjust the create path accordingly.

- [ ] **Step 3: Run to verify it fails.**

Run (from `web/`): `npm test -- src/features/library/api.test.tsx`
Expected: FAIL — `useLookup` undefined.

- [ ] **Step 4: Write `api.ts`.**

```ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type {
  Movie, Series, SeriesDetail, RootFolder, QualityProfile, MetadataResult,
  AddMovieBody, AddSeriesBody, MediaKind,
} from "./types"

export const libraryKeys = {
  movies: ["library", "movies"] as const,
  movie: (id: number) => ["library", "movie", id] as const,
  series: ["library", "series"] as const,
  seriesDetail: (id: number) => ["library", "series", id] as const,
  rootFolders: ["library", "rootfolders"] as const,
  qualityProfiles: ["library", "qualityprofiles"] as const,
  lookup: (term: string, kind: MediaKind) => ["library", "lookup", kind, term] as const,
}

// ---- reads ----
export function useMovies() {
  return useQuery({ queryKey: libraryKeys.movies, queryFn: () => apiGet<Movie[]>("/movies") })
}
export function useSeries() {
  return useQuery({ queryKey: libraryKeys.series, queryFn: () => apiGet<Series[]>("/series") })
}
export function useMovieDetail(id: number) {
  return useQuery({ queryKey: libraryKeys.movie(id), queryFn: () => apiGet<Movie>(`/movies/${id}`) })
}
export function useSeriesDetail(id: number) {
  return useQuery({ queryKey: libraryKeys.seriesDetail(id), queryFn: () => apiGet<SeriesDetail>(`/series/${id}`) })
}
export function useRootFolders() {
  return useQuery({ queryKey: libraryKeys.rootFolders, queryFn: () => apiGet<RootFolder[]>("/rootfolder") })
}
export function useQualityProfiles() {
  return useQuery({ queryKey: libraryKeys.qualityProfiles, queryFn: () => apiGet<QualityProfile[]>("/qualityprofile") })
}
export function useLookup(term: string, kind: MediaKind) {
  const q = term.trim()
  return useQuery({
    queryKey: libraryKeys.lookup(q, kind),
    queryFn: () => apiGet<MetadataResult[]>(`/media/lookup?term=${encodeURIComponent(q)}&kind=${kind}`),
    enabled: q.length > 0,
  })
}

// ---- mutations ----
export function useAddMovie() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: AddMovieBody) => apiPost<Movie>("/movies", b),
    onSuccess: () => qc.invalidateQueries({ queryKey: libraryKeys.movies }),
  })
}
export function useAddSeries() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: AddSeriesBody) => apiPost<Series>("/series", b),
    onSuccess: () => qc.invalidateQueries({ queryKey: libraryKeys.series }),
  })
}

type MonitorTarget =
  | { kind: "series"; id: number }
  | { kind: "movie"; id: number }
  | { kind: "season"; id: number }
  | { kind: "episode"; id: number }

export function useSetMonitored(invalidate: readonly unknown[]) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ target, monitored }: { target: MonitorTarget; monitored: boolean }) => {
      const path =
        target.kind === "series" ? `/series/${target.id}/monitor`
        : target.kind === "movie" ? `/movies/${target.id}/monitor`
        : target.kind === "season" ? `/season/${target.id}/monitor`
        : `/episode/${target.id}/monitor`
      return apiPut<{ ok: boolean }>(path, { monitored })
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: invalidate }),
  })
}

export function useAssignProfile(invalidate: readonly unknown[]) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ kind, id, qualityProfileId }: { kind: "movie" | "series"; id: number; qualityProfileId: number }) =>
      apiPut<{ ok: boolean }>(`/${kind === "movie" ? "movies" : "series"}/${id}/qualityprofile`, { qualityProfileId }),
    onSuccess: () => qc.invalidateQueries({ queryKey: invalidate }),
  })
}

export function useRefresh(invalidate: readonly unknown[]) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ kind, id }: { kind: "movie" | "series"; id: number }) =>
      apiPost<{ ok: boolean }>(`/${kind === "movie" ? "movies" : "series"}/${id}/refresh`),
    onSuccess: () => qc.invalidateQueries({ queryKey: invalidate }),
  })
}

export function useDelete() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ kind, id }: { kind: "movie" | "series"; id: number }) =>
      apiDelete<{ ok: boolean }>(`/${kind === "movie" ? "movies" : "series"}/${id}`),
    onSuccess: (_d, v) =>
      qc.invalidateQueries({ queryKey: v.kind === "movie" ? libraryKeys.movies : libraryKeys.series }),
  })
}

// Fire-and-forget search. 202 Accepted; results arrive later via the Activity feed.
export function useSearch() {
  return useMutation({
    mutationFn: (target:
      | { kind: "movie"; id: number }
      | { kind: "series"; id: number }
      | { kind: "season"; seriesId: number; seasonNumber: number }
      | { kind: "episode"; id: number }) => {
      const path =
        target.kind === "movie" ? `/automation/search/movie/${target.id}`
        : target.kind === "series" ? `/automation/search/series/${target.id}`
        : target.kind === "season" ? `/automation/search/series/${target.seriesId}/season/${target.seasonNumber}`
        : `/automation/search/episode/${target.id}`
      return apiPost<unknown>(path)
    },
  })
}
```

- [ ] **Step 5: Run to verify it passes + typecheck.**

Run (from `web/`): `npm test -- src/features/library/api.test.tsx && npx tsc -b`
Expected: PASS, tsc exit 0.

- [ ] **Step 6: Commit.**

```bash
git add web/src/features/library/types.ts web/src/features/library/api.ts web/src/features/library/api.test.tsx
git commit -m "feat(6-2): library types + TanStack Query data layer"
```

---

## Task 6: StatusBadge

**Files:**
- Create: `web/src/features/library/StatusBadge.tsx`, `web/src/features/library/StatusBadge.test.tsx`

**Interfaces:**
- Produces:
  - `export function movieBadge(m: { monitored: boolean; hasFile: boolean }): { label: string; tone: "ok" | "warn" | "muted" }`
  - `export function seriesBadge(s: { monitored: boolean; episodeCount: number; episodeFileCount: number }): { label: string; tone: "ok" | "warn" | "muted" }`
  - `export function StatusBadge({ tone, label }: { tone: "ok" | "warn" | "muted"; label: string }): JSX.Element`

- [ ] **Step 1: Write the failing test.**

```tsx
import { describe, it, expect } from "vitest"
import { movieBadge, seriesBadge } from "@/features/library/StatusBadge"

describe("badge logic", () => {
  it("movie downloaded → ok", () => {
    expect(movieBadge({ monitored: true, hasFile: true })).toEqual({ label: "Downloaded", tone: "ok" })
  })
  it("movie monitored, no file → warn Missing", () => {
    expect(movieBadge({ monitored: true, hasFile: false })).toEqual({ label: "Missing", tone: "warn" })
  })
  it("movie unmonitored, no file → muted", () => {
    expect(movieBadge({ monitored: false, hasFile: false })).toEqual({ label: "Unmonitored", tone: "muted" })
  })
  it("series complete → ok", () => {
    expect(seriesBadge({ monitored: true, episodeCount: 10, episodeFileCount: 10 })).toEqual({ label: "10 / 10", tone: "ok" })
  })
  it("series partial → warn", () => {
    expect(seriesBadge({ monitored: true, episodeCount: 10, episodeFileCount: 7 })).toEqual({ label: "7 / 10", tone: "warn" })
  })
  it("series unmonitored → muted", () => {
    expect(seriesBadge({ monitored: false, episodeCount: 0, episodeFileCount: 0 })).toEqual({ label: "0 / 0", tone: "muted" })
  })
})
```

- [ ] **Step 2: Run to verify it fails.**

Run (from `web/`): `npm test -- src/features/library/StatusBadge.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `StatusBadge.tsx`.**

```tsx
type Tone = "ok" | "warn" | "muted"

export function movieBadge(m: { monitored: boolean; hasFile: boolean }): { label: string; tone: Tone } {
  if (m.hasFile) return { label: "Downloaded", tone: "ok" }
  if (m.monitored) return { label: "Missing", tone: "warn" }
  return { label: "Unmonitored", tone: "muted" }
}

export function seriesBadge(s: { monitored: boolean; episodeCount: number; episodeFileCount: number }): { label: string; tone: Tone } {
  const label = `${s.episodeFileCount} / ${s.episodeCount}`
  if (!s.monitored) return { label, tone: "muted" }
  if (s.episodeCount > 0 && s.episodeFileCount >= s.episodeCount) return { label, tone: "ok" }
  return { label, tone: "warn" }
}

const toneClass: Record<Tone, string> = {
  ok: "border-[var(--color-ok)] text-[var(--color-ok)]",
  warn: "border-[var(--color-warn)] text-[var(--color-warn)]",
  muted: "border-[var(--color-border)] text-[var(--color-muted)]",
}

export function StatusBadge({ tone, label }: { tone: Tone; label: string }) {
  return (
    <span className={`inline-block rounded-full border px-2 py-0.5 text-xs font-semibold ${toneClass[tone]}`}>
      {label}
    </span>
  )
}
```

- [ ] **Step 4: Run to verify it passes.**

Run (from `web/`): `npm test -- src/features/library/StatusBadge.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add web/src/features/library/StatusBadge.tsx web/src/features/library/StatusBadge.test.tsx
git commit -m "feat(6-2): StatusBadge with movie/series badge logic"
```

---

## Task 7: MediaCard + MediaGrid

**Files:**
- Create: `web/src/features/library/MediaCard.tsx`, `web/src/features/library/MediaGrid.tsx`, `web/src/features/library/MediaGrid.test.tsx`

**Interfaces:**
- Consumes: `StatusBadge`, `movieBadge`, `seriesBadge` (Task 6); types from `./types`; `react-router-dom` `Link`.
- Produces:
  - `export function MediaCard({ to, title, subtitle, posterUrl, badge }: { to: string; title: string; subtitle: string; posterUrl: string; badge: { tone: "ok"|"warn"|"muted"; label: string } }): JSX.Element`
  - `export function MediaGrid<T>({ items, isLoading, isError, onRetry, empty, renderCard }: { items: T[] | undefined; isLoading: boolean; isError: boolean; onRetry: () => void; empty: string; renderCard: (item: T) => React.ReactNode }): JSX.Element`

- [ ] **Step 1: Write the failing test** (`MediaGrid.test.tsx`):

```tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MediaGrid } from "@/features/library/MediaGrid"

describe("MediaGrid", () => {
  it("shows loading state", () => {
    render(<MediaGrid items={undefined} isLoading isError={false} onRetry={() => {}} empty="none" renderCard={() => null} />)
    expect(screen.getByTestId("grid-loading")).toBeInTheDocument()
  })
  it("shows empty state", () => {
    render(<MediaGrid items={[]} isLoading={false} isError={false} onRetry={() => {}} empty="No movies yet" renderCard={() => null} />)
    expect(screen.getByText("No movies yet")).toBeInTheDocument()
  })
  it("shows an error state with a working retry button", async () => {
    const onRetry = vi.fn()
    render(<MediaGrid items={undefined} isLoading={false} isError onRetry={onRetry} empty="none" renderCard={() => null} />)
    await userEvent.click(screen.getByRole("button", { name: /retry/i }))
    expect(onRetry).toHaveBeenCalled()
  })
  it("renders cards for items", () => {
    render(
      <MediaGrid
        items={[{ id: 1 }, { id: 2 }]}
        isLoading={false}
        isError={false}
        onRetry={() => {}}
        empty="none"
        renderCard={(it: { id: number }) => <div key={it.id}>card-{it.id}</div>}
      />,
    )
    expect(screen.getByText("card-1")).toBeInTheDocument()
    expect(screen.getByText("card-2")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run to verify it fails.**

Run (from `web/`): `npm test -- src/features/library/MediaGrid.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `MediaCard.tsx`.**

```tsx
import { Link } from "react-router-dom"
import { StatusBadge } from "./StatusBadge"

export function MediaCard({
  to, title, subtitle, posterUrl, badge,
}: {
  to: string
  title: string
  subtitle: string
  posterUrl: string
  badge: { tone: "ok" | "warn" | "muted"; label: string }
}) {
  return (
    <Link
      to={to}
      className="group flex flex-col overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] transition-colors hover:border-[var(--color-brand)]"
    >
      <div className="aspect-[2/3] w-full bg-[var(--color-panel-2)]">
        {posterUrl ? (
          <img src={posterUrl} alt={title} className="h-full w-full object-cover" loading="lazy" />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-[var(--color-muted)]">No poster</div>
        )}
      </div>
      <div className="flex flex-1 flex-col gap-1 p-3">
        <div className="truncate text-sm font-semibold" title={title}>{title}</div>
        <div className="text-xs text-[var(--color-muted)]">{subtitle}</div>
        <div className="mt-1"><StatusBadge tone={badge.tone} label={badge.label} /></div>
      </div>
    </Link>
  )
}
```

- [ ] **Step 4: Implement `MediaGrid.tsx`.**

```tsx
import * as React from "react"

export function MediaGrid<T>({
  items, isLoading, isError, onRetry, empty, renderCard,
}: {
  items: T[] | undefined
  isLoading: boolean
  isError: boolean
  onRetry: () => void
  empty: string
  renderCard: (item: T) => React.ReactNode
}) {
  if (isLoading) {
    return (
      <div data-testid="grid-loading" className="grid grid-cols-2 gap-4 p-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
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
    <div className="grid grid-cols-2 gap-4 p-6 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
      {items.map((it) => renderCard(it))}
    </div>
  )
}
```

- [ ] **Step 5: Run to verify it passes.**

Run (from `web/`): `npm test -- src/features/library/MediaGrid.test.tsx`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add web/src/features/library/MediaCard.tsx web/src/features/library/MediaGrid.tsx web/src/features/library/MediaGrid.test.tsx
git commit -m "feat(6-2): MediaCard + generic MediaGrid with loading/empty/error states"
```

---

## Task 8: Movies & TV list pages + route wiring

**Files:**
- Create: `web/src/pages/Movies.tsx`, `web/src/pages/TvShows.tsx`, `web/src/pages/Movies.test.tsx`
- Modify: `web/src/app/routes.tsx`

**Interfaces:**
- Consumes: `useMovies`, `useSeries` (Task 5); `MediaGrid`, `MediaCard` (Task 7); `movieBadge`, `seriesBadge` (Task 6). Will also add `AddMediaDialog` (Task 9) — until then, the "+ Add" button is present but its dialog is added in Task 9. To keep this task self-contained and testable, wire a local `open` state and render nothing when closed; Task 9 fills the dialog body.
- Produces: `export function Movies(): JSX.Element`, `export function TvShows(): JSX.Element`; routes `/movies`, `/tv`, plus placeholders removed.

- [ ] **Step 1: Write the failing test** (`Movies.test.tsx`): the page renders cards from a mocked `useMovies`.

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { Movies } from "@/pages/Movies"
import * as lib from "@/features/library/api"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return { ...actual, useMovies: vi.fn() }
})

function renderMovies() {
  return render(
    <MemoryRouter>
      <Movies />
    </MemoryRouter>,
  )
}

beforeEach(() => vi.clearAllMocks())

describe("Movies page", () => {
  it("renders a card per movie", () => {
    vi.mocked(lib.useMovies).mockReturnValue({
      data: [
        { id: 1, title: "Dune", year: 2021, posterUrl: "", monitored: true, hasFile: true },
        { id: 2, title: "Arrival", year: 2016, posterUrl: "", monitored: true, hasFile: false },
      ],
      isLoading: false, isError: false, refetch: vi.fn(),
    } as unknown as ReturnType<typeof lib.useMovies>)
    renderMovies()
    expect(screen.getByText("Dune")).toBeInTheDocument()
    expect(screen.getByText("Arrival")).toBeInTheDocument()
    expect(screen.getByText("Downloaded")).toBeInTheDocument()
    expect(screen.getByText("Missing")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run to verify it fails.**

Run (from `web/`): `npm test -- src/pages/Movies.test.tsx`
Expected: FAIL — `@/pages/Movies` not found.

- [ ] **Step 3: Implement `Movies.tsx`.**

```tsx
import { useState } from "react"
import { useMovies } from "@/features/library/api"
import { MediaGrid } from "@/features/library/MediaGrid"
import { MediaCard } from "@/features/library/MediaCard"
import { movieBadge } from "@/features/library/StatusBadge"
import { AddMediaDialog } from "@/features/library/AddMediaDialog"

export function Movies() {
  const q = useMovies()
  const [filter, setFilter] = useState("")
  const [addOpen, setAddOpen] = useState(false)
  const items = (q.data ?? []).filter((m) => m.title.toLowerCase().includes(filter.toLowerCase()))

  return (
    <div>
      <div className="flex items-center gap-3 p-6 pb-0">
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter…"
          className="w-64 rounded-md border border-[var(--color-border)] bg-[var(--color-panel)] px-3 py-1.5 text-sm"
        />
        <button
          onClick={() => setAddOpen(true)}
          className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white"
        >
          + Add
        </button>
      </div>
      <MediaGrid
        items={q.data ? items : undefined}
        isLoading={q.isLoading}
        isError={q.isError}
        onRetry={() => q.refetch()}
        empty="No movies yet — click Add to get started."
        renderCard={(m) => (
          <MediaCard
            key={m.id}
            to={`/movies/${m.id}`}
            title={m.title}
            subtitle={m.year ? String(m.year) : ""}
            posterUrl={m.posterUrl}
            badge={movieBadge(m)}
          />
        )}
      />
      {addOpen && <AddMediaDialog kind="movie" open={addOpen} onOpenChange={setAddOpen} />}
    </div>
  )
}
```

> WHY conditional mount (`{addOpen && …}`): `AddMediaDialog` calls `useLookup`/`useRootFolders`/`useQualityProfiles`/`useAddMovie`/`useAddSeries` at the top of its body — these run as soon as the component is mounted, even before the Radix dialog decides to render null for `open={false}`. Mounting it only when `addOpen` keeps those queries from firing on every list-page load, and keeps `Movies.test.tsx` (which mocks only `useMovies` and provides no `QueryClientProvider`) green once the real dialog replaces the Task-8 stub.

- [ ] **Step 4: Implement `TvShows.tsx`** (same shape, series hooks/badge).

```tsx
import { useState } from "react"
import { useSeries } from "@/features/library/api"
import { MediaGrid } from "@/features/library/MediaGrid"
import { MediaCard } from "@/features/library/MediaCard"
import { seriesBadge } from "@/features/library/StatusBadge"
import { AddMediaDialog } from "@/features/library/AddMediaDialog"

export function TvShows() {
  const q = useSeries()
  const [filter, setFilter] = useState("")
  const [addOpen, setAddOpen] = useState(false)
  const items = (q.data ?? []).filter((s) => s.title.toLowerCase().includes(filter.toLowerCase()))

  return (
    <div>
      <div className="flex items-center gap-3 p-6 pb-0">
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter…"
          className="w-64 rounded-md border border-[var(--color-border)] bg-[var(--color-panel)] px-3 py-1.5 text-sm"
        />
        <button
          onClick={() => setAddOpen(true)}
          className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white"
        >
          + Add
        </button>
      </div>
      <MediaGrid
        items={q.data ? items : undefined}
        isLoading={q.isLoading}
        isError={q.isError}
        onRetry={() => q.refetch()}
        empty="No TV shows yet — click Add to get started."
        renderCard={(s) => (
          <MediaCard
            key={s.id}
            to={`/tv/${s.id}`}
            title={s.title}
            subtitle={s.firstAired ? s.firstAired.slice(0, 4) : ""}
            posterUrl={s.posterUrl}
            badge={seriesBadge(s)}
          />
        )}
      />
      {addOpen && <AddMediaDialog kind="tv" open={addOpen} onOpenChange={setAddOpen} />}
    </div>
  )
}
```

> NOTE: `AddMediaDialog` is implemented in Task 9. If executing strictly task-by-task, create a temporary stub `web/src/features/library/AddMediaDialog.tsx` exporting `export function AddMediaDialog(_: { kind: "movie" | "tv"; open: boolean; onOpenChange: (o: boolean) => void }) { return null }` so this task compiles, and Task 9 replaces it. The `Movies.test.tsx` does not open the dialog, so the stub is sufficient here.

- [ ] **Step 5: Wire routes** in `routes.tsx` — replace the movies/tv placeholders and add detail routes (detail pages land in Tasks 10–11; add their imports there). For now swap the two list routes:

```tsx
import { createBrowserRouter } from "react-router-dom"
import { RequireAuth } from "@/lib/auth"
import { Layout } from "@/app/Layout"
import { Login } from "@/pages/Login"
import { Placeholder } from "@/pages/Placeholder"
import { Dashboard } from "@/pages/Dashboard"
import { Movies } from "@/pages/Movies"
import { TvShows } from "@/pages/TvShows"

export const router = createBrowserRouter([
  { path: "/login", element: <Login /> },
  {
    path: "/",
    element: (
      <RequireAuth>
        <Layout />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <Dashboard /> },
      { path: "movies", element: <Movies /> },
      { path: "tv", element: <TvShows /> },
      { path: "calendar", element: <Placeholder title="Calendar" /> },
      { path: "activity", element: <Placeholder title="Activity" /> },
      { path: "settings", element: <Placeholder title="Settings" /> },
      { path: "system", element: <Placeholder title="System" /> },
    ],
  },
])
```

- [ ] **Step 6: Run test + typecheck.**

Run (from `web/`): `npm test -- src/pages/Movies.test.tsx && npx tsc -b`
Expected: PASS, tsc exit 0.

- [ ] **Step 7: Commit.**

```bash
git add web/src/pages/Movies.tsx web/src/pages/TvShows.tsx web/src/pages/Movies.test.tsx web/src/features/library/AddMediaDialog.tsx web/src/app/routes.tsx
git commit -m "feat(6-2): Movies + TV list pages, routes replace placeholders"
```

---

## Task 9: AddMediaDialog

**Files:**
- Create: `web/src/components/ui/dialog.tsx`, `web/src/components/ui/select.tsx` (thin `radix-ui` wrappers)
- Replace: `web/src/features/library/AddMediaDialog.tsx` (the Task-8 stub)
- Create: `web/src/features/library/AddMediaDialog.test.tsx`

**Interfaces:**
- Consumes: `useLookup`, `useAddMovie`, `useAddSeries`, `useRootFolders`, `useQualityProfiles` (Task 5); `useToast` (Task 4); `Dialog`, `Select` from `radix-ui`.
- Produces: `export function AddMediaDialog({ kind, open, onOpenChange }: { kind: "movie" | "tv"; open: boolean; onOpenChange: (o: boolean) => void }): JSX.Element`

- [ ] **Step 1: Implement the `radix-ui` wrappers.** The unified `radix-ui` package (v1.6.1, already a dependency) exports each primitive as a namespace: `import { Dialog } from "radix-ui"` → `Dialog.Root`/`Dialog.Portal`/`Dialog.Overlay`/`Dialog.Content`/`Dialog.Title`. There's no Slice-1 precedent for this import (button/card/input/label don't use radix), so if this file fails `tsc -b`, verify the export names against `node_modules/radix-ui` first. `web/src/components/ui/dialog.tsx`:

```tsx
import { Dialog as D } from "radix-ui"

export function Dialog({ open, onOpenChange, children }: { open: boolean; onOpenChange: (o: boolean) => void; children: React.ReactNode }) {
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/60" />
        <D.Content className="fixed left-1/2 top-1/2 z-50 w-[32rem] max-w-[90vw] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-[var(--color-border)] bg-[var(--color-panel)] p-5 shadow-2xl">
          {children}
        </D.Content>
      </D.Portal>
    </D.Root>
  )
}

export function DialogTitle({ children }: { children: React.ReactNode }) {
  return <D.Title className="mb-3 text-lg font-semibold">{children}</D.Title>
}
```

`web/src/components/ui/select.tsx` — a plain native `<select>` styled wrapper (simpler and fully testable; no need for the radix Select for two small dropdowns):

```tsx
export function Select({
  value, onChange, children, disabled, "aria-label": ariaLabel,
}: {
  value: string
  onChange: (v: string) => void
  children: React.ReactNode
  disabled?: boolean
  "aria-label"?: string
}) {
  return (
    <select
      aria-label={ariaLabel}
      value={value}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-3 py-1.5 text-sm disabled:opacity-50"
    >
      {children}
    </select>
  )
}
```

- [ ] **Step 2: Write the failing test** (`AddMediaDialog.test.tsx`): with no root folders, submit is disabled and the guidance shows.

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { AddMediaDialog } from "@/features/library/AddMediaDialog"
import * as lib from "@/features/library/api"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return {
    ...actual,
    useLookup: vi.fn(),
    useRootFolders: vi.fn(),
    useQualityProfiles: vi.fn(),
    useAddMovie: vi.fn(),
  }
})

beforeEach(() => vi.clearAllMocks())

function stub() {
  vi.mocked(lib.useLookup).mockReturnValue({ data: [{ tmdbId: 1, title: "Dune", year: 2021, overview: "", posterUrl: "", kind: "movie" }], isLoading: false } as unknown as ReturnType<typeof lib.useLookup>)
  vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
  vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync: vi.fn(), isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
}

describe("AddMediaDialog", () => {
  it("blocks submit and guides to Settings when there are no root folders", async () => {
    stub()
    vi.mocked(lib.useRootFolders).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useRootFolders>)
    render(
      <ToastProvider>
        <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
      </ToastProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
    await userEvent.click(await screen.findByText("Dune"))
    expect(screen.getByText(/no root folder configured/i)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /add movie/i })).toBeDisabled()
  })
})
```

- [ ] **Step 3: Run to verify it fails.**

Run (from `web/`): `npm test -- src/features/library/AddMediaDialog.test.tsx`
Expected: FAIL (stub returns null / components missing).

- [ ] **Step 4: Implement `AddMediaDialog.tsx`** (replace the stub).

```tsx
import { useState, useEffect } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"
import { Select } from "@/components/ui/select"
import { useToast } from "@/lib/toast"
import {
  useLookup, useRootFolders, useQualityProfiles, useAddMovie, useAddSeries,
} from "./api"
import type { MetadataResult, MediaKind } from "./types"

export function AddMediaDialog({
  kind, open, onOpenChange,
}: {
  kind: MediaKind
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const { toast } = useToast()
  const [term, setTerm] = useState("")
  const [debounced, setDebounced] = useState("")
  const [picked, setPicked] = useState<MetadataResult | null>(null)
  const [rootFolderId, setRootFolderId] = useState("")
  const [profileId, setProfileId] = useState("")
  const [monitorOption, setMonitorOption] = useState<"all" | "future" | "none">("all")
  const [monitored, setMonitored] = useState(true)

  // simple debounce
  useDebounce(term, 300, setDebounced)

  const lookup = useLookup(debounced, kind)
  const rootFolders = useRootFolders()
  const profiles = useQualityProfiles()
  const addMovie = useAddMovie()
  const addSeries = useAddSeries()

  const noRoots = (rootFolders.data ?? []).length === 0
  const pending = addMovie.isPending || addSeries.isPending

  async function submit() {
    if (!picked) return
    const rfId = rootFolderId ? Number(rootFolderId) : null
    try {
      if (kind === "movie") {
        await addMovie.mutateAsync({ tmdbId: picked.tmdbId, rootFolderId: rfId, monitored })
      } else {
        await addSeries.mutateAsync({ tmdbId: picked.tmdbId, rootFolderId: rfId, monitorOption })
      }
      toast(`Added ${picked.title}`, { variant: "ok" })
      reset()
      onOpenChange(false)
    } catch (e) {
      toast(e instanceof Error ? e.message : "Failed to add", { variant: "error" })
    }
  }

  function reset() {
    setTerm(""); setDebounced(""); setPicked(null); setRootFolderId(""); setProfileId("")
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) reset(); onOpenChange(o) }}>
      <DialogTitle>Add {kind === "movie" ? "Movie" : "TV Show"}</DialogTitle>

      {!picked ? (
        <div>
          <input
            autoFocus
            value={term}
            onChange={(e) => setTerm(e.target.value)}
            placeholder="Search TMDb…"
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-3 py-2 text-sm"
          />
          <ul className="mt-3 max-h-72 overflow-auto">
            {(lookup.data ?? []).map((r) => (
              <li key={r.tmdbId}>
                <button
                  onClick={() => setPicked(r)}
                  className="flex w-full items-start gap-3 rounded-md p-2 text-left hover:bg-[var(--color-panel-2)]"
                >
                  <span className="font-medium">{r.title}</span>
                  {r.year ? <span className="text-xs text-[var(--color-muted)]">{r.year}</span> : null}
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <div className="text-sm font-semibold">{picked.title}{picked.year ? ` (${picked.year})` : ""}</div>

          <label className="text-xs text-[var(--color-muted)]">Root folder</label>
          {noRoots ? (
            <p className="text-sm text-[var(--color-warn)]">No root folder configured — add one in Settings.</p>
          ) : (
            <Select aria-label="Root folder" value={rootFolderId} onChange={setRootFolderId}>
              <option value="">Select…</option>
              {(rootFolders.data ?? []).map((rf) => (
                <option key={rf.id} value={rf.id}>{rf.path}</option>
              ))}
            </Select>
          )}

          <label className="text-xs text-[var(--color-muted)]">Quality profile (optional)</label>
          <Select aria-label="Quality profile" value={profileId} onChange={setProfileId} disabled={(profiles.data ?? []).length === 0}>
            <option value="">{(profiles.data ?? []).length === 0 ? "None configured yet (optional)" : "None"}</option>
            {(profiles.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>{p.name}</option>
            ))}
          </Select>

          {kind === "tv" ? (
            <>
              <label className="text-xs text-[var(--color-muted)]">Monitor</label>
              <Select aria-label="Monitor" value={monitorOption} onChange={(v) => setMonitorOption(v as "all" | "future" | "none")}>
                <option value="all">All episodes</option>
                <option value="future">Future episodes</option>
                <option value="none">None</option>
              </Select>
            </>
          ) : (
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={monitored} onChange={(e) => setMonitored(e.target.checked)} />
              Monitored
            </label>
          )}

          <div className="mt-2 flex justify-end gap-2">
            <button onClick={() => setPicked(null)} className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm">Back</button>
            <button
              onClick={submit}
              disabled={noRoots || pending}
              className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
            >
              {pending ? "Adding…" : `Add ${kind === "movie" ? "Movie" : "Show"}`}
            </button>
          </div>
        </div>
      )}
    </Dialog>
  )
}

function useDebounce(value: string, ms: number, setter: (v: string) => void) {
  useEffect(() => {
    const t = setTimeout(() => setter(value), ms)
    return () => clearTimeout(t)
  }, [value, ms, setter])
}
```

Note: the movie submit button must read `Add Movie` and the show button `Add Show` — the disabled-state test in Step 2 targets `/add movie/i`.

- [ ] **Step 5: Run the test + typecheck.**

Run (from `web/`): `npm test -- src/features/library/AddMediaDialog.test.tsx && npx tsc -b`
Expected: PASS, tsc exit 0.

- [ ] **Step 6: Commit.**

```bash
git add web/src/components/ui/dialog.tsx web/src/components/ui/select.tsx web/src/features/library/AddMediaDialog.tsx web/src/features/library/AddMediaDialog.test.tsx
git commit -m "feat(6-2): AddMediaDialog with TMDb lookup + empty-root-folder guard"
```

---

## Task 10: Movie detail page + route

**Files:**
- Create: `web/src/features/library/MovieDetail.tsx`, `web/src/pages/MediaDetail.tsx`, `web/src/features/library/MovieDetail.test.tsx`
- Modify: `web/src/app/routes.tsx`

**Interfaces:**
- Consumes: `useMovieDetail`, `useSetMonitored`, `useAssignProfile`, `useRefresh`, `useDelete`, `useSearch`, `useQualityProfiles`, `libraryKeys` (Task 5); `useToast` (Task 4); `useParams`, `useNavigate` from `react-router-dom`.
- Produces: `export function MovieDetail({ id }: { id: number }): JSX.Element`; `export function MediaDetail(): JSX.Element` (reads `:id` + decides movie vs series by route); routes `/movies/:id` and `/tv/:id`.

- [ ] **Step 1: Write the failing test** (`MovieDetail.test.tsx`): renders title + fires search toast.

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { ToastProvider } from "@/lib/toast"
import { MovieDetail } from "@/features/library/MovieDetail"
import * as lib from "@/features/library/api"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return {
    ...actual,
    useMovieDetail: vi.fn(), useQualityProfiles: vi.fn(), useSetMonitored: vi.fn(),
    useAssignProfile: vi.fn(), useRefresh: vi.fn(), useDelete: vi.fn(), useSearch: vi.fn(),
  }
})

beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, ...extra } as unknown as never
}

describe("MovieDetail", () => {
  it("renders the movie and triggers a search toast", async () => {
    const search = vi.fn()
    vi.mocked(lib.useMovieDetail).mockReturnValue({ data: { id: 5, title: "Dune", year: 2021, overview: "x", monitored: true, hasFile: false, qualityProfileId: null, posterUrl: "", fanartUrl: "" }, isLoading: false, isError: false, refetch: vi.fn() } as unknown as ReturnType<typeof lib.useMovieDetail>)
    vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
    vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
    vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
    vi.mocked(lib.useRefresh).mockReturnValue(mut())
    vi.mocked(lib.useDelete).mockReturnValue(mut())
    vi.mocked(lib.useSearch).mockReturnValue(mut({ mutate: search }))

    render(
      <MemoryRouter>
        <ToastProvider>
          <MovieDetail id={5} />
        </ToastProvider>
      </MemoryRouter>,
    )
    expect(screen.getByText("Dune")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: /search/i }))
    expect(search).toHaveBeenCalledWith({ kind: "movie", id: 5 })
  })
})
```

- [ ] **Step 2: Run to verify it fails.**

Run (from `web/`): `npm test -- src/features/library/MovieDetail.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `MovieDetail.tsx`.**

```tsx
import { useNavigate } from "react-router-dom"
import { useToast } from "@/lib/toast"
import {
  useMovieDetail, useQualityProfiles, useSetMonitored, useAssignProfile,
  useRefresh, useDelete, useSearch, libraryKeys,
} from "./api"
import { Select } from "@/components/ui/select"
import { StatusBadge, movieBadge } from "./StatusBadge"

export function MovieDetail({ id }: { id: number }) {
  const nav = useNavigate()
  const { toast } = useToast()
  const q = useMovieDetail(id)
  const profiles = useQualityProfiles()
  const setMon = useSetMonitored(libraryKeys.movie(id))
  const assign = useAssignProfile(libraryKeys.movie(id))
  const refresh = useRefresh(libraryKeys.movie(id))
  const del = useDelete()
  const search = useSearch()

  if (q.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading…</div>
  if (q.isError || !q.data) {
    return (
      <div className="p-6">
        <p className="text-sm text-[var(--color-muted)]">Not found.</p>
        <button onClick={() => nav("/movies")} className="mt-3 text-sm text-[var(--color-brand)]">← Back to Movies</button>
      </div>
    )
  }
  const m = q.data
  const badge = movieBadge(m)

  return (
    <div className="p-6">
      <button onClick={() => nav("/movies")} className="mb-4 text-sm text-[var(--color-brand)]">← Movies</button>
      <div className="flex gap-6">
        <div className="aspect-[2/3] w-40 shrink-0 overflow-hidden rounded-lg bg-[var(--color-panel-2)]">
          {m.posterUrl ? <img src={m.posterUrl} alt={m.title} className="h-full w-full object-cover" /> : null}
        </div>
        <div className="min-w-0 flex-1">
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
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Implement `pages/MediaDetail.tsx`** (route dispatcher) and wire routes.

```tsx
import { useParams } from "react-router-dom"
import { MovieDetail } from "@/features/library/MovieDetail"
import { SeriesDetail } from "@/features/library/SeriesDetail"

export function MediaDetail({ kind }: { kind: "movie" | "series" }) {
  const { id } = useParams()
  const numId = Number(id)
  if (!id || Number.isNaN(numId)) return <div className="p-6 text-sm text-[var(--color-muted)]">Invalid id.</div>
  return kind === "movie" ? <MovieDetail id={numId} /> : <SeriesDetail id={numId} />
}
```

> NOTE: `SeriesDetail` lands in Task 11. To keep this task compiling, add a temporary stub `web/src/features/library/SeriesDetail.tsx` → `export function SeriesDetail(_: { id: number }) { return null }`; Task 11 replaces it.

Wire routes in `routes.tsx` — add imports and the two detail routes:

```tsx
import { MediaDetail } from "@/pages/MediaDetail"
// …inside children, after the tv route:
{ path: "movies/:id", element: <MediaDetail kind="movie" /> },
{ path: "tv/:id", element: <MediaDetail kind="series" /> },
```

- [ ] **Step 5: Run test + typecheck.**

Run (from `web/`): `npm test -- src/features/library/MovieDetail.test.tsx && npx tsc -b`
Expected: PASS, tsc exit 0.

- [ ] **Step 6: Commit.**

```bash
git add web/src/features/library/MovieDetail.tsx web/src/features/library/SeriesDetail.tsx web/src/pages/MediaDetail.tsx web/src/features/library/MovieDetail.test.tsx web/src/app/routes.tsx
git commit -m "feat(6-2): movie detail page + detail routes"
```

---

## Task 11: SeriesDetail + SeasonTable

**Files:**
- Create: `web/src/features/library/SeasonTable.tsx`
- Replace: `web/src/features/library/SeriesDetail.tsx` (the Task-10 stub)
- Create: `web/src/features/library/SeriesDetail.test.tsx`

**Interfaces:**
- Consumes: `useSeriesDetail`, `useSetMonitored`, `useAssignProfile`, `useRefresh`, `useDelete`, `useSearch`, `useQualityProfiles`, `libraryKeys` (Task 5); `useToast` (Task 4); types `Season`, `Episode`.
- Produces:
  - `export function SeasonTable({ seasons, episodes, seriesId, onToggleSeason, onToggleEpisode, onSearchSeason, onSearchEpisode }: {...}): JSX.Element`
  - `export function SeriesDetail({ id }: { id: number }): JSX.Element`

- [ ] **Step 1: Write the failing test** (`SeriesDetail.test.tsx`): renders seasons/episodes; per-episode file badge; episode search fires with the episode id.

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { ToastProvider } from "@/lib/toast"
import { SeriesDetail } from "@/features/library/SeriesDetail"
import * as lib from "@/features/library/api"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return {
    ...actual,
    useSeriesDetail: vi.fn(), useQualityProfiles: vi.fn(), useSetMonitored: vi.fn(),
    useAssignProfile: vi.fn(), useRefresh: vi.fn(), useDelete: vi.fn(), useSearch: vi.fn(),
  }
})
beforeEach(() => vi.clearAllMocks())
function mut(extra: object = {}) {
  return { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, ...extra } as unknown as never
}

describe("SeriesDetail", () => {
  it("renders seasons + episodes and searches an episode", async () => {
    const search = vi.fn()
    vi.mocked(lib.useSeriesDetail).mockReturnValue({
      data: {
        id: 3, title: "The Bear", firstAired: "2022-06-23", overview: "", monitored: true,
        qualityProfileId: null, posterUrl: "", fanartUrl: "", episodeCount: 2, episodeFileCount: 1,
        seasons: [{ id: 30, seriesId: 3, seasonNumber: 1, monitored: true }],
        episodes: [
          { id: 101, seriesId: 3, seasonNumber: 1, episodeNumber: 1, tmdbId: 0, title: "System", overview: "", airDate: "2022-06-23", monitored: true, hasFile: true },
          { id: 102, seriesId: 3, seasonNumber: 1, episodeNumber: 2, tmdbId: 0, title: "Hands", overview: "", airDate: "2022-06-23", monitored: true, hasFile: false },
        ],
      },
      isLoading: false, isError: false, refetch: vi.fn(),
    } as unknown as ReturnType<typeof lib.useSeriesDetail>)
    vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
    vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
    vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
    vi.mocked(lib.useRefresh).mockReturnValue(mut())
    vi.mocked(lib.useDelete).mockReturnValue(mut())
    vi.mocked(lib.useSearch).mockReturnValue(mut({ mutate: search }))

    render(
      <MemoryRouter>
        <ToastProvider>
          <SeriesDetail id={3} />
        </ToastProvider>
      </MemoryRouter>,
    )
    expect(screen.getByText("The Bear")).toBeInTheDocument()
    expect(screen.getByText("System")).toBeInTheDocument()
    expect(screen.getByText("Hands")).toBeInTheDocument()
    // per-episode search buttons; click the second episode's
    const searchButtons = screen.getAllByRole("button", { name: /search episode/i })
    await userEvent.click(searchButtons[1])
    expect(search).toHaveBeenCalledWith({ kind: "episode", id: 102 })
  })
})
```

- [ ] **Step 2: Run to verify it fails.**

Run (from `web/`): `npm test -- src/features/library/SeriesDetail.test.tsx`
Expected: FAIL — stub renders null.

- [ ] **Step 3: Implement `SeasonTable.tsx`.**

```tsx
import type { Season, Episode } from "./types"
import { StatusBadge } from "./StatusBadge"

export function SeasonTable({
  seasons, episodes, onToggleSeason, onToggleEpisode, onSearchSeason, onSearchEpisode,
}: {
  seasons: Season[]
  episodes: Episode[]
  onToggleSeason: (s: Season) => void
  onToggleEpisode: (e: Episode) => void
  onSearchSeason: (seasonNumber: number) => void
  onSearchEpisode: (e: Episode) => void
}) {
  const sorted = [...seasons].sort((a, b) => a.seasonNumber - b.seasonNumber)
  return (
    <div className="mt-6 flex flex-col gap-4">
      {sorted.map((s) => {
        const eps = episodes.filter((e) => e.seasonNumber === s.seasonNumber).sort((a, b) => a.episodeNumber - b.episodeNumber)
        const withFile = eps.filter((e) => e.hasFile).length
        return (
          <div key={s.id} className="overflow-hidden rounded-lg border border-[var(--color-border)]">
            <div className="flex items-center justify-between bg-[var(--color-panel-2)] px-4 py-2">
              <div className="flex items-center gap-3">
                <span className="font-semibold">Season {s.seasonNumber}</span>
                <StatusBadge tone={withFile >= eps.length && eps.length > 0 ? "ok" : "warn"} label={`${withFile} / ${eps.length}`} />
              </div>
              <div className="flex items-center gap-2">
                <button onClick={() => onSearchSeason(s.seasonNumber)} className="text-xs text-[var(--color-brand)]">Search season</button>
                <label className="flex items-center gap-1 text-xs text-[var(--color-muted)]">
                  <input type="checkbox" checked={s.monitored} onChange={() => onToggleSeason(s)} /> monitor
                </label>
              </div>
            </div>
            <ul>
              {eps.map((e) => (
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
            </ul>
          </div>
        )
      })}
    </div>
  )
}
```

- [ ] **Step 4: Implement `SeriesDetail.tsx`** (replace the stub).

```tsx
import { useNavigate } from "react-router-dom"
import { useToast } from "@/lib/toast"
import {
  useSeriesDetail, useQualityProfiles, useSetMonitored, useAssignProfile,
  useRefresh, useDelete, useSearch, libraryKeys,
} from "./api"
import { Select } from "@/components/ui/select"
import { StatusBadge, seriesBadge } from "./StatusBadge"
import { SeasonTable } from "./SeasonTable"

export function SeriesDetail({ id }: { id: number }) {
  const nav = useNavigate()
  const { toast } = useToast()
  const q = useSeriesDetail(id)
  const profiles = useQualityProfiles()
  const setMon = useSetMonitored(libraryKeys.seriesDetail(id))
  const assign = useAssignProfile(libraryKeys.seriesDetail(id))
  const refresh = useRefresh(libraryKeys.seriesDetail(id))
  const del = useDelete()
  const search = useSearch()

  if (q.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading…</div>
  if (q.isError || !q.data) {
    return (
      <div className="p-6">
        <p className="text-sm text-[var(--color-muted)]">Not found.</p>
        <button onClick={() => nav("/tv")} className="mt-3 text-sm text-[var(--color-brand)]">← Back to TV Shows</button>
      </div>
    )
  }
  const s = q.data
  const badge = seriesBadge(s)

  return (
    <div className="p-6">
      <button onClick={() => nav("/tv")} className="mb-4 text-sm text-[var(--color-brand)]">← TV Shows</button>
      <div className="flex gap-6">
        <div className="aspect-[2/3] w-40 shrink-0 overflow-hidden rounded-lg bg-[var(--color-panel-2)]">
          {s.posterUrl ? <img src={s.posterUrl} alt={s.title} className="h-full w-full object-cover" /> : null}
        </div>
        <div className="min-w-0 flex-1">
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

          <SeasonTable
            seasons={s.seasons}
            episodes={s.episodes}
            onToggleSeason={(sn) => setMon.mutate({ target: { kind: "season", id: sn.id }, monitored: !sn.monitored })}
            onToggleEpisode={(e) => setMon.mutate({ target: { kind: "episode", id: e.id }, monitored: !e.monitored })}
            onSearchSeason={(seasonNumber) => { search.mutate({ kind: "season", seriesId: id, seasonNumber }); toast(`Search started for season ${seasonNumber}`) }}
            onSearchEpisode={(e) => { search.mutate({ kind: "episode", id: e.id }); toast(`Search started for ${e.title}`) }}
          />
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 5: Run test + typecheck + the whole frontend suite.**

Run (from `web/`): `npm test && npx tsc -b`
Expected: all tests PASS, tsc exit 0.

- [ ] **Step 6: Commit.**

```bash
git add web/src/features/library/SeasonTable.tsx web/src/features/library/SeriesDetail.tsx web/src/features/library/SeriesDetail.test.tsx
git commit -m "feat(6-2): series detail + season/episode table with per-item actions"
```

---

## Task 12: Rebuild embedded bundle + full verification

**Files:**
- Modify: `web/dist/**` (regenerated build output)

**Interfaces:** none (release/verify task).

- [ ] **Step 1: Full frontend gate.**

Run (from `web/`): `npm run test && npx tsc -b`
Expected: all Vitest tests PASS; tsc exit 0.

- [ ] **Step 2: Rebuild the committed bundle.**

Run (from repo root): `make web` (or, if `make` is unavailable: `cd web && npm ci && npm run build`).
Expected: `web/dist` regenerated.

- [ ] **Step 3: Full backend gate.**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go build ./... && go vet ./... && go test ./... -count=1`
Expected: build clean, vet clean, all Go tests PASS (including `web/spa_test.go`).

- [ ] **Step 4: Verify the drift guard is clean after committing dist.**

```bash
git add web/dist
git commit -m "build(6-2): rebuild embedded web bundle for media library"
git diff --exit-code web/dist
```

Expected: `git diff --exit-code web/dist` exits 0 (no drift between committed dist and a fresh build).

- [ ] **Step 5: Manual smoke (optional but recommended).** Build and run the binary, log in, confirm Movies/TV render, Add dialog opens, a detail page loads.

```bash
export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go build -o nexus.exe ./cmd/nexus
NEXUS_ADMIN_PASSWORD=test ./nexus.exe   # then open http://localhost:9494/
```

Expected: login → Movies/TV nav items now render real pages (no "ships in a later slice" placeholder).

---

## Self-Review

**1. Spec coverage** (against `docs/superpowers/specs/2026-07-07-nexus-webui-media-library-design.md`):
- §3 backend enrichment → Tasks 1–2. ✅ (`hasFile`, `episodeCount`, `episodeFileCount`, per-episode `hasFile`; monitored-only denominator.)
- §4 frontend architecture / feature folder → Tasks 5–11. ✅
- §5 data flow / hooks / `apiPut`+`apiDelete` / debounced lookup → Tasks 3, 5. ✅
- §6.1 list + badges → Tasks 6–8. ✅
- §6.2 add dialog + empty-root guard → Task 9. ✅
- §6.3 movie detail actions → Task 10. ✅
- §6.4 series detail + season table → Task 11. ✅
- §7 error/empty/loading/toast/404 → Tasks 4 (toast), 7 (grid states), 10–11 (not-found). ✅
- §8 testing (Go store+handler, frontend unit, integration, router, merge gates) → Tasks 1–2, 6–11, 12. ✅
- §11 Slice-1 backlog: 401 gate → Task 3; base border layer → Task 4; WS reconnect timer → not touched this slice (deferred, per spec "fix if touched"; noted, no bare-Card WS interaction here). ✅
- §2 out-of-scope items → none implemented. ✅

**2. Placeholder scan:** No "TBD"/"handle edge cases"/"similar to". The two intentional NOTE blocks flag the `require`→`import` correction and the temporary stubs for out-of-order execution; both give exact corrected code. ✅

**3. Type consistency:** `libraryKeys.movie(id)`/`seriesDetail(id)` used identically in Tasks 5/10/11; `useSetMonitored(invalidate)` signature consumed with the same shape everywhere; `MonitorTarget` kinds (`series`/`movie`/`season`/`episode`) match the endpoint switch and the detail-page call sites; `useSearch` target union matches call sites (`{kind:"season",seriesId,seasonNumber}` etc.). Backend `movieDTO`/`seriesListItem`/`episodeDTO` field names (`hasFile`/`episodeCount`/`episodeFileCount`) match the TS types in Task 5. ✅
