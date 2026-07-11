# Nexus Web UI — Sub-project 6, Slice 3a: Settings (Indexers & Download Clients)

**Date:** 2026-07-11
**Status:** Design approved, ready for implementation planning
**Depends on:** Slice 1 (Shell/Foundation, merge `00fbeb8`); Slice 2 (Media Library, merge `ccd1db3`); backend sub-projects 2 (indexer engine), 3 (download clients)

## 1. Purpose

Replace the `Settings` nav placeholder with real management UI for the two **connection**
subsystems: **Indexers** (Newznab/Torznab) and **Download Clients** (SABnzbd/qBittorrent).
For each, the user can list configured connections with live status, add a new one via a
schema-driven form, test it against the real service before saving, edit it in place (without
having to re-enter its secret), and delete it.

This is the first half of the Settings slice (Sub-project 6, Slice 3). The remaining Settings
sections — Quality Profiles, Root Folders, Naming, and General/Automation — are Slice **3b**
(separate spec → plan → build). 3a deliberately takes the two hardest, most reusable sections
first: both are driven by a backend `/schema` endpoint and share an identical shape, so the
schema-driven form built here is the reusable core the rest of Settings builds on.

3a follows Slice 1/2's established patterns: Vite + React 19 + TS + Tailwind v4 + shadcn/ui,
TanStack Query v5, cookie auth via the typed `web/src/lib/api.ts` client, committed `web/dist`
with drift-guard.

## 2. Scope

**In scope**
- A `Settings` page under `/settings` with a horizontal sub-nav; 3a ships two tabs — *Indexers*
  and *Download Clients* — with the layout built to accept 3b's four tabs later.
- A generic **schema-driven form** (`SchemaForm`) that renders fields from a backend `/schema`
  response, driven by an implementation dropdown. Reused by both sections (and, later, 3b).
- Per-section **list** (with status badges), **Add** dialog, **Edit** dialog, **Test** action,
  and **Delete** (confirm).
- A minimal, TDD'd **backend fix**: carry forward the stored secret on `update()` when the
  incoming payload's `apiKey` is empty (both `internal/indexer` and `internal/downloadclient`).
- Fold in the deferred Slice-1 `@layer base` border default (Settings renders many bare
  `<Card>`s — this is the slice that would otherwise show near-white borders on dark).

**Out of scope (deferred)**
- Quality Profiles, Root Folders, Naming, General/Automation config → **Slice 3b**.
- Any UI to *clear* an already-set secret (see §5.4 — standard arr tradeoff).
- Indexer capability browsing beyond the inline Test result (`Caps` is `json:"-"`, not exposed by
  list/get; only the Test response carries capabilities).
- Indexer search / manual grab UI (that surface lives elsewhere; 3a is config only).
- Reordering by drag; bulk actions; per-connection history.

## 3. Backend changes (the one minimal touch)

Two symmetric handler edits, no migration, no new routes, no struct/DTO changes. Module
boundaries unchanged (`internal/indexer` and `internal/downloadclient` already own these files).

### 3.1 Carry-forward stored secret on update

**Problem:** `store.Indexer.APIKey` and `store.DownloadClient.APIKey` are `json:"-"` (write-only),
so `GET`/`list` never return the secret. The edit form therefore loads the secret field blank.
Today `update()` calls `p.toStore()` (which sets `APIKey: p.APIKey`) and writes it unconditionally
— so an edit that doesn't re-type the key **silently wipes the stored secret**. This is the
documented "blank-credential-on-update" backlog bug, and 3a's edit UI trips it on every save.

**Fix (both `internal/indexer/api.go` and `internal/downloadclient/api.go` `update()`):**
when the decoded payload's `apiKey` is empty, load the existing row and carry its stored `APIKey`
forward into the value written; when non-empty, overwrite as today.

- Applies to **`update()` only.** An empty key on `create()` stays legitimate (keyless public
  indexer). Do not change `create()`.
- **Consequence (documented, not a bug):** there is no UI path to *clear* a previously-set key —
  submitting blank preserves the old one. This matches Sonarr/Radarr/Prowlarr behavior. A future
  "clear credential" affordance is out of scope.
- Each fix is TDD'd: (a) update with empty `apiKey` preserves the stored key; (b) update with a
  non-empty `apiKey` overwrites it; (c) create with empty `apiKey` still stores empty.

No other backend change. Status fields (`status`, `lastCheck`, `failMessage`) already serialize
on both structs — verified — so status badges need nothing new.

## 4. Backend surface consumed (existing, unchanged)

Both endpoints share an identical route shape and are already mounted under authed `/api/v1`:

| Purpose            | Indexers                      | Download Clients                    |
|--------------------|-------------------------------|-------------------------------------|
| List               | `GET /indexer`                | `GET /downloadclient`               |
| Schema             | `GET /indexer/schema`         | `GET /downloadclient/schema`        |
| Create             | `POST /indexer`               | `POST /downloadclient`              |
| Get one            | `GET /indexer/{id}`           | `GET /downloadclient/{id}`          |
| Update             | `PUT /indexer/{id}`           | `PUT /downloadclient/{id}`          |
| Delete             | `DELETE /indexer/{id}`        | `DELETE /downloadclient/{id}`       |
| Test unsaved       | `POST /indexer/test`          | `POST /downloadclient/test`         |
| Test saved         | `POST /indexer/{id}/test`     | `POST /downloadclient/{id}/test`    |

### 4.1 Schema response shape
```jsonc
[
  { "implementation": "newznab", "protocol": "usenet", "fields": [ /* Field[] */ ] },
  { "implementation": "torznab", "protocol": "torrent", "fields": [ /* Field[] */ ] }
]
```
`Field = { name: string, type: "string"|"int"|"int[]"|"bool", required: bool, default?: any, label?: string }`.
Within each endpoint the two implementations have **identical fields**; they differ only in
`protocol` and (for download clients) the `apiKey` field's `label` ("API Key" vs "Password").

### 4.2 Test response shape
Both test endpoints return **HTTP 200** in success *and* failure:
```jsonc
{ "ok": true,  "capabilities": { /* indexer caps, present for indexers only */ } }
{ "ok": false, "error": "connection refused" }
```
The frontend must key off the `ok` field, not the HTTP status.

### 4.3 List/get item shape (relevant fields)
`{ id, name, implementation, enabled, priority, status, lastCheck, failMessage, ... }` plus
per-endpoint config fields (indexer: `baseUrl`, `categories`; client: `protocol`, `host`, `port`,
`useSsl`, `urlBase`, `username`, `category`). **`apiKey` is never present** (write-only). Indexer
`caps` is never present (`json:"-"`).

## 5. Frontend design (`web/src/features/settings/`)

### 5.1 Routing & layout
- Sidebar "Settings" item routes to `/settings` (already in `NAV_ITEMS`).
- `routes.tsx`: `/settings` renders `SettingsLayout` with nested tab routes; `/settings`
  redirects to `/settings/indexers` (first tab). Routes `/settings/indexers` and
  `/settings/downloadclients` in 3a; the layout's tab list is a data array so 3b appends entries.
- `SettingsLayout` = page header + horizontal tab nav (`NavLink`s styled like the sidebar's active
  state) + `<Outlet/>`.

### 5.2 `SchemaForm` (reusable core)
A controlled form that takes a `schema` (the `/schema` array) and a `value` (current field values)
and renders one input per schema field:

| Field `type` | Rendered as                                             |
|--------------|---------------------------------------------------------|
| `string`     | text input (single line)                                |
| `int`        | number input; empty → omit (server applies default)     |
| `bool`       | switch/checkbox; defaults from schema `default`         |
| `int[]`      | text input parsed as comma-separated ints (e.g. `5000,5040`) |
| `apiKey` (string, name === "apiKey") | password input, labeled by schema `label` (fallback "API Key") |

- Implementation is chosen by a **dropdown** populated from the schema entries' `implementation`
  (with `protocol` shown alongside). Changing it re-derives which field set/labels apply (labels
  can differ, e.g. Password vs API Key).
- Required fields (`required: true`) get client-side "required" validation mirroring the server's
  `valid()` checks (name, baseUrl/host, implementation) — fail fast before the network call.
- `SchemaForm` is presentation + local state only; it does not fetch. Fetching schema and
  submitting are the section component's job (keeps the form testable in isolation).

### 5.3 Section components (`IndexersSection`, `DownloadClientsSection`)
Thin wrappers over shared building blocks parameterized by endpoint base path
(`/indexer` vs `/downloadclient`) and a couple of display labels:
- **List:** TanStack Query `useQuery` on the list endpoint → cards/rows showing `name`,
  `implementation`, `enabled` (badge/switch-look), `priority`, and a **status badge**
  (`ok` = green, `failed` = red with `failMessage` on hover/title, empty/unknown = neutral) plus
  `lastCheck` as relative time (reuse `lib/time.ts`). Empty state: "No indexers configured — Add one".
- **Add** button → dialog with a fresh `SchemaForm` (schema fetched via `useQuery` on `/schema`).
- **Edit** (per row) → same dialog pre-filled from the row (secret field blank; see §5.4).
- **Delete** (per row) → confirm dialog (reuse the media-detail confirm pattern), then
  `apiDelete` + query invalidation + toast.
- Create/update via `apiPost`/`apiPut` mutations; on success invalidate the list query and toast.

### 5.4 Secret handling (add vs edit)
- **Add:** secret field is a normal (optional) password input.
- **Edit:** secret field renders **blank** with placeholder "leave blank to keep current". On
  submit, if the field is untouched/empty, the frontend **omits `apiKey`** from the payload; the
  backend carry-forward (§3.1) preserves the stored key. If the user types a new value, it's sent
  and overwrites.
- **No clear path:** consistent with §3.1 — blank means "keep", so the UI cannot null out a set key.

### 5.5 Test button (the branch that matters)
A **Test** button inside the Add/Edit dialog runs a connection test and shows the result inline
(spinner while pending; then a green "Connection OK" — with indexer capabilities listed when
present — or a red error box with `error`). It must handle the `200 {ok:false}` shape (§4.2).

Endpoint selection:
- **Add mode**, or **Edit mode where the secret field was retyped** → `POST …/test` (**unsaved**),
  sending the current form values (including the typed key).
- **Edit mode where the secret field is untouched** → `POST …/{id}/test` (**saved**), which loads
  the stored key server-side. This avoids a false failure: testing unsaved with a blank key would
  fail against any service that requires one — the same blank-secret hazard §3.1 fixes for Save.

### 5.6 Shared/base fix
Add the deferred Slice-1 base layer to `web/src/styles/index.css`:
`@layer base { * { @apply border-border outline-ring/50; } }` — so bare shadcn `<Card>`/inputs
rendered across Settings get the themed dark border instead of the near-white default. Verify the
Dashboard/library Cards (which set explicit borders) are visually unchanged.

## 6. Data flow

```
SettingsLayout ── tab nav ──▶ IndexersSection / DownloadClientsSection
                                   │
                 useQuery(list) ───┤──▶ cards + status badges
                 useQuery(schema) ─┘
                                   │
        Add/Edit dialog ─▶ SchemaForm (impl dropdown + typed fields)
                                   │
             Test ─▶ POST /test (unsaved) | POST /{id}/test (saved, secret untouched)
                                   │
          Save ─▶ apiPost / apiPut (omit apiKey when untouched) ─▶ invalidate list ─▶ toast
          Delete ─▶ confirm ─▶ apiDelete ─▶ invalidate list ─▶ toast
```

## 7. Testing strategy

**Vitest (frontend):**
- `SchemaForm` renders the correct input per field type (string/int/bool/int[]/apiKey-as-password
  with the schema label); implementation dropdown switches labels; required validation blocks
  submit.
- Section: Add submits a create payload from form values; Edit **omits `apiKey`** when the secret
  field is untouched and **includes** it when typed.
- Test button selects the **unsaved** endpoint in Add/retyped-secret and the **saved** endpoint in
  Edit-with-untouched-secret; renders `{ok:true}`, `{ok:false,error}`, and capabilities correctly.
- Status badge maps `ok`/`failed`/empty to the right variant and shows `failMessage`.

**Go (backend):**
- `indexer.update()` and `downloadclient.update()`: (a) empty `apiKey` preserves the stored key;
  (b) non-empty `apiKey` overwrites; (c) `create()` with empty `apiKey` still stores empty.

**Build hygiene:** `tsc -b` clean, Vitest green, `web/dist` rebuilt and drift-guard clean,
`CGO_ENABLED=0 go build/vet/test ./...` green.

## 8. Acceptance criteria

1. `/settings` shows a tabbed page; Indexers and Download Clients tabs each list configured
   connections with name, implementation, enabled, priority, and a live status badge.
2. Adding a connection via the schema-driven form (with implementation dropdown) creates it and it
   appears in the list.
3. The Test button reports real success/failure inline (indexer capabilities shown on success),
   correctly using unsaved vs saved test endpoints per §5.5.
4. Editing a connection **without** re-entering its secret preserves the stored secret (verified by
   a subsequent successful saved-Test), and editing **with** a new secret overwrites it.
5. Deleting a connection (after confirm) removes it from the list.
6. Bare Cards/inputs across Settings render with the themed dark border.
7. All tests green; `web/dist` drift-guard clean; Go build/vet/test green.

## 9. Risks & notes

- **Two test endpoints look interchangeable but aren't** — the unsaved/saved branch (§5.5) is the
  subtle part; it's specified explicitly and covered by a test.
- **Write-only secrets** mean the edit form can never show the current key; the "leave blank to
  keep" convention + backend carry-forward is the whole contract. Keep §3.1 and §5.4 in sync.
- **Capabilities** are only available from the Test response (indexer `Caps` is `json:"-"`); the
  list intentionally shows status only, not caps.
- Endpoint field sets are identical within each subsystem today; `SchemaForm` is written to render
  whatever the schema returns, so a future field addition needs no form change.
