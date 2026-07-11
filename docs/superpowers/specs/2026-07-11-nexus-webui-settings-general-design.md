# Nexus Web UI — Settings Slice 3b (Quality Profiles, Root Folders, Naming, General)

Date: 2026-07-11
Status: Approved (brainstorm)
Sub-project: 6 (Web UI) — Slice 3b (second half of the original Settings slice; 3a shipped Indexers + Download Clients)

## 1. Goal & scope

Complete the Settings surface with four more tabs — **Quality Profiles**, **Root
Folders**, **Naming**, and **General** — built as a UI over Nexus's *existing*
REST endpoints. This slice is almost entirely frontend, plus **one** small,
targeted backend fix (root-folder in-use delete → HTTP 409).

It mirrors the conventions established by Slice 3a: everything lives under
`web/src/features/settings/`, data flows through TanStack Query v5 hooks, user
feedback uses the existing hand-written toast, and styling uses the existing dark
CSS tokens.

### In scope
- Four new Settings tabs + nested routes, appended to the existing `SettingsLayout`.
- Quality Profiles: list + create/edit/delete over `/api/v1/qualityprofile` and
  `/api/v1/quality/definitions`.
- Root Folders: list + add + delete over `/api/v1/rootfolder`.
- Naming: single form over `/api/v1/config/naming`.
- General: read-only System Info (`/api/v1/system/status`) + editable Task
  Scheduling (`/api/v1/automation/config`).
- Backend: `DeleteRootFolder` distinguishes in-use (→ 409) and not-found (→ 404).

### Explicitly out of scope (YAGNI / not in Nexus)
- **Custom formats** — Nexus has none.
- **Language as a decision axis** — Nexus parses language but never decides on it.
- **Quality drag-to-reorder** — the Minimal editor uses fixed global order (see §4).
- **Naming live-preview endpoint** — a static token legend is used instead; the
  server owns `naming.Render`, and reimplementing it in TypeScript would be
  duplication.
- **Editable host/port/auth/TMDb key** — these are env-var-driven at startup and
  have no config API; General shows only what is genuinely editable.
- No new database migration.

## 2. Existing backend surface (verified)

| Domain | Endpoints | Shape |
|---|---|---|
| Quality profiles | `GET/POST /api/v1/qualityprofile`, `GET/PUT/DELETE /api/v1/qualityprofile/{id}` | `store.QualityProfile{ id, name, cutoffQualityId, upgradeAllowed, items:[{qualityId, allowed}], createdAt }` |
| Quality ladder | `GET /api/v1/quality/definitions` | `[]QualityDefinition{ id, name, source, resolution, rank }` — 13 fixed entries, id0 Unknown … id12 Bluray-2160p |
| Root folders | `GET/POST /api/v1/rootfolder`, `DELETE /api/v1/rootfolder/{id}` | `store.RootFolder{ id, path, createdAt }` |
| Naming | `GET/PUT /api/v1/config/naming` | `naming.Config{ seriesFolder, seasonFolder, episodeFile, movieFolder, movieFile }` |
| Automation config | `GET/PUT /api/v1/automation/config` | `automation.Config{ missingSearchIntervalHours, missingSearchBatchSize, rssSyncEnabled, rssSyncIntervalMinutes, upgradeSearchEnabled, upgradeSearchIntervalHours, upgradeSearchBatchSize, upgradeGrabCooldownHours }` |
| System status | `GET /api/v1/system/status` | `{ version, appName, healthy, taskCount }` |

Key behaviours to respect:
- **Quality profile validation** (`internal/quality/service.go`): name non-empty;
  ≥1 item; every item's `qualityId` must be a real definition; `cutoffQualityId`
  must be a real definition **and** must be in the allowed set. Partial coverage
  (fewer than 13 items) is legal.
- **Quality profile rank = position within `items`** (`profileRank` returns the
  slice index, not the global definition rank). The Minimal editor therefore emits
  `items` in global low→high definition order, giving a stable, sensible rank
  without a reorder UI.
- **Delete-in-use → 409** already exists for quality profiles
  (`store.ErrProfileInUse`); the UI must surface it.
- **Automation config applies on next restart** — intervals are read once at
  scheduler registration. The UI must say so.

## 3. Tab structure & routing

`web/src/features/settings/SettingsLayout.tsx` `TABS` grows from 2 to 6:

```
Indexers · Download Clients · Quality Profiles · Root Folders · Naming · General
```

New nested routes appended in `web/src/app/routes.tsx` under the existing
`settings` route:

- `/settings/qualityprofiles` → `<QualityProfilesSection />`
- `/settings/rootfolders` → `<RootFoldersSection />`
- `/settings/naming` → `<NamingSection />`
- `/settings/general` → `<GeneralSection />`

The existing index redirect (`/settings` → `/settings/indexers`) is unchanged.

## 4. Quality Profiles

**`QualityProfilesSection.tsx`** — list view:
- Columns/inline info: name, cutoff quality name, "Upgrades: on/off", allowed count.
- Add button opens `ProfileDialog` in create mode; each row has Edit (opens dialog
  with the profile) and Delete.
- Delete: `confirm()` then mutate; on success toast "Deleted"; **on 409 toast
  "Profile is in use"** (read the ApiError code/status), on other errors a generic
  failure toast.
- Loading / error / empty states mirror `ConnectionsSection`.

**`ProfileDialog.tsx`** — create/edit editor (Minimal):
- **Name** text input.
- **Qualities**: the 13 definitions from `useQualityDefinitions`, rendered in
  fixed global low→high order (as returned). Each row: quality name + an
  **Allowed** checkbox.
- **Cutoff**: a `<select>` whose options are the currently-allowed qualities only.
  If the current cutoff becomes disallowed (its checkbox is unticked), the cutoff
  resets to the highest still-allowed quality.
- **Upgrades Allowed**: a toggle/checkbox bound to `upgradeAllowed`.
- **Save** is disabled unless: name non-empty, ≥1 quality allowed, and cutoff ∈
  allowed — so the request can never 400. Payload:
  ```json
  {
    "name": "...",
    "cutoffQualityId": 7,
    "upgradeAllowed": true,
    "items": [ { "qualityId": 0, "allowed": false }, ... all 13 in ladder order ... ]
  }
  ```
  Create → `POST /qualityprofile`; edit → `PUT /qualityprofile/{id}`.
- Default new-profile state: a sensible baseline (e.g. all WEBDL/Bluray 1080p-and-
  below allowed, cutoff = WEBDL-1080p, upgrades on) — kept simple; exact defaults
  finalized in the plan.

## 5. Root Folders

**`RootFoldersSection.tsx`**:
- List of `{path}` rows, each with Delete.
- Inline add: a path text input + Add button (no dialog). Empty/whitespace path
  disables Add. On success toast + clear input; on error (e.g. invalid path from
  `ErrInvalidRootFolder` → 400) a toast.
- Delete: `confirm()` then mutate; success toast; **409 → toast "Root folder is in
  use by a movie or series"**; 404 → toast "Root folder not found"; else generic.

**Backend fix** (`internal/core/store/media_store.go` + `internal/media/api.go`):
- Add `var ErrRootFolderInUse = errors.New("store: root folder in use")` (mirrors
  `ErrProfileInUse`).
- `DeleteRootFolder(ctx, id)`:
  1. Pre-check references: `SELECT EXISTS(SELECT 1 FROM series WHERE root_folder_id = ? UNION ALL SELECT 1 FROM movies WHERE root_folder_id = ?)` (or two counts) → if referenced, return `ErrRootFolderInUse`.
  2. `DELETE ... WHERE id = ?`; if `RowsAffected() == 0`, return `store.ErrNotFound`.
- `media/api.go deleteRootFolder` maps `ErrRootFolderInUse` → 409 `conflict`,
  `store.ErrNotFound` → 404 `not_found`, else 500 (extend `writeMediaError` or map
  inline, consistent with the quality API's `writeProfileError`).
- This is the only backend change in the slice; no migration (the FK already
  exists — `root_folder_id INTEGER REFERENCES root_folders(id)` on both tables,
  `foreign_keys(ON)`).

## 6. Naming

**`NamingSection.tsx`** — single form over `GET/PUT /config/naming`:
- Five labelled text inputs: Series Folder, Season Folder, Episode File, Movie
  Folder, Movie File, seeded from the GET.
- Save → `PUT` → toast "Saved"; re-seed from the response.
- "Reset to defaults" button fills the inputs from a client-side constant that
  matches `naming.DefaultConfig()` (does not auto-save; user still clicks Save).
- A static **token legend** (constant list) documenting the supported tokens:
  `{Series Title}`, `{Episode Title}`, `{Movie Title}`, `{Quality}`,
  `{Release Group}`, `{season}`, `{season:00}`, `{episode}`, `{episode:00}`,
  `{year}`. No server call; no TS re-implementation of `Render`.

## 7. General

**`GeneralSection.tsx`** — two stacked cards:

- **System Info** (read-only) from `useSystemStatus` (`GET /system/status`):
  Version, App Name, Healthy (yes/no), Active Tasks. Reuses the same endpoint the
  Dashboard already reads.
- **Task Scheduling** (editable) from `useAutomationConfig` (`GET/PUT
  /automation/config`): the 8 fields grouped —
  - Missing search: interval (hours), batch size.
  - RSS sync: enabled toggle, interval (minutes).
  - Upgrade search: enabled toggle, interval (hours), batch size, grab cooldown
    (hours).
  Numeric inputs; non-positive values clamped client-side to keep parity with the
  server's defaulting-on-load. Save → `PUT` → toast.
  - A **prominent, persistent note**: "Interval and enabled changes take effect on
    the next Nexus restart."

## 8. Frontend module layout

Extends `web/src/features/settings/` (all new unless noted):

```
SettingsLayout.tsx        (edit: add 4 tabs)
QualityProfilesSection.tsx (+ .test.tsx)
ProfileDialog.tsx          (+ .test.tsx)
RootFoldersSection.tsx     (+ .test.tsx)
NamingSection.tsx          (+ .test.tsx)
GeneralSection.tsx         (+ .test.tsx)
qualityApi.ts / configApi.ts   (new hooks; may fold into api.ts if small)
qualityTypes.ts / configTypes.ts (mirror the Go JSON shapes)
```

`web/src/app/routes.tsx` edited to add the four child routes.

Reuses: `lib/toast`, `lib/api` (`apiGet/apiPost/apiPut/apiDelete` + `ApiError`),
`features/library/StatusBadge` where a status pill fits, existing dark tokens
(`--color-border`, `--color-panel`, `--color-muted`, `--color-brand`,
`--color-warn`).

## 9. Data layer (TanStack Query hooks)

Following `api.ts` conventions (query-key namespacing per domain, invalidate the
relevant list on mutate):

- Quality: `useQualityProfiles`, `useQualityDefinitions`, `useSaveProfile`
  (create/update by presence of id), `useDeleteProfile`.
- Root folders: `useRootFolders`, `useAddRootFolder`, `useDeleteRootFolder`.
- Naming: `useNamingConfig`, `useSaveNaming`.
- Automation: `useAutomationConfig`, `useSaveAutomationConfig`.
- System: `useSystemStatus`.

Errors propagate as `ApiError` (existing normalized envelope) so sections can read
`.status` / `.code` to distinguish 409/404 for targeted toasts.

## 10. Testing

**Frontend (vitest):**
- Each section: render, loading, error, empty states.
- `ProfileDialog`: toggling allowed updates the cutoff option set; unticking the
  current cutoff resets it; Save disabled until valid; emitted payload has all 13
  items in ladder order with correct `allowed` + chosen `cutoffQualityId` +
  `upgradeAllowed`.
- `QualityProfilesSection`: in-use delete (mocked 409) shows the in-use toast.
- `RootFoldersSection`: add posts the path; delete on 409 shows the in-use toast.
- `NamingSection`: seeds from GET, edit + save PUTs the new values, reset fills
  defaults, legend tokens present.
- `GeneralSection`: renders system info values; renders + edits the automation
  fields; restart note present; save PUTs.

**Backend (go):**
- `store.DeleteRootFolder`: in-use (folder referenced by a series and/or movie) →
  `ErrRootFolderInUse`; unused → deletes; missing id → `ErrNotFound`.
- `media/api.go`: DELETE in-use → 409, DELETE missing → 404, DELETE unused → 200.

**Gates (must be green before merge):**
- `web/dist` rebuilt and committed; `git diff --exit-code web/dist` clean.
- `tsc -b` exit 0; full vitest suite passes.
- `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` all pass.

## 11. Delivery

Built via the subagent-driven-development loop (sonnet implementers + reviewers
per task, opus whole-branch final review), in an isolated git worktree, roughly
8–10 TDD tasks. Merged to `master` `--no-ff` locally. **Ask before pushing the
default branch** (standing project rule).

## 12. Acceptance criteria

- (a) All six Settings tabs render and route correctly; `/settings` still
  redirects to Indexers.
- (b) Create a quality profile with a subset of qualities allowed, a chosen
  cutoff, and upgrades on; it appears in the list; editing it persists and
  re-opens with the saved state.
- (c) Deleting an in-use quality profile **and** an in-use root folder each show a
  clean "in use" message (HTTP 409), not a generic error.
- (d) Add then delete an unused root folder from the UI works end-to-end.
- (e) Naming edits persist and survive reload; "Reset to defaults" restores the
  built-in templates; the token legend is visible.
- (f) General shows live system info and lets the user edit and save the
  automation config, with the "applies on restart" caveat visible.
- (g) All gates in §10 green; verified live in the browser against a running
  `nexus.exe`.
