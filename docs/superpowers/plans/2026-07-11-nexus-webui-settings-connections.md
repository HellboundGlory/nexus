# Nexus Web UI Slice 3a — Settings (Indexers & Download Clients) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `/settings` placeholder with real management UI for Indexers and Download Clients — schema-driven add/edit forms, connection testing, status badges, and delete — plus a minimal backend fix so editing a connection never wipes its stored secret.

**Architecture:** A single generic schema-driven form (`SchemaForm`) renders whatever fields the backend `/schema` endpoint returns; a generic `ConnectionsSection` + `ConnectionDialog` drive both subsystems, parameterized by a `kind` (`"indexer"` | `"downloadclient"`). All risky value-shaping logic (payload building, secret omission, unsaved-vs-saved test-endpoint selection) lives in pure, directly-tested helpers in `payload.ts`. The only backend change is a carry-forward in each `update()` handler: when the incoming `apiKey` is empty, keep the stored one.

**Tech Stack:** Go 1.x (chi, database/sql, SQLite) backend; React 19 + TypeScript + Vite + Tailwind v4 + shadcn/ui + radix-ui + TanStack Query v5 frontend; Vitest + Testing Library + userEvent for FE tests.

## Global Constraints

- Go: prefix every Go command with `export PATH="/c/Program Files/Go/bin:$PATH"`. `-race` is unavailable (no CGO) — use `-count=N` for concurrency, not `-race`.
- Backend build/test: `CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go vet ./...`, `CGO_ENABLED=0 go test ./...` must all pass.
- Module boundaries unchanged: work stays within `internal/indexer` and `internal/downloadclient` (backend) and `web/src` (frontend). No new routes, no migration, no struct/DTO changes.
- Frontend commands run from `web/`: `npm run test` (Vitest), `npx tsc -b` (typecheck), `npm run build` (tsc + vite build).
- `web/dist` is committed and drift-guarded: after any FE source change, rebuild it and ensure `git diff --exit-code web/dist` is clean before the final commit.
- Model: use **sonnet** (never haiku) for any subagent implementers/reviewers in this repo.
- Secrets stay write-only: `store.Indexer.APIKey` / `store.DownloadClient.APIKey` are `json:"-"`. Never add them to any JSON response. The carry-forward reads the stored key server-side only.
- Existing patterns to imitate: API client `web/src/lib/api.ts` (`apiGet/apiPost/apiPut/apiDelete`); toast `web/src/lib/toast.tsx` (`useToast().toast(msg, {variant})`); `Dialog`/`DialogTitle` (`web/src/components/ui/dialog.tsx`); `Select` (`web/src/components/ui/select.tsx`); generic `StatusBadge` (`web/src/features/library/StatusBadge.tsx`, tones `"ok"|"warn"|"muted"`); native checkbox for booleans (as in `AddMediaDialog.tsx`); `confirm(...)` for delete (as in `MovieDetail.tsx`).

---

## Backend surface consumed (reference — already exists, unchanged except the two `update()` fixes)

Both endpoints share the same shape under authed `/api/v1`:

| Purpose      | Indexers                    | Download Clients                  |
|--------------|-----------------------------|-----------------------------------|
| List         | `GET /indexer`              | `GET /downloadclient`             |
| Schema       | `GET /indexer/schema`       | `GET /downloadclient/schema`      |
| Create       | `POST /indexer`             | `POST /downloadclient`            |
| Update       | `PUT /indexer/{id}`         | `PUT /downloadclient/{id}`        |
| Delete       | `DELETE /indexer/{id}`      | `DELETE /downloadclient/{id}`     |
| Test unsaved | `POST /indexer/test`        | `POST /downloadclient/test`       |
| Test saved   | `POST /indexer/{id}/test`   | `POST /downloadclient/{id}/test`  |

- **Schema:** `[{ implementation, protocol, fields: Field[] }]`, `Field = { name, type: "string"|"int"|"int[]"|"bool", required?, default?, label? }`. Field `name`s are exactly the JSON keys the create/update payloads expect. Within each endpoint both implementations share identical field sets (differ only in `protocol` and the `apiKey` `label`).
- **Test response:** always HTTP 200; body `{ ok: true, capabilities? }` or `{ ok: false, error }`. Key off `ok`, not HTTP status.
- **List/get item:** includes `id, name, implementation, enabled, priority, status, lastCheck, failMessage` + per-endpoint config fields. Never includes `apiKey`. Indexer `caps` is `json:"-"` (not exposed) — capabilities come only from the Test response.

---

## Task 1: Backend — indexer `update()` carries forward stored API key

**Files:**
- Modify: `internal/indexer/api.go` (the `update` handler, ~lines 130-153)
- Test: `internal/indexer/api_test.go` (add one test function)

**Interfaces:**
- Consumes: `store.Store.GetIndexer(ctx, id) (store.Indexer, error)` (returns a value; `.APIKey` populated in-process), `store.Store.UpdateIndexer(ctx, store.Indexer) error`.
- Produces: no new exported symbol. Behavior change only: `PUT /indexer/{id}` with empty `apiKey` preserves the stored key.

- [ ] **Step 1: Write the failing test**

Add to `internal/indexer/api_test.go`:

```go
func TestIndexerUpdatePreservesStoredKeyWhenBlank(t *testing.T) {
	st := newTestStore(t)
	svc := NewService(st).WithHTTPClient(http.DefaultClient)
	a := NewAPI(st, svc, http.DefaultClient)
	router := mountedRouter(t, svc, a)

	// Create with a secret key (bad URL is fine: caps discovery is best-effort).
	create, _ := json.Marshal(map[string]any{
		"name": "ix", "implementation": "torznab", "baseUrl": "http://127.0.0.1:1", "apiKey": "SECRET-KEY-123", "enabled": true, "priority": 25,
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/indexer", bytes.NewReader(create)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Update WITHOUT apiKey (rename only).
	update, _ := json.Marshal(map[string]any{
		"name": "ix-renamed", "implementation": "torznab", "baseUrl": "http://127.0.0.1:1", "apiKey": "", "enabled": true, "priority": 30,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/indexer/"+strconv.FormatInt(created.ID, 10), bytes.NewReader(update)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", rec.Code, rec.Body.String())
	}

	// The stored key must survive (read in-process; it's json:"-").
	got, err := st.GetIndexer(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.APIKey != "SECRET-KEY-123" {
		t.Fatalf("stored key wiped: got %q want %q", got.APIKey, "SECRET-KEY-123")
	}
	if got.Name != "ix-renamed" {
		t.Fatalf("name not updated: %q", got.Name)
	}

	// Update WITH a new apiKey overwrites.
	update2, _ := json.Marshal(map[string]any{
		"name": "ix-renamed", "implementation": "torznab", "baseUrl": "http://127.0.0.1:1", "apiKey": "NEW-KEY-456", "enabled": true, "priority": 30,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/indexer/"+strconv.FormatInt(created.ID, 10), bytes.NewReader(update2)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update2: %d", rec.Code)
	}
	got, _ = st.GetIndexer(context.Background(), created.ID)
	if got.APIKey != "NEW-KEY-456" {
		t.Fatalf("new key not stored: got %q", got.APIKey)
	}
}
```

Ensure the test file imports `"context"` and `"strconv"` (add to the import block if missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/indexer/ -run TestIndexerUpdatePreservesStoredKeyWhenBlank -count=1`
Expected: FAIL — `stored key wiped: got "" want "SECRET-KEY-123"`.

- [ ] **Step 3: Write minimal implementation**

In `internal/indexer/api.go`, inside `update()`, replace the block that builds `ix` and writes it (currently):

```go
	ix := p.toStore()
	ix.ID = id
	if err := a.store.UpdateIndexer(r.Context(), ix); err != nil {
```

with:

```go
	ix := p.toStore()
	ix.ID = id
	// Secrets are write-only (APIKey is json:"-"), so the edit form loads the key
	// blank. An empty incoming key means "keep the stored one" — otherwise every
	// edit-without-retyping would wipe it. Update-only: empty on create is a
	// legitimate keyless indexer.
	if p.APIKey == "" {
		if existing, err := a.store.GetIndexer(r.Context(), id); err == nil {
			ix.APIKey = existing.APIKey
		}
	}
	if err := a.store.UpdateIndexer(r.Context(), ix); err != nil {
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/indexer/ -run TestIndexerUpdatePreservesStoredKeyWhenBlank -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/indexer/api.go internal/indexer/api_test.go
git commit -m "fix(6-3a): indexer update carries forward stored API key when blank"
```

---

## Task 2: Backend — downloadclient `update()` carries forward stored secret

**Files:**
- Modify: `internal/downloadclient/api.go` (the `update` handler, ~lines 143-166)
- Test: `internal/downloadclient/api_test.go` (add one test function)

**Interfaces:**
- Consumes: `store.Store.GetDownloadClient(ctx, id) (*store.DownloadClient, error)` (returns a **pointer**; `.APIKey` populated in-process), `store.Store.UpdateDownloadClient(ctx, store.DownloadClient) error`.
- Produces: behavior change only: `PUT /downloadclient/{id}` with empty `apiKey` preserves the stored secret.

- [ ] **Step 1: Write the failing test**

Add to `internal/downloadclient/api_test.go` (mirror the harness already used in that file — check the top of the file for the existing `mountedRouter`/`newTestStore` helper names and reuse them; the router mounts `a.Mount` under `/api/v1`):

```go
func TestDownloadClientUpdatePreservesStoredSecretWhenBlank(t *testing.T) {
	st := newTestStore(t)
	svc := NewService(st)
	a := NewAPI(st, svc)
	router := mountedRouter(t, a) // NB: downloadclient's helper is (t, a), unlike indexer's (t, svc, a)

	create, _ := json.Marshal(map[string]any{
		"name": "sab", "implementation": "sabnzbd", "host": "localhost", "port": 8080, "apiKey": "SECRET-PW-1", "enabled": true, "priority": 25,
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/downloadclient", bytes.NewReader(create)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	update, _ := json.Marshal(map[string]any{
		"name": "sab-renamed", "implementation": "sabnzbd", "host": "localhost", "port": 8080, "apiKey": "", "enabled": true, "priority": 30,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/downloadclient/"+strconv.FormatInt(created.ID, 10), bytes.NewReader(update)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := st.GetDownloadClient(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.APIKey != "SECRET-PW-1" {
		t.Fatalf("stored secret wiped: got %q", got.APIKey)
	}
	if got.Name != "sab-renamed" {
		t.Fatalf("name not updated: %q", got.Name)
	}

	update2, _ := json.Marshal(map[string]any{
		"name": "sab-renamed", "implementation": "sabnzbd", "host": "localhost", "port": 8080, "apiKey": "NEW-PW-2", "enabled": true, "priority": 30,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/downloadclient/"+strconv.FormatInt(created.ID, 10), bytes.NewReader(update2)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update2: %d", rec.Code)
	}
	got, _ = st.GetDownloadClient(context.Background(), created.ID)
	if got.APIKey != "NEW-PW-2" {
		t.Fatalf("new secret not stored: got %q", got.APIKey)
	}
}
```

Ensure `"context"` and `"strconv"` are imported. If the existing harness helper is named differently (e.g. the router builder), adapt the three setup lines to match — read the top of `api_test.go` first.

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/downloadclient/ -run TestDownloadClientUpdatePreservesStoredSecretWhenBlank -count=1`
Expected: FAIL — `stored secret wiped: got ""`.

- [ ] **Step 3: Write minimal implementation**

In `internal/downloadclient/api.go`, inside `update()`, replace:

```go
	dc := p.toStore()
	dc.ID = id
	if err := a.store.UpdateDownloadClient(r.Context(), dc); err != nil {
```

with:

```go
	dc := p.toStore()
	dc.ID = id
	// Secrets are write-only (APIKey is json:"-"). Empty incoming key means "keep
	// the stored one" so an edit that doesn't re-enter the key can't wipe it.
	// Update-only.
	if p.APIKey == "" {
		if existing, err := a.store.GetDownloadClient(r.Context(), id); err == nil {
			dc.APIKey = existing.APIKey
		}
	}
	if err := a.store.UpdateDownloadClient(r.Context(), dc); err != nil {
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/downloadclient/ -run TestDownloadClientUpdatePreservesStoredSecretWhenBlank -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/downloadclient/api.go internal/downloadclient/api_test.go
git commit -m "fix(6-3a): downloadclient update carries forward stored secret when blank"
```

---

## Task 3: Frontend — types + pure payload/status helpers

**Files:**
- Create: `web/src/features/settings/types.ts`
- Create: `web/src/features/settings/payload.ts`
- Create: `web/src/features/settings/status.ts`
- Test: `web/src/features/settings/payload.test.ts`
- Test: `web/src/features/settings/status.test.ts`

**Interfaces:**
- Produces (consumed by Tasks 4-7):
  - Types: `FieldType`, `SchemaField`, `SchemaEntry`, `ConnectionRow`, `TestResult`, `FormValues` (`Record<string, string | boolean>`), `ConnectionKind` (`"indexer" | "downloadclient"`).
  - `basePath(kind: ConnectionKind): string`
  - `fieldsFor(schema: SchemaEntry[], impl: string): SchemaField[]`
  - `defaultValues(fields: SchemaField[]): FormValues`
  - `valuesFromRow(fields: SchemaField[], row: ConnectionRow): FormValues`
  - `parseFieldValue(field: SchemaField, raw: string | boolean): unknown`
  - `buildSavePayload(fields: SchemaField[], values: FormValues, impl: string, omitSecret: boolean): Record<string, unknown>`
  - `buildTestRequest(kind: ConnectionKind, args: { fields: SchemaField[]; values: FormValues; impl: string; id?: number; secretTouched: boolean; editing: boolean }): { path: string; body?: Record<string, unknown> }`
  - `requiredMissing(fields: SchemaField[], values: FormValues): string[]`
  - `connectionStatusBadge(row: { status: string }): { tone: "ok" | "warn" | "muted"; label: string }` (from `status.ts`)

- [ ] **Step 1: Write the failing tests**

`web/src/features/settings/payload.test.ts`:

```ts
import { describe, it, expect } from "vitest"
import {
  basePath, fieldsFor, defaultValues, valuesFromRow,
  buildSavePayload, buildTestRequest, requiredMissing,
} from "./payload"
import type { SchemaEntry, SchemaField } from "./types"

const idxSchema: SchemaEntry[] = [
  { implementation: "newznab", protocol: "usenet", fields: [
    { name: "name", type: "string", required: true },
    { name: "baseUrl", type: "string", required: true },
    { name: "apiKey", type: "string", required: false },
    { name: "categories", type: "int[]", required: false },
    { name: "priority", type: "int", required: false, default: 25 },
    { name: "enabled", type: "bool", required: false, default: true },
  ]},
  { implementation: "torznab", protocol: "torrent", fields: [
    { name: "name", type: "string", required: true },
    { name: "baseUrl", type: "string", required: true },
    { name: "apiKey", type: "string", required: false },
    { name: "categories", type: "int[]", required: false },
    { name: "priority", type: "int", required: false, default: 25 },
    { name: "enabled", type: "bool", required: false, default: true },
  ]},
]
const fields = (impl: string): SchemaField[] => fieldsFor(idxSchema, impl)

describe("basePath", () => {
  it("maps kind to route base", () => {
    expect(basePath("indexer")).toBe("/indexer")
    expect(basePath("downloadclient")).toBe("/downloadclient")
  })
})

describe("defaultValues", () => {
  it("applies schema defaults (bool boolean, others stringified) and blanks the rest", () => {
    const v = defaultValues(fields("newznab"))
    expect(v.priority).toBe("25")
    expect(v.enabled).toBe(true)
    expect(v.name).toBe("")
    expect(v.apiKey).toBe("")
  })
})

describe("valuesFromRow", () => {
  it("fills from a row but never prefills the secret; joins int[]", () => {
    const v = valuesFromRow(fields("newznab"), {
      id: 1, name: "ix", implementation: "newznab", enabled: false, priority: 10,
      status: "ok", lastCheck: null, failMessage: "", baseUrl: "http://x", categories: [5000, 5040],
    } as never)
    expect(v.name).toBe("ix")
    expect(v.baseUrl).toBe("http://x")
    expect(v.categories).toBe("5000,5040")
    expect(v.enabled).toBe(false)
    expect(v.priority).toBe("10")
    expect(v.apiKey).toBe("")
  })
})

describe("buildSavePayload", () => {
  it("coerces types, adds implementation, omits empty int", () => {
    const p = buildSavePayload(fields("newznab"), {
      name: "ix", baseUrl: "http://x", apiKey: "k", categories: "5000, 5040", priority: "", enabled: true,
    }, "newznab", false)
    expect(p).toMatchObject({
      implementation: "newznab", name: "ix", baseUrl: "http://x", apiKey: "k",
      categories: [5000, 5040], enabled: true,
    })
    expect("priority" in p).toBe(false) // empty int omitted -> server default
  })
  it("omits apiKey entirely when omitSecret is true", () => {
    const p = buildSavePayload(fields("newznab"), {
      name: "ix", baseUrl: "http://x", apiKey: "", categories: "", priority: "25", enabled: true,
    }, "newznab", true)
    expect("apiKey" in p).toBe(false)
  })
  it("includes apiKey when non-empty even if omitSecret false", () => {
    const p = buildSavePayload(fields("newznab"), {
      name: "ix", baseUrl: "http://x", apiKey: "typed", categories: "", priority: "25", enabled: true,
    }, "newznab", false)
    expect(p.apiKey).toBe("typed")
  })
})

describe("buildTestRequest", () => {
  it("uses the SAVED endpoint (no body) when editing and secret untouched", () => {
    const req = buildTestRequest("indexer", {
      fields: fields("newznab"), values: { name: "ix", baseUrl: "http://x", apiKey: "" },
      impl: "newznab", id: 7, secretTouched: false, editing: true,
    })
    expect(req).toEqual({ path: "/indexer/7/test" })
  })
  it("uses the UNSAVED endpoint with full body (incl secret) in add mode", () => {
    const req = buildTestRequest("downloadclient", {
      fields: fieldsFor([{ implementation: "sabnzbd", protocol: "usenet", fields: [
        { name: "name", type: "string", required: true },
        { name: "host", type: "string", required: true },
        { name: "apiKey", type: "string" },
      ]}], "sabnzbd"),
      values: { name: "sab", host: "h", apiKey: "k" },
      impl: "sabnzbd", secretTouched: false, editing: false,
    })
    expect(req.path).toBe("/downloadclient/test")
    expect(req.body).toMatchObject({ implementation: "sabnzbd", name: "sab", host: "h", apiKey: "k" })
  })
  it("uses the UNSAVED endpoint when editing but the secret was retyped", () => {
    const req = buildTestRequest("indexer", {
      fields: fields("newznab"), values: { name: "ix", baseUrl: "http://x", apiKey: "new" },
      impl: "newznab", id: 7, secretTouched: true, editing: true,
    })
    expect(req.path).toBe("/indexer/test")
    expect(req.body?.apiKey).toBe("new")
  })
})

describe("requiredMissing", () => {
  it("reports blank required fields only", () => {
    expect(requiredMissing(fields("newznab"), { name: "", baseUrl: "http://x" })).toEqual(["name"])
    expect(requiredMissing(fields("newznab"), { name: "ix", baseUrl: "http://x" })).toEqual([])
  })
})
```

`web/src/features/settings/status.test.ts`:

```ts
import { describe, it, expect } from "vitest"
import { connectionStatusBadge } from "./status"

describe("connectionStatusBadge", () => {
  it("maps status strings to tone + label", () => {
    expect(connectionStatusBadge({ status: "ok" })).toEqual({ tone: "ok", label: "OK" })
    expect(connectionStatusBadge({ status: "failed" })).toEqual({ tone: "warn", label: "Failed" })
    expect(connectionStatusBadge({ status: "" })).toEqual({ tone: "muted", label: "Unknown" })
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run (from `web/`): `npm run test -- payload status`
Expected: FAIL — cannot resolve `./payload` / `./status`.

- [ ] **Step 3: Write the implementation**

`web/src/features/settings/types.ts`:

```ts
export type FieldType = "string" | "int" | "int[]" | "bool"

export type SchemaField = {
  name: string
  type: FieldType
  required?: boolean
  default?: unknown
  label?: string
}

export type SchemaEntry = {
  implementation: string
  protocol: string
  fields: SchemaField[]
}

export type ConnectionRow = {
  id: number
  name: string
  implementation: string
  protocol?: string
  enabled: boolean
  priority: number
  status: string
  lastCheck: string | null
  failMessage: string
  [key: string]: unknown // other config fields (baseUrl, host, port, categories, ...)
}

export type TestResult = { ok: boolean; error?: string; capabilities?: unknown }

export type FormValues = Record<string, string | boolean>

export type ConnectionKind = "indexer" | "downloadclient"
```

`web/src/features/settings/payload.ts`:

```ts
import type { ConnectionKind, ConnectionRow, FormValues, SchemaEntry, SchemaField } from "./types"

export function basePath(kind: ConnectionKind): string {
  return kind === "indexer" ? "/indexer" : "/downloadclient"
}

export function fieldsFor(schema: SchemaEntry[], impl: string): SchemaField[] {
  return schema.find((e) => e.implementation === impl)?.fields ?? []
}

export function defaultValues(fields: SchemaField[]): FormValues {
  const v: FormValues = {}
  for (const f of fields) {
    if (f.type === "bool") v[f.name] = Boolean(f.default ?? false)
    else v[f.name] = f.default != null ? String(f.default) : ""
  }
  return v
}

export function valuesFromRow(fields: SchemaField[], row: ConnectionRow): FormValues {
  const v: FormValues = {}
  for (const f of fields) {
    if (f.name === "apiKey") { v[f.name] = ""; continue } // never prefill secrets
    const raw = row[f.name]
    if (f.type === "bool") v[f.name] = Boolean(raw)
    else if (f.type === "int[]") v[f.name] = Array.isArray(raw) ? raw.join(",") : ""
    else v[f.name] = raw != null ? String(raw) : ""
  }
  return v
}

export function parseFieldValue(field: SchemaField, raw: string | boolean): unknown {
  if (field.type === "bool") return Boolean(raw)
  const s = typeof raw === "string" ? raw : String(raw)
  if (field.type === "int") return s.trim() === "" ? undefined : Number(s)
  if (field.type === "int[]") {
    return s.split(",").map((p) => p.trim()).filter((p) => p !== "").map(Number)
  }
  return s // string
}

export function buildSavePayload(
  fields: SchemaField[], values: FormValues, impl: string, omitSecret: boolean,
): Record<string, unknown> {
  const out: Record<string, unknown> = { implementation: impl }
  for (const f of fields) {
    if (f.name === "apiKey" && omitSecret) continue
    const parsed = parseFieldValue(f, values[f.name])
    if (parsed === undefined) continue // empty int -> let server apply default
    out[f.name] = parsed
  }
  return out
}

export function buildTestRequest(
  kind: ConnectionKind,
  args: { fields: SchemaField[]; values: FormValues; impl: string; id?: number; secretTouched: boolean; editing: boolean },
): { path: string; body?: Record<string, unknown> } {
  const bp = basePath(kind)
  // Editing with an untouched secret -> test the SAVED entity so the stored key
  // is used server-side. Otherwise test the UNSAVED values (they carry the key).
  if (args.editing && args.id != null && !args.secretTouched) {
    return { path: `${bp}/${args.id}/test` }
  }
  return { path: `${bp}/test`, body: buildSavePayload(args.fields, args.values, args.impl, false) }
}

export function requiredMissing(fields: SchemaField[], values: FormValues): string[] {
  return fields
    .filter((f) => f.required && f.type !== "bool" && String(values[f.name] ?? "").trim() === "")
    .map((f) => f.name)
}
```

`web/src/features/settings/status.ts`:

```ts
type Tone = "ok" | "warn" | "muted"

export function connectionStatusBadge(row: { status: string }): { tone: Tone; label: string } {
  if (row.status === "ok") return { tone: "ok", label: "OK" }
  if (row.status === "failed") return { tone: "warn", label: "Failed" }
  return { tone: "muted", label: "Unknown" }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run (from `web/`): `npm run test -- payload status`
Expected: PASS (all cases).

- [ ] **Step 5: Commit**

```bash
git add web/src/features/settings/types.ts web/src/features/settings/payload.ts web/src/features/settings/status.ts web/src/features/settings/payload.test.ts web/src/features/settings/status.test.ts
git commit -m "feat(6-3a): settings types + pure payload/status helpers"
```

---

## Task 4: Frontend — `SchemaForm` component

**Files:**
- Create: `web/src/features/settings/SchemaForm.tsx`
- Test: `web/src/features/settings/SchemaForm.test.tsx`

**Interfaces:**
- Consumes: `SchemaField`, `FormValues` (Task 3); `Select` (`@/components/ui/select`).
- Produces (consumed by Task 5):
  - `SchemaForm(props: { schema: SchemaEntry[]; impl: string; onImplChange: (impl: string) => void; values: FormValues; onChange: (name: string, value: string | boolean) => void; editing: boolean })`
  - Renders an implementation `<select>` (aria-label `"Implementation"`) and one input per field of the selected implementation, keyed by type.
  - `apiKey` renders as `<input type="password">` labeled by the field's `label` (fallback `"API Key"`); in `editing` mode its placeholder is `"leave blank to keep current"`.

- [ ] **Step 1: Write the failing test**

`web/src/features/settings/SchemaForm.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { SchemaForm } from "./SchemaForm"
import type { SchemaEntry } from "./types"

const schema: SchemaEntry[] = [
  { implementation: "sabnzbd", protocol: "usenet", fields: [
    { name: "name", type: "string", required: true },
    { name: "host", type: "string", required: true },
    { name: "port", type: "int" },
    { name: "useSsl", type: "bool", default: false },
    { name: "apiKey", type: "string", label: "API Key" },
  ]},
  { implementation: "qbittorrent", protocol: "torrent", fields: [
    { name: "name", type: "string", required: true },
    { name: "host", type: "string", required: true },
    { name: "apiKey", type: "string", label: "Password" },
  ]},
]

function renderForm(impl = "sabnzbd", editing = false) {
  const onChange = vi.fn()
  const onImplChange = vi.fn()
  render(
    <SchemaForm
      schema={schema} impl={impl} onImplChange={onImplChange}
      values={{ name: "", host: "", port: "", useSsl: false, apiKey: "" }}
      onChange={onChange} editing={editing}
    />,
  )
  return { onChange, onImplChange }
}

describe("SchemaForm", () => {
  it("renders one input per field with the right control type", () => {
    renderForm()
    expect(screen.getByLabelText("name")).toHaveAttribute("type", "text")
    expect(screen.getByLabelText("port")).toHaveAttribute("type", "number")
    expect(screen.getByLabelText("useSsl")).toHaveAttribute("type", "checkbox")
    expect(screen.getByLabelText("API Key")).toHaveAttribute("type", "password")
  })

  it("labels the secret per implementation", () => {
    renderForm("qbittorrent")
    expect(screen.getByLabelText("Password")).toBeInTheDocument()
  })

  it("shows the keep-current placeholder in edit mode", () => {
    renderForm("sabnzbd", true)
    expect(screen.getByLabelText("API Key")).toHaveAttribute("placeholder", "leave blank to keep current")
  })

  it("emits onChange when a field is edited", async () => {
    const { onChange } = renderForm()
    await userEvent.type(screen.getByLabelText("name"), "x")
    expect(onChange).toHaveBeenCalledWith("name", "x")
  })

  it("emits onImplChange when the implementation dropdown changes", async () => {
    const { onImplChange } = renderForm()
    await userEvent.selectOptions(screen.getByLabelText("Implementation"), "qbittorrent")
    expect(onImplChange).toHaveBeenCalledWith("qbittorrent")
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `web/`): `npm run test -- SchemaForm`
Expected: FAIL — cannot resolve `./SchemaForm`.

- [ ] **Step 3: Write the implementation**

`web/src/features/settings/SchemaForm.tsx`:

```tsx
import { Select } from "@/components/ui/select"
import { fieldsFor } from "./payload"
import type { FormValues, SchemaEntry, SchemaField } from "./types"

const inputClass =
  "w-full rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-3 py-2 text-sm"

export function SchemaForm({
  schema, impl, onImplChange, values, onChange, editing,
}: {
  schema: SchemaEntry[]
  impl: string
  onImplChange: (impl: string) => void
  values: FormValues
  onChange: (name: string, value: string | boolean) => void
  editing: boolean
}) {
  const fields = fieldsFor(schema, impl)
  return (
    <div className="flex flex-col gap-3">
      <label className="text-xs text-[var(--color-muted)]">Implementation</label>
      <Select aria-label="Implementation" value={impl} onChange={onImplChange}>
        {schema.map((e) => (
          <option key={e.implementation} value={e.implementation}>
            {e.implementation} ({e.protocol})
          </option>
        ))}
      </Select>
      {fields.map((f) => (
        <Field key={f.name} field={f} value={values[f.name]} onChange={onChange} editing={editing} />
      ))}
    </div>
  )
}

function Field({
  field, value, onChange, editing,
}: {
  field: SchemaField
  value: string | boolean | undefined
  onChange: (name: string, value: string | boolean) => void
  editing: boolean
}) {
  const isSecret = field.name === "apiKey"
  const label = isSecret ? (field.label ?? "API Key") : field.name

  if (field.type === "bool") {
    return (
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          aria-label={field.name}
          checked={Boolean(value)}
          onChange={(e) => onChange(field.name, e.target.checked)}
        />
        {field.name}
      </label>
    )
  }

  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-[var(--color-muted)]" htmlFor={`f-${field.name}`}>
        {label}{field.required ? " *" : ""}
      </label>
      <input
        id={`f-${field.name}`}
        aria-label={label}
        type={isSecret ? "password" : field.type === "int" ? "number" : "text"}
        value={typeof value === "string" ? value : ""}
        placeholder={isSecret && editing ? "leave blank to keep current" : undefined}
        onChange={(e) => onChange(field.name, e.target.value)}
        className={inputClass}
      />
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (from `web/`): `npm run test -- SchemaForm`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/settings/SchemaForm.tsx web/src/features/settings/SchemaForm.test.tsx
git commit -m "feat(6-3a): schema-driven SchemaForm component"
```

---

## Task 5: Frontend — `api.ts` hooks + `ConnectionDialog`

**Files:**
- Create: `web/src/features/settings/api.ts`
- Create: `web/src/features/settings/ConnectionDialog.tsx`
- Test: `web/src/features/settings/ConnectionDialog.test.tsx`

**Interfaces:**
- Consumes: `apiGet/apiPost/apiPut/apiDelete` (`@/lib/api`); helpers + types from Task 3; `SchemaForm` (Task 4); `Dialog`/`DialogTitle` (`@/components/ui/dialog`); `StatusBadge` not needed here; `useToast` (`@/lib/toast`).
- Produces:
  - `api.ts`: `settingsKeys`, `useConnections(kind)`, `useConnectionSchema(kind)`, `useSaveConnection(kind)` (mutation over `{ payload, id? }`), `useDeleteConnection(kind)` (mutation over `id`), `useTestConnection(kind)` (mutation over `{ path, body? }` → `TestResult`).
  - `ConnectionDialog(props: { kind: ConnectionKind; existing?: ConnectionRow; open: boolean; onOpenChange: (o: boolean) => void })` (consumed by Task 6).

- [ ] **Step 1: Write `api.ts` (no separate test — covered via the dialog test, which mocks this module, matching the library feature's pattern)**

`web/src/features/settings/api.ts`:

```ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import { basePath } from "./payload"
import type { ConnectionKind, ConnectionRow, SchemaEntry, TestResult } from "./types"

export const settingsKeys = {
  list: (kind: ConnectionKind) => ["settings", kind, "list"] as const,
  schema: (kind: ConnectionKind) => ["settings", kind, "schema"] as const,
}

export function useConnections(kind: ConnectionKind) {
  return useQuery({
    queryKey: settingsKeys.list(kind),
    queryFn: () => apiGet<ConnectionRow[]>(basePath(kind)),
  })
}

export function useConnectionSchema(kind: ConnectionKind) {
  return useQuery({
    queryKey: settingsKeys.schema(kind),
    queryFn: () => apiGet<SchemaEntry[]>(`${basePath(kind)}/schema`),
  })
}

export function useSaveConnection(kind: ConnectionKind) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ payload, id }: { payload: Record<string, unknown>; id?: number }) =>
      id == null
        ? apiPost<ConnectionRow>(basePath(kind), payload)
        : apiPut<ConnectionRow>(`${basePath(kind)}/${id}`, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: settingsKeys.list(kind) }),
  })
}

export function useDeleteConnection(kind: ConnectionKind) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`${basePath(kind)}/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: settingsKeys.list(kind) }),
  })
}

export function useTestConnection(kind: ConnectionKind) {
  void kind
  return useMutation({
    mutationFn: (req: { path: string; body?: Record<string, unknown> }) =>
      apiPost<TestResult>(req.path, req.body),
  })
}
```

- [ ] **Step 2: Write the failing dialog test**

`web/src/features/settings/ConnectionDialog.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ConnectionDialog } from "./ConnectionDialog"
import * as api from "./api"
import type { SchemaEntry } from "./types"

vi.mock("./api", async (orig) => {
  const actual = await orig<typeof import("./api")>()
  return { ...actual, useConnectionSchema: vi.fn(), useSaveConnection: vi.fn(), useTestConnection: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

const schema: SchemaEntry[] = [
  { implementation: "newznab", protocol: "usenet", fields: [
    { name: "name", type: "string", required: true },
    { name: "baseUrl", type: "string", required: true },
    { name: "apiKey", type: "string", label: "API Key" },
    { name: "enabled", type: "bool", default: true },
  ]},
]

function mut(extra: object = {}) {
  return { mutate: vi.fn(), mutateAsync: vi.fn().mockResolvedValue({ ok: true }), isPending: false, ...extra } as unknown as never
}

function setup(existing?: object) {
  vi.mocked(api.useConnectionSchema).mockReturnValue({ data: schema, isLoading: false } as never)
  const save = vi.fn().mockResolvedValue({ id: 1 })
  const test = vi.fn().mockResolvedValue({ ok: false, error: "refused" })
  vi.mocked(api.useSaveConnection).mockReturnValue(mut({ mutateAsync: save }))
  vi.mocked(api.useTestConnection).mockReturnValue(mut({ mutateAsync: test }))
  render(
    <ToastProvider>
      <ConnectionDialog kind="indexer" existing={existing as never} open onOpenChange={vi.fn()} />
    </ToastProvider>,
  )
  return { save, test }
}

describe("ConnectionDialog", () => {
  it("submits a create payload built from the form (add mode)", async () => {
    const { save } = setup()
    await userEvent.type(screen.getByLabelText("name"), "My Indexer")
    await userEvent.type(screen.getByLabelText("baseUrl"), "http://x")
    await userEvent.type(screen.getByLabelText("API Key"), "k")
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0][0]).toMatchObject({
      payload: { implementation: "newznab", name: "My Indexer", baseUrl: "http://x", apiKey: "k" },
    })
    expect(save.mock.calls[0][0].id).toBeUndefined()
  })

  it("omits apiKey on save when editing and the secret is untouched", async () => {
    const existing = { id: 9, name: "ix", implementation: "newznab", enabled: true, priority: 25, status: "ok", lastCheck: null, failMessage: "", baseUrl: "http://x" }
    const { save } = setup(existing)
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    await waitFor(() => expect(save).toHaveBeenCalled())
    const arg = save.mock.calls[0][0]
    expect(arg.id).toBe(9)
    expect("apiKey" in arg.payload).toBe(false)
  })

  it("tests the SAVED endpoint when editing with untouched secret and shows the error", async () => {
    const existing = { id: 9, name: "ix", implementation: "newznab", enabled: true, priority: 25, status: "ok", lastCheck: null, failMessage: "", baseUrl: "http://x" }
    const { test } = setup(existing)
    await userEvent.click(screen.getByRole("button", { name: /test/i }))
    await waitFor(() => expect(test).toHaveBeenCalledWith({ path: "/indexer/9/test" }))
    expect(await screen.findByText(/refused/)).toBeInTheDocument()
  })

  it("tests the UNSAVED endpoint (with body) in add mode", async () => {
    const { test } = setup()
    await userEvent.type(screen.getByLabelText("name"), "ix")
    await userEvent.type(screen.getByLabelText("baseUrl"), "http://x")
    await userEvent.click(screen.getByRole("button", { name: /test/i }))
    await waitFor(() => expect(test).toHaveBeenCalled())
    const req = test.mock.calls[0][0]
    expect(req.path).toBe("/indexer/test")
    expect(req.body).toMatchObject({ implementation: "newznab", name: "ix", baseUrl: "http://x" })
  })
})
```

- [ ] **Step 3: Write `ConnectionDialog.tsx`**

`web/src/features/settings/ConnectionDialog.tsx`:

```tsx
import { useMemo, useState } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"
import { useToast } from "@/lib/toast"
import { SchemaForm } from "./SchemaForm"
import { useConnectionSchema, useSaveConnection, useTestConnection } from "./api"
import {
  buildSavePayload, buildTestRequest, defaultValues, fieldsFor, requiredMissing, valuesFromRow,
} from "./payload"
import type { ConnectionKind, ConnectionRow, FormValues, TestResult } from "./types"

export function ConnectionDialog({
  kind, existing, open, onOpenChange,
}: {
  kind: ConnectionKind
  existing?: ConnectionRow
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const { toast } = useToast()
  const schemaQ = useConnectionSchema(kind)
  const save = useSaveConnection(kind)
  const testMut = useTestConnection(kind)
  const schema = useMemo(() => schemaQ.data ?? [], [schemaQ.data])
  const editing = existing != null

  const [impl, setImpl] = useState<string>(() => existing?.implementation ?? "")
  const activeImpl = impl || schema[0]?.implementation || ""
  const fields = fieldsFor(schema, activeImpl)

  const [values, setValues] = useState<FormValues>({})
  const [initialized, setInitialized] = useState(false)
  const [secretTouched, setSecretTouched] = useState(false)
  const [result, setResult] = useState<TestResult | null>(null)

  // Seed form values once the schema has loaded (defaults for add, row for edit).
  if (!initialized && schema.length > 0 && activeImpl) {
    setValues(existing ? valuesFromRow(fields, existing) : defaultValues(fields))
    setInitialized(true)
  }

  function onImplChange(next: string) {
    setImpl(next)
    setValues(defaultValues(fieldsFor(schema, next)))
    setSecretTouched(false)
    setResult(null)
  }

  function onChange(name: string, value: string | boolean) {
    if (name === "apiKey") setSecretTouched(true)
    setValues((v) => ({ ...v, [name]: value }))
  }

  async function onTest() {
    setResult(null)
    const req = buildTestRequest(kind, { fields, values, impl: activeImpl, id: existing?.id, secretTouched, editing })
    try {
      const res = await testMut.mutateAsync(req)
      setResult(res)
    } catch (e) {
      setResult({ ok: false, error: e instanceof Error ? e.message : "test failed" })
    }
  }

  async function onSave() {
    const missing = requiredMissing(fields, values)
    if (missing.length > 0) { toast(`Required: ${missing.join(", ")}`, { variant: "error" }); return }
    const payload = buildSavePayload(fields, values, activeImpl, editing && !secretTouched)
    try {
      await save.mutateAsync({ payload, id: existing?.id })
      toast(editing ? "Saved" : "Added", { variant: "ok" })
      onOpenChange(false)
    } catch (e) {
      toast(e instanceof Error ? e.message : "Save failed", { variant: "error" })
    }
  }

  const pending = save.isPending
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>{editing ? "Edit" : "Add"} {kind === "indexer" ? "Indexer" : "Download Client"}</DialogTitle>
      {schema.length === 0 ? (
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      ) : (
        <>
          <SchemaForm
            schema={schema} impl={activeImpl} onImplChange={onImplChange}
            values={values} onChange={onChange} editing={editing}
          />
          {result && (
            <div
              role="status"
              className={`mt-3 rounded-md border px-3 py-2 text-sm ${
                result.ok
                  ? "border-[var(--color-ok)] text-[var(--color-ok)]"
                  : "border-[var(--color-warn)] text-[var(--color-warn)]"
              }`}
            >
              {result.ok ? "Connection OK" : `Test failed: ${result.error ?? "unknown error"}`}
              {result.ok && result.capabilities != null && (
                <pre className="mt-1 max-h-32 overflow-auto text-xs text-[var(--color-muted)]">
                  {JSON.stringify(result.capabilities, null, 2)}
                </pre>
              )}
            </div>
          )}
          <div className="mt-4 flex justify-end gap-2">
            <button
              onClick={onTest}
              disabled={testMut.isPending}
              className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm disabled:opacity-50"
            >
              {testMut.isPending ? "Testing…" : "Test"}
            </button>
            <button
              onClick={onSave}
              disabled={pending}
              className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
            >
              {pending ? "Saving…" : "Save"}
            </button>
          </div>
        </>
      )}
    </Dialog>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (from `web/`): `npm run test -- ConnectionDialog`
Expected: PASS (all four cases).

- [ ] **Step 5: Commit**

```bash
git add web/src/features/settings/api.ts web/src/features/settings/ConnectionDialog.tsx web/src/features/settings/ConnectionDialog.test.tsx
git commit -m "feat(6-3a): settings api hooks + ConnectionDialog with test/save"
```

---

## Task 6: Frontend — `ConnectionsSection` (list + add/edit/delete)

**Files:**
- Create: `web/src/features/settings/ConnectionsSection.tsx`
- Test: `web/src/features/settings/ConnectionsSection.test.tsx`

**Interfaces:**
- Consumes: `useConnections`, `useDeleteConnection` (Task 5); `ConnectionDialog` (Task 5); `connectionStatusBadge` (Task 3); generic `StatusBadge` (`@/features/library/StatusBadge`); `useToast`.
- Produces: `ConnectionsSection(props: { kind: ConnectionKind })` (consumed by Task 7).

- [ ] **Step 1: Write the failing test**

`web/src/features/settings/ConnectionsSection.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ConnectionsSection } from "./ConnectionsSection"
import * as api from "./api"

vi.mock("./api", async (orig) => {
  const actual = await orig<typeof import("./api")>()
  return { ...actual, useConnections: vi.fn(), useDeleteConnection: vi.fn() }
})
// The dialog fetches schema; stub it out so this test focuses on the list.
vi.mock("./ConnectionDialog", () => ({ ConnectionDialog: () => <div data-testid="dialog" /> }))
beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, ...extra } as unknown as never
}

describe("ConnectionsSection", () => {
  it("lists connections with a status badge and an empty state", () => {
    vi.mocked(api.useConnections).mockReturnValue({
      data: [{ id: 1, name: "NZBgeek", implementation: "newznab", enabled: true, priority: 25, status: "ok", lastCheck: null, failMessage: "" }],
      isLoading: false, isError: false,
    } as never)
    vi.mocked(api.useDeleteConnection).mockReturnValue(mut())
    render(<ToastProvider><ConnectionsSection kind="indexer" /></ToastProvider>)
    expect(screen.getByText("NZBgeek")).toBeInTheDocument()
    expect(screen.getByText("OK")).toBeInTheDocument()
  })

  it("confirms before deleting", async () => {
    const del = vi.fn()
    vi.mocked(api.useConnections).mockReturnValue({
      data: [{ id: 1, name: "NZBgeek", implementation: "newznab", enabled: true, priority: 25, status: "failed", lastCheck: null, failMessage: "boom" }],
      isLoading: false, isError: false,
    } as never)
    vi.mocked(api.useDeleteConnection).mockReturnValue(mut({ mutate: del }))
    vi.spyOn(window, "confirm").mockReturnValue(true)
    render(<ToastProvider><ConnectionsSection kind="indexer" /></ToastProvider>)
    await userEvent.click(screen.getByRole("button", { name: /delete/i }))
    expect(del).toHaveBeenCalledWith(1, expect.anything())
  })

  it("opens the add dialog", async () => {
    vi.mocked(api.useConnections).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    vi.mocked(api.useDeleteConnection).mockReturnValue(mut())
    render(<ToastProvider><ConnectionsSection kind="indexer" /></ToastProvider>)
    expect(screen.queryByTestId("dialog")).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: /add/i }))
    expect(screen.getByTestId("dialog")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `web/`): `npm run test -- ConnectionsSection`
Expected: FAIL — cannot resolve `./ConnectionsSection`.

- [ ] **Step 3: Write the implementation**

`web/src/features/settings/ConnectionsSection.tsx`:

```tsx
import { useState } from "react"
import { useToast } from "@/lib/toast"
import { StatusBadge } from "@/features/library/StatusBadge"
import { ConnectionDialog } from "./ConnectionDialog"
import { useConnections, useDeleteConnection } from "./api"
import { connectionStatusBadge } from "./status"
import type { ConnectionKind, ConnectionRow } from "./types"

const LABELS: Record<ConnectionKind, { singular: string; plural: string }> = {
  indexer: { singular: "Indexer", plural: "Indexers" },
  downloadclient: { singular: "Download Client", plural: "Download Clients" },
}

export function ConnectionsSection({ kind }: { kind: ConnectionKind }) {
  const { toast } = useToast()
  const q = useConnections(kind)
  const del = useDeleteConnection(kind)
  const [addOpen, setAddOpen] = useState(false)
  const [editing, setEditing] = useState<ConnectionRow | null>(null)
  const rows = q.data ?? []
  const labels = LABELS[kind]

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold">{labels.plural}</h2>
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
        <p className="text-sm text-[var(--color-muted)]">No {labels.plural.toLowerCase()} configured — click Add to create one.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((row) => {
            const badge = connectionStatusBadge(row)
            return (
              <li
                key={row.id}
                className="flex items-center gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{row.name}</span>
                    <span className="text-xs text-[var(--color-muted)]">{row.implementation}</span>
                    {!row.enabled && <span className="text-xs text-[var(--color-muted)]">(disabled)</span>}
                  </div>
                  <div className="text-xs text-[var(--color-muted)]">priority {row.priority}</div>
                </div>
                <span title={row.failMessage || undefined}>
                  <StatusBadge tone={badge.tone} label={badge.label} />
                </span>
                <button
                  onClick={() => setEditing(row)}
                  className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
                >
                  Edit
                </button>
                <button
                  onClick={() => {
                    if (confirm(`Delete ${row.name}?`)) {
                      del.mutate(row.id, { onSuccess: () => toast("Deleted") })
                    }
                  }}
                  className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
                >
                  Delete
                </button>
              </li>
            )
          })}
        </ul>
      )}

      {addOpen && <ConnectionDialog kind={kind} open={addOpen} onOpenChange={setAddOpen} />}
      {editing && (
        <ConnectionDialog
          kind={kind}
          existing={editing}
          open={editing != null}
          onOpenChange={(o) => { if (!o) setEditing(null) }}
        />
      )}
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (from `web/`): `npm run test -- ConnectionsSection`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/settings/ConnectionsSection.tsx web/src/features/settings/ConnectionsSection.test.tsx
git commit -m "feat(6-3a): ConnectionsSection list with add/edit/delete"
```

---

## Task 7: Frontend — `SettingsLayout` + routing

**Files:**
- Create: `web/src/features/settings/SettingsLayout.tsx`
- Modify: `web/src/app/routes.tsx` (replace the `settings` placeholder route with nested routes)
- Test: `web/src/features/settings/SettingsLayout.test.tsx`

**Interfaces:**
- Consumes: `NavLink`, `Outlet`, `Navigate` (`react-router-dom`); `ConnectionsSection` (Task 6).
- Produces: `SettingsLayout` (default page under `/settings`); route children `/settings/indexers` and `/settings/downloadclients`; `/settings` redirects to `/settings/indexers`.

- [ ] **Step 1: Write the failing test**

`web/src/features/settings/SettingsLayout.test.tsx`:

```tsx
import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { SettingsLayout } from "./SettingsLayout"

describe("SettingsLayout", () => {
  it("renders tab links for the 3a sections with correct hrefs", () => {
    render(<MemoryRouter initialEntries={["/settings/indexers"]}><SettingsLayout /></MemoryRouter>)
    const indexers = screen.getByRole("link", { name: "Indexers" })
    const clients = screen.getByRole("link", { name: "Download Clients" })
    expect(indexers).toHaveAttribute("href", "/settings/indexers")
    expect(clients).toHaveAttribute("href", "/settings/downloadclients")
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `web/`): `npm run test -- SettingsLayout`
Expected: FAIL — cannot resolve `./SettingsLayout`.

- [ ] **Step 3: Write `SettingsLayout.tsx`**

`web/src/features/settings/SettingsLayout.tsx`:

```tsx
import { NavLink, Outlet } from "react-router-dom"
import { cn } from "@/lib/utils"

// 3b appends { to: "/settings/qualityprofiles", label: "Quality Profiles" }, etc.
const TABS: { to: string; label: string }[] = [
  { to: "/settings/indexers", label: "Indexers" },
  { to: "/settings/downloadclients", label: "Download Clients" },
]

export function SettingsLayout() {
  return (
    <div>
      <div className="border-b border-[var(--color-border)] px-6 pt-6">
        <h1 className="mb-3 text-2xl font-bold">Settings</h1>
        <nav className="flex gap-1">
          {TABS.map((t) => (
            <NavLink
              key={t.to}
              to={t.to}
              className={({ isActive }) =>
                cn(
                  "rounded-t-md px-4 py-2 text-sm text-[var(--color-muted)]",
                  isActive && "bg-[rgba(124,92,255,0.16)] font-semibold text-[var(--color-fg)]",
                )
              }
            >
              {t.label}
            </NavLink>
          ))}
        </nav>
      </div>
      <Outlet />
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (from `web/`): `npm run test -- SettingsLayout`
Expected: PASS.

- [ ] **Step 5: Wire routes**

In `web/src/app/routes.tsx`, add imports at the top:

```tsx
import { Navigate } from "react-router-dom"
import { SettingsLayout } from "@/features/settings/SettingsLayout"
import { ConnectionsSection } from "@/features/settings/ConnectionsSection"
```

Replace this line:

```tsx
      { path: "settings", element: <Placeholder title="Settings" /> },
```

with:

```tsx
      {
        path: "settings",
        element: <SettingsLayout />,
        children: [
          { index: true, element: <Navigate to="/settings/indexers" replace /> },
          { path: "indexers", element: <ConnectionsSection kind="indexer" /> },
          { path: "downloadclients", element: <ConnectionsSection kind="downloadclient" /> },
        ],
      },
```

- [ ] **Step 6: Verify the whole FE test suite + typecheck pass**

Run (from `web/`): `npm run test` then `npx tsc -b`
Expected: all tests PASS; `tsc -b` exits 0 with no output.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/settings/SettingsLayout.tsx web/src/features/settings/SettingsLayout.test.tsx web/src/app/routes.tsx
git commit -m "feat(6-3a): SettingsLayout tabs + /settings nested routes"
```

---

## Task 8: Build the embedded bundle + full verification

**Files:**
- Modify: `web/dist/**` (regenerated artifacts — committed)

**Interfaces:** none. This task produces the committed production bundle and a green whole-repo verification.

- [ ] **Step 1: Rebuild the web bundle**

Run (from `web/`): `npm run build`
Expected: `tsc -b` clean, then Vite writes `web/dist/**`.

- [ ] **Step 2: Verify frontend suite once more**

Run (from `web/`): `npm run test`
Expected: PASS (all settings tests plus the pre-existing suite).

- [ ] **Step 3: Verify the Go build embeds and passes**

Run (from repo root): `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./... -count=1`
Expected: all packages build, vet clean, tests PASS (includes `web/spa_test.go` serving the new bundle and the two backend carry-forward tests).

- [ ] **Step 4: Confirm the dist drift-guard is clean after staging**

```bash
git add web/dist
git status --short web/dist
```
Expected: staged changes reflect the rebuilt bundle; after commit, `git diff --exit-code web/dist` is clean.

- [ ] **Step 5: Commit**

```bash
git add web/dist
git commit -m "build(6-3a): rebuild embedded web bundle for settings connections"
```

- [ ] **Step 6: Manual verification (drive the real app)**

Build and run a fresh instance, then confirm the acceptance criteria in the browser:

```bash
export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go build -o nexus.exe ./cmd/nexus
# PowerShell:  $env:NEXUS_ADMIN_PASSWORD="admin"; $env:NEXUS_DATA_DIR="C:\\Users\\James\\AppData\\Local\\Temp\\nexus-3a"; .\nexus.exe
```
Open `http://localhost:9494/` → login (`admin`/`admin`) → **Settings**. Verify: (a) Indexers and Download Clients tabs render; (b) Add opens the schema form with an implementation dropdown; (c) Test reports a real failure for a bad URL and success for a reachable one; (d) create a connection with a key, edit it without re-typing the key, save, then Test → still succeeds (secret preserved); (e) Delete removes it after confirm; (f) cards show themed dark borders.

---

## Self-Review (completed during planning)

**Spec coverage:**
- §3.1 carry-forward (indexer + downloadclient) → Tasks 1, 2.
- §4 schema/test/list shapes → consumed in Tasks 3-6.
- §5.1 routing & layout → Task 7.
- §5.2 SchemaForm (types + dropdown + labels) → Task 4.
- §5.3 section list + status badges + add/edit/delete → Task 6.
- §5.4 secret-on-edit omission → Tasks 3 (`buildSavePayload` omitSecret) + 5 (dialog).
- §5.5 unsaved-vs-saved test branch → Tasks 3 (`buildTestRequest`) + 5 (dialog test asserts both paths).
- §5.6 border default → **already present** in `web/src/styles/index.css:90-93` (global `border-color: var(--color-border)`), so no task adds it; AC#6 is a manual verification item in Task 8 Step 6.
- §7 testing → each FE task ships its test; Task 8 runs the full suite + Go build/vet/test.
- §8 acceptance criteria 1-7 → Task 8 Step 6 (manual) + automated tests.

**Placeholder scan:** No TBD/TODO; every code step shows complete code.

**Type consistency:** Helper signatures in Task 3's Interfaces block match their definitions and every call site in Tasks 4-7 (`fieldsFor`, `buildSavePayload(fields, values, impl, omitSecret)`, `buildTestRequest(kind, {fields, values, impl, id?, secretTouched, editing})`, `connectionStatusBadge`). `ConnectionKind` is `"indexer" | "downloadclient"` throughout. `useTestConnection` mutates over `{ path, body? }`, exactly what `buildTestRequest` returns.

**Note on `cn`:** `web/src/lib/utils.ts` exports `cn` (used by `Sidebar.tsx`); `SettingsLayout` reuses it.
