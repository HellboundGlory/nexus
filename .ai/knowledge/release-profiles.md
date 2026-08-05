---
id: release-profiles
title: Release Profiles (SP-3)
type: knowledge
stability: evolving
summary: >
  Sonarr-style, tag-scoped release restrictions — required (any|all) / ignored /
  preferred terms as case-insensitive substring matches on the RAW release
  title, applied at every TV and movie grab path. Implemented on
  feat/release-profiles; notes the term semantics, the Decide signature change,
  and the fixture traps.
---

# Release Profiles (SP-3)

**IMPLEMENTED on `feat/release-profiles` (2026-08-05), merged to `master`
2026-08-05.** Whole branch green (build + vet + all 23 Go packages + FE
typecheck + `web/dist` rebuilt). See [`roadmap.md`](roadmap.md) for the build
order. Spec
`docs/superpowers/specs/2026-08-05-nexus-release-profiles-design.md`; plan
`docs/superpowers/plans/2026-08-05-nexus-release-profiles.md` (5 TDD tasks).

## What shipped

| Task | Piece | Commit |
|---|---|---|
| 1 | migration `0011_release_profiles.sql` + store CRUD + batch readers + DeleteTag ext + migration-count bump | `d774373` |
| 2 | `quality.MatchReleaseProfile` matching engine | `e5b1941` |
| 3 | `internal/releaseprofile` CRUD API | `a136c2f` |
| 4 | automation wiring into search / upgrade / RSS (TV + movies) | `90f1182` |
| 5 | Settings → Release Profiles UI (page + dialog + hooks) | `5350533` |

## Data model

- `release_profiles`: `name`, `required_mode`, and four JSON-array term lists —
  `required_any`, `required_all`, `ignored`, `preferred`.
- `release_profile_tags` junction: `(release_profile_id, tag_id)`, both with
  `ON DELETE CASCADE` (profile ↔ tag).
- Term lists are JSON columns, not a normalized table; the tag association is a
  junction, not a JSON column — the same two choices SP-2 made for tags.

## Matching semantics (from release-matching spec §9 — do not re-derive)

- **Case-insensitive substring on the RAW release title**, not the parsed title,
  so tokens parsing strips (`HebDub`, `-BurCyg`) stay targetable. Regex out of
  scope.
- **Required**: mode `any` (default, Sonarr parity) or `all` (Nexus addition —
  genuine conjunction, which Sonarr can't express). Any value other than `"all"`
  **fails permissive → treated as `any`** at match time (never silently reject
  everything), but a non-`"any"`/`"all"` value is a **400** at write time.
- **Ignored**: reject if title contains any. No mode.
- **Preferred**: does NOT gate acceptance — one score point each, used only to
  rank candidates. Folded into `compare` as a tiebreaker after the quality
  comparison, before seeder/age/size tiebreakers.
- Release profiles are **orthogonal** to quality profiles: a release must pass
  both `quality.Decide` AND every applicable release profile.

## Applying release profiles

- **Scope is by tag**: applicable = profiles whose `TagIDs` intersect the
  item's tag set, **plus** any profile with NO tags (applies to everything,
  Sonarr parity). No per-media assignment endpoints — tags are already set via
  `PUT /series/{id}/tags` and `PUT /movies/{id}/tags`.
- **RSS hot path** (from SP-2's `SeriesTagIDs`/`MovieTagIDs`): build
  `SeriesReleaseProfileIDs` / `MovieReleaseProfileIDs` once per sweep via the
  batch readers, then intersect per item. Search/upgrade are not hot paths — a
  per-item lookup is fine there.
- **Where it runs**: after `quality.Decide` accepts, before the candidate enters
  the covering/pack list. A release rejected by any applicable profile drops.
- **Decide signature change**: `Decide(releases, kind, profile)` →
  `Decide(releases, kind, profile, rps []store.ReleaseProfile)`; `Candidate`
  gains `ReleaseProfileScore int` (populated by `Decide`, used by `compare`).

## Fixture traps to respect

- Mirror the SDD rules that bit SP-2/SP-B: any test pinning "profile X rejects
  release Y" must make the guarded vs unguarded outcome **visibly differ**;
  tests observing production must count the **same predicate** production uses;
  and there are **multiple grab paths** (searchEpisode, searchSeason pack,
  upgrade, RSS) — a passing test on one path proves nothing about the others.
  The whole-branch review specifically re-counts the paths.

## Status

- **Shipped: merged to `master` 2026-08-05 (commit `6c558f0`).** Whole-branch
  review was clean — 0 Critical, all findings addressed: RSS wired to the batch
  readers once per sweep (plan §6.1), per-path + tag-scoped gate tests added
  (all 10 mutation-verified — removing the `Decide` rejection loop turns all of
  them red), non-ASCII comment fixed, `DeleteTag` reports profile count.
- This page was reconciled post-merge to reflect shipped status.
