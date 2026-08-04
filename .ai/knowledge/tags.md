---
id: tags-system
title: Tags System (SP-2)
type: knowledge
stability: evolving
summary: >
  Current in-progress sub-project: the tags table + CRUD store, and the
  upcoming tag API. SeriesTagIDs/MovieTagIDs are shipped uncalled today but
  mandated so SP-3 can resolve release profiles inside rssPlaceTV. Includes the
  exact resume point and the fixture trap unique to this feature.
---

# Tags System (SP-2)

Current **in-progress** sub-project (the one to resume). See
[`roadmap.md`](roadmap.md) for where it sits in the build order.

## Status / resume point

- Branch `feat/tags` off master; execution is Subagent-Driven (sonnet
  implementers + reviewers, opus whole-branch review at the end).
- **ALL 8 TASKS DONE and reviewed clean** (store CRUD + associations + tag
  CRUD API + media endpoints + `TagInput` + Settings page + detail pages +
  web/dist rebuild).
- **WHOLE-BRANCH (opus) REVIEW DONE (2026-08-04): verdict "Ready"** over
  `master..HEAD` — no Critical/Important findings; Go 22 pkgs ok, FE 57/287,
  verify-web green. Four Minor recommendations, none blocking:
  F1 **fix** — `TagsSection.test.tsx` lacks loading/error-branch tests.
  F2 **fix** — em-dash (`—`) comments in `internal/core/store/tag_store.go` +
  `tag_store_test.go` violate the ASCII-Go-comments convention (build unaffected;
  still worth correcting).
  F3 **note** — detail-page component kind literal unpinned (hook-level
  kind→path IS pinned; code correct).
  F4 **note** — movie-side nil/empty guarantees unpinned (series-side only;
  correct by shared generic helper).
  Both GREEN mutations re-verified genuinely disjoint (T3-M2, T4-M3), and a
  throwaway probe confirmed rename-to-self is not a false 404. Cross-task
  seams checked: shared `tagKeys.all` means tags created in Settings are
  assignable on detail pages and delete propagates everywhere; FK cascade +
  in-use-delete refusal mean no dangling chips.
- **NEXT STEP: user decides "merge"**, then I ask before pushing `master`
  (publishes the image). If the F1/F2 minor fixes are wanted, apply them on
  `feat/tags` in the same branch before/at merge.
- Branch tips (rebased on `master` 034002b): T2 `3d84fba`, T3 `1124ea6`,
  T4 `667236f`, T5 `9bb93de`, T6 `9b20232`, T7 `3f2ead8` (+ fix `8596535`),
  T8 `13f405a`.
- **Task 6 done (2026-08-04):** Settings → Tags page — spec ✅, quality
  Approved, all 3 mutations RED, added a route-pin test to
  `SettingsLayout.test.tsx` (so removing the `routes.tsx` route genuinely
  fails). Commit `9b20232`. Deferred minor (whole-branch): `TagsSection.test.tsx`
  loading/error branches untested (plan-originated).
- **Task 7 done (2026-08-04):** tag assignment on the Series/Movie detail pages
  (`useMediaTags`/`useSetMediaTags` + `TagInput` row) — spec ✅ byte-compliant.
  The addendum required **real-hooks tests in `library/api.test.tsx`** (mock
  `@/lib/api`, `renderHook`) because the plan's mutations 1–3 target real hooks
  the detail tests fully mock. Those pin `.tagIds` extraction, `{tagIds}` payload,
  and the hook's kind→path routing (all RED). Commit `3f2ead8` + fix `8596535`:
  an **Important** review finding (caller-side kind literal in
  Series/MovieDetail unpinned — a cross-kind copy-paste bug would ship with all
  tests green) was closed by asserting the mocked hooks' kind arg in the detail
  tests, mutation-verified RED. Suite **57 files / 287 tests**, typecheck 0
  errors. Source-only: `web/dist` not rebuilt until Task 8.
- Plan: `docs/superpowers/plans/2026-07-25-nexus-tags.md` (8 TDD tasks);
  spec: `docs/superpowers/specs/2026-07-25-nexus-tags-design.md`.
- The ledger (`.superpowers/sdd/2026-07-25-nexus-tags/progress.md`) tracks
  task status, per-task mutations, and deferred minors.

## What it is

A `tags` table (name + optional label/color) with CRUD, plus
`series_tags`/`movie_tags` associations and the store methods:
`ListTags`, `CreateTag`, `UpdateTag`, `DeleteTag`, per-media `…TagIDs` readers,
and `ReplaceSeriesTags`/`ReplaceMovieTags` (delete-then-insert inside a
transaction; the tag-existence pre-check exists so callers get a **typed**
`ErrTagNotFound` instead of a raw FK driver error).

**`SeriesTagIDs` / `MovieTagIDs` are exported but uncalled in SP-2** — this is
deliberate (YAGNI-flagged, but user-approved in the spec). SP-3 resolves
release profiles inside `rssPlaceTV`, which builds its library index up front,
so a per-id lookup there is N queries in the RSS hot path; Sonarr has the same
method (`GetAllSeriesTags`). The plan governs — keep them even if a reviewer
flags them as dead code.

## The fixture trap specific to this feature

`series` and `movies` have **independent rowid sequences**. In a fixture with
two series and one movie, the ids are `s1=1, s2=2, m1=1` — the movie id
**collides** with `s1`. Any test that keys `series_tags`/`movie_tags` on the
raw entity id must therefore give the two sides *distinct* ids (e.g. tag movie
1 and series 2), or it cannot catch a `series_tags`/`movie_tags` mixup. This
bug actually shipped once (a fixture comment claimed "the ids on the two sides
differ" when they didn't) and was fixed by inserting untagged filler movies so
the tagged movie lands at id 3.

## Whole-branch review must triage these deferred minors

1. Non-ASCII em-dashes in Go comments
   (`internal/core/store/tag_store.go` and `_test.go`) — violates the ASCII-Go-
   comments rule (see [`sdd-process`](../conventions/sdd-process.md)); `go
   build`/`go vet` cannot catch it.
2. `TestSetSeriesTagsRejectsUnknownTagAndRollsBack` doesn't exercise a genuine
   mid-transaction rollback (the existence check returns before any write) —
   naming nuance, no defect.
3. The non-nil/empty guarantees are pinned on the SERIES side only;
   `MovieTagIDs`/`TagsForMovie` have no equivalent assertion.
