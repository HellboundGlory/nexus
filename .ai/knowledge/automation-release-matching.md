---
id: automation-release-matching
title: Automation Release Matching (SP-1)
type: knowledge
stability: stable
summary: >
  How Nexus decides a release belongs to a monitored item before grabbing it:
  alias matching via TMDB alternative titles, the episode-title contradiction
  check, the four grab paths that are all gated, and the fail-toward-silence
  principle. Shipped as the SP-1 release-matching sub-project.
---

# Automation Release Matching (SP-1)

This is the deep knowledge built by the **release-matching** sub-project
(plan `docs/superpowers/plans/2026-07-20-nexus-release-matching.md`, spec
`…-design.md`). The trigger incident: a live instance monitoring **Pokémon
(1997)** grabbed six episodes of *other* shows (Pokémon Trainer Tour, then
Pokémon Horizons) because search results were filtered on season/episode
number only, with no check that the release belonged to the right show.

## The four grab paths — all gated

A release can be grabbed through any of **four** independent paths, and a
passing test on one proves *nothing* about the others (three earlier fixes on
this project each missed a site):

1. `searchEpisode` (`internal/automation/search.go`)
2. the `searchSeason` **pack branch** (a season pack covers many episodes, so it
   takes **no** episode-title check — by design, see below)
3. `upgradeEpisode` (`internal/automation/upgrade.go`)
4. `rssPlaceTV` (`internal/automation/rss.go`) — the per-episode covering loop

All but the pack branch gate on `episodeTitleContradicts` after the
`releaseIsForSeries` guard.

## The two checks

- **`releaseIsForSeries`** — the release's parsed title must match the series'
  primary title *or* any alias. The alias index (`titleIndex`) is built from
  `normTitle(series/alias title)`; the lookup key for the release side strips a
  bare season token. It deliberately does **not** strip year (unlike the older
  matchers) so a bare-year series like "1923" doesn't normalize to an
  unmatchable empty string.
- **`episodeTitleContradicts(stored, p)`** — vetoes **only on positive
  evidence**: if the release's parsed episode title and the stored episode
  title are both non-empty and neither contains the other, it's a different
  episode and the release is rejected. An absent or unrecognizable episode
  title on either side is never grounds for rejection.

## Fail toward silence

The governing principle: **a false rejection means nothing downloads** (the
failure the user suffered twice), while a missed signal merely falls back to
the other checks. So the matcher *rejects only on positive contradiction*, and
the episode-title parser over-cuts toward silence (see
[`normtitle-cannot-restore-dropped-letters`](../memory/lessons/normtitle-cannot-restore-dropped-letters.md)
for why over-cutting is provably safe).

## Aliases, and the honest limit of them

Aliases come from TMDB `alternative_titles` (`/tv/{id}/alternative_titles`),
fetched on add + refresh, and persist through the RSS `buildLibraryIndex`. An
alias fetch failure must **never** fail a series add or refresh; a series with
no aliases falls back to primary-title matching.

**But aliases do essentially nothing for the Pokémon case itself:** the
`Pocket Monsters` releases aliases would newly match are exactly the ones
already filtered out for having no `S##E##`. The alias work was kept (the user
chose to keep it — it helps any show whose scene name differs) but does not
"fix" Pokémon. What *does* is the episode-title check: every candidate for the
wrong and right Pokémon shows normalizes to the identical series title
`pokemon`, so only the episode title can separate them.

## The zero-grab outcome (correct, not a bug)

After this fix, real Pokémon S01E01 grabs nothing: the only quality-eligible
release is the wrong show, and the right shows are SD/invisible. **"Fixed"
means stops grabbing wrong, not starts grabbing right.** The paths to an actual
download are an SD-tolerant profile and/or absolute numbering — queued as
follow-ups in [`roadmap.md`](roadmap.md).

## Verification notes

- The saga regression test (`TestSagaPokemonGrabsOnlyTheRightShow`) is the
  guard for the whole incident; its fixtures make *both* wrong- and right-show
  releases quality-eligible so the episode-title contradiction is the **sole**
  discriminator (see the fixture-trap rule in
  [`sdd-process`](../conventions/sdd-process.md)).
- Two-mark divergence to keep in mind: `match.go`'s release-title cleaner strips
  season-only, while `rss.go` `matchSeries` strips year+season as a fallback.
  Judged acceptable; not reconciled.
