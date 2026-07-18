# Nexus — Movie file-info box + delete-file (+ back-button hover)

Date: 2026-07-18
Status: Approved (design)

## 1. Goal

When a movie has an imported file, the movie detail page today shows nothing
about that file — only a "Downloaded" badge. The user can delete the *movie*
(which also removes the DB rows) but cannot remove just the downloaded *file*
to force a re-grab of a better release without re-adding the movie.

This feature adds, on the movie detail page:

1. A **file-info box** showing the imported file's name, quality, size, path, and
   the date it was added — rendered only when a file exists.
2. A **Delete-file** action distinct from Delete-movie: it removes the file from
   disk (best-effort) and the DB row, flipping the movie back to "Missing" so it
   can be searched again. The movie itself stays in the library.

It also folds in a small polish (request #6): **hover + focus-visible states on
the back buttons** of both the movie and TV detail pages, which are currently
plain brand-coloured text with no interaction affordance.

Covers requests #5 (movie file-info box + delete-file) and #6 (back-button hover).

## 2. Scope

**In scope:**
- An optional `file` object on the `GET /movies/{id}` response (detail only).
- A `DELETE /movies/{id}/file` endpoint backed by a new
  `media.Service.DeleteMovieFile`.
- A file-info box on `MovieDetail.tsx`, shown only when a file is present, with a
  warn-styled Delete-file button behind a `confirm()`.
- `hover:underline` + a focus-visible ring on the back buttons of both
  `MovieDetail.tsx` and `SeriesDetail.tsx`.

**Out of scope (explicit non-goals):**
- **TV / per-episode file-info boxes.** Movie-only for this pass. The same
  `MediaFile` model backs episodes, but the episode UI surface is a separate
  future slice.
- **Codec / container / audio media-info.** Nexus stores only `quality`
  (source + resolution) per file — it never parses the media itself. A
  Radarr-style media-info panel would require a new parsing pass and is out of
  scope. The box shows only what Nexus already knows.
- **Deleting the whole item folder / disk cleanup on Delete-movie.** That is the
  separate SP3 ("delete with disk cleanup"). This feature deletes only the single
  media file.
- **History event, re-search, or blocklist on delete-file.** Delete-file is a
  manual action; the user can press Search / Interactive afterward. No automatic
  follow-on.

## 3. Backend design

### 3.1 File info on `GET /movies/{id}`

`movieDTO` (`internal/media/api.go`) currently embeds `store.Movie` plus a
`hasFile bool`. It gains an **optional** `file` object:

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
```

Wire shape:

```json
{
  "id": 12, "title": "…", "hasFile": true,
  "file": {
    "relativePath": "Blade Runner 2049 (2017)/Blade.Runner.2049.2017.1080p.mkv",
    "size": 8455160320,
    "qualityId": 9,
    "quality": "Bluray-1080p",
    "addedAt": "2026-07-10T14:22:03Z"
  }
}
```

- `file` is a **pointer with `omitempty`** — when the movie has no imported file
  the key is **absent** (not `null`, not a zero object). This is the recurring
  null-not-zero wire rule: absence is the signal, and a real file always has a
  non-zero `size`/`qualityId`, so an empty object could never be mistaken for a
  real one.
- `quality` is the **resolved display name** for `qualityId`, via
  `quality.DefinitionByID(qualityId)` (adds a `quality` import to the media
  package). If the id somehow does not resolve (`ok == false`), `quality` is left
  empty — the numeric `qualityId` is still present. `hasFile` is unchanged and
  stays authoritative for the badge.

`getMovie` (`api.go:480`) already loads the movie; it additionally calls
`store.MediaFileForMovie(ctx, id)` (`import_store.go:187`, returns `*MediaFile`,
`nil` when none) and, when non-nil, populates `File`.

**`listMovies` is unchanged.** It keeps the cheap `hasFile` flag from
`MovieFileIDs` and does **not** attach a `File` object — a per-row
`MediaFileForMovie` call would be an N+1 on the library grid, and the grid never
needs the file detail. File info is a detail-page-only concern.

### 3.2 `DELETE /movies/{id}/file`

New route inside the existing `/movies` chi block (`api.go:44`):

```go
r.Delete("/{id}/file", a.deleteMovieFile)
```

The handler resolves the id (`mediaID`) and calls a new service method
`media.Service.DeleteMovieFile(ctx, movieID)`, then returns `200 {"ok": true}`
(mirroring `deleteMovie`, `api.go:502`). Errors go through `writeMediaError`.

`DeleteMovieFile` (in `internal/media/media.go`):

1. `file, err := store.MediaFileForMovie(ctx, movieID)`.
   - If `file == nil` → **200 no-op** (idempotent: deleting an already-absent
     file is success, matching the delete-file-twice case).
2. Best-effort disk removal:
   - Load the movie (`store.GetMovie`) to get `RootFolderID` (`*int64`,
     `media_store.go:338`).
   - If `RootFolderID != nil`, resolve `store.GetRootFolder(*RootFolderID)`.
     - On success, `os.Remove(filepath.Join(root.Path,
       filepath.FromSlash(file.RelativePath)))`. `RelativePath` is stored
       ToSlash (`importer.go:168`), so it is converted back to OS separators.
     - The removal is **best-effort**: any error (file already gone, permission,
       path on an unavailable mount) is **logged, never fatal**. An already-gone
       file counts as success.
   - If the root folder is `nil` or unresolvable → **skip removal**, proceed to
     the DB delete anyway (the DB is the source of truth for "has a file").
3. `store.DeleteMediaFile(file.ID)` **always** runs (`import_store.go:203`,
   deletes the DB row). On success the movie now reports `hasFile: false`.
4. Return `200`.

**Design decision (user-locked): best-effort delete.** Always remove the DB row
and flip the movie to Missing; ignore file-removal errors; already-gone counts as
success. This mirrors the importer's own `_ = os.Remove` (`importer.go:175`) and
Radarr's behaviour, and lines up with SP3 (delete-with-disk-cleanup) later. The
UI must never be left showing a file that the DB no longer tracks just because a
network mount hiccuped.

No history event, no re-search, no blocklist — a manual action, per §2.

## 4. Frontend design

### 4.1 `Movie` type

`web/src/features/library/types.ts` `Movie` (line 22) gains:

```ts
file?: {
  relativePath: string
  size: number
  qualityId: number
  quality: string
  addedAt: string
} | null
```

Optional/nullable to match the omitempty wire contract (absent on the grid's
list rows and on file-less movies; present only on a detail fetch of a movie with
a file).

### 4.2 File-info box on `MovieDetail.tsx`

Rendered **below** the `DetailBanner`, only when `m.file` is present:

- **Filename** — basename of `relativePath` (split on `/`, last segment).
- **Quality** — `file.quality` (falls back to nothing if empty).
- **Size** — human-readable (e.g. "7.87 GB"). Reuse an existing `formatSize`
  helper if one exists in the search/activity features; otherwise add a small
  local helper. (Implementation note for the plan: confirm which helper exists
  and is exported before duplicating.)
- **Relative path** — the full `relativePath`, muted.
- **Added date** — `addedAt`, formatted like other dates in the app.
- A **Delete-file** button, warn-styled (matching the existing Delete-movie
  button at `MovieDetail.tsx:93`), behind a `confirm()` (matching the
  Delete-movie confirm at `MovieDetail.tsx:89`) → calls a new
  `useDeleteMovieFile` hook.

### 4.3 `useDeleteMovieFile` hook

New hook in `web/src/features/library/api.ts`, mirroring `useDelete`
(`api.ts:101`):

```ts
export function useDeleteMovieFile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/movies/${id}/file`),
    onSuccess: (_d, id) => qc.invalidateQueries({ queryKey: libraryKeys.movie(id) }),
  })
}
```

On success it invalidates the movie-detail query (`libraryKeys.movie(id)`) so the
banner badge flips to Missing and the file box disappears, and the caller shows a
toast "File deleted".

### 4.4 Back-button hover / focus (request #6)

Both detail pages render their back control as
`className="text-sm text-[var(--color-brand)]"` with **no** hover or focus state:

- `MovieDetail.tsx:44` (banner `back={…}`) and its not-found fallback at
  `MovieDetail.tsx:31`.
- `SeriesDetail.tsx:47` (banner `back={…}`) and its not-found fallback at
  `SeriesDetail.tsx:34`.

Add `hover:underline` and a focus-visible ring (e.g.
`focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-brand)]
rounded`, matching the app's existing focus-ring convention — the plan confirms
the exact utility). The banner back buttons are the primary target; the not-found
fallbacks get the same treatment for consistency.

This is a pure CSS/className change — visual only, not unit-tested (§5).

## 5. Testing

**Go**
- `getMovie` includes a fully-populated `file` (with the **resolved quality
  name**) when the movie has a file, and **omits** the `file` key entirely when it
  does not — assert absence via `map[string]json.RawMessage` (the omitempty
  wire-shape guard; a typed-struct round-trip cannot distinguish absent from a
  zero object).
- `DeleteMovieFile` removes the DB row **and** a real temp file on disk (create a
  root folder + a real file under it, assert the file is gone and
  `MediaFileForMovie` returns nil afterward).
- `DeleteMovieFile` is idempotent with no file present (returns success, no
  panic).
- `DeleteMovieFile` still removes the DB row when the root folder is unresolvable
  (nil `RootFolderID` or a since-deleted root) — the disk step is skipped, the row
  still goes.

**Frontend**
- The file box renders name / quality / size / path / date when `m.file` is
  present, and is absent when `m.file` is missing.
- Delete-file (through the `confirm()`) hits `DELETE /movies/{id}/file` and
  invalidates the movie-detail query.
- `web/dist` rebuild committed (CI drift-checks it).

**Not tested**
- Back-button hover/focus (#6) — CSS-only, visual.

## 6. Source facts (verified 2026-07-18)

- `store.MediaFile{ID, MediaKind, EpisodeID, MovieID, RelativePath, Size,
  QualityID, AddedAt}` — `import_store.go:133`.
- `store.MediaFileForMovie(ctx, id) (*MediaFile, error)` — returns `nil` (no
  error) when none — `import_store.go:187`.
- `store.DeleteMediaFile(ctx, id)` — deletes the DB row only (`ErrNotFound` if the
  row is gone) — `import_store.go:203`.
- `quality.DefinitionByID(id) (QualityDefinition{ID, Name, …}, bool)` —
  `definitions.go:56`.
- `store.Movie.RootFolderID *int64` — `media_store.go:338`;
  `store.GetRootFolder(ctx, id) (*RootFolder, error)` — `media_store.go:28`.
- `movieDTO{store.Movie; HasFile bool}`, `getMovie` at `api.go:480`,
  `deleteMovie` at `api.go:502`, `/movies` chi block at `api.go:44` (chi router —
  `r.Delete("/{id}/file", …)` is a clean addition).
- `RelativePath` is stored ToSlash — `importer.go:168`; importer's best-effort
  `_ = os.Remove` precedent — `importer.go:175`.
- FE `useDelete` + `confirm()` + warn-button pattern — `api.ts:101`,
  `MovieDetail.tsx:88-96`; `libraryKeys.movie(id)` query key already used
  throughout `MovieDetail.tsx`.

## 7. Deferred

- TV / per-episode file-info boxes and delete-file (same model, separate surface).
- Full media-info (codec/container/audio) — needs a parsing pass; not tracked.
- Disk cleanup on Delete-movie (SP3).
- Re-search / blocklist follow-on after delete-file (manual action by design).
