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
- **Tasks 1–6 done and reviewed clean** (store CRUD + associations + tag CRUD
  API + media tag-assignment endpoints + `TagInput` component + Settings →
  Tags page).
- **NEXT STEP, EXACTLY: dispatch Task 7** — Tags on the Series and Movie detail
  pages (uses `TagInput`). BASE for its review package = `9b20232` (Task 6's
  commit). The branch was rebased onto `master` 034002b, so pre-rebase hashes
  (e.g. `b5312ee`) are stale — task tips: T2 `3d84fba`, T3 `1124ea6`,
  T4 `667236f`, T5 `9bb93de`, T6 `9b20232`.
- **Task 6 done (2026-08-04):** Settings → Tags page (`tagTypes`/`tagApi`/
  `TagsSection` + test, TABS entry, routes.tsx route). spec ✅ byte-compliant,
  quality Approved. All 3 named mutations independently re-run by the reviewer
  (all RED). The implementer added a **route-pin test** to `SettingsLayout.test.tsx`
  (`createMemoryRouter(router.routes, ...)`) so removing the `routes.tsx` route
  genuinely fails (the pre-existing TABS-href test couldn't catch it) — valid
  and approved by review. Commit `9b20232`. **Deferred minor (whole-branch
  triage):** `TagsSection.test.tsx` doesn't cover the loading/error branches of
  `TagsSection.tsx` (plan-originated gap; the repo testing policy wants both).
  Source-only task: `web/dist` deliberately not rebuilt until Task 8.
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
