---
id: code-style
title: Code Style
type: convention
stability: evolving
summary: >
  Nexus is a Go + TypeScript modular monolith. gofmt, go vet, and the TypeScript
  compiler (via `tsc -b` in the build) are the enforced baseline; everything else
  is a judgement call that this document pins down so reviewers stop repeating it.
---

# Code Style

## Automated

- **Go**: `gofmt` (the standard Go formatter) and `go vet`. `make test`
  (`go test ./...`) is the correctness gate. Note the Makefile lists `lint` in
  `.PHONY` but defines **no** `lint:` rule — there is no wired Go linter, so
  formatting is checked manually, not in CI.
- **Frontend**: the TypeScript compiler is the only enforced static check —
  `web/` has **no ESLint or Prettier config committed**, and `npm run build`
  runs `tsc -b` before `vite build`. Style in TS/TSX is therefore by convention
  (below), not by machine.

```bash
gofmt -l .                                    # non-empty output = not gofmt-clean
go vet ./...
npx tsc --noEmit -p tsconfig.app.json         # frontend typecheck (see workflows/test.md)
```

## Judgement calls

- **Go: wrap errors with context, then match with `errors.Is`, never reformat the
  chain into one string.** The store returns a sentinel `ErrNotFound`
  (`internal/core/store/store.go`); handlers test `errors.Is(err, sql.ErrNoRows)`
  / `errors.Is(err, store.ErrNotFound)` and return a user-facing message, not the
  raw error.
- **Go: export only what must cross the package boundary.** Feature modules reach
  sibling capabilities only through the small interfaces declared in
  `internal/core/provider`, injected at the composition root. Compile-time
  interface assertions make this explicit: `var _ TaskScheduler = (*scheduler.Scheduler)(nil)` in `internal/core/api/api.go`.
- **Go: REST handlers are thin.** A handler parses and validates the request,
  calls the store/service, and answers via `api.WriteJSON` / `api.WriteError`
  from `internal/core/api`; it contains no business logic.
- **Go: log with `log/slog` key-value pairs** (e.g.
  `b.log.Error("event handler panicked", "event", ..., "recover", r)`), never
  printf-style `slog` helpers.
- **TS: route every network call through the `@/lib/api` helpers.** `apiGet /
  apiPost / apiPut / apiDelete` centralize `credentials: "include"`, throw
  `ApiError` carrying `.status`/`.code`, and fire the 401 handler. Never call
  `fetch` directly in a component or feature file.
- **TS: share query keys through a `...Keys` factory.** `useQuery`/`useMutation`
  in `web/src/features/*/api.ts` center on a named keys object (e.g. `libraryKeys`)
  so `invalidateQueries` and the reads share one source of truth.
- **Every component renders a graceful loading and error state**, not just the
  happy path — `StatusSection` shows "Loading…" plus fallbacks, and
  `Dashboard.test.tsx` asserts the error branch.

## Naming

- **Go packages**: `internal/core/*` = shared infrastructure; `internal/<feature>/*`
  = feature modules; `internal/{parsing,quality,naming}` = leaf packages. Test
  files are co-located as `<file>_test.go` beside the source (`parser.go` /
  `parser_test.go`).
- **Go endpoints**: a feature module exposes an `API` struct built by `NewAPI(...)`
  with a `Mount(r chi.Router)` method that registers routes under the
  authenticated `/api/v1` group; handlers are methods named for the action —
  `list`, `create`, `get`, `update`, `delete`, `schema`, `test` (see
  `internal/indexer/api.go`).
- **Go REST payloads**: request DTOs are local structs with `json:` tags plus a
  `toStore()` (DTO → model) and a `valid() (string, bool)` validation method.
- **React**: components are function components (`function Name()`); hooks are
  `use*`; tests are co-located `<Name>.test.tsx` beside the component.
- **Frontend directories**: shared primitives live in `web/src/components/ui/`
  (shadcn-style), feature code under `web/src/features/<feature>/`,
  cross-cutting helpers under `web/src/lib/`.

## Comments

- **Go: comment WHY, not WHAT.** A choice that looks accidental gets a short
  doc comment — e.g. why `writePump` is the *only* goroutine that writes to a
  websocket connection (gorilla/websocket's single-writer requirement). Exported
  symbols get a standard Go doc comment starting at the name.
- **TS/TSX: comment non-obvious state transitions and mutation side effects**
  (e.g. why `onSuccess` invalidates a particular key set). Do not comment what
  the JSX already says.

<!--
  Delete any section you don't have a real answer for. An empty heading is
  worse than a missing one: it reads as a rule that was never written down,
  and an agent may try to infer one.
-->
