---
title: Run the Test Suite
type: workflow
stability: evolving
command: test
triggers:
  - test
  - run tests
  - go test
  - run the frontend tests
destructive: false
idempotent: true
summary: >
  Runs the Go backend tests and the React frontend tests. The two suites are
  independent — run the backend with `make test` and the web suite with
  `make web-test`. Neither requires an external service; tests run in-process.
---

# Run the Test Suite

## Prerequisites

- Go toolchain on PATH
- Node.js and npm (for the frontend suite)

## Steps

1. Backend suite (Go, all packages under ./...):

   ```bash
   make test
   ```

2. Frontend suite (Vitest + React Testing Library, in web/). This runs `npm ci`
   first, then `npm test`:

   ```bash
   make web-test
   ```

3. Run both together by chaining the two make targets:

   ```bash
   make test web-test
   ```

## Validation

- `go test ./...` reports zero failures for the backend
- `npm test` (Vitest) reports zero failures for the frontend
- Note the frontend typecheck gotcha: a bare `tsc --noEmit` in `web/` is
  vacuous — use `npx tsc --noEmit -p tsconfig.app.json` if you typecheck the UI

## Expected Outputs

- Go test runner output; Vitest output under `web/`; no persisted report file

<!--
  If some tests are slow, flaky, or require credentials, say so here explicitly.
  That is exactly the kind of knowledge that never survives in someone's head,
  and an agent that doesn't know it will draw the wrong conclusion from a
  failure.
-->
