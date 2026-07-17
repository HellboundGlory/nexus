# Nexus — Add Defaults (per-kind default root folder + quality profile)

Date: 2026-07-17
Status: Approved (design)

## 1. Goal

Make adding a movie or TV show a one-decision action. Today the Add dialog's
root-folder dropdown defaults to blank and there is **no** quality-profile picker
at all — a profile can only be assigned *after* the item exists, via a separate
endpoint. So every add is followed by a manual profile assignment, and a search
started before that assignment silently no-ops (the Wave B profile-guard toast
exists precisely because of this gap).

This feature lets the user set, per media kind, a **default root folder** and a
**default quality profile** in Settings. The Add dialog pre-selects both, adds the
missing profile dropdown, and threads the chosen profile through the add request so
the item is created *with* its profile. Both fields stay overridable per add.

Covers requests #1 (default root folder for TV), #2 (default root folder for
movies), and #3 (quality-profile dropdown on add + per-kind default profile).

## 2. Scope

**In scope:**
- Per-kind (movie / tv) default root folder and default quality profile, stored
  server-side and edited in a new Settings tab.
- A quality-profile dropdown in the Add dialog (currently absent).
- Pre-selecting root folder + profile from the per-kind defaults when a result is
  picked; both remain user-overridable.
- Threading `qualityProfileId` through `AddMovie` / `AddSeries` so the item is born
  with its profile.

**Out of scope (explicit non-goals):**
- Tagging root folders with a kind. Root folders stay `{id, path}`; "which folder
  is the TV one" is expressed only as a *default*, not a property of the folder. A
  folder can be the default for both kinds, or neither.
- Other add-time defaults (default monitor option, default language, tags, etc.).
  Only root folder + quality profile, per the request. YAGNI.
- Auto-migrating existing profile-less library items. This changes the *add* path;
  existing items are untouched and keep their current (possibly null) profile.
- Removing the separate `PUT /{id}/qualityprofile` assign endpoint. It stays for
  changing a profile after add; this feature only sets the *initial* one.

## 3. Storage

Four keys in the existing generic `settings` key-value table (`GetSetting` /
`SetSetting`, `internal/core/store/store.go`). No migration, no schema change.

| Key | Value |
|-----|-------|
| `defaults.movie.rootFolderId` | root folder id (decimal string); absent = unset |
| `defaults.movie.qualityProfileId` | quality profile id; absent = unset |
| `defaults.tv.rootFolderId` | root folder id; absent = unset |
| `defaults.tv.qualityProfileId` | quality profile id; absent = unset |

A key is written only when a non-null default is chosen; choosing "none" **deletes**
the key (or writes empty, treated as unset on read — see §4.1). Storing the id, not
the path/name, means a renamed root folder or profile keeps working.

## 4. Backend design

### 4.1 Config endpoint

A typed config pair on the media API router (`internal/media/api.go`), mounted
beside the existing routes, mirroring the `/config/naming` (importing) and
`/config` (automation) precedent:

```
GET /api/v1/config/media-defaults
PUT /api/v1/config/media-defaults
```

Wire shape (both directions):

```json
{
  "movie": { "rootFolderId": 1,    "qualityProfileId": 2 },
  "tv":    { "rootFolderId": null,  "qualityProfileId": 4 }
}
```

Each of the four fields is a **nullable integer** — `null` (JSON `null`, not absent)
means "no default set". The frontend always sends all four keys; a `null` clears
that default.

### 4.2 Stale-reference validation is load-bearing

A stored default is an id that can later be deleted (the user removes a root folder
or a quality profile). The **GET** must never hand back an id that no longer
resolves, or the Add dialog would pre-select a phantom option.

On GET, each stored id is looked up against the live set (`GetRootFolder` /
`GetQualityProfile`); if the lookup returns `ErrNotFound`, that field is returned as
`null` (the stale key is treated as unset — it may also be lazily deleted, but that
is not required for correctness). This means:

- A deleted root folder that was the movie default → GET returns
  `movie.rootFolderId: null` → the dialog opens with a blank root folder, exactly as
  if no default were ever set.

On **PUT**, every non-null id is validated to exist before anything is written; an
unknown id → `400 bad_request` naming the offending field, and **no** key is written
(all-or-nothing, so a partial save can't half-apply).

### 4.3 Threading the profile through Add

`AddMovieRequest` / `AddSeriesRequest` (`internal/media/media.go`) and their HTTP
bodies (`internal/media/api.go`) gain an optional `QualityProfileID *int64`.

- `AddMovie` / `AddSeries` validate it the same way `validateRootFolder` already
  validates the folder (unknown id → `ErrInvalidQualityProfile` → `400`), then set
  it on the `store.CreateMovie` / `store.CreateSeries` row (the column already
  exists — it is simply never populated at create time today).
- **Purely additive:** the field is a pointer defaulting to `nil`; every existing
  caller and test that omits it creates a profile-less item exactly as before. The
  separate `PUT /{id}/qualityprofile` assign path is untouched.

`ErrInvalidQualityProfile` is a new sentinel in `internal/media/errors.go`, mapped
to `400` in the media API's error writer beside the existing
`ErrInvalidRootFolder`.

## 5. Frontend design

### 5.1 Settings — new "Media Management" tab

A seventh Settings tab (`web/src/features/settings/SettingsLayout.tsx` `TABS`) at
`/settings/mediamanagement`, label "Media Management". Its section renders four
dropdowns in two rows:

| | Root folder | Quality profile |
|--|--|--|
| **Movies** | default root folder | default quality profile |
| **TV** | default root folder | default quality profile |

Each dropdown lists the live options plus a "None" entry (value `""` → `null`).
Options come from the existing `useRootFolders` and quality-profile hooks. Saving
issues the `PUT`. Empty states: if there are no root folders (or no profiles),
those dropdowns show only "None" and a hint pointing to the relevant tab.

New files under `web/src/features/settings/`:
`mediaDefaultsApi.ts` (typed GET/PUT hooks), `mediaDefaultsTypes.ts`,
`MediaManagementSection.tsx` (+ test), plus a route + `TABS` entry.

### 5.2 Add dialog

`web/src/features/library/AddMediaDialog.tsx`:

- Fetch media-defaults and quality profiles alongside the existing root-folder fetch.
- When a result is picked, initialise `rootFolderId` and a **new** `qualityProfileId`
  state from the per-kind defaults (blank if the default is unset or came back `null`).
- Render a **Quality profile** `Select` beneath the root-folder select. **A profile
  is required to add, and there is no "None" option** — the point is to eliminate the
  profile-less add that the Wave B guard-toast exists to catch:
  - The select lists the live profiles only. If the per-kind default profile is set,
    it is **pre-selected**; the user may leave it or choose another.
  - If no default is set (or it came back `null`/stale), the select shows a
    non-selectable "Select a profile…" placeholder and the **Add button is disabled
    until a profile is chosen** — in addition to the existing root-folder gating.
  - If **no quality profiles exist at all**, show a hint ("No quality profile
    configured — add one in Settings") and keep Add disabled, mirroring the existing
    no-root-folders guard.
- Send `qualityProfileId` in the `addMovie` / `addSeries` mutation. Because the UI
  gates on it, the submitted value is always a real id — the UI never sends `null`.

Note the split: the **server** keeps `qualityProfileId` optional (§4.3 additive), so
non-UI callers (RSS, tests) still create profile-less items. The requirement is a UI
affordance, not a server constraint. Root folder in the dialog is unchanged by this
feature — pre-seeded from its default, but not newly required.

`AddMovieBody` / `AddSeriesBody` (`web/src/features/library/types.ts`) and the
`useAddMovie` / `useAddSeries` mutations gain the `qualityProfileId` field
(typed `number | null` for wire symmetry, though the dialog always sends a number).

## 6. Error handling

| Case | Behavior |
|------|----------|
| GET, stored default id was deleted | that field returns `null`; dialog blank (§4.2) |
| PUT with an unknown root folder / profile id | `400 bad_request`, field named, nothing written |
| Add with an unknown `qualityProfileId` | `400` (`ErrInvalidQualityProfile`), item not created |
| Add with `qualityProfileId` omitted / null (server-side, non-UI caller) | item created profile-less, as today (additive) |
| Add dialog, no profile selected | Add button disabled; cannot submit without a profile |
| Add dialog, no quality profiles configured | hint shown, Add disabled (mirrors the no-root-folders guard) |
| Settings, no root folders / no profiles | those dropdowns show only "None"; the default can't be set |

## 7. Testing

**Go**
- media-defaults GET returns stored ids; a default pointing at a since-deleted root
  folder / profile returns `null` for that field (the stale-reference guard, §4.2) —
  and a regression test that a *valid* default is **not** nulled.
- PUT validates: unknown root folder id and unknown profile id each → `400`, and the
  store is unchanged (nothing half-written).
- PUT then GET round-trips all four fields including a mix of set and `null`.
- `AddMovie` / `AddSeries` with a valid `qualityProfileId` create the row with that
  profile; with an unknown id → `ErrInvalidQualityProfile`; omitted → null (the
  additive-guarantee test — the case most likely to regress existing add tests).
- **Wire shape:** assert the four nullable fields serialise as JSON `null` when unset
  (not absent, not `0`) via `map[string]json.RawMessage` — a `0` id and a `null`
  default must be distinguishable, and `0` is never a valid id.

**Frontend**
- `MediaManagementSection`: renders four dropdowns seeded from the GET; changing one
  and saving issues the PUT with the right shape; "None" sends `null`.
- Add dialog: with defaults set, picking a result pre-selects the matching root
  folder and profile; overriding either changes what the mutation sends; the add
  mutation body carries `qualityProfileId`.
- Add dialog, profile required: with **no** default profile, Add is disabled until a
  profile is chosen, then enabled; with **no profiles configured**, the hint renders
  and Add stays disabled. No selectable "None" in the profile dropdown.
- `web/dist` rebuild (committed; CI drift-checks it).

## 8. Deferred

- Root-folder-as-typed (a `kind` column on root folders). Not needed — defaults
  express the association.
- Additional add-time defaults (monitor option, tags, language). Out of scope.
- Back-filling profiles onto existing profile-less items.
