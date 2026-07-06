# Nexus Web UI: Shell / Foundation (Sub-project 6, Slice 1) Design

## 1. Goal

Deliver the web application **shell** and the **frontend build pipeline** that every
later UI slice (media library, settings, activity, calendar) plugs into, plus one
real data-bound page — the **Dashboard**. This slice proves the whole transport
layer end-to-end (login → REST → WebSocket) inside a routed, themed app frame.

Sub-project 6 (Web UI) is decomposed into 5 slices, each with its own
spec → plan → build cycle:

1. **Shell / foundation** — *this slice*.
2. Media library (browse/add/detail/monitor).
3. Settings (indexers, download clients, quality profiles, naming).
4. Activity (queue, history, manual search, automation config).
5. Calendar (upcoming/airing — the view deferred into sub-6).

**Key constraint: Slice 1 adds NO new backend endpoints and changes no Go code
under `internal/**` or `cmd/**`.** It consumes only the already-shipped
`GET /api/v1/system/status`, `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`,
and the `GET /api/v1/ws` WebSocket. `web/embed.go` is unchanged (the embed already serves
`dist/` with SPA fallback, and the API router already routes unknown non-`/api`
paths to the SPA). `web/spa_test.go` stays but its **assertions are updated**: they
currently match the placeholder string `"Nexus is running"`, which disappears once
`dist/` becomes the real Vite build — so they must instead assert stable markers of
the built index (the `<div id="root">` mount point and a hashed JS asset).

## 2. Settled decisions (from brainstorming)

- **Tech stack (fixed by foundation spec §4):** React 18 + TypeScript + Tailwind CSS
  + shadcn/ui. Transport: REST + WebSocket. Not re-opened.
- **Additional libraries:** Vite (build/dev), react-router v6 (client routing),
  TanStack Query v5 (REST data fetching/cache).
- **`web/dist` is committed to git.** A bare `CGO_ENABLED=0 go build ./...` must keep
  producing a complete, working, cross-compilable UI binary with **no Node
  required** — this is the foundation spec's single-binary packaging thesis.
  Gitignoring `dist` would break it (a bare `go build` would embed a stale
  placeholder, and later slices are built/verified by subagents running the Go
  ritual without Node). Source/`dist` drift is mitigated by a mechanical drift
  guard (§3.2), not by relying on discipline.
- **Nav style:** labeled left sidebar (classic Sonarr/Radarr feel), **dark theme
  only** for this slice (light theme deferred; tokens must stay theme-clean so it
  can slot in later).
- **Dashboard scope:** system-status cards **plus** a live activity feed rendered
  from the WebSocket — exercises both REST and WS in Slice 1.
- **Activity feed is live-only.** There is no event-history endpoint, so the feed
  shows only events received since page load, capped to a ring buffer (~50), and is
  cleared on reload. Accepted slice-1 limitation.
- **Frontend testing:** Vitest + React Testing Library for unit + key component
  tests. No browser E2E (Playwright) in this slice. The Go `spa_test.go` stays.

## 3. Architecture

All new code lives under `web/src/**` plus config files under `web/`. Nothing under
`internal/**` or `cmd/**` changes.

```
web/
  package.json  vite.config.ts  tsconfig.json  tailwind.config.ts  index.html
  dist/                      # committed build output (embed.FS source)
  embed.go                 # unchanged
  spa_test.go              # kept; assertions updated for the real built index
  src/
    main.tsx                 # React root, router, QueryClient, providers
    app/
      Layout.tsx             # sidebar + top bar frame
      Sidebar.tsx  TopBar.tsx
      routes.tsx             # route table
    pages/
      Dashboard.tsx
      Login.tsx
      Placeholder.tsx        # reused by Movies/TV/Calendar/Activity/Settings/System
    lib/
      api.ts                 # typed fetch client
      ws.ts                  # WebSocket client + activity store
      auth.tsx               # auth probe + guard/context
      time.ts                # relative-time formatting
    components/ui/           # shadcn/ui generated primitives
    styles/index.css         # Tailwind + dark theme tokens
```

### 3.1 Build & dev toolchain (`web/`, `Makefile`)

- **Vite** builds `web/src` → `web/dist` (Vite `build.outDir = "dist"`), committed.
  `index.html` at `web/` is the Vite entry; built asset filenames are content-hashed.
- **Makefile targets:**
  - `web` → `cd web && npm ci && npm run build`; add as a prerequisite of the
    binary/release build target so `dist` is fresh before `go build`.
  - `web-dev` → `cd web && npm run dev` (Vite dev server on `:5173`).
  - `web-test` → `cd web && npm test` (Vitest, run once/non-watch in CI).
  - `verify-web` (drift guard) → rebuild `dist`, then `git diff --exit-code
    web/dist` — non-empty diff fails the build, catching a stale committed `dist`.
- **Dev proxy:** `vite.config.ts` proxies `/api` and `/ws` (with `ws: true`) to the
  Go backend's address so the Vite dev server gets HMR while the cookie + WebSocket
  flow through to the real backend. `web/node_modules/` is already gitignored.

### 3.2 Serving & routing

- **Prod:** unchanged Go path — `web.Handler()` serves embedded `dist`; the API
  router's `NotFound` sends unknown non-`/api` paths to `index.html` for client-side
  routing (both already implemented).
- **Client routes (react-router):** `/` Dashboard, `/login`, and placeholder pages
  at `/movies`, `/tv`, `/calendar`, `/activity`, `/settings`, `/system`. Placeholders
  render a shared "coming in a later slice" component inside the full shell so the
  layout, nav highlighting, and routing are proven now.

### 3.3 Auth (`lib/auth.tsx`)

The session cookie (`nexus_session`) is HttpOnly, so JS cannot read auth state
directly. Instead:

- **Boot probe:** on app load, call `GET /api/v1/system/status`. `200` → authenticated,
  render the app. `401` → redirect to `/login`.
- **Login:** `Login.tsx` posts `{username, password}` to `/api/v1/auth/login` (server
  sets the cookie), then re-probes `/system/status` and navigates to `/` on success;
  shows an inline error on `401`.
- **401 interceptor:** the API client (§3.4) surfaces `401` distinctly; a global
  handler clears cached auth state and redirects to `/login`.
- **Logout:** top-bar action posts `/api/v1/auth/logout`, then routes to `/login`.
- All requests set `credentials: "include"` (same-origin in prod; dev proxy forwards
  the cookie).

### 3.4 API client (`lib/api.ts`)

Thin typed wrapper over `fetch` with base path `/api/v1`:

- JSON request/response helpers; sets `credentials: "include"`.
- Normalizes the backend error envelope `{error:{code,message}}` (emitted by
  `WriteError`) into a typed `ApiError{status, code, message}`; a bare non-JSON
  error becomes `ApiError{status, code:"unknown", message:<statusText>}`.
- `401` is thrown as an `ApiError` that the auth layer recognizes.
- Typed `getStatus()` returns `{version, appName, healthy, taskCount}` (matches
  `statusResponse`).

### 3.5 WebSocket client + activity store (`lib/ws.ts`)

- Connects to `/api/v1/ws`, deriving `ws://`/`wss://` from `window.location`.
- Parses the server envelope `{type: string, data: any}` (matches `wsMessage`).
  Recognized types include `task.updated` plus the forwarded events
  (`indexer.status`, `download.status`, `media.series.updated`,
  `media.movie.updated`, `import.completed`, `queue.updated`,
  `automation.search.completed`, `automation.rss.completed`,
  `automation.upgrade.completed`).
- **Auto-reconnect** with exponential backoff (capped) on close/error.
- Maintains an **in-memory ring buffer** of the most recent ~50 events (live-only,
  not persisted; cleared on reload) exposed via a subscribe API the Dashboard reads.
  A per-event `id` + `receivedAt` is assigned client-side for React keys and
  relative-time display.

### 3.6 Layout & theme (`app/**`, `styles/index.css`)

- **Dark-only** theme expressed as Tailwind CSS-variable design tokens (background,
  panel, border, text, muted, brand, ok/warn) so a future light theme is a token
  swap, not a component rewrite.
- **Shell:** labeled left sidebar (Dashboard / Movies / TV Shows / Calendar /
  Activity / Settings / System) with active-route highlighting, and a top bar
  (current page title, health dot, version, logout menu).
- **Responsive:** below a breakpoint the sidebar collapses to a toggle-able drawer
  (basic; full mobile polish deferred).

### 3.7 Dashboard (`pages/Dashboard.tsx`)

- Three status cards from `GET /system/status` via TanStack Query: **Version**,
  **Health** (Healthy/Unhealthy), **Active Tasks** (`taskCount`).
- **Live Activity feed:** subscribes to the WS activity store; each row shows a
  type tag, a human-readable message derived from the event, and a relative
  timestamp. Renders an empty state until the first event arrives.

## 4. Data flow

**Boot:** load app → `GET /system/status` → 200 render shell / 401 → `/login`.
**Login:** submit → `POST /auth/login` (cookie set) → re-probe `/system/status` → `/`.
**Dashboard:** TanStack Query fetches `/system/status` (cards); WS client opens
`/api/v1/ws`, pushes each `{type,data}` into the ring buffer; the feed re-renders.
**Session loss:** any request returns `401` → auth handler → redirect `/login`.

## 5. Error handling

- Failed `/system/status` (non-401) on the Dashboard → cards show an error/retry
  state (TanStack Query error), not a crash.
- WS disconnect → reconnect with backoff; the feed keeps its buffered rows and a
  subtle "reconnecting" indicator may show. No user-facing crash.
- Login failure (`401`) → inline "invalid credentials"; other errors → generic
  inline error.
- API error envelope always normalized to `ApiError`; the UI never renders raw
  `[object Object]` or unparsed bodies.

## 6. Testing

**Vitest + React Testing Library (`web/src/**`):**

- **Unit:** `api.ts` (base-path URL building, error-envelope normalization, 401
  surfacing); `ws.ts` (envelope parse, reconnect/backoff scheduling with fake
  timers, ring-buffer cap/eviction); `auth` probe logic; `time.ts` relative
  formatting.
- **Component:** `Login` (submit → success navigates, 401 → inline error);
  `Dashboard` (renders three cards from a mocked query; appends rows from mocked WS
  events; shows empty state first); `Sidebar` (active-route highlight);
  `Placeholder` page renders inside the shell.

**Go:** `web/spa_test.go` is updated to assert against the **real built `dist`**
(its old assertions match the placeholder string `"Nexus is running"`, which no
longer exists). New assertions: `/` serves the built index containing
`<div id="root">`; a hashed JS asset under `/assets/…` is served with a JS
content type; an unknown non-`/api` path (e.g. `/movies/123`) falls back to the
built `index.html`.

**Slice verification ritual (all must pass):**
`cd web && npm ci && npm run build && npm test`  **+**
`CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`  **+**
`verify-web` drift guard (`git diff --exit-code web/dist`).

## 7. Out of scope (Slice 1)

- Real content for Movies, TV, Calendar, Activity, Settings, System pages (routed
  placeholders only — filled by slices 2–5).
- Light theme / theme toggle.
- Browser E2E tests (Playwright).
- Any new backend endpoint, including an event-history/backfill endpoint for the
  activity feed (feed stays live-only).
- Notifications/toasts system, global search, user/account management UI.

## 8. Acceptance criteria

1. `cd web && npm ci && npm run build` produces `web/dist`; `CGO_ENABLED=0 go build
   ./...` then embeds it and the binary serves the real React UI at `/`.
2. Visiting any app route while unauthenticated redirects to `/login`; logging in
   with valid credentials lands on the Dashboard; logout returns to `/login`.
3. The Dashboard shows Version, Health, and Active Tasks from `/system/status`, and
   appends live rows to the Activity feed as WebSocket events arrive; the feed shows
   an empty state before the first event.
4. All seven nav entries route to a page rendered inside the shell with correct
   active-nav highlighting (Dashboard is real; the rest are placeholders).
5. The WebSocket client reconnects automatically after a dropped connection.
6. `npm test` (Vitest) passes; `go build ./...`, `go vet ./...`, `go test ./...`
   (including `web/spa_test.go` against the built `dist`) pass; the `verify-web`
   drift guard reports no uncommitted `dist` changes.
7. No files under `internal/**` or `cmd/**` are modified.
