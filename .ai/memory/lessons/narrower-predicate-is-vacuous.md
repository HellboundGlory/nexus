---
id: narrower-predicate-is-vacuous
title: A test observing a narrower predicate than production uses is silently vacuous
type: memory
confidence: high
verified: 2026-07-21
source: incident-sp-b-t6
related: [sdd-process, fixtures-must-make-outcomes-visibly-differ]
summary: >
  If a test counts only what the fake records (QueueGrabbed) while the real gate
  counts more (Grabbed + Importing), the "budget hit" pin is observing a subset
  and proves nothing. Mirror production's predicate exactly.
---

# A test observing a narrower predicate than production uses is silently vacuous

## What happened

In SP-B Task 6, a test asserting "a grab budget is hit" pinned itself on the
fake's record of `QueueGrabbed` events. But the real budget gate in production
counts `QueueGrabbed` **and** `QueueImporting`. The test was observing a subset
of what production counts, so it could not actually verify the gate's behavior —
it stayed green for the wrong reason.

## Why it matters

It looks like coverage and provides none. A later change to the real predicate
goes unnoticed because the test never observed that predicate.

## What to do instead

Before asserting production behavior, read exactly what predicate production
uses and make the test observe **the same one** — same event types, same count
semantics. If a fake has to be extended to record what production counts, extend
it.

## General shape

A test is only as faithful as the predicate it observes. Observing a subset is
worse than not testing: it reports green.
