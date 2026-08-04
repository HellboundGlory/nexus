---
id: sdd-process
title: SDD Controller Process
type: convention
stability: stable
summary: >
  The rules that make Nexus's Subagent-Driven task loop reliable — the
  controller addendum, named mutations that must go red, independent
  re-verification, fixtures that visibly differ, mirroring production's
  predicate, and ASCII-only Go comments. Read before dispatching an SDD task.
---

# SDD Controller Process

These are the accumulated process rules that turned every Nexus sub-project
(SP-A → SP-B → SP-1 → SP-2) into a reliable loop. They live here so a future
controller applies them without re-deriving them. See the
[`sdd`](../workflows/sdd.md) workflow for the loop itself.

## The controller addendum

Append an **addendum** to every extracted task brief before dispatching it,
covering:

- **(a) ambiguity in the plan's prose, resolved explicitly** — the implementer
  must not guess.
- **(b) additional tests the plan's fixtures cannot discriminate** — if the
  plan's fixtures can't tell the guarded from the unguarded path, the addendum
  adds fixtures that can (see ADR
  [`0005`](../decisions/0005-fixtures-must-make-outcomes-visibly-differ.md)).
- **(c) NAMED mutations** with an explicit instruction to **report** a green
  one rather than paper over it.

The addendum is why tasks pass first-time: in SP-B, the two tasks without an
addendum each needed a fix wave; the tasks with one passed clean.

## Named mutations — a green one means an inert guard

Every task names mutations (e.g. "make the guard always return false", "remove
the wiring line") that **must go red** against the task's tests before it's
reported done. Each is applied, the test confirmed to fail, then reverted.

**A named mutation can come back green because of guard redundancy, not weak
fixtures** (SP-B T3 hit this). If a mutation stays green, require an **isolating
test** or a **written explanation** before accepting it — do not dismiss it.

## Reviewers must independently re-run the mutations

Both per-task reviewers on every sub-project independently reproduced the
implementer's red mutations rather than trusting the report — and this caught
real nuance twice. Always ask the reviewer to re-apply the mutations
themselves, not just read the observed output in the report.

## A test that observes production must mirror the SAME predicate production uses

SP-B T6: the fake counted only `QueueGrabbed` while the real budget gate counts
`QueueGrabbed` **and** `QueueImporting`, so the test that pinned "a budget is
hit" was observing a **narrower** predicate than production — silently vacuous.
If a test asserts production behavior, count exactly what production counts.

## Fixtures must make the outcomes visibly differ

The "fixture trap" — covered in full by ADR
[`0005`](../decisions/0005-fixtures-must-make-outcomes-visibly-differ.md).
The recurring shapes: quality profiles that filter before the check runs,
independent entity-id spaces (series vs movies rowids), and predicates that
narrow. Make the guard under test the *sole* differentiator when the test
intends that.

## Verification mechanics

- **Backend:** `CGO_ENABLED=0 go build ./... && go vet ./... && go test -count=1 ./...`
  (all packages; `-count=1` because no `-race` under pure-Go SQLite).
- **Frontend:** `cd web && npx tsc -p tsconfig.app.json --noEmit` (a **bare**
  `tsc --noEmit` is vacuous here — see
  [`frontend-typecheck`](testing.md)) and `npx vitest run`.
- **`web/dist`:** when a task touches `web/`, feed reviewers a **source-only**
  diff; rebuild `web/dist` only when `web/src` actually changed (a comment-only
  change produces no bundle diff — comments are stripped by Vite's minifier).

## Source hygiene

- **Go comments stay ASCII.** Python `open()` defaults to cp1252 on the dev
  machine and mangles non-ASCII in Go sources into build errors. `go
  build`/`go vet` cannot catch this — it shows up as an encoding failure. Strip
  non-ASCII from plan code blocks before transcribing.
- **But never strip accents from string *literals* that are real data.**
  `"Pokémon - I Choose You!"` in a fixture is the actual stored title; an
  ASCII-naive cleanup that drops the `é` silently corrupts the fixture. ASCII
  rule is about *comments*, not literal strings.
- Keep `gofmt -l` judgement: the repo's CRLF line endings can make `gofmt -l`
  flag files; trust `go build`/`go vet` as the gate.
