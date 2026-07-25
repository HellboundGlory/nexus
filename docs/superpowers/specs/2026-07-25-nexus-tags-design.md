# Nexus Tags Subsystem (SP-2) — Design

Date: 2026-07-25
Status: Approved
Sub-project: SP-2 of the SP-2 → SP-3 → SP-C batch

## 1. Purpose and scope

Nexus has no tags. SP-3 (Release Profiles) needs them: a release profile is a
reusable named rule scoped by tag, exactly as Sonarr places it at
Settings → Profiles → Release Profiles. Without tags there is nothing to scope a
profile to.

SP-2 delivers the tag subsystem and nothing else. It changes no automation
behaviour: after SP-2 ships, tags exist, can be created and applied, and are
read by nobody. That is intentional — SP-3 is the first consumer.

### 1.1 In scope

- A `tags` table plus `series_tags` and `movie_tags` junction tables.
- Store CRUD for tags, per-entity read/replace of tag sets, and a batch reader
  returning the whole library's associations in one query.
- A `tag` API package: list / create / rename / delete.
- Tag assignment endpoints on the existing media routes.
- A `TagInput` UI component, a Tags row on the Series and Movie detail pages,
  and a Settings → Tags page.

### 1.2 Explicitly out of scope

- Tag colours. Sonarr's `Tag` is `{Id, Label}` only; colour is not a thing.
- Auto-tagging rules (Sonarr's `AutoTagging`).
- Tag chips on library cards, and filtering the library by tag.
- Bulk tag editing from the library grid.
- Tagging entities other than series and movies. Sonarr additionally tags
  indexers, download clients, notifications, delay profiles and import lists.
  None of those are needed by SP-3, and adding them now would multiply the
  surface for no gain.
- Any `TagsUpdatedEvent` equivalent. Nexus has `events.Bus` and `WSForward`, but
  nothing needs a live tag push.

### 1.3 Why movies are included

The user confirmed movies must be tagged from the start, because Radarr has
release profiles for movies. A series-only tag system would need reopening
immediately in SP-3.

## 2. Data model

Migration `internal/core/database/migrations/0010_tags.sql`, following the style
of `0009_series_aliases.sql`.

```sql
CREATE TABLE tags (
  id    INTEGER PRIMARY KEY,
  label TEXT NOT NULL,
  UNIQUE(label COLLATE NOCASE)
);

CREATE TABLE series_tags (
  series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  tag_id    INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
  PRIMARY KEY(series_id, tag_id)
);

CREATE TABLE movie_tags (
  movie_id INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
  tag_id   INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
  PRIMARY KEY(movie_id, tag_id)
);

CREATE INDEX idx_series_tags_tag ON series_tags(tag_id);
CREATE INDEX idx_movie_tags_tag  ON movie_tags(tag_id);
```

### 2.1 Two junction tables, not one polymorphic table

SP-3's query is "given this series, which release profiles apply?" — that is
`series → series_tags → profile_tags`, a plain join. A polymorphic
`entity_type`/`entity_id` table buys extensibility only to entity kinds that
§1.2 rules out, at the cost of an unindexable discriminator on every join.

### 2.2 Junction tables, not a JSON column on the entity

Sonarr stores an int list on the entity row. Rejected here for two reasons:
SP-3 would have to scan and decode every row instead of joining, and every
existing `seriesSelect` / `movieSelect` column list and `scanSeriesRow` /
`scanMovieRow` implementation would need editing — a read-path edit in a
codebase where duplicated read paths have already caused bugs.

### 2.3 Cascade is real here

`internal/core/database/database.go:17` opens the DSN with
`_pragma=foreign_keys(ON)`, so `ON DELETE CASCADE` is honoured. Deleting a
series or a movie removes its junction rows with no explicit cleanup code.
Deleting a tag likewise cascades — but §4.3 refuses that delete before it gets
there.

### 2.4 Label uniqueness

`UNIQUE(label COLLATE NOCASE)` plus a `strings.TrimSpace` on write. `HD` and
`hd` cannot both exist, and a label cannot be empty after trimming.

## 3. Store surface

New file `internal/core/store/tag_store.go`.

```go
type Tag struct {
    ID          int64  `json:"id"`
    Label       string `json:"label"`
    SeriesCount int    `json:"seriesCount"`
    MovieCount  int    `json:"movieCount"`
}

ListTags(ctx) ([]Tag, error)                        // counts populated
CreateTag(ctx, label string) (Tag, error)           // ErrTagExists
RenameTag(ctx, id int64, label string) error        // ErrTagExists, ErrNotFound
DeleteTag(ctx, id int64) error                      // ErrTagInUse, ErrNotFound

TagsForSeries(ctx, seriesID int64) ([]int64, error)
SetSeriesTags(ctx, seriesID int64, tagIDs []int64) error
SeriesTagIDs(ctx) (map[int64][]int64, error)        // batch, whole library

TagsForMovie(ctx, movieID int64) ([]int64, error)
SetMovieTags(ctx, movieID int64, tagIDs []int64) error
MovieTagIDs(ctx) (map[int64][]int64, error)
```

New sentinel errors `ErrTagExists`, `ErrTagInUse` and `ErrTagNotFound`,
mirroring the existing `ErrProfileInUse` in the quality store.

`ErrTagInUse` is a struct error carrying `SeriesCount` and `MovieCount`, so the
API can name them in the 409 message (§4.3) without a second query. It is
matched with `errors.As`, and also satisfies `errors.Is(err, ErrTagInUse)` via
an `Is` method so call sites can use either form.

`CreateTag` returns a `Tag` whose `SeriesCount` and `MovieCount` are zero by
construction — a newly created tag has no associations. Only `ListTags`
populates the counts from the database.

Labels are trimmed with `strings.TrimSpace` before any read or write, and an
empty result is rejected with `ErrInvalidTag`. Validation lives in the store,
not the API, so every caller gets it.

### 3.1 Set semantics

`SetSeriesTags` / `SetMovieTags` are **replace-set**, run in a single
transaction: delete the entity's existing rows, insert the given ids. Passing an
empty or nil slice clears all tags for that entity.

An unknown tag id returns the sentinel **`ErrTagNotFound`** and rolls the
transaction back, so no partial write lands. The store checks the ids
explicitly inside the transaction rather than relying on the foreign-key
violation, because a raw FK error is indistinguishable from any other driver
error at the API layer and could not be mapped to a 400 (§4.2) without string
matching. Duplicate ids in the input slice are deduplicated, not an error.

`ListTags` returns an empty slice, never nil, so the API emits `[]`.
`TagsForSeries` likewise. `SeriesTagIDs` returns an empty map for an empty
library, and omits entities with no tags rather than mapping them to an empty
slice.

### 3.2 The batch reader is deliberate scope, even though SP-2 does not call it

`SeriesTagIDs` and `MovieTagIDs` have no caller in SP-2. They are here because
SP-3 applies release profiles inside `rssPlaceTV`, which builds
`buildLibraryIndex` over the whole library up front. A per-id `TagsForSeries`
there is N queries in the RSS hot path. Sonarr has exactly this method
(`GetAllSeriesTags`, consumed by `TagService.cs:125`). Ten lines now; awkward to
retrofit into a hot path later.

## 4. API

### 4.1 New package `internal/tag`

`internal/tag/api.go` with `func (a *API) Mount(r chi.Router)`, mounted through
the existing `NewRouter(..., mounts...)` varargs in `internal/core/api/api.go`.
The shape follows `internal/quality/api.go`.

| Method | Path | Body | Success | Errors |
|---|---|---|---|---|
| GET | `/api/v1/tag` | — | 200 `[Tag]` | — |
| POST | `/api/v1/tag` | `{label}` | 201 `Tag` | 400 empty label, 409 `tag_exists` |
| PUT | `/api/v1/tag/{id}` | `{label}` | 200 `{ok:true}` | 400, 404, 409 `tag_exists` |
| DELETE | `/api/v1/tag/{id}` | — | 200 `{ok:true}` | 404, 409 `tag_in_use` |

The 409 body carries the in-use counts so the UI can name them:
`{"error":"tag_in_use","message":"tag is in use by 3 series and 1 movie"}`.

### 4.2 Assignment lives on the media routes

Added in `internal/media/api.go` alongside the existing
`PUT /series/{id}/qualityprofile`:

```
GET /api/v1/series/{id}/tags   → {tagIds:[…]}
PUT /api/v1/series/{id}/tags   ← {tagIds:[…]}   replace-set
GET /api/v1/movies/{id}/tags   → {tagIds:[…]}
PUT /api/v1/movies/{id}/tags   ← {tagIds:[…]}   replace-set
```

A `PUT` naming a tag id that does not exist returns 400, not a silent partial
write (§3.1).

### 4.3 Deleting a tag that is in use is refused

`DELETE /api/v1/tag/{id}` returns 409 when the tag has any series or movie
association. This mirrors the existing `ErrProfileInUse` → 409 pattern at
`internal/quality/api.go:50`.

The alternative — cascade the delete — was rejected because in SP-3 a tag
deletion would silently un-scope a release profile with no warning. A refusal
is recoverable; a silently-widened release profile is the class of failure this
project has already shipped twice.

### 4.4 Type-to-create is a client-side composition, not an endpoint

Creating a tag from the detail page is two calls: `POST /tag` to get an id,
then `PUT /series/{id}/tags` including it. There is no create-if-missing
endpoint. This keeps the tag API a plain CRUD, and makes a failed create
visible as its own error instead of being buried in an assignment response.

### 4.5 `store.Series` and `store.Movie` do NOT gain a `Tags` field

Tags are read through the sibling endpoints in §4.2, not embedded in the series
or movie payload.

If `Tags` were a struct field, every read path would have to populate it or some
endpoints would silently return `[]`. That is the same defect signature as SP-1
(four grab paths, one missed) and SP-B (three grab paths), and `media_store.go`
already has several `scanSeriesRow` callers. Because tag assignment is
detail-page-only (§5), the library grid never needs tags, so hydrating them
everywhere buys nothing.

If tag chips on library cards are wanted later, that is the moment to add the
field plus a batch join — and §3.2's batch reader already exists for it.

## 5. Frontend

### 5.1 `components/ui/tag-input.tsx`

```tsx
<TagInput
  value={number[]}                              // selected tag ids
  options={Tag[]}                               // all existing tags
  onChange={(ids: number[]) => void}
  onCreate={(label: string) => Promise<number>} // resolves to the new tag id
/>
```

Selected tags render as removable chips. A text input below filters `options`
into a suggestion list, which is click-to-select.

**Enter acts on the typed text, not on the suggestion list:** if the trimmed
input case-insensitively equals an existing label, that tag is selected;
otherwise `onCreate` is called and the returned id added. Enter deliberately
does not pick the first suggestion — typing `an` with `anime` in the list would
then silently select `anime` when the user meant to create `an`.

Hand-rolled rather than a native `<datalist>`: datalist is barely styleable and
effectively untestable in jsdom, and this repo tests its components. The UI kit
is hand-rolled throughout — `components/ui/select.tsx` is a thin native
`<select>` wrapper, there is no Radix.

**Case-insensitive duplicate guard in the control:** typing `anime` when `Anime`
exists selects the existing tag and does not call `onCreate`. Without this the
POST 409s and the user sees an error for an action that should just work. The
server-side 409 remains as the backstop.

Styling uses CSS custom properties only (`var(--color-…)`), per the repo rule.

### 5.2 Detail pages

A **Tags** row on `features/library/SeriesDetail.tsx` and `MovieDetail.tsx`,
directly below the existing quality-profile `Select`. Changing the selection
fires `PUT …/tags` and invalidates the detail query, following the
`assign.mutate({ kind, id, qualityProfileId })` pattern already at
`SeriesDetail.tsx:80`.

### 5.3 Settings → Tags

A new entry in the `TABS` array in `features/settings/SettingsLayout.tsx`,
placed after **Quality Profiles**. `TagsSection` lists each tag with its in-use
counts and rename / delete affordances:

```
anime            3 series, 0 movies     [rename] [delete]
uk-tv            0 series, 2 movies     [rename] [delete]
```

Creation is an inline input plus button. Deleting an in-use tag surfaces the
409's message as an error toast naming the counts, so the refusal is actionable.
Rename is an inline edit on the row.

## 6. Testing

### 6.1 Go

`tag_store_test.go`:

- CRUD round-trip; `ListTags` counts reflect actual associations.
- Case-insensitive uniqueness: creating `HD` then `hd` returns `ErrTagExists`;
  renaming one tag onto another's label likewise.
- Empty / whitespace-only label rejected.
- `ErrTagInUse` on deleting a tag with a series association, and separately with
  only a movie association.
- Replace-set: `Set…([1,2])` then `Set…([2,3])` leaves exactly `{2,3}`;
  `Set…(nil)` clears.
- Unknown tag id in `Set…` fails and leaves the prior set intact.
- Cascade: deleting a *series* removes its `series_tags` rows.
- `SeriesTagIDs` with ≥2 series each holding ≥2 tags, so a "returns only the
  first row per entity" bug is visible.

**Fixture rule — this repo has been bitten three times by fixtures that cannot
discriminate.** The series and movie tests must use **different tag ids and
different media ids**. If both sides use id 1, a `series_tags` /`movie_tags`
mixup in the store passes by construction. Same rule as SP-A's
`DownloadClientID` and SP-B's one-vs-several missing episodes.

API tests: 409 on duplicate create, 409 on rename-to-existing, 409 on in-use
delete with the counts in the message, 400 on `PUT …/tags` with an unknown id,
404 on unknown tag id, and `[]` rather than `null` from an empty list.

### 6.2 Frontend (vitest)

- `TagInput`: add chip, remove chip, filter suggestions, Enter-selects-existing,
  Enter-creates-new, and the case-differing label selecting rather than creating.
- `TagsSection`: in-use counts render; the 409 surfaces as a toast; rename and
  create round-trip through mocked hooks.
- Detail pages: changing tags fires the mutation with the expected payload.

### 6.3 Process

Every task mutation-verified before it is reported done, with the named
mutations written into the task brief up front — the controller-addendum
pattern from SP-B, where every fix wave traced to a plan snippet that survived
mutation rather than to implementer error.

## 7. Build and verification

- `web/dist` **must** be rebuilt, committed, and verified reproducible. SP-2
  ships UI; SP-1's "backend only, no dist rebuild" rule does not carry over.
- Frontend typecheck is `cd web && npx tsc -p tsconfig.app.json --noEmit`. A
  bare `npx tsc --noEmit` in `web/` typechecks nothing and always exits 0.
- `gofmt -l` is useless in this repo (line-ending noise lists nearly every
  file). Trust `go build` and `go vet`.

## 8. What SP-3 will build on this

Stated so SP-3 does not have to reopen SP-2's decisions:

- Release profiles get their own `release_profiles` table plus a
  `release_profile_tags` junction, matching §2's shape.
- Scoping resolves via `SeriesTagIDs` / `MovieTagIDs` (§3.2) built once per
  sweep, not per-release.
- A profile with no tags applies to everything, matching Sonarr.
- All SP-3 behavioural decisions are already settled in
  `docs/superpowers/specs/2026-07-20-nexus-release-matching-design.md` §9 and
  must not be re-brainstormed.
