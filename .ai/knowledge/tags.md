---
id: tags-system
title: Tags System (SP-2)
type: knowledge
stability: evolving
summary: >
  Shipped sub-project: the tags table + store CRUD, tag CRUD API, media
  tag-assignment endpoints, and the web UI (Settings › Tags page, add-modal
  assignment, read-only per-item tags). SeriesTagIDs/MovieTagIDs are shipped
  uncalled today but mandated so SP-3 can resolve release profiles inside
  rssPlaceTV. Includes what shipped and the fixture traps unique to this feature.
---

# Tags System (SP-2)

**SHIPPED (2026-08-04).** SP-2 tags is merged to `master` and published in a
GitHub Release. See [`roadmap.md`](roadmap.md) for what's next (SP-3 release
profiles).

## Shipped status

- Merged to `master` (2026-08-04), built, and released. All 8 TDD tasks plus a
  post-merge **UX rework** are in.
- Shipped pieces: the `tags` table + store CRUD (`internal/core/store/tag_store.go`),
  the `internal/tag` REST API (list/create/rename/delete), media
  `GET|PUT /series/{id}/tags` and `GET|PUT /movies/{id}/tags` endpoints, the
  Settings › Tags page, and per-item tags.
- **UX (user-directed, trimmed from the original design):**
  - Tags are assigned **when adding a show/movie** in the Add modal, from
    Settings-managed existing tags (multi-select; no type-to-create in the
    modal — new tags are created in Settings).
  - Detail pages are **read-only**: a "Tags" chip reveals the assigned tags on
    hover (state-driven `onMouseEnter/Leave`). No editing, no create.
  - **Per-item tag editing is deferred** to a future redesign (assign-at-add
    only for now).
  - The type-to-create `TagInput` component was removed (it became unused); git
    history preserves it for that future redesign.
- Design history: plan `docs/superpowers/plans/2026-07-25-nexus-tags.md`
  (8 TDD tasks), spec `docs/superpowers/specs/2026-07-25-nexus-tags-design.md`;
  the working ledger `.superpowers/sdd/2026-07-25-nexus-tags/progress.md`
  (gitignored) has the per-task detail.

## What it is

A `tags` table (name + optional label/color) with CRUD, plus
`series_tags`/`movie_tags` associations and store methods: `ListTags`,
`CreateTag`, `UpdateTag`, `DeleteTag`, per-media `…TagIDs` readers, and
`ReplaceSeriesTags`/`ReplaceMovieTags` (delete-then-insert inside a transaction;
the tag-existence pre-check exists so callers get a **typed** `ErrTagNotFound`
instead of a raw FK driver error). The web layer adds the Settings-page hooks in
`web/src/features/settings/tagApi.ts` and the read/assign hooks in
`web/src/features/library/api.ts` (`useMediaTags`, `useSetMediaTags(kind)`).

**`SeriesTagIDs` / `MovieTagIDs` are exported but uncalled in SP-2** — this is
deliberate (YAGNI-flagged, but user-approved in the spec). SP-3 resolves release
profiles inside `rssPlaceTV`, which builds its library index up front, so a
per-id lookup there is N queries in the RSS hot path; Sonarr has the same method
(`GetAllSeriesTags`). The plan governs — keep them even if a reviewer flags them
as dead code.

## Fixture traps unique to this feature

- **Independent rowid sequences.** `series` and `movies` have separate id
  counters, so in a two-series + one-movie fixture the ids are `s1=1, s2=2,
  m1=1` — the movie id **collides** with `s1`. Any test keying
  `series_tags`/`movie_tags` on the raw entity id must give the two sides
  *distinct* ids (e.g. tag movie 1 and series 2), or it cannot catch a
  series/movie mixup (shipped once; fixed with filler movies).
- **Empty vs null.** `GET …/tags` must return `"tagIds":[]`, never `null`;
  `ListTags` returns `[]Tag{}`. The API tests pin `[]` to keep the non-nil
  contract.
- **404 vs 400 on write.** A missing *entity* → `ErrNotFound` → 404; an unknown
  *tag id* → `ErrTagNotFound` → 400. The two sentinels are genuinely disjoint
  (mutation-verified), so their case order in `writeTagAssignError` is inert.

## Known notes / deferred (non-blocking)

- **F3:** the detail-page component's kind literal still isn't pinned by a test
  (the hook-level kind→path routing IS pinned in `library/api.test.tsx`; the
  production code is correct). Worth an assertion if the detail page is ever
  edited.
- **F4:** movie-side nil/empty guarantees are pinned only on the series side
  (`MovieTagIDs`/`TagsForMovie` have no equivalent assertion) — correct today
  because the helpers are shared.
- `TestSetSeriesTagsRejectsUnknownTagAndRollsBack` doesn't exercise a genuine
  mid-transaction rollback (the FK pre-check returns before any write) — naming
  nuance; the property that matters (no partial write) IS pinned.
- F1/F2 (Settings-page loading/error coverage and ASCII em-dash comments) were
  **fixed** and closed.
