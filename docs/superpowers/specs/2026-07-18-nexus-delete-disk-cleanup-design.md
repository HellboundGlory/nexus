# Nexus — Delete with disk cleanup (opt-in "Delete files from disk")

Date: 2026-07-18
Status: Approved (design)

## 1. Goal

Deleting a movie or series today removes it from the library (DB rows cascade
away) but always leaves the downloaded files on disk. This feature adds an
**opt-in** "Delete files from disk" checkbox to the delete confirmation, so a
user can remove the item *and* its files in one action — the Radarr/Sonarr
pattern. The checkbox **defaults to off**: the DB rows are always removed, and
the on-disk folder is removed only when the box is checked. Default-off guards a
misclick against a network media mount.

Covers request #4.

## 2. Scope

**In scope:**
- An optional `?deleteFiles=true` query param on the existing
  `DELETE /movies/{id}` and `DELETE /series/{id}` endpoints.
- Route both delete handlers through new `media.Service.DeleteMovie` /
  `DeleteSeries` methods that own the disk-cleanup logic.
- Best-effort removal of the item's **whole folder** on disk (`os.RemoveAll`)
  when `deleteFiles` is set, guarded so it can never escape or delete the root.
- A new `store.MediaFilesForSeries` query to gather a series' episode files.
- A reusable `DeleteConfirmDialog` frontend component (checkbox default off),
  replacing the native `confirm()` on both the movie and series detail pages.

**Out of scope (explicit non-goals):**
- **Grid / list-level delete.** Delete stays a detail-page action, as today.
  Neither the Movies nor TV grid gets a delete affordance.
- **Computing a folder from the naming config when no file exists.** Nexus stores
  only `RootFolderID` + each file's `relativePath` (no per-item folder path like
  Radarr's `Path`), so the folder is derived from a tracked file. An item with no
  imported file has nothing to derive from → disk deletion is skipped (DB rows
  still removed). We do not reconstruct the expected folder from naming rules.
- **Precise per-file deletion / partial cleanup.** When the box is checked we
  remove the item's whole folder (subtitles, `.nfo`, artwork included), matching
  Radarr/Sonarr — not just the tracked media files.
- **Changing the default-off behavior or removing the checkbox.** The DB-only
  path is the default and must stay backward compatible.

## 3. Backend design

### 3.1 Wire

Both existing endpoints gain an optional query param:

```
DELETE /api/v1/movies/{id}?deleteFiles=true
DELETE /api/v1/series/{id}?deleteFiles=true
```

`deleteFiles` is read as `r.URL.Query().Get("deleteFiles") == "true"`. **Absent or
any other value = `false`** = today's DB-only delete. This is purely additive —
no existing caller passes the param, so every current delete keeps its behavior.

### 3.2 Route through the Service

Today `deleteMovie` (`api.go:532`) and `deleteSeries` (`api.go:320`) call
`store.DeleteMovie` / `store.DeleteSeries` **directly**. This feature introduces:

```go
func (s *Service) DeleteMovie(ctx context.Context, id int64, deleteFiles bool) error
func (s *Service) DeleteSeries(ctx context.Context, id int64, deleteFiles bool) error
```

The handlers parse the param and call these instead. **No `MovieUpdated` /
`SeriesUpdated` event is emitted** — the item is gone, so there is nothing to
notify a live view about (the frontend already invalidates the *list* query on
delete success). This matches the current direct-delete behavior.

### 3.3 Delete flow (ordering is load-bearing)

For both movie and series, when `deleteFiles` is true:

1. **Gather folder targets BEFORE the DB delete.** The FK cascade wipes
   `media_files`, so the file paths must be read first.
   - Movie: `store.MediaFileForMovie(id)` → 0 or 1 file.
   - Series: `store.MediaFilesForSeries(id)` (new, §3.5) → N episode files.
   - Resolve the item's root once via its `RootFolderID` → `store.GetRootFolder`.
     If `RootFolderID` is nil or the root does not resolve, **skip disk cleanup**
     (proceed to the DB delete).
   - Derive the set of distinct folder targets (§3.4).
   - If gathering the files or resolving the root **errors** (a DB read failure),
     log it and **skip disk cleanup** — still proceed to the DB delete. The disk
     step is best-effort and must never block removing the item from the library.
2. **DB delete** via the existing `store.DeleteMovie` / `store.DeleteSeries`
   (unchanged FK cascade). This **always** runs and is the operation whose error
   (if any) fails the request.
3. **Best-effort `os.RemoveAll`** each derived folder target. Errors are
   `slog.Warn`-logged and **never fatal** (same discipline as SP2's
   `DeleteMovieFile`). A folder already gone counts as success.

When `deleteFiles` is false, steps 1 and 3 are skipped entirely — identical to
today.

### 3.4 Folder derivation + containment guard (the crux)

`relativePath` is stored ToSlash (`importer.go:168`) and looks like
`"Blade Runner 2049 (2017)/BR.2049.1080p.mkv"` for movies or
`"The Wire/Season 01/S01E01.mkv"` for series. The **item folder** is the first
path segment. A shared helper computes the safe absolute folder to remove:

```
segment := first element of strings.Split(filepath.ToSlash(relPath), "/")
if segment is "" or "." or ".." → return "", skip
abs := filepath.Join(root.Path, segment)
rel, err := filepath.Rel(root.Path, abs)
if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) → return "", skip
return abs
```

This guarantees the target is a **direct child of the root** and can never be the
root itself, a parent of the root, or an escape via `..`. Only a passing target is
`os.RemoveAll`-ed. For a series the derivation runs per gathered file and the
**distinct** targets are removed (normally one series folder; robust if files span
folders). SP2's whole-branch review flagged exactly this containment check for
SP3 — it is load-bearing here because the folder-level blast radius is larger than
SP2's single-file delete.

Edge cases the guard handles safely: empty `relativePath` → skip (never nukes
root); a file directly in the root (`"movie.mkv"`, no subfolder) → target is that
file, `RemoveAll` removes just it; a malicious `"../../etc/x.mkv"` → first segment
`".."` → skip.

### 3.5 New store query

```go
func (s *Store) MediaFilesForSeries(ctx context.Context, seriesID int64) ([]MediaFile, error)
```

Returns every media file belonging to a series' episodes, via a join:
`SELECT ... FROM media_files mf JOIN episodes e ON mf.episode_id = e.id WHERE e.series_id = ?`.
Used only to gather folder targets for series disk cleanup. (Movies already have
`MediaFileForMovie`.)

## 4. Frontend design

### 4.1 `DeleteConfirmDialog` (new, reusable)

The native `confirm()` cannot host a checkbox, so both detail pages switch to a
new component `web/src/features/library/DeleteConfirmDialog.tsx`, built on the
existing `Dialog` (`components/ui/dialog.tsx`, props `open`/`onOpenChange`/
`children`/`className`) + `DialogTitle`:

- Title: `Delete {title}?`
- A **"Delete files from disk" checkbox, default unchecked**, with a short
  caption (e.g. "Also remove the folder and its files from disk").
- **Cancel** and a warn-styled **Delete** button.
- Props: `open`, `onOpenChange(open)`, `title`, and `onConfirm(deleteFiles: boolean)`.
  The checkbox state resets to off each time the dialog opens.

### 4.2 `useDelete` gains `deleteFiles`

`useDelete` (`api.ts:101`) currently sends `DELETE /{kind}/{id}`. It gains a
`deleteFiles?: boolean` field on its mutation input and appends
`?deleteFiles=true` when set (omitted otherwise, so the wire stays clean for the
default path). `onSuccess` still invalidates the movies/series list query as today.

### 4.3 Detail pages

`MovieDetail.tsx` (delete button at :91) and `SeriesDetail.tsx` (delete at :70)
replace their inline `confirm()` with local dialog state: the Delete button opens
`DeleteConfirmDialog`; `onConfirm(deleteFiles)` fires
`del.mutate({ kind, id, deleteFiles }, { onSuccess: () => { toast("Deleted"); nav(...) } })`.
The toast/navigation behavior is unchanged.

## 5. Error handling

| Case | Behavior |
|------|----------|
| `deleteFiles` absent / not "true" | DB-only delete, exactly as today |
| `deleteFiles=true`, item has a file | folder derived + `os.RemoveAll` (best-effort) + DB delete |
| `deleteFiles=true`, item has no file | disk skipped, DB rows still deleted |
| `deleteFiles=true`, `RootFolderID` nil / root unresolvable | disk skipped, DB rows still deleted |
| derived segment empty / `.` / `..` / escapes root | that target skipped (never removed); other valid targets still processed |
| gathering files / resolving root errors (DB read fail) | logged, disk cleanup skipped; DB delete still runs |
| `os.RemoveAll` errors (permission, unavailable mount) | `slog.Warn`-logged, non-fatal; DB delete already succeeded |
| `store.DeleteMovie`/`DeleteSeries` errors | 500, request fails (as today) — disk step for that item does not run |

## 6. Testing

**Go**
- `Service.DeleteMovie(deleteFiles=true)` removes a **real temp folder** on disk
  (create root + `"Title (Year)/file.mkv"` under it) and the DB rows; the folder
  is gone and `GetMovie` returns `ErrNotFound` after.
- `Service.DeleteMovie(deleteFiles=false)` deletes the DB rows but **leaves the
  folder** on disk (regression guard for the default path).
- `deleteFiles=true` with **no file** → DB rows deleted, no panic, nothing removed.
- Containment guard: a media file whose `relativePath` is `"../../evil/x.mkv"`
  (or empty) → the root folder and its parent are **untouched**; DB rows still
  deleted. (Assert a sentinel file under/above root survives.)
- `Service.DeleteSeries(deleteFiles=true)` removes the series folder (seed a
  series + episodes + episode files under one folder) and all DB rows;
  `MediaFilesForSeries` returns the seeded files.
- The `DELETE /movies/{id}?deleteFiles=true` and `/series/{id}?deleteFiles=true`
  handlers parse the param and trigger disk removal; without it, DB-only.

**Frontend**
- `DeleteConfirmDialog`: renders the checkbox **unchecked by default**; confirming
  with it unchecked calls `onConfirm(false)`, checked calls `onConfirm(true)`;
  reopening resets the checkbox to off.
- `useDelete` appends `?deleteFiles=true` only when `deleteFiles` is true.
- MovieDetail and SeriesDetail open the dialog (not native `confirm`) and the
  delete mutation carries the chosen `deleteFiles` value.
- `web/dist` rebuild committed (CI drift-checks it).

## 7. Source facts (verified 2026-07-18)

- `deleteMovie` (`api.go:532`) / `deleteSeries` (`api.go:320`) call
  `store.DeleteMovie` / `store.DeleteSeries` directly today; `/movies` + `/series`
  are chi route blocks (`api.go:44` / `:32`).
- `store.DeleteMovie` (`media_store.go:443`) / `DeleteSeries` (`:189`) delete the
  row; FK `ON DELETE CASCADE` (migration `0006_import.sql`) removes
  episodes/seasons/media_files/blocklist; `history` is `ON DELETE SET NULL`.
- `store.Movie.RootFolderID *int64` (`media_store.go:338`),
  `store.Series.RootFolderID *int64` (`:90`); `store.GetRootFolder(id)`
  (`:28`) resolves `{id, path}`.
- `store.MediaFileForMovie` (`import_store.go:187`) → `*MediaFile` (nil if none);
  `store.MediaFile.RelativePath` stored ToSlash (`importer.go:168`); importer's
  best-effort `_ = os.Remove` precedent (`importer.go:175`); SP2's `slog.Warn`
  best-effort discipline in `media.Service.DeleteMovieFile` (`media.go:427`).
- FE `useDelete` (`api.ts:101`) sends `DELETE /{kind}/{id}` + invalidates the list;
  `confirm()` delete in `MovieDetail.tsx:91` / `SeriesDetail.tsx:70`; `Dialog`
  component `components/ui/dialog.tsx` (props `open`/`onOpenChange`/`children`/
  `className`, `DialogTitle`); `Dialog` precedent in `AddMediaDialog.tsx`.

## 8. Deferred

- Grid/list-level delete affordance.
- Naming-config-derived folder for file-less items.
- Per-file selective deletion / keeping non-imported files.
