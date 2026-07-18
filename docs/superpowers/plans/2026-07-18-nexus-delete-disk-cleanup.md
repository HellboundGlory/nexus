# Delete with disk cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an opt-in "Delete files from disk" checkbox to the movie/series delete flow that best-effort removes the item's whole folder on disk (guarded against escaping the root), while DB rows are always removed.

**Architecture:** The existing `DELETE /movies|series/{id}` endpoints gain a `?deleteFiles=true` query param and route through new `media.Service.DeleteMovie/DeleteSeries(ctx, id, deleteFiles)` methods. When set, the service gathers the item's folder(s) from tracked files BEFORE the DB cascade, deletes the DB rows, then `os.RemoveAll`s each derived folder (best-effort, `slog.Warn` on error) — using a shared containment helper so the target is always a direct child of the root. Frontend replaces the native `confirm()` on both detail pages with a reusable `DeleteConfirmDialog` (checkbox default off).

**Tech Stack:** Go (chi router, database/sql over SQLite), React + TypeScript, TanStack Query, Vitest + Testing Library, Tailwind, radix Dialog.

## Global Constraints

- Build/test with `CGO_ENABLED=0`.
- `deleteFiles` is parsed as `r.URL.Query().Get("deleteFiles") == "true"`. Absent or any other value = `false` = today's DB-only delete (backward compatible — no existing caller passes it).
- Disk removal is **best-effort, never fatal**: real `os.RemoveAll` errors are `slog.Warn`-logged (package-level `slog.Default()`, no logger threaded through `Service`). The DB delete always runs and is the only operation whose error fails the request.
- **Ordering is load-bearing:** gather folder targets BEFORE the DB delete (the FK cascade wipes `media_files`), then delete DB rows, then remove folders.
- **Containment guard (load-bearing):** the folder target is `root.Path` + the first path segment of a file's `relativePath`; reject empty/`.`/`..`/anything `filepath.Rel` shows escaping root, so `RemoveAll` can never hit the root itself or climb out.
- No `MovieUpdated`/`SeriesUpdated` event on delete — the item is gone; the FE invalidates the list query on success (unchanged).
- Checkbox **defaults to off** and resets to off each time the dialog opens.
- `web/dist` is committed and CI drift-checks it — the final frontend task rebuilds it.
- Follow existing patterns: service methods on `media.Service` (holds `store`, `meta`, `bus`; the API struct exposes `a.svc` and `a.store`), FE hooks in `library/api.ts`, dialogs on `components/ui/dialog.tsx`.

---

### Task 1: Backend — movie delete with disk cleanup

**Files:**
- Modify: `internal/media/media.go` (add `DeleteMovie`, `diskTargetsForMovie`, and the shared `itemFolderTarget` + `removeItemFolders` helpers; add `os`, `path/filepath`, `strings` [already imported], `log/slog` imports as needed)
- Modify: `internal/media/api.go` (wire `deleteMovie` handler at :532 through `a.svc.DeleteMovie` + parse the query param)
- Test: `internal/media/service_test.go`, `internal/media/api_test.go`

**Interfaces:**
- Consumes: `store.MediaFileForMovie(ctx, id) (*store.MediaFile, error)` (`import_store.go:187`); `store.GetMovie` / `store.Movie.RootFolderID *int64`; `store.GetRootFolder(ctx, id) (*store.RootFolder, error)` (`media_store.go:28`, `RootFolder{ID, Path}`); `store.DeleteMovie(ctx, id) error` (`media_store.go:443`).
- Produces: `func (s *Service) DeleteMovie(ctx context.Context, id int64, deleteFiles bool) error`; shared `itemFolderTarget(rootPath, relPath string) string` and `func (s *Service) removeItemFolders(root store.RootFolder, files []store.MediaFile)` (reused by Task 2).

- [ ] **Step 1: Write the failing tests**

Add to `internal/media/service_test.go` (imports `os`, `path/filepath` in addition to existing):

```go
func TestDeleteMovieWithDiskRemovesFolderAndRows(t *testing.T) {
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
	folder := filepath.Join(root, "Film (2020)")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "Film.2020.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: "Film (2020)/Film.2020.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteMovie(ctx, mid, true); err != nil {
		t.Fatalf("DeleteMovie: %v", err)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder should be gone, stat err = %v", err)
	}
	if _, err := st.GetMovie(ctx, mid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("movie should be deleted, got %v", err)
	}
}

func TestDeleteMovieWithoutDiskKeepsFolder(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	root := t.TempDir()
	rid, _ := st.CreateRootFolder(ctx, root)
	mid, _ := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rid})
	folder := filepath.Join(root, "Film (2020)")
	os.MkdirAll(folder, 0o755)
	os.WriteFile(filepath.Join(folder, "f.mkv"), []byte("x"), 0o644)
	st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &mid, RelativePath: "Film (2020)/f.mkv", Size: 1, QualityID: 9})

	if err := svc.DeleteMovie(ctx, mid, false); err != nil {
		t.Fatalf("DeleteMovie: %v", err)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("folder should remain when deleteFiles=false, got %v", err)
	}
	if _, err := st.GetMovie(ctx, mid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("movie should still be deleted from DB, got %v", err)
	}
}

func TestDeleteMovieWithDiskNoFileSkipsDisk(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	mid, _ := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film"})
	if err := svc.DeleteMovie(ctx, mid, true); err != nil {
		t.Fatalf("want nil for no-file, got %v", err)
	}
	if _, err := st.GetMovie(ctx, mid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("movie should be deleted, got %v", err)
	}
}

func TestDeleteMovieContainmentGuardRejectsEscape(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// A sentinel OUTSIDE the root that a naive RemoveAll of "../victim" would hit.
	victim := filepath.Join(parent, "victim")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	rid, _ := st.CreateRootFolder(ctx, root)
	mid, _ := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rid})
	st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &mid, RelativePath: "../victim/f.mkv", Size: 1, QualityID: 9})

	if err := svc.DeleteMovie(ctx, mid, true); err != nil {
		t.Fatalf("DeleteMovie: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("containment guard failed — victim outside root was removed: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root itself must survive: %v", err)
	}
}
```

Add the handler test to `internal/media/api_test.go`:

```go
func TestDeleteMovieRouteParsesDeleteFiles(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()

	root := t.TempDir()
	rid, _ := st.CreateRootFolder(ctx, root)
	mid, _ := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rid})
	folder := filepath.Join(root, "Film (2020)")
	os.MkdirAll(folder, 0o755)
	os.WriteFile(filepath.Join(folder, "f.mkv"), []byte("x"), 0o644)
	st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &mid, RelativePath: "Film (2020)/f.mkv", Size: 1, QualityID: 9})

	req := httptest.NewRequest(http.MethodDelete, "/movies/"+strconv.FormatInt(mid, 10)+"?deleteFiles=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder should be gone after deleteFiles=true, stat err = %v", err)
	}
}
```

(`api_test.go` needs `os`, `path/filepath` imports added.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/media/ -run 'TestDeleteMovie(WithDisk|WithoutDisk|Containment|RouteParses)' -v`
Expected: FAIL — `svc.DeleteMovie` undefined (wrong arg count) / handler ignores the param.

- [ ] **Step 3: Write minimal implementation**

In `internal/media/media.go`, ensure imports include `"log/slog"`, `"os"`, `"path/filepath"`, `"strings"` (strings is already imported), then add:

```go
// itemFolderTarget returns the absolute folder to remove for a file's relative
// path under rootPath, or "" if it cannot be safely derived. The result is
// always a direct child of rootPath: an empty/"."/".." first segment or any
// path that would escape the root returns "" (so RemoveAll can never hit the
// root itself or climb out of it).
func itemFolderTarget(rootPath, relPath string) string {
	seg := strings.SplitN(filepath.ToSlash(relPath), "/", 2)[0]
	if seg == "" || seg == "." || seg == ".." {
		return ""
	}
	abs := filepath.Join(rootPath, seg)
	rel, err := filepath.Rel(rootPath, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ""
	}
	return abs
}

// removeItemFolders best-effort deletes each distinct item folder derived from
// the given files. Errors are logged, never fatal.
func (s *Service) removeItemFolders(root store.RootFolder, files []store.MediaFile) {
	seen := map[string]bool{}
	for _, f := range files {
		target := itemFolderTarget(root.Path, f.RelativePath)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		if err := os.RemoveAll(target); err != nil {
			slog.Warn("media: delete item folder from disk failed", "path", target, "err", err)
		}
	}
}

// diskTargetsForMovie gathers (root, files) for a movie's disk cleanup, or
// (nil, nil) when deleteFiles is false, the movie has no file, or the root
// can't be resolved. Best-effort; logs on a gather error.
func (s *Service) diskTargetsForMovie(ctx context.Context, id int64, deleteFiles bool) (*store.RootFolder, []store.MediaFile) {
	if !deleteFiles {
		return nil, nil
	}
	file, err := s.store.MediaFileForMovie(ctx, id)
	if err != nil {
		slog.Warn("media: gather movie file for disk delete failed", "movieId", id, "err", err)
		return nil, nil
	}
	if file == nil {
		return nil, nil
	}
	m, err := s.store.GetMovie(ctx, id)
	if err != nil || m.RootFolderID == nil {
		return nil, nil
	}
	root, err := s.store.GetRootFolder(ctx, *m.RootFolderID)
	if err != nil {
		return nil, nil
	}
	return root, []store.MediaFile{*file}
}

// DeleteMovie removes a movie from the library. When deleteFiles is set, its
// on-disk folder is also removed (best-effort, after the DB delete).
func (s *Service) DeleteMovie(ctx context.Context, id int64, deleteFiles bool) error {
	root, files := s.diskTargetsForMovie(ctx, id, deleteFiles)
	if err := s.store.DeleteMovie(ctx, id); err != nil {
		return err
	}
	if root != nil {
		s.removeItemFolders(*root, files)
	}
	return nil
}
```

In `internal/media/api.go`, change `deleteMovie` (:532) to parse the param and call the service:

```go
func (a *API) deleteMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	deleteFiles := r.URL.Query().Get("deleteFiles") == "true"
	if err := a.svc.DeleteMovie(r.Context(), id, deleteFiles); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete movie")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/media/ -run 'TestDeleteMovie(WithDisk|WithoutDisk|Containment|RouteParses)' -v`
Expected: PASS (all).

- [ ] **Step 5: Full package check + commit**

Run: `CGO_ENABLED=0 go build ./... && go vet ./internal/media/ && go test ./internal/media/`
Expected: all pass.

```bash
git add internal/media/media.go internal/media/api.go internal/media/service_test.go internal/media/api_test.go
git commit -m "feat(media): delete movie with opt-in disk cleanup"
```

---

### Task 2: Backend — series delete with disk cleanup

**Files:**
- Modify: `internal/core/store/media_store.go` (add `MediaFilesForSeries`)
- Modify: `internal/media/media.go` (add `DeleteSeries`, `diskTargetsForSeries`)
- Modify: `internal/media/api.go` (wire `deleteSeries` handler at :320)
- Test: `internal/core/store/import_store_test.go` (package `store`, uses `newImportTestStore`), `internal/media/service_test.go`, `internal/media/api_test.go`

**Interfaces:**
- Consumes: Task 1's `itemFolderTarget` + `removeItemFolders`; `store.GetSeries` / `store.Series.RootFolderID *int64` (`media_store.go:90`); `store.DeleteSeries(ctx, id) error` (`media_store.go:189`); `store.ListEpisodes(ctx, seriesID)` (`media_store.go:251`).
- Produces: `func (s *Store) MediaFilesForSeries(ctx context.Context, seriesID int64) ([]MediaFile, error)`; `func (s *Service) DeleteSeries(ctx context.Context, id int64, deleteFiles bool) error`.

- [ ] **Step 1: Write the failing tests**

Add the store test (place beside the other `import_store` tests in `internal/core/store/import_store_test.go`). This file is `package store`, so types are **unqualified** (`Series{}`, not `store.Series{}`):

```go
func TestMediaFilesForSeries(t *testing.T) {
	st := newImportTestStore(t) // existing helper at import_store_test.go:12 (Open+Migrate+New)
	ctx := context.Background()

	sid, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "Show"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSeason(ctx, Season{SeriesID: sid, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "E1"}); err != nil {
		t.Fatal(err)
	}
	eps, _ := st.ListEpisodes(ctx, sid)
	if len(eps) == 0 {
		t.Fatal("no episodes")
	}
	if _, err := st.UpsertMediaFile(ctx, MediaFile{
		MediaKind: "episode", EpisodeID: &eps[0].ID, RelativePath: "Show/Season 01/E01.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	files, err := st.MediaFilesForSeries(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].RelativePath != "Show/Season 01/E01.mkv" {
		t.Fatalf("want 1 series file, got %+v", files)
	}
}
```

> Confirmed helpers (do not invent): `newImportTestStore(t) *Store` (`import_store_test.go:12`); `st.CreateSeries(ctx, Series) (int64, error)` (`media_store.go:142`); `st.UpsertSeason`/`UpsertEpisode`/`ListEpisodes` (`media_store.go:205/241/251`). The `MediaFilesForSeries` test lives in the store package — no `store.` prefix.

Add the service + route tests to `internal/media/service_test.go` and `internal/media/api_test.go`:

```go
// service_test.go
func TestDeleteSeriesWithDiskRemovesFolderAndRows(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	root := t.TempDir()
	rid, err := st.CreateRootFolder(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	se, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, RootFolderID: &rid, MonitorOption: MonitorAll})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := st.ListEpisodes(ctx, se.ID)
	if len(eps) == 0 {
		t.Fatal("no episodes")
	}
	seasonDir := filepath.Join(root, "Show", "Season 01")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(seasonDir, "E01.mkv"), []byte("x"), 0o644)
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "episode", EpisodeID: &eps[0].ID, RelativePath: "Show/Season 01/E01.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteSeries(ctx, se.ID, true); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Show")); !os.IsNotExist(err) {
		t.Fatalf("series folder should be gone, stat err = %v", err)
	}
	if _, err := st.GetSeries(ctx, se.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("series should be deleted, got %v", err)
	}
}
```

```go
// api_test.go
func TestDeleteSeriesRouteParsesDeleteFiles(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()
	root := t.TempDir()
	rid, _ := st.CreateRootFolder(ctx, root)
	// Seed a series with one episode file on disk.
	sid, _ := st.CreateSeries(ctx, store.Series{TMDBID: 100, Title: "Show", RootFolderID: &rid})
	st.UpsertSeason(ctx, store.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true})
	st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "E1"})
	eps, _ := st.ListEpisodes(ctx, sid)
	dir := filepath.Join(root, "Show", "Season 01")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "E01.mkv"), []byte("x"), 0o644)
	st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "episode", EpisodeID: &eps[0].ID, RelativePath: "Show/Season 01/E01.mkv", Size: 1, QualityID: 9})

	req := httptest.NewRequest(http.MethodDelete, "/series/"+strconv.FormatInt(sid, 10)+"?deleteFiles=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "Show")); !os.IsNotExist(err) {
		t.Fatalf("series folder should be gone, stat err = %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/store/ -run TestMediaFilesForSeries -v && go test ./internal/media/ -run 'TestDeleteSeries' -v`
Expected: FAIL — `MediaFilesForSeries` / `svc.DeleteSeries` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/core/store/media_store.go`, add:

```go
// MediaFilesForSeries returns every media file belonging to a series' episodes.
func (s *Store) MediaFilesForSeries(ctx context.Context, seriesID int64) ([]MediaFile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT mf.id, mf.media_kind, mf.episode_id, mf.movie_id, mf.relative_path, mf.size, mf.quality_id, mf.added_at
		 FROM media_files mf JOIN episodes e ON mf.episode_id = e.id
		 WHERE e.series_id = ?`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaFile
	for rows.Next() {
		f, err := scanMediaFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
```

> `scanMediaFile` is the existing row-scanner in `import_store.go:177` (`scanMediaFile(sc interface{ Scan(...any) error })`), which `*sql.Rows` satisfies. Match the column order to that scanner: `id, media_kind, episode_id, movie_id, relative_path, size, quality_id, added_at`.

In `internal/media/media.go`, add:

```go
// diskTargetsForSeries gathers (root, files) for a series' disk cleanup, or
// (nil, nil) when deleteFiles is false or the root can't be resolved.
func (s *Service) diskTargetsForSeries(ctx context.Context, id int64, deleteFiles bool) (*store.RootFolder, []store.MediaFile) {
	if !deleteFiles {
		return nil, nil
	}
	files, err := s.store.MediaFilesForSeries(ctx, id)
	if err != nil {
		slog.Warn("media: gather series files for disk delete failed", "seriesId", id, "err", err)
		return nil, nil
	}
	if len(files) == 0 {
		return nil, nil
	}
	se, err := s.store.GetSeries(ctx, id)
	if err != nil || se.RootFolderID == nil {
		return nil, nil
	}
	root, err := s.store.GetRootFolder(ctx, *se.RootFolderID)
	if err != nil {
		return nil, nil
	}
	return root, files
}

// DeleteSeries removes a series from the library. When deleteFiles is set, its
// on-disk folder is also removed (best-effort, after the DB delete).
func (s *Service) DeleteSeries(ctx context.Context, id int64, deleteFiles bool) error {
	root, files := s.diskTargetsForSeries(ctx, id, deleteFiles)
	if err := s.store.DeleteSeries(ctx, id); err != nil {
		return err
	}
	if root != nil {
		s.removeItemFolders(*root, files)
	}
	return nil
}
```

In `internal/media/api.go`, change `deleteSeries` (:320):

```go
func (a *API) deleteSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	deleteFiles := r.URL.Query().Get("deleteFiles") == "true"
	if err := a.svc.DeleteSeries(r.Context(), id, deleteFiles); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete series")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/store/ -run TestMediaFilesForSeries -v && go test ./internal/media/ -run 'TestDeleteSeries' -v`
Expected: PASS.

- [ ] **Step 5: Full check + commit**

Run: `CGO_ENABLED=0 go build ./... && go vet ./internal/media/ ./internal/core/store/ && go test ./internal/media/ ./internal/core/store/`
Expected: all pass.

```bash
git add internal/core/store/media_store.go internal/core/store/import_store_test.go internal/media/media.go internal/media/api.go internal/media/service_test.go internal/media/api_test.go
git commit -m "feat(media): delete series with opt-in disk cleanup"
```

---

### Task 3: Frontend — `DeleteConfirmDialog` + `useDelete` deleteFiles

**Files:**
- Create: `web/src/features/library/DeleteConfirmDialog.tsx`
- Create: `web/src/features/library/DeleteConfirmDialog.test.tsx`
- Modify: `web/src/features/library/api.ts` (`useDelete` at :101)
- Test: `web/src/features/library/api.test.tsx` (or inline in the dialog test — see below)

**Interfaces:**
- Consumes: `Dialog` + `DialogTitle` from `@/components/ui/dialog` (props `open`, `onOpenChange`, `children`, `className`); `apiDelete<T>(path)` (`lib/api.ts:62`).
- Produces: `DeleteConfirmDialog({ open, onOpenChange, title, onConfirm })` where `onConfirm: (deleteFiles: boolean) => void`; `useDelete` mutation input gains `deleteFiles?: boolean`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/library/DeleteConfirmDialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { DeleteConfirmDialog } from "@/features/library/DeleteConfirmDialog"

describe("DeleteConfirmDialog", () => {
  it("defaults the checkbox to off and confirms with false", async () => {
    const onConfirm = vi.fn()
    render(<DeleteConfirmDialog open title="Film" onOpenChange={vi.fn()} onConfirm={onConfirm} />)
    expect((screen.getByRole("checkbox") as HTMLInputElement).checked).toBe(false)
    await userEvent.click(screen.getByRole("button", { name: /^delete$/i }))
    expect(onConfirm).toHaveBeenCalledWith(false)
  })

  it("confirms with true when the box is checked", async () => {
    const onConfirm = vi.fn()
    render(<DeleteConfirmDialog open title="Film" onOpenChange={vi.fn()} onConfirm={onConfirm} />)
    await userEvent.click(screen.getByRole("checkbox"))
    await userEvent.click(screen.getByRole("button", { name: /^delete$/i }))
    expect(onConfirm).toHaveBeenCalledWith(true)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/library/DeleteConfirmDialog.test.tsx`
Expected: FAIL — module `DeleteConfirmDialog` not found.

- [ ] **Step 3: Write minimal implementation**

Create `web/src/features/library/DeleteConfirmDialog.tsx`:

```tsx
import { useEffect, useState } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"

export function DeleteConfirmDialog({
  open,
  onOpenChange,
  title,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  onConfirm: (deleteFiles: boolean) => void
}) {
  const [deleteFiles, setDeleteFiles] = useState(false)
  useEffect(() => {
    if (open) setDeleteFiles(false)
  }, [open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>Delete {title}?</DialogTitle>
      <label className="mt-2 flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={deleteFiles}
          onChange={(e) => setDeleteFiles(e.target.checked)}
        />
        Delete files from disk
      </label>
      <p className="mt-1 text-xs text-[var(--color-muted)]">
        Also remove the folder and its files from disk.
      </p>
      <div className="mt-4 flex justify-end gap-2">
        <button
          onClick={() => onOpenChange(false)}
          className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
        >
          Cancel
        </button>
        <button
          onClick={() => onConfirm(deleteFiles)}
          className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
        >
          Delete
        </button>
      </div>
    </Dialog>
  )
}
```

In `web/src/features/library/api.ts`, extend `useDelete`:

```ts
export function useDelete() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ kind, id, deleteFiles }: { kind: "movie" | "series"; id: number; deleteFiles?: boolean }) =>
      apiDelete<{ ok: boolean }>(
        `/${kind === "movie" ? "movies" : "series"}/${id}${deleteFiles ? "?deleteFiles=true" : ""}`,
      ),
    onSuccess: (_d, v) =>
      qc.invalidateQueries({ queryKey: v.kind === "movie" ? libraryKeys.movies : libraryKeys.series }),
  })
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/library/DeleteConfirmDialog.test.tsx`
Expected: PASS.

- [ ] **Step 5: Typecheck + commit**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: tsc clean; full suite green.

```bash
git add web/src/features/library/DeleteConfirmDialog.tsx web/src/features/library/DeleteConfirmDialog.test.tsx web/src/features/library/api.ts
git commit -m "feat(webui): DeleteConfirmDialog + deleteFiles on useDelete"
```

---

### Task 4: Frontend — wire both detail pages + `web/dist` rebuild

**Files:**
- Modify: `web/src/features/library/MovieDetail.tsx` (delete button at :91)
- Modify: `web/src/features/library/SeriesDetail.tsx` (delete button at :70)
- Modify: `web/src/features/library/MovieDetail.test.tsx`, `web/src/features/library/SeriesDetail.test.tsx`
- Modify: `web/dist/**` (rebuild)

**Interfaces:**
- Consumes: `DeleteConfirmDialog` (Task 3); `useDelete` with `deleteFiles` (Task 3).
- Produces: both detail pages open the dialog instead of `confirm()`; the delete mutation carries the chosen `deleteFiles`.

- [ ] **Step 1: Write the failing test**

In `MovieDetail.test.tsx`, add (the harness already mocks `useDelete` → `mut()`; add a `del` spy like the file-delete test):

```tsx
it("opens the delete dialog and deletes with the chosen disk option", async () => {
  const del = vi.fn()
  vi.mocked(lib.useDelete).mockReturnValue(mut({ mutate: del }))
  renderMovie(5, { id: 5, title: "Film", year: 2020, overview: "x", monitored: true, hasFile: false, qualityProfileId: 1, posterUrl: "", fanartUrl: "" })
  await userEvent.click(screen.getByRole("button", { name: /^delete$/i }))
  // dialog now open; toggle the disk checkbox and confirm
  await userEvent.click(screen.getByRole("checkbox"))
  await userEvent.click(screen.getByRole("button", { name: /^delete$/i, hidden: false }).closest("[role=dialog]") ? screen.getAllByRole("button", { name: /^delete$/i })[1] : screen.getByRole("button", { name: /^delete$/i }))
  expect(del).toHaveBeenCalledWith(expect.objectContaining({ kind: "movie", id: 5, deleteFiles: true }), expect.anything())
})
```

> Implementer note: the page's "Delete" button and the dialog's "Delete" button share an accessible name. Query them unambiguously — after opening the dialog, use `within(screen.getByRole("dialog"))` to scope to the dialog's Delete button rather than the brittle expression sketched above. Rewrite the click line as:
> ```tsx
> import { within } from "@testing-library/react"
> // ...
> await userEvent.click(screen.getByRole("button", { name: /^delete$/i })) // page button opens dialog
> const dialog = screen.getByRole("dialog")
> await userEvent.click(within(dialog).getByRole("checkbox"))
> await userEvent.click(within(dialog).getByRole("button", { name: /^delete$/i }))
> expect(del).toHaveBeenCalledWith(expect.objectContaining({ kind: "movie", id: 5, deleteFiles: true }), expect.anything())
> ```
> Use the `within`-scoped version as the actual test.

Add the analogous test to `SeriesDetail.test.tsx` (kind `"series"`, its own render helper).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/library/MovieDetail.test.tsx src/features/library/SeriesDetail.test.tsx`
Expected: FAIL — no dialog opens (still native `confirm`); no `role="dialog"`.

- [ ] **Step 3: Write minimal implementation**

In `MovieDetail.tsx`: import `useState` (already imported for search) and `DeleteConfirmDialog`; add `const [confirmDelete, setConfirmDelete] = useState(false)`. Change the Delete button's `onClick` (currently the `confirm(...)` block at :88-92) to just `onClick={() => setConfirmDelete(true)}`. Then render the dialog near the InteractiveSearchDialog at the bottom:

```tsx
      <DeleteConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={m.title}
        onConfirm={(deleteFiles) => {
          setConfirmDelete(false)
          del.mutate({ kind: "movie", id, deleteFiles }, { onSuccess: () => { toast("Deleted"); nav("/movies") } })
        }}
      />
```

In `SeriesDetail.tsx`: import `useState` + `DeleteConfirmDialog`; add `const [confirmDelete, setConfirmDelete] = useState(false)`. Change the Delete button (`:70`) `onClick` from the inline `confirm(...)` to `onClick={() => setConfirmDelete(true)}`, and render:

```tsx
      <DeleteConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={s.title}
        onConfirm={(deleteFiles) => {
          setConfirmDelete(false)
          del.mutate({ kind: "series", id, deleteFiles }, { onSuccess: () => { toast("Deleted"); nav("/tv") } })
        }}
      />
```

(Leave the movie **file**-delete button's own `confirm()` in MovieDetail untouched — this task only replaces the movie/series *item* delete.)

- [ ] **Step 4: Run tests + typecheck**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: tsc clean; full suite green (including the two new dialog tests; existing tests that don't click Delete are unaffected).

- [ ] **Step 5: Rebuild the committed bundle**

Run: `cd web && npm run build`
Expected: build succeeds; `web/dist` reflects Tasks 3 + 4.

- [ ] **Step 6: Confirm no stray drift + commit**

Run: `git status --short web/dist`
Expected: only expected `web/dist` asset changes.

```bash
git add web/src/features/library/MovieDetail.tsx web/src/features/library/SeriesDetail.tsx web/src/features/library/MovieDetail.test.tsx web/src/features/library/SeriesDetail.test.tsx web/dist
git commit -m "feat(webui): wire DeleteConfirmDialog into detail pages + rebuild dist"
```

---

## Self-Review

**Spec coverage:**
- §3.1 `?deleteFiles=true` on both endpoints (absent=false) → T1 (movie handler), T2 (series handler). ✓
- §3.2 route through `Service.DeleteMovie/DeleteSeries` (no emit) → T1, T2. ✓
- §3.3 ordering (gather → DB delete → best-effort RemoveAll), gather-error skips disk → T1/T2 `diskTargetsFor*` (logs + returns nil,nil on error). ✓
- §3.4 folder derivation + containment guard → T1 `itemFolderTarget` (+ containment test). ✓
- §3.5 `store.MediaFilesForSeries` → T2. ✓
- §4.1 `DeleteConfirmDialog` (checkbox default off, resets on open) → T3. ✓
- §4.2 `useDelete` gains `deleteFiles`, appends param → T3. ✓
- §4.3 detail pages open dialog, carry deleteFiles → T4. ✓
- §5 error table: no-file skip (T1), false keeps folder (T1), containment reject (T1), root-unresolvable skip (covered by `diskTargetsFor*` returning nil,nil — same code path as no-file). ✓
- §6 tests: movie disk/no-disk/no-file/guard + route (T1); MediaFilesForSeries + series disk + route (T2); dialog checkbox default/true (T3); page opens dialog + carries deleteFiles (T4); dist rebuild (T4). ✓

**Placeholder scan:** No TBDs. The one soft spot is the store-test helper name (`newTestStore`) — flagged inline for the implementer to match the store package's actual helper rather than invent one; `CreateSeries`/`UpsertSeason`/`UpsertEpisode`/`ListEpisodes` signatures are cited from source.

**Type consistency:** `DeleteMovie(ctx, id int64, deleteFiles bool) error` / `DeleteSeries(...)` used identically in handlers (T1/T2) and tests. `itemFolderTarget(rootPath, relPath string) string` and `removeItemFolders(root store.RootFolder, files []store.MediaFile)` defined in T1, reused in T2. `MediaFilesForSeries(ctx, seriesID int64) ([]MediaFile, error)` defined + consumed in T2. FE `useDelete` input `{ kind, id, deleteFiles? }` matches the `del.mutate(...)` calls in T4 and the `DeleteConfirmDialog` `onConfirm(deleteFiles)` signature.
