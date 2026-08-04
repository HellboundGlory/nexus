---
id: adr-0004-rss-grab-path-is-gated
title: The RSS grab path is gated with the episode-title check
type: decision
stability: stable
summary: >
  The approved SP-1 spec claimed the RSS per-episode path "needs neither check".
  A whole-branch review empirically grabbed a wrong-show Horizons-style release
  through RSS, so the user overrode the spec: rssPlaceTV now applies the
  episode-title contradiction guard too. The season-pack branch stays ungated.
---

# 0004 — The RSS grab path is gated with the episode-title check

## Status

Accepted — 2026-07-21 (overrides a note in the approved SP-1 spec §5).

## Context

SP-1 wired `episodeTitleContradicts` into `searchEpisode` and `upgradeEpisode`.
The spec §5 explicitly said the **RSS** per-episode path "needs neither check"
(and the `searchSeason` pack branch takes none because a pack has no single
episode title). The opus whole-branch review then **empirically reproduced the
original incident through RSS**: it fed `RSSSync` a wrong-show Horizons-style
release that resolved to the monitored series by title and verified it would be
grabbed. RSS was the one grab path the sub-project had not gated — the fourth
path, exactly the class of miss this project had hit three times before.

## Decision

The user chose (2026-07-21) to **fix RSS now, then merge**. `rssPlaceTV`'s
per-episode covering loop now applies the `episodeTitleContradicts` guard, and a
trap-free regression test (`TestRSSSyncRejectsContradictingEpisodeTitle`) pins
it — a mutation (guard removed) fails by grabbing the *wrong* release, not by
grabbing zero.

The **season-PACK branch** remains ungated by design: a pack covers many
episodes and carries no single episode title to contradict.

## Consequences

- The original incident is closed across **all four** grab paths, not three.
- A documented deviation from the approved spec — recorded rather than hidden,
  so the spec's §5 note and the code don't silently disagree.
- Reinforces the rule that no single grab path test proves the others (see
  [`sdd-process`](../conventions/sdd-process.md)).

## Alternatives considered

- **Defer RSS to a later sub-project.** Rejected by the user — the whole point
  of the feature was to stop grabbing the wrong show, and RSS was a live path
  to that bug.
