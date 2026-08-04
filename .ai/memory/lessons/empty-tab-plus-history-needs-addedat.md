---
id: empty-tab-plus-history-needs-addedat
title: An empty Blocklist tab beside a long history does not mean the blocklist is broken
type: memory
confidence: high
verified: 2026-07-16
source: incident-c1-prod-verification
related: [where-things-live]
summary: >
  Blocklist rows are scoped per movie/series id, but history preserves source
  titles unscoped. If items were deleted and re-added, old rows cascade-die
  while history survives — so reconcile against addedAt before calling a bug.
---

# An empty Blocklist tab beside a long history does not mean the blocklist is broken

## What happened

During prod verification of the failed→blocklist→retry loop (C1), the Blocklist
tab looked empty while `download_failed` history was long. That read as "C1 is
broken". In fact the user had **deleted and re-added** both movies, giving them
new ids. Blocklist rows are scoped per media id and cascade-died with the old
records; history is not scoped and kept the source titles.

## Why it matters

It cost real debugging time, and it's a general trap: two sources of truth that
look contradictory can both be *correct* under different scoping.

## What to do instead

Before concluding a bug from an empty tab + populated history, check the
`addedAt` on the current media record. If items were re-added recently, the old
scoped rows are gone by design while history (unscoped) survives; the feature is
working, not broken.

## General shape

When two UI surfaces disagree, reconcile against the underlying identity
(`addedAt`/id) rather than assuming one is wrong. Scoped data cascades; unscoped
data does not.
