# Nexus Import & Rename (Sub-project 4c) Design

**Status:** Approved (brainstorm complete). Ready for implementation plan.
**Depends on:** 4a (media library), 4b (parsing & quality), sub-3 (download clients).

## 1. Goal

Give Nexus the **import pipeline**: detect a completed download, attribute it to
the exact library item it was grabbed for, decide accept/reject/upgrade against
the item's quality profile, rename it via editable templates, hardlink (fallback
copy) it into the item's root folder, track the resulting file, and record
history. This is the file-handling half of "media management"; the release
*selection*/search that feeds it is sub-5 (automation).

## 2. Settled decisions (from brainstorming)

1. **Attribution = grab-tracking queue.** A `download_queue` row is written when a
   release is grabbed *for* a media item (clientItemId → series/movie + episode
   list + resolved quality). On completion the importer looks up that row and
   imports to those exact items. This closes the gap sub-3 left (no grab→item
   linkage existed). Parse-match fallback for untracked downloads is **out of scope**.
2. **Renaming = small editable token templates.** A focused token vocabulary with
   sensible default templates stored in settings and editable via API — not *arr's
   full format-specifier engine.
3. **File placement = hardlink, fallback to copy.** `os.Link`; on cross-device/FS
   failure, copy. The original in the download dir is left in place (seeding-safe).
4. **Trigger = automatic + manual.** A scheduled `ImportCompleted` command (~1 min,
   same-instance factory like the queue monitor) auto-imports completed tracked
   items; `POST /queue/{id}/import` forces/retries one.
5. **Upgrades = upgrade-if-better, else reject.** When the target already has a
   file, use 4b's profile-ranked comparison; replace + delete old only if the new
   file outranks it and the profile allows upgrades and the cutoff isn't met;
   otherwise reject and keep the existing file. Outcome recorded in history.
6. **Multi-file = single + season packs, skip junk.** Walk the output path for
   video files (extension allowlist), drop samples (name + min-size threshold) and
   non-video files; a single video imports as movie/episode; a season pack parses
   each video file and imports each to the matching episode recorded in the row.

## 3. Architecture

New feature package **`internal/importing`** owns the download-tracking queue, the
enqueue-for-media action, and the import pipeline. Its dependencies obey the
established module-boundary rule (feature packages never import each other; they
communicate through `internal/core/*` and injected `provider` interfaces):

- Imports **only** `internal/core/*` + `internal/parsing` + `internal/quality` +
  `internal/core/provider`. (`parsing`/`quality` are the pure decision layer, the
  same way `quality` imports `parsing`.)
- **No** import of `internal/downloadclient`, `internal/media`, `internal/indexer`,
  or `internal/automation`.
- Reaches the download clients via two **consumer-defined interfaces** (declared in
  `internal/importing`, satisfied by sub-3's `downloadclient.Service`, wired at the
  composition root):
  - `Grabber{ Grab(ctx, provider.DownloadRequest, clientID string) (itemID string, err error) }`
  - `QueueReader{ Queue(ctx) []provider.DownloadItem; Remove(ctx, clientID, itemID string, deleteData bool) error }`
- Reads/writes library rows (series/episodes/movies/`media_files`/`history`)
  **directly via `store`** (that is `core`, allowed) — never through `internal/media`.

Rejected alternatives: (a) fold the importer into `internal/media` — breaks the
`media`↛`quality` boundary (import needs `quality`); (b) split enqueue into
`downloadclient` and import into a new package — spreads one lifecycle across two
packages and still needs media/quality access.

Required sub-3 change (small): add `OutputPath string` to `provider.DownloadItem`
and populate it — SAB from the history `storage` field, qBit from `content_path`.
Without it the importer cannot locate completed files.

## 4. Data model — migration `0006_import.sql`

### `download_queue` (grab-tracking rows)
```sql
CREATE TABLE download_queue (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    download_client_id TEXT    NOT NULL,
    client_item_id     TEXT    NOT NULL,          -- SAB nzo_id / qBit hash
    protocol           TEXT    NOT NULL,
    source_title       TEXT    NOT NULL,          -- release name (parse + history)
    media_kind         TEXT    NOT NULL,          -- 'tv' | 'movie'
    series_id          INTEGER REFERENCES series(id) ON DELETE CASCADE,
    movie_id           INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    episode_ids        TEXT    NOT NULL DEFAULT '[]',  -- JSON []int64
    quality_id         INTEGER NOT NULL,          -- resolved quality at grab time
    status             TEXT    NOT NULL,          -- grabbed|importing|imported|failed
    error              TEXT    NOT NULL DEFAULT '',
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(download_client_id, client_item_id)
);
```

### `media_files` (one row per imported file; "hasFile" is derived)
```sql
CREATE TABLE media_files (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    media_kind    TEXT    NOT NULL,               -- 'tv' | 'movie'
    episode_id    INTEGER REFERENCES episodes(id) ON DELETE CASCADE,
    movie_id      INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    relative_path TEXT    NOT NULL,               -- path under the root folder
    size          INTEGER NOT NULL,
    quality_id    INTEGER NOT NULL,               -- for future upgrade comparisons
    added_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(episode_id),
    UNIQUE(movie_id)
);
```
`UNIQUE(episode_id)`/`UNIQUE(movie_id)` enforce "one current file"; an upgrade
deletes the old row + physical file and inserts the new. No column change to 4a's
series/episodes/movies tables — "hasFile" derives from this table.

### `history` (append-only event log)
```sql
CREATE TABLE history (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type   TEXT    NOT NULL,   -- grabbed|imported|upgraded|import_failed|deleted
    media_kind   TEXT    NOT NULL,
    series_id    INTEGER REFERENCES series(id)  ON DELETE SET NULL,
    episode_id   INTEGER REFERENCES episodes(id) ON DELETE SET NULL,
    movie_id     INTEGER REFERENCES movies(id)  ON DELETE SET NULL,
    source_title TEXT    NOT NULL DEFAULT '',
    quality_id   INTEGER,
    message      TEXT    NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

Naming templates live in the existing `settings` key/value table under `naming.*`
keys (no new table), editable via the API.

## 5. Components (all in `internal/importing` unless noted)

1. **`provider.DownloadItem.OutputPath`** (sub-3): new field; SAB from history
   `storage`, qBit from `content_path`. Plus the `Grabber` / `QueueReader`
   consumer interfaces.
2. **`internal/core/store/import_store.go`**: `download_queue`, `media_files`,
   `history` access — `EnqueueGrab`, `ListQueue`, `QueueByStatus`, `GetQueueItem`,
   `SetQueueStatus`, `DeleteQueueItem`, `UpsertMediaFile`, `MediaFileForEpisode`,
   `MediaFileForMovie`, `DeleteMediaFile`, `AddHistory`, `ListHistory`. Reuses
   `boolToInt`, sentinel `ErrNotFound`; JSON for `episode_ids`.
3. **`naming.go`** — pure `Render(template string, tok Tokens) string` +
   `Sanitize(name string) string`. Tokens: `{Series Title}`, `{season:00}`,
   `{episode:00}`, `{Episode Title}`, `{Quality}`, `{Movie Title}`, `{year}`,
   `{Release Group}`. Default templates:
   - series folder: `{Series Title}`
   - season folder: `Season {season:00}`
   - episode file: `{Series Title} - S{season:00}E{episode:00} - {Episode Title} [{Quality}]`
   - movie folder: `{Movie Title} ({year})`
   - movie file: `{Movie Title} ({year}) [{Quality}]`
   Loaded/saved via settings; a `Config` struct with these five templates.
4. **`quality.IsUpgrade(newID, existingID int, profile store.QualityProfile) bool`**
   (added to `internal/quality`) — pure: true iff `newID` outranks `existingID` in
   the profile order, `profile.UpgradeAllowed`, and the existing quality is below
   the profile cutoff. Imports only `parsing`+`core/*` (boundary preserved).
5. **`Service`** — the pipeline, two entry points:
   - `Enqueue(ctx, EnqueueRequest)`: run `quality.Decide` (reject early if the
     release's quality isn't allowed by the profile), `Grabber.Grab`, write the
     `download_queue` row (`grabbed`) + a `grabbed` history row.
   - `ImportItem(ctx, queueID)` and `ImportCompleted(ctx)`: the import algorithm
     (§6) for a specific row / all `grabbed` rows whose client item is `Completed`.
6. **`ImportCompleted` command** — scheduled ~1 min (same-instance factory), emits
   `import.completed` and `queue.updated` events.
7. **`api.go`** (mounted in authed `/api/v1`): `GET /queue`, `POST /queue`
   (enqueue), `DELETE /queue/{id}`, `POST /queue/{id}/import`, `GET /history`,
   `GET|PUT /config/naming`.
8. **Wiring** (`cmd/nexus/main.go`): construct `importing.Service` with `store` +
   `parsing`/`quality` + the download-client service (as `Grabber`+`QueueReader`);
   mount the API; register the scheduled import; add `import.completed` and
   `queue.updated` to `WSForward`.

## 6. Import algorithm (per completed queue row)

```
mark row 'importing'
load target series/movie -> root folder path + quality profile
walk OutputPath -> candidate video files (ext allowlist: .mkv/.mp4/.avi/.m4v/.ts/.wmv/.mov)
    drop names containing 'sample'; drop files < ~50 MB; ignore .nfo/.srt/.sub/.txt/.jpg/etc
for each candidate video file:
    parsed  = parsing.Parse(basename, kind)
    newQ    = quality.Resolve(parsed)          (fallback to row.quality_id if Unknown)
    target  = the movie, OR the episode in row.episode_ids matching parsed S/E
    if target not resolvable: record skip; continue
    existing = store.MediaFileFor{Episode,Movie}(target)
    if existing != nil:
        if !quality.IsUpgrade(newQ.ID, existing.QualityID, profile):
            history 'import_failed' (message: not an upgrade); continue
        plan replace = existing (delete old file + row after successful place)
    dst = rootFolder / render(folderTemplate, tok) [ / render(seasonTemplate, tok) ]
                     / render(fileTemplate, tok) + originalExt
    ensure dst parent dirs; sanitize each path segment
    if dst exists and is not our tracked file -> mark row 'failed' (conflict); stop
    hardlink(src, dst); on error copy(src, dst)
    UpsertMediaFile(target, relpath, size, newQ.ID)
    if plan replace: delete old physical file + old media_files row
    history 'imported' (or 'upgraded' when replacing)
if every targeted episode / the movie now has a file:
    status 'imported'; optionally QueueReader.Remove(clientID, itemID, deleteData=false)
else:
    status 'failed'; set error; history 'import_failed'
emit import.completed + queue.updated
```

Import is **per-file best-effort within a row**; a manual `POST /queue/{id}/import`
retries a `failed` row and is idempotent (a hardlink whose dst already exists and
is our tracked file is treated as success, not duplicated).

## 7. Error handling

- **Filesystem:** hardlink→copy fallback; parent dirs created; each path segment
  sanitized (strip `<>:"/\|?*` and control chars, trim trailing dots/spaces);
  never blind-overwrite — a dst that exists and isn't our tracked file is a
  conflict → row `failed`.
- **Attribution gaps:** a completed client item with no queue row is skipped
  silently (not ours; parse-fallback is out of scope). A season-pack video file
  matching no episode in the row is logged and skipped; if any targeted episode is
  left unimported the row ends `failed`.
- **Deletes:** removing a series/movie cascades `download_queue` + `media_files`
  rows via FKs and nulls `history` FKs; physical files are left on disk in 4c (a
  "delete files" action is deferred).
- **Enqueue rejection:** if `quality.Decide` rejects the release for the profile,
  `Enqueue` returns a typed error (→400) and writes nothing.

## 8. Acceptance criteria

1. `provider.DownloadItem` carries `OutputPath`, populated by SAB + qBit clients.
2. `Enqueue` runs `Decide`, grabs via the injected `Grabber`, and writes a
   `download_queue` row + `grabbed` history; a profile-rejected release → error,
   no row.
3. `naming.Render`/`Sanitize` produce the templated paths for the default
   templates across a representative token set; illegal characters are stripped.
4. `quality.IsUpgrade` honors profile order + `upgradeAllowed` + cutoff.
5. Store CRUD for `download_queue`/`media_files`/`history` round-trips; the
   `UNIQUE(episode_id)`/`UNIQUE(movie_id)` upgrade-replace path works.
6. `ImportCompleted` imports a single-file movie/episode and a season pack into the
   correct rendered paths (hardlink, copy fallback exercised), writes `media_files`
   + `history`, skips samples/junk, replaces on upgrade, rejects a non-upgrade, and
   ends rows `imported`/`failed` correctly.
7. REST: enqueue/list/delete/import/history/naming-config behave and map errors
   (400/404/409/500) via `api.WriteError`.
8. `cmd/nexus` mounts `/api/v1/queue`, `/api/v1/history`, `/api/v1/config/naming`
   and registers the scheduled import; `import.completed`/`queue.updated` reach WS.
9. `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./... -count=1` green.
10. Boundaries hold: `internal/importing` → `internal/core/*` + `internal/parsing`
    + `internal/quality` only (no `downloadclient`/`media`/`indexer`/`automation`);
    `internal/quality` still → `internal/parsing` + `internal/core/*` only.

## 9. Out of scope (later slices / sub-projects)

- Parse-match import of **untracked** downloads (no queue row).
- Physical file **deletion** (delete-item-with-files); 4c leaves files on disk.
- Subtitle / extra / metadata (.nfo) import; artwork.
- The search that **chooses** releases and calls `Enqueue` (sub-5 automation);
  RSS/wanted-missing.
- Manual "interactive import" file-picker UI (web is sub-6); 4c ships the API.
- Per-quality size limits, custom formats (deferred with 4b).

## 10. Notes & deviations

- **Reference:** *arr's `DownloadedEpisodesImportService` / `DownloadedMovieImportService`,
  `UpgradeMediaFileService`, and `FileNameBuilder` inform *which* behaviors matter
  (sample rejection, upgrade-then-delete-old, template tokens) — do **not** transcribe
  their C# structure. Nexus keeps one focused importer.
- **Credentials:** none new. Grab reuses sub-3's server-side fetch (indexer apikey
  stays server-side); no secrets in `download_queue`/`media_files`/`history`.
- **`OutputPath` privacy:** it is a local filesystem path, not a secret; safe to
  surface in the queue API.
- **Season/episode matching** uses the episodes recorded in `episode_ids` at grab
  time as the candidate set, intersected with each file's parsed S/E — so a pack
  that over-delivers (extra episodes) imports only what was grabbed/monitored.
