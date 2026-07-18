# Movie file-info box + delete-file Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a movie's imported file (name/quality/size/path/date) on its detail page and let the user delete just that file (best-effort disk + DB row) to force a re-grab, plus add hover/focus states to both detail pages' back buttons.

**Architecture:** Backend adds an optional `file` object to the movie-detail DTO (detail endpoint only — list stays cheap) and a best-effort `DELETE /movies/{id}/file` that removes the file from disk and always deletes the DB row. Frontend renders a file-info box on `MovieDetail` gated on `m.file`, wired to a new delete hook that invalidates the detail query; back buttons get Tailwind hover/focus utilities.

**Tech Stack:** Go (chi router, database/sql over SQLite), React + TypeScript, TanStack Query, Vitest + Testing Library, Tailwind.

## Global Constraints

- Build/test with `CGO_ENABLED=0` — copied from prior Nexus plans; the repo builds pure-Go SQLite.
- Wire rule (load-bearing): optional objects/ids serialise as **absent** (omitempty) when unset, never as `0` or an empty object. Test key absence via `map[string]json.RawMessage`, not a typed round-trip.
- `web/dist` is committed and CI drift-checks it — the final frontend task must rebuild it.
- Follow existing patterns exactly: chi routes in `API.Mount`, service methods on `media.Service` (holds `store`, `meta`, `bus` — **no logger**), FE hooks in `library/api.ts`, component tests via `vi.mock("@/features/library/api")`.
- Best-effort file removal is never fatal, but **real errors are logged via `slog.Warn`** — the dominant codebase convention (automation/importing/downloadclient all call package-level `slog.Warn("<pkg>: <msg>", "key", val, "err", err)`; nothing is threaded through `Service`). A benign already-gone file (`os.IsNotExist`) counts as success and is **not** logged. This honors spec §3.2 ("logged, never fatal").

---

### Task 1: Backend — `file` object on `GET /movies/{id}`

**Files:**
- Modify: `internal/media/api.go` (add `movieFileDTO`, extend `movieDTO`, populate in `getMovie` at :480; `listMovies` at :462 stays untouched)
- Test: `internal/media/api_test.go`

**Interfaces:**
- Consumes: `store.MediaFileForMovie(ctx, movieID) (*store.MediaFile, error)` (`import_store.go:187`, nil when none); `quality.DefinitionByID(id int) (quality.QualityDefinition, bool)` (`definitions.go:56`); existing `movieDTO{store.Movie; HasFile bool}`.
- Produces: `movieDTO` now carries `File *movieFileDTO json:"file,omitempty"` with fields `relativePath,size,qualityId,quality,addedAt`. Task 3 (frontend) mirrors this shape.

- [ ] **Step 1: Write the failing test**

Add to `internal/media/api_test.go` (imports `encoding/json`, `net/http`, `net/http/httptest`, `strconv`, `github.com/hellboundg/nexus/internal/core/store`, `github.com/hellboundg/nexus/internal/quality` — add `quality` to the import block):

```go
func TestGetMovieIncludesFile(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()

	rf, err := st.CreateRootFolder(ctx, "/data/movies")
	if err != nil {
		t.Fatal(err)
	}
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rf})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: "Film (2020)/Film.2020.1080p.mkv",
		Size: 8455160320, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/movies/"+strconv.FormatInt(mid, 10), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}

	var got struct {
		HasFile bool `json:"hasFile"`
		File    *struct {
			RelativePath string `json:"relativePath"`
			Size         int64  `json:"size"`
			QualityID    int    `json:"qualityId"`
			Quality      string `json:"quality"`
		} `json:"file"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.HasFile || got.File == nil {
		t.Fatalf("want hasFile+file, got %+v", got)
	}
	if got.File.Size != 8455160320 || got.File.QualityID != 9 {
		t.Fatalf("file fields wrong: %+v", got.File)
	}
	wantName, _ := quality.DefinitionByID(9)
	if got.File.Quality != wantName.Name {
		t.Fatalf("quality name = %q want %q", got.File.Quality, wantName.Name)
	}
}

func TestGetMovieOmitsFileWhenAbsent(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	mid, err := st.CreateMovie(context.Background(), store.Movie{TMDBID: 200, Title: "Film"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/movies/"+strconv.FormatInt(mid, 10), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["file"]; present {
		t.Fatalf("file key must be absent when no media file, got %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/media/ -run 'TestGetMovie(IncludesFile|OmitsFileWhenAbsent)' -v`
Expected: FAIL — compile error (`movieDTO` has no `File` field) or `file` present/missing mismatch.

- [ ] **Step 3: Write minimal implementation**

In `internal/media/api.go`, add `"time"` and `"github.com/hellboundg/nexus/internal/quality"` to the imports if not present. Replace the `movieDTO` block (currently at :457) with:

```go
type movieFileDTO struct {
	RelativePath string    `json:"relativePath"`
	Size         int64     `json:"size"`
	QualityID    int       `json:"qualityId"`
	Quality      string    `json:"quality"`
	AddedAt      time.Time `json:"addedAt"`
}

type movieDTO struct {
	store.Movie
	HasFile bool          `json:"hasFile"`
	File    *movieFileDTO `json:"file,omitempty"`
}

func movieFile(f *store.MediaFile) *movieFileDTO {
	if f == nil {
		return nil
	}
	name := ""
	if def, ok := quality.DefinitionByID(f.QualityID); ok {
		name = def.Name
	}
	return &movieFileDTO{
		RelativePath: f.RelativePath, Size: f.Size, QualityID: f.QualityID,
		Quality: name, AddedAt: f.AddedAt,
	}
}
```

In `getMovie` (:480), after the existing `MovieFileIDs` load, fetch the file and attach it. Replace the final `api.WriteJSON(...)` line with:

```go
	file, err := a.store.MediaFileForMovie(r.Context(), id)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load movie")
		return
	}
	api.WriteJSON(w, http.StatusOK, movieDTO{Movie: *m, HasFile: files[m.ID], File: movieFile(file)})
```

(`listMovies` keeps `movieDTO{Movie: m, HasFile: files[m.ID]}` — `File` stays nil there, omitempty drops it.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/media/ -run 'TestGetMovie(IncludesFile|OmitsFileWhenAbsent)' -v`
Expected: PASS (both).

- [ ] **Step 5: Full package check + commit**

Run: `CGO_ENABLED=0 go build ./... && go vet ./internal/media/ && go test ./internal/media/`
Expected: all pass.

```bash
git add internal/media/api.go internal/media/api_test.go
git commit -m "feat(media): expose imported file on GET /movies/{id}"
```

---

### Task 2: Backend — `DeleteMovieFile` service + `DELETE /movies/{id}/file`

**Files:**
- Modify: `internal/media/media.go` (add `DeleteMovieFile`; add `log/slog`, `os`, `path/filepath` imports)
- Modify: `internal/media/api.go` (add route in the `/movies` block at :44; add `deleteMovieFile` handler)
- Test: `internal/media/service_test.go` (service behaviour), `internal/media/api_test.go` (route)

**Interfaces:**
- Consumes: `store.MediaFileForMovie` (Task-1 usage); `store.GetMovie(ctx, id) (*store.Movie, error)` (`store.Movie.RootFolderID *int64`); `store.GetRootFolder(ctx, id) (*store.RootFolder, error)` (`media_store.go:28`); `store.DeleteMediaFile(ctx, id) error` (`import_store.go:203`).
- Produces: `func (s *Service) DeleteMovieFile(ctx context.Context, movieID int64) error` — idempotent (nil when no file); `DELETE /movies/{id}/file` → `200 {"ok":true}`.

- [ ] **Step 1: Write the failing test**

Add to `internal/media/service_test.go` (imports `os`, `path/filepath` in addition to existing):

```go
func TestDeleteMovieFileRemovesRowAndDisk(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	root := t.TempDir()
	rid, err := st.CreateRootFolder(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rid})
	if err != nil {
		t.Fatal(err)
	}
	rel := "Film (2020)/Film.2020.1080p.mkv"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: rel, Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteMovieFile(ctx, mid); err != nil {
		t.Fatalf("DeleteMovieFile: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, stat err = %v", err)
	}
	f, err := st.MediaFileForMovie(ctx, mid)
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatal("media file row should be deleted")
	}
}

func TestDeleteMovieFileIdempotentWhenNoFile(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMovieFile(ctx, mid); err != nil {
		t.Fatalf("want nil for no-file, got %v", err)
	}
}

func TestDeleteMovieFileRemovesRowWhenRootUnresolvable(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	// No RootFolderID set → disk step skipped, row still deleted.
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: "nowhere/x.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMovieFile(ctx, mid); err != nil {
		t.Fatalf("DeleteMovieFile: %v", err)
	}
	f, err := st.MediaFileForMovie(ctx, mid)
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatal("row should be deleted even when root unresolvable")
	}
}
```

Add the route test to `internal/media/api_test.go`:

```go
func TestDeleteMovieFileRoute(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: "a/b.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/movies/"+strconv.FormatInt(mid, 10)+"/file", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	f, _ := st.MediaFileForMovie(ctx, mid)
	if f != nil {
		t.Fatal("file row should be gone after DELETE")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/media/ -run 'TestDeleteMovieFile' -v`
Expected: FAIL — `svc.DeleteMovieFile` undefined / route 404 or 405.

- [ ] **Step 3: Write minimal implementation**

In `internal/media/media.go`, add `"log/slog"`, `"os"`, and `"path/filepath"` to imports, then add:

```go
// DeleteMovieFile removes a movie's imported file: best-effort disk removal
// (real errors logged, never fatal; already-gone counts as success) then the
// DB row is always deleted, flipping the movie back to missing. No-op (nil)
// when the movie has no file.
func (s *Service) DeleteMovieFile(ctx context.Context, movieID int64) error {
	file, err := s.store.MediaFileForMovie(ctx, movieID)
	if err != nil {
		return err
	}
	if file == nil {
		return nil
	}
	if m, err := s.store.GetMovie(ctx, movieID); err == nil && m.RootFolderID != nil {
		if root, err := s.store.GetRootFolder(ctx, *m.RootFolderID); err == nil {
			abs := filepath.Join(root.Path, filepath.FromSlash(file.RelativePath))
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				slog.Warn("media: delete movie file from disk failed", "movieId", movieID, "err", err)
			}
		}
	}
	return s.store.DeleteMediaFile(ctx, file.ID)
}
```

In `internal/media/api.go`, inside the `/movies` route block (:44), add after the delete line:

```go
		r.Delete("/{id}/file", a.deleteMovieFile)
```

And add the handler beside `deleteMovie` (:502):

```go
func (a *API) deleteMovieFile(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.svc.DeleteMovieFile(r.Context(), id); err != nil {
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/media/ -run 'TestDeleteMovieFile' -v`
Expected: PASS (all four).

- [ ] **Step 5: Full package check + commit**

Run: `CGO_ENABLED=0 go build ./... && go vet ./internal/media/ && go test ./internal/media/`
Expected: all pass.

```bash
git add internal/media/media.go internal/media/api.go internal/media/service_test.go internal/media/api_test.go
git commit -m "feat(media): DELETE /movies/{id}/file — best-effort delete of a movie file"
```

---

### Task 3: Frontend — `Movie.file` type + file-info box + `useDeleteMovieFile`

**Files:**
- Modify: `web/src/features/library/types.ts` (`Movie` gains `file?`)
- Modify: `web/src/features/library/api.ts` (add `useDeleteMovieFile`)
- Modify: `web/src/features/library/MovieDetail.tsx` (render the box)
- Test: `web/src/features/library/MovieDetail.test.tsx`

**Interfaces:**
- Consumes: backend `file` shape from Task 1; `formatSize` from `@/features/search/resolve` (`resolve.ts:35`, returns `"—"` for 0); `apiDelete` + `libraryKeys.movie(id)` already in `api.ts`; `useToast` from `@/lib/toast`.
- Produces: `useDeleteMovieFile()` → mutation over `(id: number)`; a File box region with a Delete-file button.

- [ ] **Step 1: Write the failing test**

Edit `MovieDetail.test.tsx`. Add `useDeleteMovieFile` to the `vi.mock` factory's returned object (the list at line 14-15) and mock it in `renderMovie` (add `vi.mocked(lib.useDeleteMovieFile).mockReturnValue(mut({ mutate: del }))` — extend `renderMovie` to accept and wire a `del` fn like `search`). Then add:

```ts
const FILE = {
  relativePath: "Film (2020)/Film.2020.1080p.mkv",
  size: 8455160320, qualityId: 9, quality: "Bluray-1080p",
  addedAt: "2026-07-10T14:22:03Z",
}

it("renders the file box when a file is present", () => {
  renderMovie(5, { id: 5, title: "Film", year: 2020, overview: "x", monitored: true, hasFile: true, qualityProfileId: 1, posterUrl: "", fanartUrl: "", file: FILE })
  expect(screen.getByText("Film.2020.1080p.mkv")).toBeInTheDocument()
  expect(screen.getByText(/Bluray-1080p/)).toBeInTheDocument()
  expect(screen.getByRole("button", { name: /delete file/i })).toBeInTheDocument()
})

it("hides the file box when no file", () => {
  renderMovie(5, { id: 5, title: "Film", year: 2020, overview: "x", monitored: true, hasFile: false, qualityProfileId: 1, posterUrl: "", fanartUrl: "" })
  expect(screen.queryByRole("button", { name: /delete file/i })).not.toBeInTheDocument()
})

it("deletes the file after confirm", async () => {
  vi.spyOn(window, "confirm").mockReturnValue(true)
  const del = vi.fn()
  renderMovie(5, { id: 5, title: "Film", year: 2020, overview: "x", monitored: true, hasFile: true, qualityProfileId: 1, posterUrl: "", fanartUrl: "", file: FILE }, vi.fn(), del)
  await userEvent.click(screen.getByRole("button", { name: /delete file/i }))
  expect(del).toHaveBeenCalledWith(5, expect.anything())
})
```

Update the `renderMovie` signature to `(id, movie, search = vi.fn(), del = vi.fn())` and add `vi.mocked(lib.useDeleteMovieFile).mockReturnValue(mut({ mutate: del }))`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/library/MovieDetail.test.tsx`
Expected: FAIL — `useDeleteMovieFile` is not a function / file box text not found.

- [ ] **Step 3: Write minimal implementation**

In `web/src/features/library/types.ts`, add to the `Movie` type (after `hasFile`):

```ts
  file?: {
    relativePath: string
    size: number
    qualityId: number
    quality: string
    addedAt: string
  } | null
```

In `web/src/features/library/api.ts`, add (mirroring `useDelete` at :101):

```ts
export function useDeleteMovieFile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/movies/${id}/file`),
    onSuccess: (_d, id) => qc.invalidateQueries({ queryKey: libraryKeys.movie(id) }),
  })
}
```

In `web/src/features/library/MovieDetail.tsx`:
- Add imports: `import { formatSize } from "@/features/search/resolve"` and add `useDeleteMovieFile` to the existing `./api` import.
- Add the hook near the others: `const delFile = useDeleteMovieFile()`.
- After the closing `</DetailBanner>` and before `<InteractiveSearchDialog ...>`, add:

```tsx
      {m.file ? (
        <div className="mt-6 rounded-lg border border-[var(--color-border)] p-4">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
              <p className="truncate font-medium">{m.file.relativePath.split("/").pop()}</p>
              <p className="mt-1 text-sm text-[var(--color-muted)]">
                {m.file.quality ? <span>{m.file.quality} · </span> : null}
                {formatSize(m.file.size)} · added {new Date(m.file.addedAt).toLocaleDateString()}
              </p>
              <p className="mt-1 truncate text-xs text-[var(--color-muted)]">{m.file.relativePath}</p>
            </div>
            <button
              onClick={() => {
                if (confirm("Delete this file from disk?")) {
                  delFile.mutate(id, { onSuccess: () => toast("File deleted") })
                }
              }}
              className="shrink-0 rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
            >
              Delete file
            </button>
          </div>
        </div>
      ) : null}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/library/MovieDetail.test.tsx`
Expected: PASS.

- [ ] **Step 5: Typecheck + commit**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: tsc clean; full suite green.

```bash
git add web/src/features/library/types.ts web/src/features/library/api.ts web/src/features/library/MovieDetail.tsx web/src/features/library/MovieDetail.test.tsx
git commit -m "feat(webui): movie file-info box + delete-file"
```

---

### Task 4: Frontend — back-button hover/focus + `web/dist` rebuild

**Files:**
- Modify: `web/src/features/library/MovieDetail.tsx` (banner back at :44 + not-found back at :31)
- Modify: `web/src/features/library/SeriesDetail.tsx` (banner back at :47 + not-found back at :34)
- Modify: `web/dist/**` (rebuild)

**Interfaces:**
- Consumes: nothing new.
- Produces: back buttons gain `hover:underline` + focus-visible ring. No test (CSS-only, per spec §5).

- [ ] **Step 1: Apply the className change (no test — visual only)**

In both files, the back buttons currently read `className="text-sm text-[var(--color-brand)]"` (the not-found ones also carry `mt-3`). Append the hover/focus utilities so each becomes:

```
text-sm text-[var(--color-brand)] rounded hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-brand)]
```

Apply to all four spots:
- `MovieDetail.tsx:31` (keep the leading `mt-3`), `MovieDetail.tsx:44`
- `SeriesDetail.tsx:34` (keep the leading `mt-3`), `SeriesDetail.tsx:47`

- [ ] **Step 2: Verify existing tests still pass**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: tsc clean; full suite green (the back-button existence tests, e.g. `floats the back link above the banner`, still pass — text unchanged).

- [ ] **Step 3: Rebuild the committed frontend bundle**

Run: `cd web && npm run build`
Expected: build succeeds; `web/dist` reflects Tasks 3 + 4.

- [ ] **Step 4: Confirm no stray drift**

Run: `git status --short web/dist`
Expected: only expected `web/dist` asset changes.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/library/MovieDetail.tsx web/src/features/library/SeriesDetail.tsx web/dist
git commit -m "feat(webui): back-button hover/focus states + rebuild dist"
```

---

## Self-Review

**Spec coverage:**
- §3.1 `file` on getMovie (resolved quality name, omitempty, listMovies untouched) → Task 1. ✓
- §3.2 `DELETE /movies/{id}/file` + `DeleteMovieFile` (best-effort disk, always DB row, idempotent, root-unresolvable) → Task 2. ✓
- §4.1 `Movie.file` type → Task 3. ✓
- §4.2 file-info box (name/quality/size/path/date, warn Delete-file behind confirm) → Task 3. ✓
- §4.3 `useDeleteMovieFile` (invalidate `libraryKeys.movie(id)`, toast) → Task 3. ✓
- §4.4 back-button hover/focus on both pages incl. not-found fallbacks → Task 4. ✓
- §5 tests: getMovie present+resolved / absent-via-RawMessage (T1); DeleteMovieFile row+disk / idempotent / root-unresolvable (T2); FE box present/absent + delete-invalidates (T3); dist rebuild (T4); #6 untested (T4). ✓

**Placeholder scan:** No TBDs; every code step carries full code and exact commands. Best-effort disk removal logs real errors via `slog.Warn` per spec §3.2 (Global Constraints), skipping benign already-gone files.

**Type consistency:** `movieFileDTO` fields (`relativePath/size/qualityId/quality/addedAt`) match the FE `Movie.file` shape and the Task-1 test's inline struct. `DeleteMovieFile(ctx, movieID int64) error` is used identically in the handler (T2) and service tests. `useDeleteMovieFile()` mutate signature `(id: number)` matches the component call `delFile.mutate(id, …)` and the test `del` assertion. `quality.DefinitionByID` name field (`.Name`) used consistently.
