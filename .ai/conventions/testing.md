---
id: testing-policy
title: Testing Policy
type: convention
stability: evolving
# applies_to:
#   - "**/*.test.*"
summary: >
  Every behavior change ships with tests at the layer it lives in: table-driven
  Go tests for backend logic, Vitest + React Testing Library for the UI. Test
  files sit beside the code they cover; generated output (web/dist) is guarded
  by a drift check, not unit tests.
---

# Testing Policy

## What requires a test

- Every new or changed REST endpoint gets a request-level test in the owning
  Go package (`internal/indexer/api_test.go`, `internal/automation/api_test.go`,
  and so on).
- Every parsing / quality-decision / naming rule gets a **table-driven** test over
  realistic release-name fixtures (see `internal/parsing/parser_test.go`,
  `internal/quality/decision_test.go`).
- Every React component with non-trivial behavior gets a co-located `*.test.tsx`
  covering render, interaction (via `@testing-library/user-event`), and both the
  loading and error branches.

## What doesn't

- `web/dist` — it is committed but treated as generated binary (`-text` in
  `.gitattributes`) and is protected by a drift guard (`make verify-web`), not
  by unit tests.
- Trivial accessors and one-line data conversions that are already exercised
  through their callers' tests.
- Throwaway tooling and test scaffolding under `web/src/test/`.

## Kinds of test

| Tier | Use it for | Run with |
|---|---|---|
| Go unit | Pure logic and per-package handlers/stores (table-driven) | `make test` (`go test ./...`) |
| Go integration | Cross-boundary flows done in-process (store → migrations, event bus) | `make test` |
| Frontend component | React components + hooks against a mocked API (`vi.mock("@/lib/api")`) | `make web-test` (Vitest) |
| Frontend smoke | Toolchain sanity (e.g. `web/src/smoke.test.ts`) | `make web-test` |

Fixtures for Go network clients live in per-package `testdata/` directories with
realistic payloads (`internal/indexer/testdata/{caps,newznab_search,torznab_search}.xml`).

## Before opening a pull request

Run `make test web-test` and confirm neither suite fails, then `make verify-web`
to confirm `web/dist` has not drifted. See `.ai/workflows/test.md`.

Frontend typecheck gotcha: a bare `tsc --noEmit` in `web/` is vacuous — use
`npx tsc --noEmit -p tsconfig.app.json`.

## Known problems

- The Go **race detector is unavailable** under `CGO_ENABLED=0` (pure-Go SQLite
  driver). The `-race` flag cannot run, so don't read a missing race result as
  a pass — verify concurrency by repeating the suite: `go test -count=N`
  (documented in README).
- Store tests execute real SQLite migrations in-process; keep them
  order-independent and self-contained (no shared external state).
- No test tier requires external services or credentials at this time, and no
  flaky suite is currently known.
