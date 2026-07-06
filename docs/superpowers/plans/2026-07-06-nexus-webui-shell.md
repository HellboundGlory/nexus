# Nexus Web UI — Shell / Foundation (Sub-project 6, Slice 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Nexus web app shell (Vite+React+TS+Tailwind+shadcn/ui, dark labeled-sidebar layout, cookie-auth flow) plus a live Dashboard, and wire the frontend build into the Go binary via committed `web/dist`.

**Architecture:** A Vite/React/TypeScript SPA under `web/src`, built to `web/dist` (committed) and served by the existing `web.Handler()` embed. Pure-logic modules (`lib/api.ts`, `lib/ws.ts`) are TDD'd with Vitest; React pieces (`auth`, shell, Login, Dashboard) with React Testing Library. The app talks only to already-shipped backend endpoints (`/system/status`, `/auth/*`, `/ws`).

**Tech Stack:** Vite 6, React 19, TypeScript 5, Tailwind CSS v4 (`@tailwindcss/vite`), shadcn/ui (Radix + lucide), react-router v6, TanStack Query v5, Vitest 2 + Testing Library + jsdom.

## Global Constraints

- Slice 1 adds **NO new backend endpoints** and modifies **NO files under `internal/**` or `cmd/**`**. Consumes only `GET /api/v1/system/status`, `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/ws`.
- `web/embed.go` is **unchanged**. `web/dist` is **committed to git** — a bare `CGO_ENABLED=0 go build ./...` must produce a working UI binary with no Node.
- Dark theme **only**; express colors as CSS variables/tokens so a light theme can slot in later.
- The WS activity feed is **live-only** (ring buffer, cap 50, cleared on reload) — there is no event-history endpoint.
- All API requests use `credentials: "include"` (cookie `nexus_session` is HttpOnly).
- WS envelope is `{ "type": string, "data": any }`. `/system/status` returns `{ version, appName, healthy, taskCount }`.
- Backend error envelope is `{ "error": { "code": string, "message": string } }`.
- Go verification prefix (Go is not on PATH): `export PATH="/c/Program Files/Go/bin:$PATH"`. `-race` is unavailable (no CGO).
- Node/npm commands run from inside `web/` (`cd web && ...`).

---

## File Structure

**Created (frontend, all under `web/`):**
- Config: `package.json`, `package-lock.json`, `vite.config.ts`, `tsconfig.json`, `tsconfig.app.json`, `tsconfig.node.json`, `components.json`, `.gitignore` (already covers `node_modules`), `index.html`, `src/vite-env.d.ts`, `src/test/setup.ts`
- Styles: `src/styles/index.css`
- Lib: `src/lib/utils.ts` (shadcn `cn`), `src/lib/api.ts`, `src/lib/ws.ts`, `src/lib/time.ts`, `src/lib/auth.tsx`, `src/lib/activity.tsx`
- shadcn primitives (generated): `src/components/ui/{button,card,input,label}.tsx`
- App shell: `src/app/Layout.tsx`, `src/app/Sidebar.tsx`, `src/app/TopBar.tsx`, `src/app/routes.tsx`
- Pages: `src/pages/Dashboard.tsx`, `src/pages/Login.tsx`, `src/pages/Placeholder.tsx`
- Entry: `src/main.tsx`
- Tests colocated as `*.test.ts(x)` next to sources.

**Modified:**
- `web/spa_test.go` — update assertions from the placeholder string to the real built index.
- `Makefile` (repo root) — add `web`, `web-dev`, `web-test`, `verify-web` targets; make binary build depend on `web`.
- `web/dist/**` — replaced by the real Vite build (committed).

**Unchanged:** `web/embed.go`, everything under `internal/**` and `cmd/**`.

---

### Task 1: Frontend scaffold, build pipeline, and embed integration

Stand up the toolchain: a minimal React app that builds to `web/dist`, is served by the Go embed, runs Vitest, and is wired into the Makefile with a drift guard. No app logic beyond a blank shell yet.

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/tsconfig.app.json`, `web/tsconfig.node.json`, `web/index.html`, `web/components.json`, `web/src/main.tsx`, `web/src/vite-env.d.ts`, `web/src/styles/index.css`, `web/src/lib/utils.ts`, `web/src/test/setup.ts`, `web/src/smoke.test.ts`
- Generate (shadcn CLI): `web/src/components/ui/{button,card,input,label}.tsx`
- Modify: `web/spa_test.go`, repo-root `Makefile`
- Replace: `web/dist/**` (real build output, committed)

**Interfaces:**
- Produces: a working `npm run build` → `web/dist`; `npm test` (Vitest, jsdom, `@` alias, `@testing-library/jest-dom` matchers); the `cn(...)` util at `@/lib/utils`; shadcn primitives at `@/components/ui/*`; dark CSS tokens on `:root`.

- [ ] **Step 1: Create `web/package.json`**

```json
{
  "name": "nexus-web",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.62.0",
    "class-variance-authority": "^0.7.1",
    "clsx": "^2.1.1",
    "lucide-react": "^0.468.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0",
    "react-router-dom": "^6.28.0",
    "tailwind-merge": "^2.6.0"
  },
  "devDependencies": {
    "@tailwindcss/vite": "^4.0.0",
    "@testing-library/jest-dom": "^6.6.3",
    "@testing-library/react": "^16.1.0",
    "@testing-library/user-event": "^14.5.2",
    "@types/node": "^22.10.0",
    "@types/react": "^19.0.0",
    "@types/react-dom": "^19.0.0",
    "@vitejs/plugin-react": "^4.3.4",
    "jsdom": "^25.0.1",
    "tailwindcss": "^4.0.0",
    "typescript": "^5.7.2",
    "vite": "^6.0.0",
    "vitest": "^2.1.8"
  }
}
```

- [ ] **Step 2: Create the TypeScript configs**

`web/tsconfig.json`:

```json
{
  "files": [],
  "references": [{ "path": "./tsconfig.app.json" }, { "path": "./tsconfig.node.json" }],
  "compilerOptions": {
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] }
  }
}
```

`web/tsconfig.app.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "useDefineForClassFields": true,
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] },
    "types": ["vite/client", "vitest/globals", "@testing-library/jest-dom"]
  },
  "include": ["src"]
}
```

`web/tsconfig.node.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2023"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "strict": true
  },
  "include": ["vite.config.ts"]
}
```

- [ ] **Step 3: Create `web/vite.config.ts`** (Vite + React + Tailwind v4 plugin, `@` alias, Vitest config)

```typescript
/// <reference types="vitest/config" />
import path from "node:path"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    // Dev-only: forward API + WebSocket to the Go backend (default port 9494,
    // from config.Addr(); override if you set NEXUS_PORT). `ws: true` also
    // proxies /api/v1/ws so the live activity feed works under HMR.
    proxy: {
      "/api": { target: "http://localhost:9494", changeOrigin: true, ws: true },
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
})
```

- [ ] **Step 4: Create `web/index.html`, entry, env, styles, and test setup**

`web/index.html`:

```html
<!doctype html>
<html lang="en" class="dark">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Nexus</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

`web/src/vite-env.d.ts`:

```typescript
/// <reference types="vite/client" />
```

`web/src/styles/index.css` (Tailwind v4 CSS-first; dark tokens applied at `:root`, dark-only):

```css
@import "tailwindcss";

@theme {
  --color-bg: #0d1117;
  --color-panel: #161b22;
  --color-panel-2: #1c232c;
  --color-border: #2a323c;
  --color-fg: #e6edf3;
  --color-muted: #8b949e;
  --color-brand: #7c5cff;
  --color-ok: #3fb950;
  --color-warn: #d29922;
}

html, body, #root { height: 100%; }
body {
  margin: 0;
  background: var(--color-bg);
  color: var(--color-fg);
  font-family: -apple-system, "Segoe UI", Roboto, sans-serif;
}
```

`web/src/test/setup.ts`:

```typescript
import "@testing-library/jest-dom/vitest"
```

`web/src/main.tsx` (minimal for now — replaced with full wiring in Task 6):

```tsx
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import "@/styles/index.css"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <div>Nexus</div>
  </StrictMode>,
)
```

- [ ] **Step 5: Install dependencies and generate shadcn primitives**

Run from `web/`:

```bash
cd web && npm install
```

Create `web/components.json`:

```json
{
  "$schema": "https://ui.shadcn.com/schema.json",
  "style": "new-york",
  "rsc": false,
  "tsx": true,
  "tailwind": {
    "config": "",
    "css": "src/styles/index.css",
    "baseColor": "neutral",
    "cssVariables": true,
    "prefix": ""
  },
  "aliases": {
    "components": "@/components",
    "utils": "@/lib/utils",
    "ui": "@/components/ui",
    "lib": "@/lib",
    "hooks": "@/hooks"
  },
  "iconLibrary": "lucide"
}
```

Then generate the primitives (creates `src/components/ui/*.tsx` and `src/lib/utils.ts`, and pulls the needed Radix deps):

```bash
cd web && npx shadcn@latest add button card input label --yes
```

Expected: `src/components/ui/button.tsx`, `card.tsx`, `input.tsx`, `label.tsx`, and `src/lib/utils.ts` (exports `cn`) now exist.

- [ ] **Step 6: Write the smoke test**

`web/src/smoke.test.ts`:

```typescript
import { describe, it, expect } from "vitest"
import { cn } from "@/lib/utils"

describe("toolchain smoke", () => {
  it("cn merges class names", () => {
    expect(cn("a", false && "b", "c")).toBe("a c")
  })
})
```

- [ ] **Step 7: Run the smoke test — expect PASS**

Run: `cd web && npm test`
Expected: `smoke.test.ts` passes (proves Vitest, jsdom, `@` alias, and shadcn `cn` all resolve).

- [ ] **Step 8: Build the app and verify `dist` is produced**

Run: `cd web && npm run build`
Expected: exits 0; `web/dist/index.html` exists containing `<div id="root">`, and `web/dist/assets/*.js` exists.

- [ ] **Step 9: Update `web/spa_test.go` to assert the real build**

Replace the file contents with:

```go
package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("index: got %d %q", rec.Code, rec.Body.String())
	}
}

func TestFallsBackToIndexForClientRoute(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/movies/123", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("fallback: got %d %q", rec.Code, rec.Body.String())
	}
}

// TestServesHashedAsset verifies a built JS asset is served with a JS content type.
func TestServesHashedAsset(t *testing.T) {
	sub, err := fs.Sub(distFS, "dist/assets")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	var asset string
	_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, _ error) error {
		if !d.IsDir() && strings.HasSuffix(p, ".js") {
			asset = "/assets/" + p
		}
		return nil
	})
	if asset == "" {
		t.Fatal("no built .js asset found under dist/assets")
	}
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset %s: got %d", asset, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("asset %s: content-type %q not javascript", asset, ct)
	}
}
```

- [ ] **Step 10: Run the Go embed tests — expect PASS**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./web/...`
Expected: all three tests PASS against the committed real build.

- [ ] **Step 11: Add Makefile targets and drift guard**

Add to the repo-root `Makefile` (match the file's existing tab indentation for recipe lines):

```makefile
.PHONY: web web-dev web-test verify-web

web:
	cd web && npm ci && npm run build

web-dev:
	cd web && npm run dev

web-test:
	cd web && npm ci && npm test

verify-web: web
	git diff --exit-code web/dist || (echo "web/dist is stale — run 'make web' and commit"; exit 1)
```

If the Makefile has a target that builds the Go binary (e.g. `build`), add `web` as its first prerequisite so `dist` is fresh before `go build`.

- [ ] **Step 12: Commit**

```bash
git add web/ Makefile
git commit -m "feat(6-1): frontend scaffold, build pipeline, embed + drift guard"
```

---

### Task 2: Typed API client (`lib/api.ts`)

**Files:**
- Create: `web/src/lib/api.ts`, `web/src/lib/api.test.ts`

**Interfaces:**
- Produces:
  - `class ApiError extends Error { status: number; code: string }`
  - `type SystemStatus = { version: string; appName: string; healthy: boolean; taskCount: number }`
  - `apiGet<T>(path: string): Promise<T>`
  - `apiPost<T>(path: string, body?: unknown): Promise<T>`
  - `getStatus(): Promise<SystemStatus>`
  - `login(username: string, password: string): Promise<void>`
  - `logout(): Promise<void>`
  - `setUnauthorizedHandler(fn: (() => void) | null): void` — invoked whenever any request receives HTTP 401.

- [ ] **Step 1: Write the failing tests**

`web/src/lib/api.test.ts`:

```typescript
import { describe, it, expect, beforeEach, vi } from "vitest"
import { apiGet, apiPost, ApiError, getStatus, setUnauthorizedHandler } from "@/lib/api"

function mockFetch(status: number, body: unknown, contentType = "application/json") {
  return vi.fn(async () =>
    new Response(body == null ? null : JSON.stringify(body), {
      status,
      headers: { "Content-Type": contentType },
    }),
  )
}

beforeEach(() => {
  setUnauthorizedHandler(null)
  vi.restoreAllMocks()
})

describe("api client", () => {
  it("apiGet prefixes /api/v1 and sends credentials", async () => {
    const f = mockFetch(200, { ok: true })
    vi.stubGlobal("fetch", f)
    await apiGet("/system/status")
    const [url, init] = f.mock.calls[0]
    expect(url).toBe("/api/v1/system/status")
    expect((init as RequestInit).credentials).toBe("include")
  })

  it("getStatus returns typed status", async () => {
    vi.stubGlobal("fetch", mockFetch(200, { version: "0.1.0", appName: "Nexus", healthy: true, taskCount: 2 }))
    const s = await getStatus()
    expect(s.version).toBe("0.1.0")
    expect(s.taskCount).toBe(2)
  })

  it("normalizes the error envelope into ApiError", async () => {
    vi.stubGlobal("fetch", mockFetch(400, { error: { code: "bad_request", message: "invalid JSON" } }))
    await expect(apiPost("/auth/login", {})).rejects.toMatchObject({
      status: 400,
      code: "bad_request",
      message: "invalid JSON",
    })
  })

  it("wraps a non-JSON error body", async () => {
    vi.stubGlobal("fetch", mockFetch(500, "boom", "text/plain"))
    const err = await apiGet("/x").catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(500)
    expect(err.code).toBe("unknown")
  })

  it("invokes the unauthorized handler on 401", async () => {
    vi.stubGlobal("fetch", mockFetch(401, { error: { code: "unauthorized", message: "nope" } }))
    const onUnauth = vi.fn()
    setUnauthorizedHandler(onUnauth)
    await expect(getStatus()).rejects.toBeInstanceOf(ApiError)
    expect(onUnauth).toHaveBeenCalledOnce()
  })
})
```

- [ ] **Step 2: Run the tests — expect FAIL**

Run: `cd web && npm test -- api`
Expected: FAIL (`@/lib/api` has no exports / module not found).

- [ ] **Step 3: Implement `web/src/lib/api.ts`**

```typescript
const BASE = "/api/v1"

export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
  }
}

export type SystemStatus = {
  version: string
  appName: string
  healthy: boolean
  taskCount: number
}

let unauthorizedHandler: (() => void) | null = null
export function setUnauthorizedHandler(fn: (() => void) | null): void {
  unauthorizedHandler = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
    credentials: "include",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  }
  const res = await fetch(`${BASE}${path}`, init)
  if (res.status === 401 && unauthorizedHandler) unauthorizedHandler()
  if (!res.ok) throw await toApiError(res)
  if (res.status === 204) return undefined as T
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

async function toApiError(res: Response): Promise<ApiError> {
  try {
    const data = await res.clone().json()
    if (data && data.error && typeof data.error.code === "string") {
      return new ApiError(res.status, data.error.code, data.error.message ?? res.statusText)
    }
  } catch {
    // fall through to a generic error
  }
  return new ApiError(res.status, "unknown", res.statusText || "request failed")
}

export function apiGet<T>(path: string): Promise<T> {
  return request<T>("GET", path)
}
export function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return request<T>("POST", path, body)
}

export function getStatus(): Promise<SystemStatus> {
  return apiGet<SystemStatus>("/system/status")
}
export function login(username: string, password: string): Promise<void> {
  return apiPost<void>("/auth/login", { username, password })
}
export function logout(): Promise<void> {
  return apiPost<void>("/auth/logout")
}
```

- [ ] **Step 4: Run the tests — expect PASS**

Run: `cd web && npm test -- api`
Expected: all api tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/api.ts web/src/lib/api.test.ts
git commit -m "feat(6-1): typed API client with error normalization + 401 hook"
```

---

### Task 3: WebSocket client + activity ring buffer (`lib/ws.ts`)

**Files:**
- Create: `web/src/lib/ws.ts`, `web/src/lib/ws.test.ts`

**Interfaces:**
- Produces:
  - `type ActivityEvent = { id: string; type: string; data: unknown; receivedAt: number }`
  - `interface SocketLike { close(): void; onopen: (() => void) | null; onmessage: ((ev: { data: string }) => void) | null; onclose: (() => void) | null; onerror: ((e?: unknown) => void) | null }`
  - `interface WsClientOptions { url?: string; cap?: number; factory?: (url: string) => SocketLike; now?: () => number; schedule?: (fn: () => void, ms: number) => void; backoffBaseMs?: number; backoffMaxMs?: number }`
  - `interface WsClient { connect(): void; close(): void; getEvents(): ActivityEvent[]; subscribe(fn: (events: ActivityEvent[]) => void): () => void }`
  - `function createWsClient(opts?: WsClientOptions): WsClient`

- [ ] **Step 1: Write the failing tests**

`web/src/lib/ws.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { createWsClient, type SocketLike } from "@/lib/ws"

class FakeSocket implements SocketLike {
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: ((e?: unknown) => void) | null = null
  closed = false
  close() {
    this.closed = true
    this.onclose?.()
  }
  emit(type: string, data: unknown) {
    this.onmessage?.({ data: JSON.stringify({ type, data }) })
  }
}

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

function harness(cap = 50) {
  const sockets: FakeSocket[] = []
  let t = 1000
  const client = createWsClient({
    url: "ws://x/ws",
    cap,
    now: () => ++t,
    factory: () => {
      const s = new FakeSocket()
      sockets.push(s)
      return s
    },
    backoffBaseMs: 100,
    backoffMaxMs: 800,
  })
  return { client, sockets }
}

describe("ws client", () => {
  it("buffers parsed events and notifies subscribers", () => {
    const { client, sockets } = harness()
    const seen: number[] = []
    client.subscribe((evs) => seen.push(evs.length))
    client.connect()
    sockets[0].onopen?.()
    sockets[0].emit("import.completed", { title: "X" })
    const evs = client.getEvents()
    expect(evs).toHaveLength(1)
    expect(evs[0].type).toBe("import.completed")
    expect(seen.at(-1)).toBe(1)
  })

  it("keeps newest events first and caps the buffer", () => {
    const { client, sockets } = harness(3)
    client.connect()
    sockets[0].onopen?.()
    for (let i = 0; i < 5; i++) sockets[0].emit("indexer.status", { i })
    const evs = client.getEvents()
    expect(evs).toHaveLength(3)
    expect((evs[0].data as { i: number }).i).toBe(4) // newest first
    expect((evs[2].data as { i: number }).i).toBe(2)
  })

  it("ignores malformed messages without throwing", () => {
    const { client, sockets } = harness()
    client.connect()
    sockets[0].onmessage?.({ data: "not json" })
    expect(client.getEvents()).toHaveLength(0)
  })

  it("reconnects with backoff after a close", () => {
    const { client, sockets } = harness()
    client.connect()
    sockets[0].onopen?.()
    sockets[0].onclose?.() // unexpected drop
    expect(sockets).toHaveLength(1)
    vi.advanceTimersByTime(100)
    expect(sockets).toHaveLength(2) // reconnected
  })

  it("does not reconnect after an explicit close()", () => {
    const { client, sockets } = harness()
    client.connect()
    sockets[0].onopen?.()
    client.close()
    vi.advanceTimersByTime(5000)
    expect(sockets).toHaveLength(1)
  })
})
```

- [ ] **Step 2: Run the tests — expect FAIL**

Run: `cd web && npm test -- ws`
Expected: FAIL (`@/lib/ws` not found).

- [ ] **Step 3: Implement `web/src/lib/ws.ts`**

```typescript
export type ActivityEvent = { id: string; type: string; data: unknown; receivedAt: number }

export interface SocketLike {
  close(): void
  onopen: (() => void) | null
  onmessage: ((ev: { data: string }) => void) | null
  onclose: (() => void) | null
  onerror: ((e?: unknown) => void) | null
}

export interface WsClientOptions {
  url?: string
  cap?: number
  factory?: (url: string) => SocketLike
  now?: () => number
  schedule?: (fn: () => void, ms: number) => void
  backoffBaseMs?: number
  backoffMaxMs?: number
}

export interface WsClient {
  connect(): void
  close(): void
  getEvents(): ActivityEvent[]
  subscribe(fn: (events: ActivityEvent[]) => void): () => void
}

function defaultUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${window.location.host}/api/v1/ws`
}

export function createWsClient(opts: WsClientOptions = {}): WsClient {
  const url = opts.url ?? defaultUrl()
  const cap = opts.cap ?? 50
  const now = opts.now ?? (() => Date.now())
  const schedule = opts.schedule ?? ((fn, ms) => setTimeout(fn, ms))
  const factory = opts.factory ?? ((u) => new WebSocket(u) as unknown as SocketLike)
  const base = opts.backoffBaseMs ?? 500
  const max = opts.backoffMaxMs ?? 15000

  let sock: SocketLike | null = null
  let stopped = false
  let attempt = 0
  let seq = 0
  let events: ActivityEvent[] = []
  const listeners = new Set<(e: ActivityEvent[]) => void>()

  const notify = () => listeners.forEach((fn) => fn(events))

  const open = () => {
    if (stopped) return
    const s = factory(url)
    sock = s
    s.onopen = () => {
      attempt = 0
    }
    s.onmessage = (ev) => {
      let parsed: { type?: unknown; data?: unknown }
      try {
        parsed = JSON.parse(ev.data)
      } catch {
        return
      }
      if (typeof parsed.type !== "string") return
      const item: ActivityEvent = {
        id: `${now()}-${seq++}`,
        type: parsed.type,
        data: parsed.data,
        receivedAt: now(),
      }
      events = [item, ...events].slice(0, cap)
      notify()
    }
    s.onclose = () => {
      sock = null
      if (stopped) return
      const delay = Math.min(max, base * 2 ** attempt)
      attempt++
      schedule(open, delay)
    }
    s.onerror = () => s.close()
  }

  return {
    connect() {
      stopped = false
      if (!sock) open()
    },
    close() {
      stopped = true
      sock?.close()
      sock = null
    },
    getEvents: () => events,
    subscribe(fn) {
      listeners.add(fn)
      fn(events)
      return () => listeners.delete(fn)
    },
  }
}
```

- [ ] **Step 4: Run the tests — expect PASS**

Run: `cd web && npm test -- ws`
Expected: all ws tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/ws.ts web/src/lib/ws.test.ts
git commit -m "feat(6-1): websocket client with reconnect + activity ring buffer"
```

---

### Task 4: Relative-time util + auth context (`lib/time.ts`, `lib/auth.tsx`)

**Files:**
- Create: `web/src/lib/time.ts`, `web/src/lib/time.test.ts`, `web/src/lib/auth.tsx`, `web/src/lib/auth.test.tsx`

**Interfaces:**
- Consumes: `getStatus`, `login`, `logout`, `ApiError`, `setUnauthorizedHandler` from `@/lib/api`.
- Produces:
  - `relativeTime(from: number, now?: number): string` (e.g. `"just now"`, `"3s ago"`, `"2m ago"`, `"1h ago"`).
  - `type AuthStatus = "loading" | "authed" | "unauthed"`
  - `AuthProvider` component (wraps children, runs the boot probe, registers the 401 handler).
  - `useAuth(): { status: AuthStatus; login(u: string, p: string): Promise<void>; logout(): Promise<void>; refresh(): Promise<void> }`
  - `RequireAuth` component — renders children when authed, `<Navigate to="/login" replace>` when unauthed, `null` while loading.

- [ ] **Step 1: Write the failing tests**

`web/src/lib/time.test.ts`:

```typescript
import { describe, it, expect } from "vitest"
import { relativeTime } from "@/lib/time"

describe("relativeTime", () => {
  const now = 1_000_000
  it("shows just now under 5s", () => {
    expect(relativeTime(now - 2_000, now)).toBe("just now")
  })
  it("shows seconds", () => {
    expect(relativeTime(now - 12_000, now)).toBe("12s ago")
  })
  it("shows minutes", () => {
    expect(relativeTime(now - 3 * 60_000, now)).toBe("3m ago")
  })
  it("shows hours", () => {
    expect(relativeTime(now - 2 * 3_600_000, now)).toBe("2h ago")
  })
})
```

`web/src/lib/auth.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Routes, Route } from "react-router-dom"
import { AuthProvider, RequireAuth, useAuth } from "@/lib/auth"
import * as api from "@/lib/api"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, getStatus: vi.fn(), login: vi.fn(), logout: vi.fn() }
})

function Protected() {
  return <div>secret</div>
}
function LoginStub() {
  const { login } = useAuth()
  return <button onClick={() => login("a", "b")}>do-login</button>
}

function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<LoginStub />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <Protected />
            </RequireAuth>
          }
        />
      </Routes>
    </AuthProvider>
  )
}

beforeEach(() => vi.clearAllMocks())

describe("auth", () => {
  it("renders protected content when the probe succeeds", async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ version: "1", appName: "Nexus", healthy: true, taskCount: 0 })
    render(<MemoryRouter initialEntries={["/"]}><App /></MemoryRouter>)
    expect(await screen.findByText("secret")).toBeInTheDocument()
  })

  it("redirects to /login when the probe 401s", async () => {
    vi.mocked(api.getStatus).mockRejectedValue(new api.ApiError(401, "unauthorized", "no"))
    render(<MemoryRouter initialEntries={["/"]}><App /></MemoryRouter>)
    expect(await screen.findByText("do-login")).toBeInTheDocument()
  })

  it("login() then a successful probe reveals protected content", async () => {
    vi.mocked(api.getStatus)
      .mockRejectedValueOnce(new api.ApiError(401, "unauthorized", "no"))
      .mockResolvedValue({ version: "1", appName: "Nexus", healthy: true, taskCount: 0 })
    vi.mocked(api.login).mockResolvedValue()
    render(<MemoryRouter initialEntries={["/"]}><App /></MemoryRouter>)
    await screen.findByText("do-login")
    await userEvent.click(screen.getByText("do-login"))
    expect(await screen.findByText("secret")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the tests — expect FAIL**

Run: `cd web && npm test -- "time|auth"`
Expected: FAIL (`@/lib/time` and `@/lib/auth` not found).

- [ ] **Step 3: Implement `web/src/lib/time.ts`**

```typescript
export function relativeTime(from: number, now: number = Date.now()): string {
  const s = Math.max(0, Math.floor((now - from) / 1000))
  if (s < 5) return "just now"
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}
```

- [ ] **Step 4: Implement `web/src/lib/auth.tsx`**

```tsx
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react"
import { Navigate } from "react-router-dom"
import { ApiError, getStatus, login as apiLogin, logout as apiLogout, setUnauthorizedHandler } from "@/lib/api"

export type AuthStatus = "loading" | "authed" | "unauthed"

type AuthContextValue = {
  status: AuthStatus
  login: (u: string, p: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading")
  const mounted = useRef(true)

  const refresh = useCallback(async () => {
    try {
      await getStatus()
      if (mounted.current) setStatus("authed")
    } catch {
      // Any failed probe (401 or transient error) sends the user to /login for
      // this slice; a network hiccup on boot simply re-prompts. Accepted.
      if (mounted.current) setStatus("unauthed")
    }
  }, [])

  const login = useCallback(
    async (u: string, p: string) => {
      await apiLogin(u, p) // throws ApiError(401) on bad credentials; caller handles
      await refresh()
    },
    [refresh],
  )

  const logout = useCallback(async () => {
    try {
      await apiLogout()
    } catch {
      // ignore — we clear local state regardless
    }
    if (mounted.current) setStatus("unauthed")
  }, [])

  useEffect(() => {
    mounted.current = true
    setUnauthorizedHandler(() => setStatus("unauthed"))
    void refresh()
    return () => {
      mounted.current = false
      setUnauthorizedHandler(null)
    }
  }, [refresh])

  return <AuthContext.Provider value={{ status, login, logout, refresh }}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error("useAuth must be used within AuthProvider")
  return ctx
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth()
  if (status === "loading") return null
  if (status === "unauthed") return <Navigate to="/login" replace />
  return <>{children}</>
}

export { ApiError }
```

- [ ] **Step 5: Run the tests — expect PASS**

Run: `cd web && npm test -- "time|auth"`
Expected: all time + auth tests PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/time.ts web/src/lib/time.test.ts web/src/lib/auth.tsx web/src/lib/auth.test.tsx
git commit -m "feat(6-1): relative-time util + auth context/probe/guard"
```

---

### Task 5: App shell — layout, sidebar, top bar, placeholder, activity provider

**Files:**
- Create: `web/src/app/Sidebar.tsx`, `web/src/app/TopBar.tsx`, `web/src/app/Layout.tsx`, `web/src/lib/activity.tsx`, `web/src/pages/Placeholder.tsx`, `web/src/app/Sidebar.test.tsx`, `web/src/pages/Placeholder.test.tsx`

**Interfaces:**
- Consumes: `useAuth` from `@/lib/auth`; `createWsClient`, `ActivityEvent` from `@/lib/ws`.
- Produces:
  - `NAV_ITEMS: { to: string; label: string; icon: LucideIcon }[]` (exported from `Sidebar.tsx`).
  - `Sidebar` component (labeled nav, active-route highlight via `NavLink`).
  - `TopBar({ title }: { title: string })` — shows the page title + a logout button.
  - `Layout` — the shell frame: `<Sidebar/>` + `<TopBar/>` + `<Outlet/>`, wrapped in `ActivityProvider`.
  - `ActivityProvider` + `useActivity(): ActivityEvent[]` in `@/lib/activity` (owns one `WsClient`, connects on mount, closes on unmount).
  - `Placeholder({ title }: { title: string })`.

- [ ] **Step 1: Write the failing tests**

`web/src/app/Sidebar.test.tsx`:

```tsx
import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { Sidebar, NAV_ITEMS } from "@/app/Sidebar"

describe("Sidebar", () => {
  it("renders all nav items", () => {
    render(<MemoryRouter initialEntries={["/"]}><Sidebar /></MemoryRouter>)
    for (const item of NAV_ITEMS) {
      expect(screen.getByRole("link", { name: new RegExp(item.label, "i") })).toBeInTheDocument()
    }
  })

  it("marks the active route with aria-current", () => {
    render(<MemoryRouter initialEntries={["/movies"]}><Sidebar /></MemoryRouter>)
    const active = screen.getByRole("link", { name: /movies/i })
    expect(active).toHaveAttribute("aria-current", "page")
  })
})
```

`web/src/pages/Placeholder.test.tsx`:

```tsx
import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { Placeholder } from "@/pages/Placeholder"

describe("Placeholder", () => {
  it("shows the title and a coming-soon note", () => {
    render(<Placeholder title="Movies" />)
    expect(screen.getByRole("heading", { name: "Movies" })).toBeInTheDocument()
    expect(screen.getByText(/later slice/i)).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the tests — expect FAIL**

Run: `cd web && npm test -- "Sidebar|Placeholder"`
Expected: FAIL (components not found).

- [ ] **Step 3: Implement `web/src/lib/activity.tsx`**

```tsx
import { createContext, useContext, useEffect, useState, type ReactNode } from "react"
import { createWsClient, type ActivityEvent } from "@/lib/ws"

const ActivityContext = createContext<ActivityEvent[]>([])

export function ActivityProvider({ children }: { children: ReactNode }) {
  const [events, setEvents] = useState<ActivityEvent[]>([])
  useEffect(() => {
    const client = createWsClient()
    const unsub = client.subscribe(setEvents)
    client.connect()
    return () => {
      unsub()
      client.close()
    }
  }, [])
  return <ActivityContext.Provider value={events}>{children}</ActivityContext.Provider>
}

export function useActivity(): ActivityEvent[] {
  return useContext(ActivityContext)
}
```

- [ ] **Step 4: Implement `web/src/app/Sidebar.tsx`**

```tsx
import { NavLink } from "react-router-dom"
import { LayoutDashboard, Film, Tv, Calendar, Activity, Settings, Cpu, type LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"

export const NAV_ITEMS: { to: string; label: string; icon: LucideIcon }[] = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/movies", label: "Movies", icon: Film },
  { to: "/tv", label: "TV Shows", icon: Tv },
  { to: "/calendar", label: "Calendar", icon: Calendar },
  { to: "/activity", label: "Activity", icon: Activity },
  { to: "/settings", label: "Settings", icon: Settings },
  { to: "/system", label: "System", icon: Cpu },
]

export function Sidebar() {
  return (
    <aside className="w-52 shrink-0 border-r border-[var(--color-border)] bg-[#10151c] py-4">
      <div className="flex items-center gap-2 px-5 pb-4 text-lg font-bold">
        <span className="h-5 w-5 rounded-md bg-gradient-to-br from-[var(--color-brand)] to-[#4da8ff]" />
        Nexus
      </div>
      <nav className="flex flex-col gap-0.5 px-2.5">
        {NAV_ITEMS.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === "/"}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm text-[var(--color-muted)]",
                isActive && "bg-[rgba(124,92,255,0.16)] font-semibold text-[var(--color-fg)]",
              )
            }
          >
            <Icon className="h-4 w-4" />
            {label}
          </NavLink>
        ))}
      </nav>
    </aside>
  )
}
```

- [ ] **Step 5: Implement `web/src/app/TopBar.tsx`**

```tsx
import { useNavigate } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { useAuth } from "@/lib/auth"

export function TopBar({ title }: { title: string }) {
  const { logout } = useAuth()
  const navigate = useNavigate()
  const onLogout = async () => {
    await logout()
    navigate("/login", { replace: true })
  }
  return (
    <header className="flex items-center justify-between border-b border-[var(--color-border)] bg-[var(--color-panel)] px-6 py-3.5">
      <h1 className="text-base font-semibold">{title}</h1>
      <Button variant="ghost" size="sm" onClick={onLogout}>
        Log out
      </Button>
    </header>
  )
}
```

- [ ] **Step 6: Implement `web/src/pages/Placeholder.tsx`**

```tsx
export function Placeholder({ title }: { title: string }) {
  return (
    <div className="p-6">
      <h2 className="text-xl font-semibold">{title}</h2>
      <p className="mt-2 text-[var(--color-muted)]">This page ships in a later slice of the Web UI.</p>
    </div>
  )
}
```

- [ ] **Step 7: Implement `web/src/app/Layout.tsx`**

```tsx
import { Outlet, useLocation } from "react-router-dom"
import { Sidebar, NAV_ITEMS } from "@/app/Sidebar"
import { TopBar } from "@/app/TopBar"
import { ActivityProvider } from "@/lib/activity"

function titleForPath(pathname: string): string {
  const match = NAV_ITEMS.find((n) => (n.to === "/" ? pathname === "/" : pathname.startsWith(n.to)))
  return match?.label ?? "Nexus"
}

export function Layout() {
  const { pathname } = useLocation()
  return (
    <ActivityProvider>
      <div className="flex h-screen overflow-hidden">
        <Sidebar />
        <div className="flex min-w-0 flex-1 flex-col">
          <TopBar title={titleForPath(pathname)} />
          <main className="flex-1 overflow-auto">
            <Outlet />
          </main>
        </div>
      </div>
    </ActivityProvider>
  )
}
```

- [ ] **Step 8: Run the tests — expect PASS**

Run: `cd web && npm test -- "Sidebar|Placeholder"`
Expected: both test files PASS.

- [ ] **Step 9: Commit**

```bash
git add web/src/app web/src/lib/activity.tsx web/src/pages/Placeholder.tsx
git commit -m "feat(6-1): app shell (sidebar/topbar/layout) + activity provider + placeholder"
```

---

### Task 6: Login page + full app wiring (`pages/Login.tsx`, `app/routes.tsx`, `main.tsx`)

**Files:**
- Create: `web/src/pages/Login.tsx`, `web/src/pages/Login.test.tsx`, `web/src/app/routes.tsx`
- Modify: `web/src/main.tsx` (replace the Task 1 minimal body with full provider + router wiring)

**Interfaces:**
- Consumes: `useAuth` (`@/lib/auth`), `ApiError` (`@/lib/api`), `Layout` (`@/app/Layout`), `RequireAuth` (`@/lib/auth`), `Placeholder` (`@/pages/Placeholder`), `Dashboard` (`@/pages/Dashboard`, created in Task 7).
- Produces:
  - `Login` component — username/password form; on submit calls `login`, navigates to `/` on success, shows "Invalid username or password" on `ApiError` 401, a generic message otherwise.
  - `router` (from `createBrowserRouter`) in `routes.tsx` wiring `/login` and the `RequireAuth`+`Layout` protected tree.

> **Ordering note:** Task 7 creates `@/pages/Dashboard`. Implement this task's `routes.tsx` importing `Dashboard`; if executing tasks strictly in order, temporarily point the index route at `<Placeholder title="Dashboard" />` and switch to `<Dashboard />` in Task 7 Step 6. The build must stay green at each commit.

- [ ] **Step 1: Write the failing test**

`web/src/pages/Login.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Routes, Route } from "react-router-dom"
import { AuthProvider } from "@/lib/auth"
import { Login } from "@/pages/Login"
import * as api from "@/lib/api"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, getStatus: vi.fn(), login: vi.fn(), logout: vi.fn() }
})

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={["/login"]}>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/" element={<div>home</div>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getStatus).mockRejectedValue(new api.ApiError(401, "unauthorized", "no"))
})

describe("Login", () => {
  it("logs in and navigates home on success", async () => {
    vi.mocked(api.login).mockResolvedValue()
    // Boot probe (while on /login) 401s → unauthed; the post-login probe succeeds.
    vi.mocked(api.getStatus)
      .mockReset()
      .mockRejectedValueOnce(new api.ApiError(401, "unauthorized", "no"))
      .mockResolvedValue({ version: "1", appName: "Nexus", healthy: true, taskCount: 0 })
    renderLogin()
    await userEvent.type(screen.getByLabelText(/username/i), "admin")
    await userEvent.type(screen.getByLabelText(/password/i), "secret")
    await userEvent.click(screen.getByRole("button", { name: /sign in/i }))
    expect(await screen.findByText("home")).toBeInTheDocument()
  })

  it("shows an error on invalid credentials", async () => {
    vi.mocked(api.login).mockRejectedValue(new api.ApiError(401, "unauthorized", "invalid credentials"))
    renderLogin()
    await userEvent.type(screen.getByLabelText(/username/i), "admin")
    await userEvent.type(screen.getByLabelText(/password/i), "wrong")
    await userEvent.click(screen.getByRole("button", { name: /sign in/i }))
    expect(await screen.findByText(/invalid username or password/i)).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the test — expect FAIL**

Run: `cd web && npm test -- Login`
Expected: FAIL (`@/pages/Login` not found).

- [ ] **Step 3: Implement `web/src/pages/Login.tsx`**

```tsx
import { useState, type FormEvent } from "react"
import { useNavigate } from "react-router-dom"
import { ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function Login() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      await login(username, password)
      navigate("/", { replace: true })
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) setError("Invalid username or password")
      else setError("Something went wrong. Please try again.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-[var(--color-bg)]">
      <form
        onSubmit={onSubmit}
        className="w-80 rounded-xl border border-[var(--color-border)] bg-[var(--color-panel)] p-6"
      >
        <div className="mb-5 flex items-center gap-2 text-lg font-bold">
          <span className="h-5 w-5 rounded-md bg-gradient-to-br from-[var(--color-brand)] to-[#4da8ff]" />
          Nexus
        </div>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="username">Username</Label>
            <Input id="username" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="password">Password</Label>
            <Input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          {error && <p className="text-sm text-red-400">{error}</p>}
          <Button type="submit" className="w-full" disabled={busy}>
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </div>
      </form>
    </div>
  )
}
```

- [ ] **Step 4: Implement `web/src/app/routes.tsx`**

```tsx
import { createBrowserRouter } from "react-router-dom"
import { RequireAuth } from "@/lib/auth"
import { Layout } from "@/app/Layout"
import { Login } from "@/pages/Login"
import { Placeholder } from "@/pages/Placeholder"
import { Dashboard } from "@/pages/Dashboard"

export const router = createBrowserRouter([
  { path: "/login", element: <Login /> },
  {
    path: "/",
    element: (
      <RequireAuth>
        <Layout />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <Dashboard /> },
      { path: "movies", element: <Placeholder title="Movies" /> },
      { path: "tv", element: <Placeholder title="TV Shows" /> },
      { path: "calendar", element: <Placeholder title="Calendar" /> },
      { path: "activity", element: <Placeholder title="Activity" /> },
      { path: "settings", element: <Placeholder title="Settings" /> },
      { path: "system", element: <Placeholder title="System" /> },
    ],
  },
])
```

- [ ] **Step 5: Replace `web/src/main.tsx` with full wiring**

```tsx
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { RouterProvider } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { AuthProvider } from "@/lib/auth"
import { router } from "@/app/routes"
import "@/styles/index.css"

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </QueryClientProvider>
  </StrictMode>,
)
```

> If Task 7 is not yet done, comment out the `Dashboard` import in `routes.tsx` and use `<Placeholder title="Dashboard" />` for the index route so the build compiles; revert in Task 7.

- [ ] **Step 6: Run the test — expect PASS**

Run: `cd web && npm test -- Login`
Expected: both Login tests PASS.

- [ ] **Step 7: Type-check the app wiring**

Run: `cd web && npx tsc -b`
Expected: exits 0 (no type errors across routes/main/providers).

- [ ] **Step 8: Commit**

```bash
git add web/src/pages/Login.tsx web/src/pages/Login.test.tsx web/src/app/routes.tsx web/src/main.tsx
git commit -m "feat(6-1): login page + router/provider wiring"
```

---

### Task 7: Dashboard — status cards + live activity feed

**Files:**
- Create: `web/src/pages/Dashboard.tsx`, `web/src/pages/Dashboard.test.tsx`
- Modify: `web/src/app/routes.tsx` (point index route at `<Dashboard/>` if it was stubbed)

**Interfaces:**
- Consumes: `getStatus` (`@/lib/api`) via TanStack Query; `useActivity` (`@/lib/activity`); `relativeTime` (`@/lib/time`); `Card` (`@/components/ui/card`).
- Produces: `Dashboard` component (default page at `/`).

- [ ] **Step 1: Write the failing test**

`web/src/pages/Dashboard.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { Dashboard } from "@/pages/Dashboard"
import * as api from "@/lib/api"
import * as activity from "@/lib/activity"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, getStatus: vi.fn() }
})
vi.mock("@/lib/activity", async (orig) => {
  const actual = await orig<typeof import("@/lib/activity")>()
  return { ...actual, useActivity: vi.fn() }
})

function renderDash() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <Dashboard />
    </QueryClientProvider>,
  )
}

beforeEach(() => vi.clearAllMocks())

describe("Dashboard", () => {
  it("renders status cards from /system/status", async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ version: "0.1.0", appName: "Nexus", healthy: true, taskCount: 3 })
    vi.mocked(activity.useActivity).mockReturnValue([])
    renderDash()
    expect(await screen.findByText("0.1.0")).toBeInTheDocument()
    expect(screen.getByText(/healthy/i)).toBeInTheDocument()
    expect(screen.getByText("3")).toBeInTheDocument()
  })

  it("shows an empty state when there are no events", async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ version: "0.1.0", appName: "Nexus", healthy: true, taskCount: 0 })
    vi.mocked(activity.useActivity).mockReturnValue([])
    renderDash()
    expect(await screen.findByText(/no activity yet/i)).toBeInTheDocument()
  })

  it("renders activity rows from the WS store", async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ version: "0.1.0", appName: "Nexus", healthy: true, taskCount: 0 })
    vi.mocked(activity.useActivity).mockReturnValue([
      { id: "1", type: "import.completed", data: { title: "The Bear" }, receivedAt: Date.now() },
    ])
    renderDash()
    expect(await screen.findByText("import.completed")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the test — expect FAIL**

Run: `cd web && npm test -- Dashboard`
Expected: FAIL (`@/pages/Dashboard` not found).

- [ ] **Step 3: Implement `web/src/pages/Dashboard.tsx`**

```tsx
import { useQuery } from "@tanstack/react-query"
import { getStatus } from "@/lib/api"
import { useActivity } from "@/lib/activity"
import { relativeTime } from "@/lib/time"
import { Card } from "@/components/ui/card"

function StatCard({ label, value, ok }: { label: string; value: string; ok?: boolean }) {
  return (
    <Card className="border-[var(--color-border)] bg-[var(--color-panel)] p-4">
      <div className="text-xs uppercase tracking-wide text-[var(--color-muted)]">{label}</div>
      <div className={`mt-2 text-2xl font-bold ${ok ? "text-[var(--color-ok)]" : ""}`}>{value}</div>
    </Card>
  )
}

export function Dashboard() {
  const status = useQuery({ queryKey: ["system-status"], queryFn: getStatus })
  const events = useActivity()

  return (
    <div className="p-6">
      <div className="mb-6 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard label="Version" value={status.data?.version ?? (status.isError ? "—" : "…")} />
        <StatCard
          label="Health"
          value={status.isError ? "Unknown" : status.data?.healthy ? "Healthy" : status.data ? "Unhealthy" : "…"}
          ok={status.data?.healthy}
        />
        <StatCard label="Active Tasks" value={status.data ? String(status.data.taskCount) : status.isError ? "—" : "…"} />
      </div>

      <div className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-panel)]">
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-4 py-3">
          <span className="text-sm font-semibold">Activity</span>
          <span className="text-xs font-semibold text-[var(--color-ok)]">LIVE</span>
        </div>
        {events.length === 0 ? (
          <div className="px-4 py-8 text-center text-sm text-[var(--color-muted)]">No activity yet.</div>
        ) : (
          <ul>
            {events.map((e) => (
              <li
                key={e.id}
                className="flex items-center gap-3 border-b border-[var(--color-border)] px-4 py-2.5 text-sm last:border-b-0"
              >
                <span className="rounded-full border border-[var(--color-border)] bg-[var(--color-panel-2)] px-2 py-0.5 text-xs text-[var(--color-muted)]">
                  {e.type}
                </span>
                <span className="min-w-0 flex-1 truncate text-[var(--color-muted)]">{describe(e.data)}</span>
                <span className="whitespace-nowrap text-xs text-[var(--color-muted)]">{relativeTime(e.receivedAt)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function describe(data: unknown): string {
  if (data && typeof data === "object") {
    const o = data as Record<string, unknown>
    const title = o.title ?? o.name ?? o.message
    if (typeof title === "string") return title
  }
  return ""
}
```

- [ ] **Step 4: Run the test — expect PASS**

Run: `cd web && npm test -- Dashboard`
Expected: all three Dashboard tests PASS.

- [ ] **Step 5: Point the index route at the real Dashboard**

Ensure `web/src/app/routes.tsx` imports `Dashboard` from `@/pages/Dashboard` and uses `{ index: true, element: <Dashboard /> }` (revert any temporary placeholder stub from Task 6).

- [ ] **Step 6: Full verification — build, type-check, all tests, embed, drift guard**

Run each; all must pass:

```bash
cd web && npm ci && npm run build && npm test
export PATH="/c/Program Files/Go/bin:$PATH"
cd .. && CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...
git add -A web/dist && git diff --cached --exit-code web/dist >/dev/null && echo "dist committed clean" || echo "dist changed — will be committed this task"
```

Expected: Vitest all green; `go build`/`vet`/`test` all green (incl. `web/spa_test.go` against the freshly built `dist`).

- [ ] **Step 7: Commit (including the rebuilt `web/dist`)**

```bash
git add web/src/pages/Dashboard.tsx web/src/pages/Dashboard.test.tsx web/src/app/routes.tsx web/dist
git commit -m "feat(6-1): dashboard status cards + live activity feed"
```

---

## Self-Review

**Spec coverage:**
- §2/§3.1 committed `dist` + drift guard + Makefile → Task 1 (Steps 8, 11) and Task 7 Step 6.
- §3.1 Vite dev proxy — Task 1 Step 3 (`server.proxy` → `localhost:9494`, `ws: true`).
- §3.2 serving/routing + placeholder pages → Task 5 (Placeholder), Task 6 (routes).
- §3.3 auth probe/login/logout/401 → Task 2 (`setUnauthorizedHandler`), Task 4 (AuthProvider/RequireAuth), Task 6 (Login).
- §3.4 API client/error envelope → Task 2.
- §3.5 WS client/reconnect/ring buffer → Task 3; React binding → Task 5 (`activity.tsx`).
- §3.6 dark tokens + labeled sidebar + top bar → Task 1 (tokens), Task 5.
- §3.7 Dashboard cards + feed → Task 7.
- §6 Vitest suites + updated `spa_test.go` → Tasks 2–7 + Task 1 Step 9.
- §8 acceptance criteria → covered by the Task 7 Step 6 full-verification gate.

**Placeholder scan:** No TBD/TODO; every code step contains complete code. The one forward reference (routes → Dashboard) is called out explicitly with a compile-green fallback.

**Type consistency:** `SystemStatus`, `ApiError(status,code,message)`, `ActivityEvent{id,type,data,receivedAt}`, `AuthStatus`, `NAV_ITEMS`, `useActivity()`, `createWsClient` — names/signatures match across Tasks 2–7.
