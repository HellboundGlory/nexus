---
id: adr-0002-subagent-driven-execution
title: Every plan builds Subagent-Driven
type: decision
stability: stable
summary: >
  Nexus never builds a plan inline. Every sub-project executes via the
  Subagent-Driven model — sonnet implementers + sonnet per-task reviewers +
  one opus whole-branch review — and the mode choice is never re-presented.
---

# 0002 — Every plan builds Subagent-Driven

## Status

Accepted — user stated 2026-07-19 and confirmed on every sub-project since.

## Context

Each sub-project (SP1–SP4, SP-1 release-matching, SP-2 tags) is executed from a
TDD plan of many small tasks. The user was repeatedly asked the same two-way
question — "inline or Subagent-Driven?" — and repeatedly chose Subagent-Driven.
On 2026-07-19 they settled it: *"whenever I'm asked that the answer will always
be Subagent-Driven."*

## Decision

After a plan is written, state that execution is Subagent-Driven and proceed —
**do not present the mode choice again**. The approved model plan: sonnet
implementers + sonnet per-task reviewers, and one **opus** whole-branch review
at the end. Each implementer/reviewer is a sub-agent; the controller (main
loop) writes the task brief and addendum, dispatches, and consolidates.

## Consequences

- Consistent, reviewed execution across every feature; per-task reviewers who
  independently re-run the implementer's mutations catch nuance the report
  omits.
- The opus whole-branch review is the final safety net and has found real gaps
  the per-task reviews missed (see
  [`0004`](0004-rss-grab-path-is-gated.md)).
- Merging is still gated on the user saying "merge", and pushing `master` is
  still a separate ask (see [`deploy`](../workflows/deploy.md)).

## Alternatives considered

- **Inline execution.** Rejected by standing user preference — less thorough,
  and reviewers can't independently reproduce results.
