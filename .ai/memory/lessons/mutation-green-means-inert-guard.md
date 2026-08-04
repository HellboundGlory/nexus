---
id: mutation-green-means-inert-guard
title: A named mutation coming back green means the guard is inert, not that verification was skipped
type: memory
confidence: high
verified: 2026-07-21
source: incident-sp-b-t3
related: [sdd-process, fixtures-must-make-outcomes-visibly-differ]
summary: >
  When a mutation you expect to break a test stays green, that is evidence the
  code under test is redundant with another guard — require an isolating test
  or a written explanation instead of accepting it.
---

# A named mutation coming back green means the guard is inert, not that verification was skipped

## What happened

In SP-B Task 3, a named mutation (removing an added guard) was expected to turn
a test red but stayed **green**. On inspection the guard was redundant: the same
outcome was already enforced by another mechanism in the same code path, so the
mutation changed nothing observable. It was not a case of weak fixtures or a
skipped step.

## Why it matters

Mutation verification is only meaningful if a mutation that *should* change the
test's outcome actually does. If a green mutation is waved off, the next one —
where the fixture genuinely can't discriminate — gets waved off too, and the
discipline quietly rots.

## What to do instead

Treat a green mutation as a finding, not a pass. Either add an **isolating
test** that exercises the specific guard in a context where the redundancy
doesn't rescue it, or require a **written explanation** of why the outcome is
redundantly enforced. Record which it was in the task report.

## General shape

Redundant guards are a real thing; a green mutation doesn't disprove your code,
it proves your test can't see that code. Make it able to see it.
