---
id: roadmap
title: Roadmap and Current State
type: knowledge
stability: evolving
summary: >
  The user-locked build order — SP-2 tags (shipped) → SP-3 release profiles →
  Pokémon follow-ups → SP-C rename modal — plus the Discover-page and
  anime-support TODOs, and the exact resume point at any time.
---

# Roadmap and Current State

Nexus is feature-complete and pre-release: each sub-project ships as a
reviewed, merged `master` and the user pulls the `:latest` image. Each
build-relevant `master` push also creates a **GitHub Release** with an
auto-changelog (see [`deploy`](../workflows/deploy.md)); there are no
hand-tagged releases.

## The build order (locked by the user 2026-07-25 — do not re-ask)

1. **SP-2: tags** — **shipped** (2026-08-04; see [`tags.md`](tags.md)).
2. **SP-3: release profiles** — **shipped** (merged to `master` 2026-08-05;
   see [`release-profiles.md`](release-profiles.md)). The next sub-project
   starts now.
3. **Pokémon follow-up A** — an SD-tolerant quality profile for the Pokémon
   (1997) series.
4. **Pokémon follow-up B** — proper anime support (absolute numbering).
5. **SP-C: rename modal** — needs screenshots from the user first.

## Explicitly queued TODOs (don't forget)

- **Discover page** (Seerr/Overseerr style). User asked 2026-07-19 not to
  forget it. Scope is fixed: a TMDb **Discover** page with exactly **4 filters**
  — type / year / platform (watch-provider) / genre. Backend lacks
  discover/genres/watch-providers endpoints (`internal/media/tmdb.go` has only
  `SearchTV`/`SearchMovie`), so it needs new TMDb methods + routes + a new FE
  page. It is the last unstarted item of the 2026-07-14 A→B→C→D web-UI batch
  (A/B/D are done). Brainstorm → spec → plan → Subagent-Driven when picked up.
  Ask for a Seerr/Overseerr reference screenshot.
- **Anime / dub-language support** (user asked 2026-07-20). Proper anime
  compatibility including Sonarr-style "grab only dubbed releases in a specific
  language". Two concrete blockers surfaced from the Pokémon saga: absolute
  episode numbering is unparseable today (releases like
  `[EncoderAnon]Pocket Monsters (Pokemon) Episode 001 …` carry no `S##E##`, so
  `internal/parsing` extracts nothing and automation filters them out), and
  language/dub is not modelled as a filter (`parsing.ParsedRelease` has
  `Languages []string` but nothing filters on it). Sonarr reference:
  `NzbDrone.Core/Parser/` (absolute numbering), `Profiles/Releases/` +
  `DecisionEngine/Specifications/` (language filters), `DataAugmentation/Scene*`.
  **Absolute numbering is now the only path to an actual Pokémon download.**

## Why Pokémon is stuck (context for the follow-ups)

After SP-1 correctly stopped grabbing the wrong show, **zero eligible
candidates remain** for Pokémon S01E01: the only release passing the user's
1080p profile *is* the wrong show, and every correct-show release with an
`S##E##` marker is SD / quality-ineligible; the genuinely ideal grabs (1080p
BluRay, correct title) are invisible purely because of absolute numbering. So
SP-1 "fixed" it in the fail-safe direction: **stops grabbing wrong**, not
starts grabbing right. The SD-tolerant profile and absolute numbering are the
paths to an actual download. See [`automation-release-matching.md`](automation-release-matching.md).

## Operational invariants to preserve

- **Ask before pushing master when build-relevant.** The `docker-publish`
  workflow publishes the prod image only when a push changes build-relevant
  files (Go/web/Dockerfile); a docs-only push (`.ai/**`, `*.md`, `docs/`,
  `.superpowers/`) is skipped by path filters and doesn't publish. See
  [`deploy`](../workflows/deploy.md).
- **Every build-relevant `master` push also creates a GitHub Release** with an
  auto-generated grouped changelog (`.github/scripts/changelog.sh`), tagged by
  bare short SHA and displayed as `v<shortsha>` (docs-only pushes produce
  neither an image nor a release). Confirmed working on 2026-08-04.
- Every sub-project runs the SDD loop ([`sdd`](../workflows/sdd.md)) with the
  Subagent-Driven model; no scope expansion past what the user approved.
- The database migration count assertion in
  `internal/core/database/database_test.go` is a hardcoded "expected N applied
  migrations" — **every added migration must bump it**, or the database suite
  fails. (This exact assertion broke when the 0009 and 0010 migrations were added.)
