---
title: Spec-Driven-Development Task Loop
type: workflow
stability: evolving
command: sdd
triggers:
  - sdd
  - spec-driven
  - dispatch a task
  - implement a plan task
  - feature plan
  - subagent-driven
destructive: false
idempotent: false
summary: >
  The exact loop every Nexus sub-project builds through: a design spec + a
  TDD plan under docs/superpowers/, executed Subagent-Driven task-by-task with
  mutation verification and an independent reviewer, then one whole-branch
  review. This is how Nexus features are made — follow it rather than
  improvising.
---

# Spec-Driven-Development (SDD) Task Loop

Nexus develops features as **sub-projects**: a design spec + a plan of small
TDD tasks in `docs/superpowers/` (the history of every sub-project is there),
executed one task at a time. This workflow is how one task — and the whole
sub-project — is driven.

The process notes that make this work are load-bearing and live in
[`sdd-process`](../conventions/sdd-process.md) — read them before dispatching.

## When

A new feature (e.g. tags, release profiles) follows this order:
`brainstorm → spec → plan → Subagent-Driven execution → whole-branch review →
merge`. The execution phase is what this workflow covers.

## Steps

For each task in the plan (tasks are sequential; task *N+1* starts only after
*N* is reviewed clean):

1. **Extract the task brief** from the plan (each plan names its files, TDD
   steps, named mutations, and commit).
2. **Append a controller addendum** — resolve the plan's ambiguity, add the
   discriminating tests its fixtures can't catch, and restate the **named
   mutations** with an explicit "report, don't paper over" instruction. See the
   [`sdd-process`](../conventions/sdd-process.md) convention — this addendum
   is what makes tasks pass first-time.
3. **Dispatch an implementer** (Subagent-Driven). Tell it to run each named
   mutation and report any that comes back **green** rather than hiding it. TDD
   per the brief: failing test first, then the fix, then mutations, then the
   full suite.
4. **Dispatch an independent reviewer** for the same task. The reviewer must
   **re-run the mutations themselves**, not trust the implementer's report —
   this has caught real nuance on every sub-project.
5. On a block-release commit, record the task in the SDD ledger and move to the
   next task's base.

## Task routing

`make task-brief PLAN N` (where available) extracts a task's brief the same way
the controller does. Review packages are produced from a `MERGE_BASE..HEAD`
diff; when `web/` is involved the reviewer gets a **source-only** diff (no
`web/dist` noise).

## Whole-branch review

Once every task is reviewed clean, run one **whole-branch review** (opus) over
the entire `MERGE_BASE..HEAD` diff. It triages the accumulated Minor findings
and, on past sub-projects, has found the one real gap the per-task reviewers
missed (see
[`rss-grab-path-is-gated`](../decisions/0004-rss-grab-path-is-gated.md)). The
user then decides "merge" — at which point the
[`deploy`](deploy.md) workflow's rule takes over: **ask before pushing master**.

## Ledger / record

- Keep the running state (current base, task status, deferred minors, next
  step) in a ledger. Historically that was `.superpowers/sdd/…/progress.md`;
  migrate durable outcomes into `.ai/` (decisions, lessons, knowledge) and drop
  the ephemeral per-task detail.
- Do **not** commit the `review-*.diff` artifacts — they are redundant with git
  history. A task's review conclusion, not its diff, is the durable record.

## Validation

- The named mutations go red, then green; none silently green.
- `go build ./... && go vet ./... && go test -count=1 ./...` all pass (and
  `cd web && npx tsc -p tsconfig.app.json --noEmit` + `npx vitest run` when the
  frontend is touched).
- The reviewer independently reproduced the mutations' red states.
- No `web/dist` drift (`make verify-web`) unless a task intentionally rebuilds it.
