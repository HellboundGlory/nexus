---
id: adr-0003-fail-toward-silence
title: Release matching fails toward silence
type: decision
stability: stable
summary: >
  The release-matching matcher rejects a release only on positive evidence of a
  mismatch. A false rejection is treated as the worst failure (nothing
  downloads) and an absent signal as merely falling back to the other checks.
---

# 0003 — Release matching fails toward silence

## Status

Accepted — 2026-07-21 (SP-1, spec §4).

## Context

The Pokémon incident showed the failure mode that matters: a wrong-show release
was grabbed because matching filtered on season/episode number only. But the
*symmetric* failure is the one the user already suffered twice before this —
**a false rejection means nothing downloads at all**. The matcher had to be
built so that when in doubt, it errs toward *not rejecting* (silence on the
signal side) rather than toward *rejecting*.

## Decision

- **Reject only on positive contradiction.** `episodeTitleContradicts` vetoes
  only when both the release's parsed episode title and the stored title are
  non-empty and neither contains the other. An absent or unrecognizable episode
  title is never grounds for rejection.
- **A missed signal only falls back to the other checks**; it never blocks the
  grab. So over-parsing the episode title (cutting too aggressively) is safe:
  it can only lose a signal, never fabricate a rejection.
- **Alias fetch failure never fails a series add or refresh.**

## Consequences

- The matcher is fail-safe in the correct direction: the wrong show is rejected,
  but a dubious-but-possibly-right release is still downloaded rather than
  silently dropped.
- The cost is that "doing nothing" is a normal outcome — after SP-1, Pokémon
  S01E01 grabs *nothing* (correctly), which reads as a bug unless you know it
  means "stops grabbing wrong". Documented in
  [`automation-release-matching`](../knowledge/automation-release-matching.md).

## Alternatives considered

- **Reject on any ambiguity.** Rejected: this is exactly the false-rejection
  failure that produces "nothing downloads".
- **Fuzzy title matching** to salvage dropped letters (`Pokmon`). Rejected: it
  would reintroduce wrong-show grabs and cannot restore a genuinely dropped
  letter (see
  [`normtitle-cannot-restore-dropped-letters`](../memory/lessons/normtitle-cannot-restore-dropped-letters.md)).
