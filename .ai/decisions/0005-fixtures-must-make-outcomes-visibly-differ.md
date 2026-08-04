---
id: adr-0005-fixtures-must-make-outcomes-visibly-differ
title: Regression-test fixtures must make the guarded and unguarded outcomes visibly differ
type: decision
stability: stable
summary: >
  A regression test is worthless if its fixtures can't tell the guarded from the
  unguarded path. Every gate on this project has been pinned by fixtures that
  make the two outcomes visibly differ, so a mutation deleting the guard fails
  for the right reason — not for an unrelated one.
---

# 0005 — Regression-test fixtures must make the guarded and unguarded outcomes visibly differ

## Status

Accepted — established across SP-A, SP-B, and SP-1 (2026-07).

## Context

A regression test that "passes" with its guard deleted pins nothing. This
project has hit the failure repeatedly:

- **SP-A** — `DownloadClientID` fixtures that didn't discriminate.
- **SP-B** — a saga whose `hdProfile()` filtered the right-show release on
  *quality* before the check under test ran, so the test would stay green with
  the guard deleted.
- **SP-1 T7** — the Pokémon saga: if only the wrong-show release is
  quality-eligible, deleting `episodeTitleContradicts` changes what gets grabbed
  but for a reason unrelated to what the test claims to pin (and a later profile
  tweak silently flips it).
- **SP-2 tags** — `series`/`movies` have independent rowid sequences, so a
  fixture can silently fail to catch a `series_tags`/`movie_tags` mixup if the
  entity ids collide.

## Decision

Before writing a regression test, make the fixture such that **removing the
code under test changes an observable outcome in the specific way the test
asserts** — not just "the test happens to fail." The check is the mutation: a
named mutation applied and the test failing for the *discriminating* reason
(e.g. "grabbed the wrong show" rather than "grabbed nothing") confirms the
fixture is well-formed. Quality profiles, id spaces, and predicates must all be
arranged so the guard under test is the **sole** differentiator when that is
what the test intends.

## Consequences

- Mutation verification is meaningful: a red mutation means the test actually
  pins the behavior.
- Fixture design is part of the task, not afterthought — the SDD briefs call it
  out explicitly and name the mutations that must go red (see
  [`sdd-process`](../conventions/sdd-process.md)).

## Alternatives considered

- **Write fixtures from the plan's happy path only.** Rejected: silently
  vacuous, the exact trap this decision prevents.
