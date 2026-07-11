# Nexus Web UI — Settings Slice 3b Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four Settings tabs — Quality Profiles, Root Folders, Naming, General — as a UI over Nexus's existing REST endpoints, plus one backend fix (root-folder in-use delete → 409/404).

**Architecture:** Almost entirely frontend under `web/src/features/settings/`, mirroring Slice 3a's conventions (TanStack Query v5 hooks, hand-written toast, dark CSS tokens, co-located vitest files). New sections mount into the existing `SettingsLayout` + `/settings` nested routes. One targeted backend change gives `DeleteRootFolder` in-use (409) and not-found (404) semantics, mirroring the existing `ErrProfileInUse` precedent.

**Tech Stack:** Go 1.x (chi router, database/sql + SQLite), React 19 + TypeScript + Tailwind v4 + shadcn/ui, TanStack Query v5, Vitest + Testing Library, Vite.

## Global Constraints

- Go binary builds with `CGO_ENABLED=0`; verify with `go build ./... && go vet ./... && go test ./...`. Go is at `C:\Program Files\Go\bin` — prefix `export PATH="/c/Program Files/Go/bin:$PATH"`. `-race` is unavailable (no CGO); use `-count=N` for concurrency.
- Frontend gates: `cd web && npx tsc -b` exit 0; `npm test` (vitest) green.
- `web/dist` is committed and drift-guarded: `git diff --exit-code web/dist` must be clean after `npm run build`.
- No new database migration. No custom formats. No language as a decision axis. No naming live-preview endpoint (static token legend only). No editable host/port/auth/TMDb key.
- Quality-profile `items` order = decision rank; the editor emits all 13 items in global low→high definition order. Cutoff must be in the allowed set; ≥1 quality allowed; name non-empty (mirror `internal/quality/service.go` validation client-side so save can't 400).
- Automation config changes apply on next restart — the General tab must say so.
- Module boundary: `internal/media` imports only `internal/core/*` (+ its own). The backend fix touches `internal/core/store` and `internal/media` only.
- Standing rule: **ask before pushing the default branch.**

## Existing surface (reference, do not re-derive)

- Quality: `GET/POST /api/v1/qualityprofile`, `GET/PUT/DELETE /api/v1/qualityprofile/{id}`, `GET /api/v1/quality/definitions`.
  - `QualityProfile { id:number; name:string; cutoffQualityId:number; upgradeAllowed:boolean; items:{qualityId:number;allowed:boolean}[]; createdAt:string }`
  - `QualityDefinition { id:number; name:string; source:string; resolution:string; rank:number }` (13 fixed, id0…id12, returned low→high).
  - Delete in-use → 409 `{error:{code:"conflict"}}` (via `store.ErrProfileInUse`).
- Root folders: `GET/POST /api/v1/rootfolder`, `DELETE /api/v1/rootfolder/{id}`. `RootFolder { id:number; path:string; createdAt:string }`. POST body `{path:string}`; invalid path → 400 `bad_request`.
- Naming: `GET/PUT /api/v1/config/naming`. `NamingConfig { seriesFolder; seasonFolder; episodeFile; movieFolder; movieFile }` (all strings).
- Automation: `GET/PUT /api/v1/automation/config`. `AutomationConfig { missingSearchIntervalHours; missingSearchBatchSize; rssSyncEnabled; rssSyncIntervalMinutes; upgradeSearchEnabled; upgradeSearchIntervalHours; upgradeSearchBatchSize; upgradeGrabCooldownHours }`.
- System: `GET /api/v1/system/status` → `SystemStatus { version; appName; healthy; taskCount }` (type already in `web/src/lib/api.ts`; `getStatus()` exists).
- Frontend helpers: `apiGet<T>(path)`, `apiPost<T>(path,body?)`, `apiPut<T>(path,body?)`, `apiDelete<T>(path)`, `class ApiError extends Error { status:number; code:string }` (all in `web/src/lib/api.ts`). `useToast()` → `{ toast(msg, {variant?:"ok"|"error"}) }` from `@/lib/toast`. `Dialog`/`DialogTitle` from `@/components/ui/dialog` (`Dialog` props `{open, onOpenChange, children}`).

## File structure

```
internal/core/store/media_store.go        (modify: ErrRootFolderInUse + DeleteRootFolder rewrite)
internal/core/store/media_store_test.go   (modify: DeleteRootFolder tests)
internal/media/api.go                      (modify: deleteRootFolder error mapping)
internal/media/api_test.go                 (modify: delete 409/404/200 tests)

web/src/features/settings/
  qualityTypes.ts        (new)  QualityDefinition, QualityProfile, QualityProfileItem, ProfilePayload, ProfileFormState
  qualityForm.ts         (new)  pure helpers: defaultNewProfile, buildProfileItems, buildProfilePayload, resolveCutoff, isProfileFormValid
  qualityForm.test.ts    (new)
  qualityApi.ts          (new)  useQualityProfiles, useQualityDefinitions, useSaveProfile, useDeleteProfile
  QualityProfilesSection.tsx      (new) + .test.tsx
  ProfileDialog.tsx               (new) + .test.tsx
  configTypes.ts         (new)  RootFolder, NamingConfig, AutomationConfig
  configApi.ts           (new)  useRootFolders/useAddRootFolder/useDeleteRootFolder, useNamingConfig/useSaveNaming, useAutomationConfig/useSaveAutomationConfig, useSystemStatus
  RootFoldersSection.tsx          (new) + .test.tsx
  NamingSection.tsx               (new) + .test.tsx  (includes NAMING_TOKENS legend + DEFAULT_NAMING)
  GeneralSection.tsx              (new) + .test.tsx
  SettingsLayout.tsx     (modify: add 4 tabs)
  SettingsLayout.test.tsx (modify: assert new tab hrefs)
web/src/app/routes.tsx   (modify: add 4 child routes)
web/dist/**              (rebuilt in final task)
```

---

### Task 1: Backend — root-folder in-use (409) and not-found (404) delete

**Files:**
- Modify: `internal/core/store/media_store.go` (add `ErrRootFolderInUse`; rewrite `DeleteRootFolder`)
- Modify: `internal/core/store/media_store_test.go`
- Modify: `internal/media/api.go` (`deleteRootFolder` handler)
- Modify: `internal/media/api_test.go`

**Interfaces:**
- Consumes: existing `store.ErrNotFound`, `store.CreateRootFolder(ctx, path) (int64, error)`, series/movies tables with `root_folder_id`.
- Produces: `store.ErrRootFolderInUse error`; `DeleteRootFolder` returns `ErrRootFolderInUse` when referenced, `store.ErrNotFound` when 0 rows deleted, `nil` on success. API `DELETE /rootfolder/{id}` → 409 `conflict` / 404 `not_found` / 200.

- [ ] **Step 1: Write the failing store test**

Add to `internal/core/store/media_store_test.go` (follow the existing test setup in that file for obtaining a `*Store` — reuse whatever helper the other tests use, e.g. `newTestStore(t)`):

```go
func TestDeleteRootFolderInUseAndMissing(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t) // reuse the file's existing store test helper

	// Unused folder deletes cleanly.
	id, err := st.CreateRootFolder(ctx, "/data/unused")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.DeleteRootFolder(ctx, id); err != nil {
		t.Fatalf("delete unused: %v", err)
	}

	// Missing id → ErrNotFound.
	if err := st.DeleteRootFolder(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}

	// In-use folder → ErrRootFolderInUse.
	inUse, err := st.CreateRootFolder(ctx, "/data/inuse")
	if err != nil {
		t.Fatalf("create inuse: %v", err)
	}
	if _, err := st.CreateSeries(ctx, Series{TMDBID: 555, Title: "Ref", RootFolderID: &inUse}); err != nil {
		t.Fatalf("create series: %v", err)
	}
	if err := st.DeleteRootFolder(ctx, inUse); !errors.Is(err, ErrRootFolderInUse) {
		t.Fatalf("delete in-use: want ErrRootFolderInUse, got %v", err)
	}
}
```

Note for implementer: confirm the exact `CreateSeries` signature/return in `media_store.go` and the test-store helper name already used in `media_store_test.go`; adapt the two constructor calls to match (the assertions are the point). Ensure `errors` is imported in the test file.

- [ ] **Step 2: Run the test, verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/core/store/ -run TestDeleteRootFolderInUseAndMissing -v`
Expected: FAIL — `ErrRootFolderInUse` undefined, and/or missing-id returns nil instead of ErrNotFound.

- [ ] **Step 3: Implement the store change**

In `internal/core/store/media_store.go`, add the sentinel near the other root-folder code (top-level `var`):

```go
// ErrRootFolderInUse is returned by DeleteRootFolder when a series or movie
// still references the root folder.
var ErrRootFolderInUse = errors.New("store: root folder in use")
```

Replace the existing `DeleteRootFolder` body (mirrors `DeleteQualityProfile`):

```go
func (s *Store) DeleteRootFolder(ctx context.Context, id int64) error {
	var refs int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM series WHERE root_folder_id = ?) +
		        (SELECT COUNT(*) FROM movies WHERE root_folder_id = ?)`, id, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrRootFolderInUse
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM root_folders WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

Confirm `errors` is imported in `media_store.go` (it already uses `time`; add `"errors"` to the import block if absent).

- [ ] **Step 4: Run the store test, verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/core/store/ -run TestDeleteRootFolderInUseAndMissing -v`
Expected: PASS.

- [ ] **Step 5: Write the failing API test**

In `internal/media/api_test.go`, add (adapt request/router setup to the file's existing helper — the other API tests there already build a router and a store; reuse that harness):

```go
func TestDeleteRootFolderStatuses(t *testing.T) {
	// Build the media API + store using this file's existing test harness.
	env := newAPITestEnv(t) // reuse whatever the file already uses to get {router, store}

	// Missing → 404.
	rec := env.do(t, http.MethodDelete, "/api/v1/rootfolder/99999", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing: want 404, got %d", rec.Code)
	}

	// In-use → 409.
	rfID, _ := env.store.CreateRootFolder(context.Background(), "/data/x")
	_, _ = env.store.CreateMovie(context.Background(), store.Movie{TMDBID: 777, Title: "M", RootFolderID: &rfID})
	rec = env.do(t, http.MethodDelete, "/api/v1/rootfolder/"+strconv.FormatInt(rfID, 10), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("in-use: want 409, got %d", rec.Code)
	}

	// Unused → 200.
	rf2, _ := env.store.CreateRootFolder(context.Background(), "/data/y")
	rec = env.do(t, http.MethodDelete, "/api/v1/rootfolder/"+strconv.FormatInt(rf2, 10), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("unused: want 200, got %d", rec.Code)
	}
}
```

Note: the helper names (`newAPITestEnv`, `env.do`, `env.store`) are placeholders — wire to the actual harness already present in `internal/media/api_test.go`. Adapt `CreateMovie` to its real signature.

- [ ] **Step 6: Run the API test, verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/media/ -run TestDeleteRootFolderStatuses -v`
Expected: FAIL — in-use returns 500 (current handler), missing returns 500.

- [ ] **Step 7: Map the errors in the handler**

In `internal/media/api.go`, replace the body of `deleteRootFolder` so it distinguishes the store errors (inline, consistent with the file's other `errors.Is(err, store.ErrNotFound)` checks):

```go
func (a *API) deleteRootFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	err := a.store.DeleteRootFolder(r.Context(), id)
	switch {
	case err == nil:
		api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case errors.Is(err, store.ErrRootFolderInUse):
		api.WriteError(w, http.StatusConflict, "conflict", "root folder is in use")
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "root folder not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete root folder")
	}
}
```

Confirm `errors` and `store` are already imported in `api.go` (they are — `writeMediaError` uses both).

- [ ] **Step 8: Run both tests, verify pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/media/ ./internal/core/store/ -run 'TestDeleteRootFolder' -v`
Expected: PASS.

- [ ] **Step 9: Full backend verify**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go build ./... && go vet ./... && go test ./...`
Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/core/store/media_store.go internal/core/store/media_store_test.go internal/media/api.go internal/media/api_test.go
git commit -m "fix(6-3b): root folder delete returns 409 in-use / 404 not-found"
```

---

### Task 2: Quality-profile types + pure form helpers

**Files:**
- Create: `web/src/features/settings/qualityTypes.ts`
- Create: `web/src/features/settings/qualityForm.ts`
- Test: `web/src/features/settings/qualityForm.test.ts`

**Interfaces:**
- Produces:
  - `QualityDefinition { id:number; name:string; source:string; resolution:string; rank:number }`
  - `QualityProfileItem { qualityId:number; allowed:boolean }`
  - `QualityProfile { id:number; name:string; cutoffQualityId:number; upgradeAllowed:boolean; items:QualityProfileItem[]; createdAt:string }`
  - `ProfilePayload { name:string; cutoffQualityId:number; upgradeAllowed:boolean; items:QualityProfileItem[] }`
  - `ProfileFormState { name:string; allowed:Record<number,boolean>; cutoffQualityId:number; upgradeAllowed:boolean }`
  - `defaultNewProfile(defs:QualityDefinition[]): ProfileFormState`
  - `formStateFromProfile(p:QualityProfile, defs:QualityDefinition[]): ProfileFormState`
  - `buildProfileItems(allowed:Record<number,boolean>, defs:QualityDefinition[]): QualityProfileItem[]` — one entry per def, in `defs` order.
  - `buildProfilePayload(form:ProfileFormState, defs:QualityDefinition[]): ProfilePayload`
  - `resolveCutoff(allowed:Record<number,boolean>, current:number, defs:QualityDefinition[]): number` — keep `current` if still allowed; else the highest-rank allowed id; else `0`.
  - `isProfileFormValid(form:ProfileFormState): boolean` — name non-empty (trimmed), ≥1 allowed, cutoff allowed.

- [ ] **Step 1: Write the failing test**

`web/src/features/settings/qualityForm.test.ts`:

```ts
import { describe, it, expect } from "vitest"
import {
  buildProfileItems, buildProfilePayload, resolveCutoff, isProfileFormValid,
  defaultNewProfile, formStateFromProfile,
} from "./qualityForm"
import type { QualityDefinition, QualityProfile } from "./qualityTypes"

const defs: QualityDefinition[] = [
  { id: 0, name: "Unknown", source: "unknown", resolution: "unknown", rank: 0 },
  { id: 6, name: "WEBDL-720p", source: "webdl", resolution: "720p", rank: 1 },
  { id: 7, name: "WEBDL-1080p", source: "webdl", resolution: "1080p", rank: 2 },
]

describe("qualityForm", () => {
  it("builds one item per definition in definition order", () => {
    const items = buildProfileItems({ 7: true }, defs)
    expect(items).toEqual([
      { qualityId: 0, allowed: false },
      { qualityId: 6, allowed: false },
      { qualityId: 7, allowed: true },
    ])
  })

  it("builds a payload from form state", () => {
    const payload = buildProfilePayload(
      { name: "HD", allowed: { 6: true, 7: true }, cutoffQualityId: 7, upgradeAllowed: true },
      defs,
    )
    expect(payload).toEqual({
      name: "HD", cutoffQualityId: 7, upgradeAllowed: true,
      items: [
        { qualityId: 0, allowed: false },
        { qualityId: 6, allowed: true },
        { qualityId: 7, allowed: true },
      ],
    })
  })

  it("keeps a still-allowed cutoff", () => {
    expect(resolveCutoff({ 6: true, 7: true }, 7, defs)).toBe(7)
  })

  it("moves cutoff to the highest allowed when current is disallowed", () => {
    expect(resolveCutoff({ 6: true }, 7, defs)).toBe(6)
  })

  it("returns 0 cutoff when nothing is allowed", () => {
    expect(resolveCutoff({}, 7, defs)).toBe(0)
  })

  it("validates name, at-least-one-allowed, cutoff-allowed", () => {
    expect(isProfileFormValid({ name: "x", allowed: { 7: true }, cutoffQualityId: 7, upgradeAllowed: false })).toBe(true)
    expect(isProfileFormValid({ name: " ", allowed: { 7: true }, cutoffQualityId: 7, upgradeAllowed: false })).toBe(false)
    expect(isProfileFormValid({ name: "x", allowed: {}, cutoffQualityId: 0, upgradeAllowed: false })).toBe(false)
    expect(isProfileFormValid({ name: "x", allowed: { 6: true }, cutoffQualityId: 7, upgradeAllowed: false })).toBe(false)
  })

  it("round-trips a profile into form state (allowed map + cutoff)", () => {
    const p: QualityProfile = {
      id: 1, name: "HD", cutoffQualityId: 7, upgradeAllowed: true, createdAt: "",
      items: [{ qualityId: 6, allowed: true }, { qualityId: 7, allowed: true }, { qualityId: 0, allowed: false }],
    }
    const fs = formStateFromProfile(p, defs)
    expect(fs.name).toBe("HD")
    expect(fs.allowed[7]).toBe(true)
    expect(fs.allowed[0]).toBeFalsy()
    expect(fs.cutoffQualityId).toBe(7)
    expect(fs.upgradeAllowed).toBe(true)
  })

  it("default new profile is valid", () => {
    expect(isProfileFormValid(defaultNewProfile(defs))).toBe(true)
  })
})
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `cd web && npx vitest run src/features/settings/qualityForm.test.ts`
Expected: FAIL — module `./qualityForm` / `./qualityTypes` not found.

- [ ] **Step 3: Implement the types**

`web/src/features/settings/qualityTypes.ts`:

```ts
export type QualityDefinition = {
  id: number
  name: string
  source: string
  resolution: string
  rank: number
}

export type QualityProfileItem = { qualityId: number; allowed: boolean }

export type QualityProfile = {
  id: number
  name: string
  cutoffQualityId: number
  upgradeAllowed: boolean
  items: QualityProfileItem[]
  createdAt: string
}

export type ProfilePayload = {
  name: string
  cutoffQualityId: number
  upgradeAllowed: boolean
  items: QualityProfileItem[]
}

export type ProfileFormState = {
  name: string
  allowed: Record<number, boolean>
  cutoffQualityId: number
  upgradeAllowed: boolean
}
```

- [ ] **Step 4: Implement the helpers**

`web/src/features/settings/qualityForm.ts`:

```ts
import type {
  ProfileFormState, ProfilePayload, QualityDefinition, QualityProfile, QualityProfileItem,
} from "./qualityTypes"

export function buildProfileItems(
  allowed: Record<number, boolean>,
  defs: QualityDefinition[],
): QualityProfileItem[] {
  return defs.map((d) => ({ qualityId: d.id, allowed: !!allowed[d.id] }))
}

export function buildProfilePayload(form: ProfileFormState, defs: QualityDefinition[]): ProfilePayload {
  return {
    name: form.name.trim(),
    cutoffQualityId: form.cutoffQualityId,
    upgradeAllowed: form.upgradeAllowed,
    items: buildProfileItems(form.allowed, defs),
  }
}

// Keep the current cutoff if it is still allowed; otherwise the highest-rank
// allowed quality; otherwise 0.
export function resolveCutoff(
  allowed: Record<number, boolean>,
  current: number,
  defs: QualityDefinition[],
): number {
  if (allowed[current]) return current
  const allowedDefs = defs.filter((d) => allowed[d.id])
  if (allowedDefs.length === 0) return 0
  return allowedDefs.reduce((hi, d) => (d.rank > hi.rank ? d : hi), allowedDefs[0]).id
}

export function isProfileFormValid(form: ProfileFormState): boolean {
  if (form.name.trim() === "") return false
  const anyAllowed = Object.values(form.allowed).some(Boolean)
  if (!anyAllowed) return false
  return !!form.allowed[form.cutoffQualityId]
}

export function formStateFromProfile(p: QualityProfile, defs: QualityDefinition[]): ProfileFormState {
  const allowed: Record<number, boolean> = {}
  for (const d of defs) allowed[d.id] = false
  for (const it of p.items) allowed[it.qualityId] = it.allowed
  return {
    name: p.name,
    allowed,
    cutoffQualityId: p.cutoffQualityId,
    upgradeAllowed: p.upgradeAllowed,
  }
}

// A sensible baseline: allow every 1080p-and-below WEBDL/Bluray/HDTV/SDTV
// quality, cutoff at the highest allowed, upgrades on. Falls back gracefully
// for an unexpected ladder.
export function defaultNewProfile(defs: QualityDefinition[]): ProfileFormState {
  const allowed: Record<number, boolean> = {}
  for (const d of defs) {
    allowed[d.id] = d.resolution === "480p" || d.resolution === "720p" || d.resolution === "1080p"
  }
  if (!Object.values(allowed).some(Boolean) && defs.length > 0) {
    // Unexpected ladder — allow the single highest so the form is valid.
    const top = defs.reduce((hi, d) => (d.rank > hi.rank ? d : hi), defs[0])
    allowed[top.id] = true
  }
  const cutoff = resolveCutoff(allowed, -1, defs)
  return { name: "", allowed, cutoffQualityId: cutoff, upgradeAllowed: true }
}
```

- [ ] **Step 5: Run the test, verify it passes**

Run: `cd web && npx vitest run src/features/settings/qualityForm.test.ts`
Expected: PASS (all cases).

- [ ] **Step 6: Typecheck + commit**

```bash
cd web && npx tsc -b
```
Expected: exit 0.

```bash
git add web/src/features/settings/qualityTypes.ts web/src/features/settings/qualityForm.ts web/src/features/settings/qualityForm.test.ts
git commit -m "feat(6-3b): quality-profile types + pure form helpers"
```

---

### Task 3: Quality API hooks + QualityProfilesSection + ProfileDialog

**Files:**
- Create: `web/src/features/settings/qualityApi.ts`
- Create: `web/src/features/settings/ProfileDialog.tsx` + `ProfileDialog.test.tsx`
- Create: `web/src/features/settings/QualityProfilesSection.tsx` + `QualityProfilesSection.test.tsx`

**Interfaces:**
- Consumes: `qualityForm.ts` helpers; `qualityTypes.ts`; `apiGet/apiPost/apiPut/apiDelete`, `ApiError`; `useToast`; `Dialog`/`DialogTitle`.
- Produces:
  - `qualityKeys = { profiles:["settings","quality","profiles"], definitions:["settings","quality","definitions"] }`
  - `useQualityProfiles()` → query of `QualityProfile[]`
  - `useQualityDefinitions()` → query of `QualityDefinition[]`
  - `useSaveProfile()` → mutation `({payload:ProfilePayload, id?:number})`
  - `useDeleteProfile()` → mutation `(id:number)`
  - `<QualityProfilesSection />`, `<ProfileDialog open onOpenChange existing? />`

- [ ] **Step 1: Implement the hooks (no separate test; verified via section tests + tsc)**

`web/src/features/settings/qualityApi.ts`:

```ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type { ProfilePayload, QualityDefinition, QualityProfile } from "./qualityTypes"

export const qualityKeys = {
  profiles: ["settings", "quality", "profiles"] as const,
  definitions: ["settings", "quality", "definitions"] as const,
}

export function useQualityProfiles() {
  return useQuery({ queryKey: qualityKeys.profiles, queryFn: () => apiGet<QualityProfile[]>("/qualityprofile") })
}

export function useQualityDefinitions() {
  return useQuery({ queryKey: qualityKeys.definitions, queryFn: () => apiGet<QualityDefinition[]>("/quality/definitions") })
}

export function useSaveProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ payload, id }: { payload: ProfilePayload; id?: number }) =>
      id == null
        ? apiPost<QualityProfile>("/qualityprofile", payload)
        : apiPut<{ ok: boolean }>(`/qualityprofile/${id}`, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: qualityKeys.profiles }),
  })
}

export function useDeleteProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/qualityprofile/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: qualityKeys.profiles }),
  })
}
```

- [ ] **Step 2: Write the failing ProfileDialog test**

`web/src/features/settings/ProfileDialog.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ProfileDialog } from "./ProfileDialog"
import * as api from "./qualityApi"

vi.mock("./qualityApi", async (orig) => {
  const actual = await orig<typeof import("./qualityApi")>()
  return { ...actual, useQualityDefinitions: vi.fn(), useSaveProfile: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

const defs = [
  { id: 6, name: "WEBDL-720p", source: "webdl", resolution: "720p", rank: 1 },
  { id: 7, name: "WEBDL-1080p", source: "webdl", resolution: "1080p", rank: 2 },
]

function saveMut(mutate = vi.fn()) {
  return { mutate, isPending: false } as unknown as never
}

function renderDialog(save = vi.fn()) {
  vi.mocked(api.useQualityDefinitions).mockReturnValue({ data: defs, isLoading: false } as never)
  vi.mocked(api.useSaveProfile).mockReturnValue(saveMut(save))
  render(<ToastProvider><ProfileDialog open onOpenChange={() => {}} /></ToastProvider>)
}

describe("ProfileDialog", () => {
  it("renders a checkbox per quality and a cutoff option per allowed quality", async () => {
    renderDialog()
    expect(screen.getByLabelText("WEBDL-720p")).toBeInTheDocument()
    expect(screen.getByLabelText("WEBDL-1080p")).toBeInTheDocument()
  })

  it("saves a payload with all items in ladder order and the chosen cutoff", async () => {
    const save = vi.fn()
    renderDialog(save)
    await userEvent.type(screen.getByLabelText(/name/i), "HD")
    // defaults already allow 720p+1080p; save.
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({
        payload: expect.objectContaining({
          name: "HD",
          items: [
            { qualityId: 6, allowed: true },
            { qualityId: 7, allowed: true },
          ],
          cutoffQualityId: expect.any(Number),
          upgradeAllowed: expect.any(Boolean),
        }),
      }),
      expect.anything(),
    )
  })

  it("disables save when name is empty", () => {
    renderDialog()
    expect(screen.getByRole("button", { name: /save/i })).toBeDisabled()
  })
})
```

- [ ] **Step 3: Run it, verify it fails**

Run: `cd web && npx vitest run src/features/settings/ProfileDialog.test.tsx`
Expected: FAIL — `./ProfileDialog` not found.

- [ ] **Step 4: Implement ProfileDialog**

`web/src/features/settings/ProfileDialog.tsx`:

```tsx
import { useMemo, useState } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { useQualityDefinitions, useSaveProfile } from "./qualityApi"
import {
  buildProfilePayload, defaultNewProfile, formStateFromProfile, isProfileFormValid, resolveCutoff,
} from "./qualityForm"
import type { ProfileFormState, QualityProfile } from "./qualityTypes"

export function ProfileDialog({
  existing, open, onOpenChange,
}: {
  existing?: QualityProfile
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const { toast } = useToast()
  const defsQ = useQualityDefinitions()
  const save = useSaveProfile()
  const defs = useMemo(() => defsQ.data ?? [], [defsQ.data])

  const [form, setForm] = useState<ProfileFormState | null>(null)
  const [initialized, setInitialized] = useState(false)
  if (!initialized && defs.length > 0) {
    setForm(existing ? formStateFromProfile(existing, defs) : defaultNewProfile(defs))
    setInitialized(true)
  }

  if (!form) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogTitle>{existing ? "Edit Quality Profile" : "Add Quality Profile"}</DialogTitle>
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      </Dialog>
    )
  }

  const toggle = (id: number, on: boolean) => {
    const allowed = { ...form.allowed, [id]: on }
    setForm({ ...form, allowed, cutoffQualityId: resolveCutoff(allowed, form.cutoffQualityId, defs) })
  }

  const valid = isProfileFormValid(form)
  const allowedDefs = defs.filter((d) => form.allowed[d.id])

  const onSave = () => {
    save.mutate(
      { payload: buildProfilePayload(form, defs), id: existing?.id },
      {
        onSuccess: () => { toast("Saved"); onOpenChange(false) },
        onError: (e) => toast(e instanceof ApiError ? e.message : "Save failed", { variant: "error" }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>{existing ? "Edit Quality Profile" : "Add Quality Profile"}</DialogTitle>
      <div className="flex flex-col gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span>Name</span>
          <input
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </label>

        <fieldset className="flex flex-col gap-1">
          <legend className="mb-1 text-sm font-medium">Qualities</legend>
          {defs.map((d) => (
            <label key={d.id} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                aria-label={d.name}
                checked={!!form.allowed[d.id]}
                onChange={(e) => toggle(d.id, e.target.checked)}
              />
              <span>{d.name}</span>
            </label>
          ))}
        </fieldset>

        <label className="flex flex-col gap-1 text-sm">
          <span>Cutoff</span>
          <select
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.cutoffQualityId}
            onChange={(e) => setForm({ ...form, cutoffQualityId: Number(e.target.value) })}
          >
            {allowedDefs.map((d) => (
              <option key={d.id} value={d.id}>{d.name}</option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={form.upgradeAllowed}
            onChange={(e) => setForm({ ...form, upgradeAllowed: e.target.checked })}
          />
          <span>Upgrades allowed</span>
        </label>

        <div className="mt-2 flex justify-end gap-2">
          <button className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm" onClick={() => onOpenChange(false)}>
            Cancel
          </button>
          <button
            disabled={!valid || save.isPending}
            onClick={onSave}
            className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
          >
            Save
          </button>
        </div>
      </div>
    </Dialog>
  )
}
```

- [ ] **Step 5: Run ProfileDialog test, verify pass**

Run: `cd web && npx vitest run src/features/settings/ProfileDialog.test.tsx`
Expected: PASS.

- [ ] **Step 6: Write the failing QualityProfilesSection test**

`web/src/features/settings/QualityProfilesSection.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { QualityProfilesSection } from "./QualityProfilesSection"
import * as api from "./qualityApi"

vi.mock("./qualityApi", async (orig) => {
  const actual = await orig<typeof import("./qualityApi")>()
  return { ...actual, useQualityProfiles: vi.fn(), useDeleteProfile: vi.fn() }
})
vi.mock("./ProfileDialog", () => ({ ProfileDialog: () => <div data-testid="dialog" /> }))
beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

const profile = {
  id: 1, name: "HD-1080p", cutoffQualityId: 7, upgradeAllowed: true, createdAt: "",
  items: [{ qualityId: 7, allowed: true }],
}

describe("QualityProfilesSection", () => {
  it("lists profiles", () => {
    vi.mocked(api.useQualityProfiles).mockReturnValue({ data: [profile], isLoading: false, isError: false } as never)
    vi.mocked(api.useDeleteProfile).mockReturnValue(mut())
    render(<ToastProvider><QualityProfilesSection /></ToastProvider>)
    expect(screen.getByText("HD-1080p")).toBeInTheDocument()
  })

  it("shows an in-use toast on a 409 delete", async () => {
    const mutate = vi.fn((_id, opts) => opts.onError(new ApiError(409, "conflict", "in use")))
    vi.mocked(api.useQualityProfiles).mockReturnValue({ data: [profile], isLoading: false, isError: false } as never)
    vi.mocked(api.useDeleteProfile).mockReturnValue(mut({ mutate }))
    vi.spyOn(window, "confirm").mockReturnValue(true)
    render(<ToastProvider><QualityProfilesSection /></ToastProvider>)
    await userEvent.click(screen.getByRole("button", { name: /delete/i }))
    expect(await screen.findByText(/in use/i)).toBeInTheDocument()
  })
})
```

- [ ] **Step 7: Run it, verify it fails**

Run: `cd web && npx vitest run src/features/settings/QualityProfilesSection.test.tsx`
Expected: FAIL — `./QualityProfilesSection` not found.

- [ ] **Step 8: Implement QualityProfilesSection**

`web/src/features/settings/QualityProfilesSection.tsx`:

```tsx
import { useState } from "react"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { ProfileDialog } from "./ProfileDialog"
import { useQualityProfiles, useDeleteProfile } from "./qualityApi"
import type { QualityProfile } from "./qualityTypes"

export function QualityProfilesSection() {
  const { toast } = useToast()
  const q = useQualityProfiles()
  const del = useDeleteProfile()
  const [addOpen, setAddOpen] = useState(false)
  const [editing, setEditing] = useState<QualityProfile | null>(null)
  const rows = q.data ?? []

  const onDelete = (p: QualityProfile) => {
    if (!confirm(`Delete ${p.name}?`)) return
    del.mutate(p.id, {
      onSuccess: () => toast("Deleted"),
      onError: (e) =>
        toast(
          e instanceof ApiError && e.status === 409 ? "Profile is in use" : "Delete failed",
          { variant: "error" },
        ),
    })
  }

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold">Quality Profiles</h2>
        <button
          onClick={() => setAddOpen(true)}
          className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white"
        >
          + Add
        </button>
      </div>

      {q.isLoading ? (
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      ) : q.isError ? (
        <p className="text-sm text-[var(--color-warn)]">Failed to load.</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-[var(--color-muted)]">No quality profiles — click Add to create one.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((p) => {
            const cutoff = p.items.find((it) => it.qualityId === p.cutoffQualityId)
            const allowedCount = p.items.filter((it) => it.allowed).length
            return (
              <li
                key={p.id}
                className="flex items-center gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="font-medium">{p.name}</div>
                  <div className="text-xs text-[var(--color-muted)]">
                    {allowedCount} qualities · cutoff #{p.cutoffQualityId} · upgrades {p.upgradeAllowed ? "on" : "off"}
                    {cutoff ? "" : ""}
                  </div>
                </div>
                <button onClick={() => setEditing(p)} className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm">
                  Edit
                </button>
                <button
                  onClick={() => onDelete(p)}
                  className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
                >
                  Delete
                </button>
              </li>
            )
          })}
        </ul>
      )}

      {addOpen && <ProfileDialog open={addOpen} onOpenChange={setAddOpen} />}
      {editing && (
        <ProfileDialog existing={editing} open={editing != null} onOpenChange={(o) => { if (!o) setEditing(null) }} />
      )}
    </div>
  )
}
```

- [ ] **Step 9: Run section test, verify pass; typecheck**

Run: `cd web && npx vitest run src/features/settings/QualityProfilesSection.test.tsx src/features/settings/ProfileDialog.test.tsx && npx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 10: Commit**

```bash
git add web/src/features/settings/qualityApi.ts web/src/features/settings/ProfileDialog.tsx web/src/features/settings/ProfileDialog.test.tsx web/src/features/settings/QualityProfilesSection.tsx web/src/features/settings/QualityProfilesSection.test.tsx
git commit -m "feat(6-3b): quality profiles list + editor dialog"
```

---

### Task 4: config types + root-folder hooks + RootFoldersSection

**Files:**
- Create: `web/src/features/settings/configTypes.ts`
- Create: `web/src/features/settings/configApi.ts` (root-folder hooks now; naming/automation/system added in Tasks 5–6)
- Create: `web/src/features/settings/RootFoldersSection.tsx` + `RootFoldersSection.test.tsx`

**Interfaces:**
- Produces:
  - `RootFolder { id:number; path:string; createdAt:string }`
  - `NamingConfig { seriesFolder; seasonFolder; episodeFile; movieFolder; movieFile: string }`
  - `AutomationConfig { missingSearchIntervalHours; missingSearchBatchSize:number; rssSyncEnabled:boolean; rssSyncIntervalMinutes:number; upgradeSearchEnabled:boolean; upgradeSearchIntervalHours; upgradeSearchBatchSize; upgradeGrabCooldownHours:number }`
  - `configKeys` (rootFolders/naming/automation/systemStatus keys)
  - `useRootFolders()`, `useAddRootFolder()` (mutation `(path:string)`), `useDeleteRootFolder()` (mutation `(id:number)`)
  - `<RootFoldersSection />`

- [ ] **Step 1: Implement configTypes + configApi (root-folder slice)**

`web/src/features/settings/configTypes.ts`:

```ts
export type RootFolder = { id: number; path: string; createdAt: string }

export type NamingConfig = {
  seriesFolder: string
  seasonFolder: string
  episodeFile: string
  movieFolder: string
  movieFile: string
}

export type AutomationConfig = {
  missingSearchIntervalHours: number
  missingSearchBatchSize: number
  rssSyncEnabled: boolean
  rssSyncIntervalMinutes: number
  upgradeSearchEnabled: boolean
  upgradeSearchIntervalHours: number
  upgradeSearchBatchSize: number
  upgradeGrabCooldownHours: number
}
```

`web/src/features/settings/configApi.ts`:

```ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type { RootFolder } from "./configTypes"

export const configKeys = {
  rootFolders: ["settings", "rootfolders"] as const,
  naming: ["settings", "naming"] as const,
  automation: ["settings", "automation"] as const,
  systemStatus: ["settings", "systemStatus"] as const,
}

export function useRootFolders() {
  return useQuery({ queryKey: configKeys.rootFolders, queryFn: () => apiGet<RootFolder[]>("/rootfolder") })
}

export function useAddRootFolder() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (path: string) => apiPost<RootFolder>("/rootfolder", { path }),
    onSuccess: () => qc.invalidateQueries({ queryKey: configKeys.rootFolders }),
  })
}

export function useDeleteRootFolder() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/rootfolder/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: configKeys.rootFolders }),
  })
}
```

- [ ] **Step 2: Write the failing RootFoldersSection test**

`web/src/features/settings/RootFoldersSection.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { RootFoldersSection } from "./RootFoldersSection"
import * as api from "./configApi"

vi.mock("./configApi", async (orig) => {
  const actual = await orig<typeof import("./configApi")>()
  return { ...actual, useRootFolders: vi.fn(), useAddRootFolder: vi.fn(), useDeleteRootFolder: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

describe("RootFoldersSection", () => {
  it("lists root folders and adds a new path", async () => {
    const add = vi.fn()
    vi.mocked(api.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/tv", createdAt: "" }], isLoading: false, isError: false } as never)
    vi.mocked(api.useAddRootFolder).mockReturnValue(mut({ mutate: add }))
    vi.mocked(api.useDeleteRootFolder).mockReturnValue(mut())
    render(<ToastProvider><RootFoldersSection /></ToastProvider>)
    expect(screen.getByText("/media/tv")).toBeInTheDocument()
    await userEvent.type(screen.getByPlaceholderText(/path/i), "/media/movies")
    await userEvent.click(screen.getByRole("button", { name: /add/i }))
    expect(add).toHaveBeenCalledWith("/media/movies", expect.anything())
  })

  it("shows an in-use toast on a 409 delete", async () => {
    const mutate = vi.fn((_id, opts) => opts.onError(new ApiError(409, "conflict", "in use")))
    vi.mocked(api.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/tv", createdAt: "" }], isLoading: false, isError: false } as never)
    vi.mocked(api.useAddRootFolder).mockReturnValue(mut())
    vi.mocked(api.useDeleteRootFolder).mockReturnValue(mut({ mutate }))
    vi.spyOn(window, "confirm").mockReturnValue(true)
    render(<ToastProvider><RootFoldersSection /></ToastProvider>)
    await userEvent.click(screen.getByRole("button", { name: /delete/i }))
    expect(await screen.findByText(/in use/i)).toBeInTheDocument()
  })
})
```

- [ ] **Step 3: Run it, verify it fails**

Run: `cd web && npx vitest run src/features/settings/RootFoldersSection.test.tsx`
Expected: FAIL — `./RootFoldersSection` not found.

- [ ] **Step 4: Implement RootFoldersSection**

`web/src/features/settings/RootFoldersSection.tsx`:

```tsx
import { useState } from "react"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { useRootFolders, useAddRootFolder, useDeleteRootFolder } from "./configApi"
import type { RootFolder } from "./configTypes"

export function RootFoldersSection() {
  const { toast } = useToast()
  const q = useRootFolders()
  const add = useAddRootFolder()
  const del = useDeleteRootFolder()
  const [path, setPath] = useState("")
  const rows = q.data ?? []

  const onAdd = () => {
    const p = path.trim()
    if (p === "") return
    add.mutate(p, {
      onSuccess: () => { toast("Added"); setPath("") },
      onError: (e) => toast(e instanceof ApiError ? e.message : "Add failed", { variant: "error" }),
    })
  }

  const onDelete = (rf: RootFolder) => {
    if (!confirm(`Delete ${rf.path}?`)) return
    del.mutate(rf.id, {
      onSuccess: () => toast("Deleted"),
      onError: (e) =>
        toast(
          e instanceof ApiError && e.status === 409
            ? "Root folder is in use by a movie or series"
            : "Delete failed",
          { variant: "error" },
        ),
    })
  }

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">Root Folders</h2>

      <div className="mb-4 flex gap-2">
        <input
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="/path/to/library"
          className="flex-1 rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1.5 text-sm"
        />
        <button
          onClick={onAdd}
          disabled={path.trim() === "" || add.isPending}
          className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
        >
          Add
        </button>
      </div>

      {q.isLoading ? (
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      ) : q.isError ? (
        <p className="text-sm text-[var(--color-warn)]">Failed to load.</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-[var(--color-muted)]">No root folders — add one above.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((rf) => (
            <li
              key={rf.id}
              className="flex items-center gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] px-4 py-3"
            >
              <span className="min-w-0 flex-1 truncate text-sm">{rf.path}</span>
              <button
                onClick={() => onDelete(rf)}
                className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
              >
                Delete
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
```

- [ ] **Step 5: Run test, verify pass; typecheck**

Run: `cd web && npx vitest run src/features/settings/RootFoldersSection.test.tsx && npx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/settings/configTypes.ts web/src/features/settings/configApi.ts web/src/features/settings/RootFoldersSection.tsx web/src/features/settings/RootFoldersSection.test.tsx
git commit -m "feat(6-3b): root folders section (add/delete, 409 in-use toast)"
```

---

### Task 5: Naming hooks + NamingSection + token legend

**Files:**
- Modify: `web/src/features/settings/configApi.ts` (add naming hooks)
- Create: `web/src/features/settings/NamingSection.tsx` + `NamingSection.test.tsx`

**Interfaces:**
- Consumes: `NamingConfig` from `configTypes.ts`; `configKeys.naming`.
- Produces: `useNamingConfig()` → query `NamingConfig`; `useSaveNaming()` → mutation `(cfg:NamingConfig)`; `<NamingSection />` (exports `DEFAULT_NAMING`, `NAMING_TOKENS` internally).

- [ ] **Step 1: Add naming hooks to configApi.ts**

Append to `web/src/features/settings/configApi.ts` (add `NamingConfig` to the type import):

```ts
// add to imports:  import type { NamingConfig, RootFolder } from "./configTypes"

export function useNamingConfig() {
  return useQuery({ queryKey: configKeys.naming, queryFn: () => apiGet<NamingConfig>("/config/naming") })
}

export function useSaveNaming() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (cfg: NamingConfig) => apiPut<NamingConfig>("/config/naming", cfg),
    onSuccess: () => qc.invalidateQueries({ queryKey: configKeys.naming }),
  })
}
```

- [ ] **Step 2: Write the failing NamingSection test**

`web/src/features/settings/NamingSection.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { NamingSection } from "./NamingSection"
import * as api from "./configApi"

vi.mock("./configApi", async (orig) => {
  const actual = await orig<typeof import("./configApi")>()
  return { ...actual, useNamingConfig: vi.fn(), useSaveNaming: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

const cfg = {
  seriesFolder: "{Series Title}", seasonFolder: "Season {season:00}",
  episodeFile: "E", movieFolder: "{Movie Title} ({year})", movieFile: "M",
}

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

describe("NamingSection", () => {
  it("seeds inputs from the config and saves edits", async () => {
    const save = vi.fn()
    vi.mocked(api.useNamingConfig).mockReturnValue({ data: cfg, isLoading: false, isError: false } as never)
    vi.mocked(api.useSaveNaming).mockReturnValue(mut({ mutate: save }))
    render(<ToastProvider><NamingSection /></ToastProvider>)
    const series = screen.getByLabelText(/series folder/i)
    expect(series).toHaveValue("{Series Title}")
    // Brace-free text: userEvent treats { and } as special key sequences.
    await userEvent.clear(series)
    await userEvent.type(series, "Custom")
    await userEvent.click(screen.getByRole("button", { name: /^save$/i }))
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({ seriesFolder: "Custom" }),
      expect.anything(),
    )
  })

  it("renders the token legend", () => {
    vi.mocked(api.useNamingConfig).mockReturnValue({ data: cfg, isLoading: false, isError: false } as never)
    vi.mocked(api.useSaveNaming).mockReturnValue(mut())
    render(<ToastProvider><NamingSection /></ToastProvider>)
    expect(screen.getByText("{Series Title}", { selector: "code" })).toBeInTheDocument()
    expect(screen.getByText("{season:00}", { selector: "code" })).toBeInTheDocument()
  })
})
```

- [ ] **Step 3: Run it, verify it fails**

Run: `cd web && npx vitest run src/features/settings/NamingSection.test.tsx`
Expected: FAIL — `./NamingSection` not found.

- [ ] **Step 4: Implement NamingSection**

`web/src/features/settings/NamingSection.tsx` (DEFAULT_NAMING mirrors `naming.DefaultConfig()`):

```tsx
import { useState } from "react"
import { useToast } from "@/lib/toast"
import { useNamingConfig, useSaveNaming } from "./configApi"
import type { NamingConfig } from "./configTypes"

const DEFAULT_NAMING: NamingConfig = {
  seriesFolder: "{Series Title}",
  seasonFolder: "Season {season:00}",
  episodeFile: "{Series Title} - S{season:00}E{episode:00} - {Episode Title} [{Quality}]",
  movieFolder: "{Movie Title} ({year})",
  movieFile: "{Movie Title} ({year}) [{Quality}]",
}

const NAMING_TOKENS = [
  "{Series Title}", "{Episode Title}", "{Movie Title}", "{Quality}", "{Release Group}",
  "{season}", "{season:00}", "{episode}", "{episode:00}", "{year}",
]

const FIELDS: { key: keyof NamingConfig; label: string }[] = [
  { key: "seriesFolder", label: "Series Folder" },
  { key: "seasonFolder", label: "Season Folder" },
  { key: "episodeFile", label: "Episode File" },
  { key: "movieFolder", label: "Movie Folder" },
  { key: "movieFile", label: "Movie File" },
]

export function NamingSection() {
  const { toast } = useToast()
  const q = useNamingConfig()
  const save = useSaveNaming()
  const [form, setForm] = useState<NamingConfig | null>(null)
  const [initialized, setInitialized] = useState(false)
  if (!initialized && q.data) {
    setForm(q.data)
    setInitialized(true)
  }

  if (q.isLoading || !form) return <div className="p-6"><p className="text-sm text-[var(--color-muted)]">Loading…</p></div>
  if (q.isError) return <div className="p-6"><p className="text-sm text-[var(--color-warn)]">Failed to load.</p></div>

  const onSave = () => {
    save.mutate(form, {
      onSuccess: (saved) => { setForm(saved); toast("Saved") },
      onError: () => toast("Save failed", { variant: "error" }),
    })
  }

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">Naming</h2>
      <div className="flex max-w-2xl flex-col gap-3">
        {FIELDS.map((f) => (
          <label key={f.key} className="flex flex-col gap-1 text-sm">
            <span>{f.label}</span>
            <input
              value={form[f.key]}
              onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
              className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1.5 font-mono text-xs"
            />
          </label>
        ))}
        <div className="flex gap-2">
          <button
            onClick={onSave}
            disabled={save.isPending}
            className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
          >
            Save
          </button>
          <button
            onClick={() => setForm(DEFAULT_NAMING)}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            Reset to defaults
          </button>
        </div>
      </div>

      <div className="mt-6">
        <h3 className="mb-2 text-sm font-medium">Available tokens</h3>
        <div className="flex flex-wrap gap-2">
          {NAMING_TOKENS.map((t) => (
            <code key={t} className="rounded bg-[var(--color-panel)] px-2 py-0.5 text-xs text-[var(--color-muted)]">{t}</code>
          ))}
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 5: Run test, verify pass; typecheck**

Run: `cd web && npx vitest run src/features/settings/NamingSection.test.tsx && npx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/settings/configApi.ts web/src/features/settings/NamingSection.tsx web/src/features/settings/NamingSection.test.tsx
git commit -m "feat(6-3b): naming section with form, reset, and token legend"
```

---

### Task 6: Automation + system hooks + GeneralSection

**Files:**
- Modify: `web/src/features/settings/configApi.ts` (add automation + system-status hooks)
- Create: `web/src/features/settings/GeneralSection.tsx` + `GeneralSection.test.tsx`

**Interfaces:**
- Consumes: `AutomationConfig` from `configTypes.ts`; `SystemStatus` + `getStatus` from `@/lib/api`; `configKeys.automation` / `configKeys.systemStatus`.
- Produces: `useAutomationConfig()`, `useSaveAutomationConfig()` (mutation `(cfg:AutomationConfig)`), `useSystemStatus()`; `<GeneralSection />`.

- [ ] **Step 1: Add automation + system hooks to configApi.ts**

Append to `web/src/features/settings/configApi.ts` (extend imports: `import type { AutomationConfig, NamingConfig, RootFolder } from "./configTypes"` and `import { apiGet, apiPost, apiPut, apiDelete, getStatus } from "@/lib/api"`):

```ts
export function useAutomationConfig() {
  return useQuery({ queryKey: configKeys.automation, queryFn: () => apiGet<AutomationConfig>("/automation/config") })
}

export function useSaveAutomationConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (cfg: AutomationConfig) => apiPut<AutomationConfig>("/automation/config", cfg),
    onSuccess: () => qc.invalidateQueries({ queryKey: configKeys.automation }),
  })
}

export function useSystemStatus() {
  return useQuery({ queryKey: configKeys.systemStatus, queryFn: () => getStatus() })
}
```

- [ ] **Step 2: Write the failing GeneralSection test**

`web/src/features/settings/GeneralSection.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { GeneralSection } from "./GeneralSection"
import * as api from "./configApi"

vi.mock("./configApi", async (orig) => {
  const actual = await orig<typeof import("./configApi")>()
  return { ...actual, useSystemStatus: vi.fn(), useAutomationConfig: vi.fn(), useSaveAutomationConfig: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

const cfg = {
  missingSearchIntervalHours: 6, missingSearchBatchSize: 100,
  rssSyncEnabled: true, rssSyncIntervalMinutes: 15,
  upgradeSearchEnabled: true, upgradeSearchIntervalHours: 12,
  upgradeSearchBatchSize: 100, upgradeGrabCooldownHours: 168,
}

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

function setup(save = vi.fn()) {
  vi.mocked(api.useSystemStatus).mockReturnValue({ data: { version: "1.2.3", appName: "Nexus", healthy: true, taskCount: 4 }, isLoading: false } as never)
  vi.mocked(api.useAutomationConfig).mockReturnValue({ data: cfg, isLoading: false, isError: false } as never)
  vi.mocked(api.useSaveAutomationConfig).mockReturnValue(mut({ mutate: save }))
  render(<ToastProvider><GeneralSection /></ToastProvider>)
}

describe("GeneralSection", () => {
  it("shows system info", () => {
    setup()
    expect(screen.getByText("1.2.3")).toBeInTheDocument()
    expect(screen.getByText("4")).toBeInTheDocument()
  })

  it("shows the restart caveat", () => {
    setup()
    expect(screen.getByText(/next.*restart/i)).toBeInTheDocument()
  })

  it("saves edited automation config", async () => {
    const save = vi.fn()
    setup(save)
    const batch = screen.getByLabelText(/missing search batch size/i)
    await userEvent.clear(batch)
    await userEvent.type(batch, "50")
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ missingSearchBatchSize: 50 }), expect.anything())
  })
})
```

- [ ] **Step 3: Run it, verify it fails**

Run: `cd web && npx vitest run src/features/settings/GeneralSection.test.tsx`
Expected: FAIL — `./GeneralSection` not found.

- [ ] **Step 4: Implement GeneralSection**

`web/src/features/settings/GeneralSection.tsx`:

```tsx
import { useState } from "react"
import { useToast } from "@/lib/toast"
import { useSystemStatus, useAutomationConfig, useSaveAutomationConfig } from "./configApi"
import type { AutomationConfig } from "./configTypes"

const NUM_FIELDS: { key: keyof AutomationConfig; label: string }[] = [
  { key: "missingSearchIntervalHours", label: "Missing search interval (hours)" },
  { key: "missingSearchBatchSize", label: "Missing search batch size" },
  { key: "rssSyncIntervalMinutes", label: "RSS sync interval (minutes)" },
  { key: "upgradeSearchIntervalHours", label: "Upgrade search interval (hours)" },
  { key: "upgradeSearchBatchSize", label: "Upgrade search batch size" },
  { key: "upgradeGrabCooldownHours", label: "Upgrade grab cooldown (hours)" },
]
const BOOL_FIELDS: { key: keyof AutomationConfig; label: string }[] = [
  { key: "rssSyncEnabled", label: "RSS sync enabled" },
  { key: "upgradeSearchEnabled", label: "Upgrade search enabled" },
]

export function GeneralSection() {
  const { toast } = useToast()
  const statusQ = useSystemStatus()
  const cfgQ = useAutomationConfig()
  const save = useSaveAutomationConfig()
  const [form, setForm] = useState<AutomationConfig | null>(null)
  const [initialized, setInitialized] = useState(false)
  if (!initialized && cfgQ.data) {
    setForm(cfgQ.data)
    setInitialized(true)
  }

  const s = statusQ.data

  const onSave = () => {
    if (!form) return
    // Clamp non-positive numbers to keep parity with the server's defaulting.
    const clamped = { ...form }
    for (const f of NUM_FIELDS) {
      if ((clamped[f.key] as number) <= 0) delete (clamped as Record<string, unknown>)[f.key]
    }
    save.mutate(clamped as AutomationConfig, {
      onSuccess: () => toast("Saved"),
      onError: () => toast("Save failed", { variant: "error" }),
    })
  }

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">General</h2>

      <section className="mb-6 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] p-4">
        <h3 className="mb-2 text-sm font-medium">System Info</h3>
        {statusQ.isLoading || !s ? (
          <p className="text-sm text-[var(--color-muted)]">Loading…</p>
        ) : (
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
            <dt className="text-[var(--color-muted)]">Version</dt><dd>{s.version}</dd>
            <dt className="text-[var(--color-muted)]">App</dt><dd>{s.appName}</dd>
            <dt className="text-[var(--color-muted)]">Healthy</dt><dd>{s.healthy ? "Yes" : "No"}</dd>
            <dt className="text-[var(--color-muted)]">Active tasks</dt><dd>{s.taskCount}</dd>
          </dl>
        )}
      </section>

      <section className="rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] p-4">
        <h3 className="mb-1 text-sm font-medium">Task Scheduling</h3>
        <p className="mb-3 text-xs text-[var(--color-warn)]">
          Interval and enabled changes take effect on the next Nexus restart.
        </p>
        {cfgQ.isLoading || !form ? (
          <p className="text-sm text-[var(--color-muted)]">Loading…</p>
        ) : cfgQ.isError ? (
          <p className="text-sm text-[var(--color-warn)]">Failed to load.</p>
        ) : (
          <div className="flex max-w-md flex-col gap-3">
            {BOOL_FIELDS.map((f) => (
              <label key={f.key} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={form[f.key] as boolean}
                  onChange={(e) => setForm({ ...form, [f.key]: e.target.checked })}
                />
                <span>{f.label}</span>
              </label>
            ))}
            {NUM_FIELDS.map((f) => (
              <label key={f.key} className="flex flex-col gap-1 text-sm">
                <span>{f.label}</span>
                <input
                  type="number"
                  aria-label={f.label}
                  value={form[f.key] as number}
                  onChange={(e) => setForm({ ...form, [f.key]: Number(e.target.value) })}
                  className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1.5"
                />
              </label>
            ))}
            <div>
              <button
                onClick={onSave}
                disabled={save.isPending}
                className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
              >
                Save
              </button>
            </div>
          </div>
        )}
      </section>
    </div>
  )
}
```

Note: the save test types "50" into batch size (a positive value) so the clamp/delete branch does not remove it; the assertion checks `missingSearchBatchSize: 50` survives.

- [ ] **Step 5: Run test, verify pass; typecheck**

Run: `cd web && npx vitest run src/features/settings/GeneralSection.test.tsx && npx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/settings/configApi.ts web/src/features/settings/GeneralSection.tsx web/src/features/settings/GeneralSection.test.tsx
git commit -m "feat(6-3b): general section (system info + task scheduling config)"
```

---

### Task 7: Wire tabs + routes

**Files:**
- Modify: `web/src/features/settings/SettingsLayout.tsx`
- Modify: `web/src/features/settings/SettingsLayout.test.tsx`
- Modify: `web/src/app/routes.tsx`

**Interfaces:**
- Consumes: the four section components from Tasks 3–6.
- Produces: `/settings/qualityprofiles|rootfolders|naming|general` routes + tabs.

- [ ] **Step 1: Update the failing SettingsLayout test**

In `web/src/features/settings/SettingsLayout.test.tsx`, add assertions for the four new tab hrefs (keep the existing two). Example additions (adapt to the file's existing render/query style):

```tsx
expect(screen.getByRole("link", { name: "Quality Profiles" })).toHaveAttribute("href", "/settings/qualityprofiles")
expect(screen.getByRole("link", { name: "Root Folders" })).toHaveAttribute("href", "/settings/rootfolders")
expect(screen.getByRole("link", { name: "Naming" })).toHaveAttribute("href", "/settings/naming")
expect(screen.getByRole("link", { name: "General" })).toHaveAttribute("href", "/settings/general")
```

- [ ] **Step 2: Run it, verify it fails**

Run: `cd web && npx vitest run src/features/settings/SettingsLayout.test.tsx`
Expected: FAIL — new tab links not found.

- [ ] **Step 3: Add the tabs**

In `web/src/features/settings/SettingsLayout.tsx`, extend `TABS`:

```tsx
const TABS: { to: string; label: string }[] = [
  { to: "/settings/indexers", label: "Indexers" },
  { to: "/settings/downloadclients", label: "Download Clients" },
  { to: "/settings/qualityprofiles", label: "Quality Profiles" },
  { to: "/settings/rootfolders", label: "Root Folders" },
  { to: "/settings/naming", label: "Naming" },
  { to: "/settings/general", label: "General" },
]
```

- [ ] **Step 4: Add the routes**

In `web/src/app/routes.tsx`, add imports and child routes under the `settings` route (after `downloadclients`):

```tsx
// imports
import { QualityProfilesSection } from "@/features/settings/QualityProfilesSection"
import { RootFoldersSection } from "@/features/settings/RootFoldersSection"
import { NamingSection } from "@/features/settings/NamingSection"
import { GeneralSection } from "@/features/settings/GeneralSection"

// inside settings children, after the downloadclients entry:
{ path: "qualityprofiles", element: <QualityProfilesSection /> },
{ path: "rootfolders", element: <RootFoldersSection /> },
{ path: "naming", element: <NamingSection /> },
{ path: "general", element: <GeneralSection /> },
```

- [ ] **Step 5: Run the layout test + full suite, verify pass; typecheck**

Run: `cd web && npx vitest run src/features/settings/ && npx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/settings/SettingsLayout.tsx web/src/features/settings/SettingsLayout.test.tsx web/src/app/routes.tsx
git commit -m "feat(6-3b): settings tabs + routes for quality/rootfolders/naming/general"
```

---

### Task 8: Rebuild web/dist + full verification + live browser check

**Files:**
- Modify: `web/dist/**` (build artifacts)

- [ ] **Step 1: Full frontend gate**

Run: `cd web && npx tsc -b && npm test`
Expected: tsc exit 0; all vitest files pass.

- [ ] **Step 2: Rebuild the embedded bundle**

Run: `cd web && npm run build`
Expected: Vite build succeeds, writes `web/dist`.

- [ ] **Step 3: Full backend gate + drift guard**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
go build ./... && go vet ./... && go test ./...
git diff --exit-code web/dist && echo "DIST CLEAN"
```
Expected: all Go pass; `git diff` shows only intended `web/dist` changes now staged (run `git add web/dist` before the drift check if the build changed committed files — the guard is that *after commit* there is no drift). Practically: `npm run build` then `git add web/dist`.

- [ ] **Step 4: Commit the rebuilt bundle**

```bash
git add web/dist
git commit -m "build(6-3b): rebuild embedded web bundle for settings 3b"
```

- [ ] **Step 5: Live browser smoke check**

Build and run the binary, then verify each acceptance criterion in the browser:

```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build -o nexus.exe ./cmd/nexus
NEXUS_ADMIN_PASSWORD=admin NEXUS_DATA_DIR=$(mktemp -d) ./nexus.exe &
```

Open `http://localhost:9494/`, log in `admin`/`admin`, and confirm spec §12 acceptance criteria (a)–(g): all six tabs render/route; create + edit a quality profile; in-use delete shows a clean 409 message for a profile and a root folder (seed an in-use one via the UI: add a root folder, add a movie/series onto it, then attempt delete); add+delete an unused root folder; naming edit persists + reset fills defaults + legend visible; General shows live system info + saves automation config with the restart caveat visible. Stop the server when done.

- [ ] **Step 6: Final full verify (record evidence)**

Run and capture output:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
go build ./... && go vet ./... && go test ./...
cd web && npx tsc -b && npm test
cd .. && git diff --exit-code web/dist && echo "DIST CLEAN"
```
Expected: everything green; DIST CLEAN.

---

## Self-review notes

- **Spec coverage:** §3 tabs/routes → Task 7; §4 quality profiles → Tasks 2–3; §5 root folders + backend 409/404 → Tasks 1 & 4; §6 naming + legend → Task 5; §7 general (system info + task scheduling + restart note) → Task 6; §8 module layout → Tasks 2–7; §9 data layer → hooks across Tasks 3–6; §10 testing/gates → each task + Task 8; §12 acceptance → Task 8 Step 5.
- **Backend scope:** only Task 1 touches Go (store + media api + their tests); no migration.
- **Type consistency:** `ProfilePayload`/`ProfileFormState`/`QualityProfile` defined in Task 2 and consumed unchanged in Task 3; `AutomationConfig`/`NamingConfig`/`RootFolder` defined in Task 4 and extended-by-import in Tasks 5–6; `configKeys` defined once in Task 4.
- **Known adaptation points (flagged in-task, not placeholders):** Task 1 test-harness helper names (`newTestStore`, `newAPITestEnv`, `CreateSeries`/`CreateMovie` signatures) must be matched to what already exists in `media_store_test.go` / `api_test.go` — the assertions are fixed, only the setup calls adapt.
