# `memory/archived/`

Retired entries kept for history. **No trust** — nothing here describes the
system as it is now.

## When to archive instead of delete

Archive when the *reasoning* still has value even though the conclusion no
longer holds:

- A lesson from an architecture you've since replaced
- A constraint that has been lifted, where knowing it once existed explains an
  odd shape in the code
- A preference the team deliberately changed

Delete — don't archive — when the entry was simply wrong, trivial, or
superseded by something with no interesting history. Git remembers either way;
this directory is for things you expect a human to actually re-read.

## Mark why it was archived

```yaml
---
title: Ledger snapshots are not supported
type: memory
stability: deprecated
summary: >
  Archived 2026-08-01 — snapshots were added in v4. Kept because the original
  constraint explains why replay logic is structured the way it is.
---
```

An archive nobody prunes becomes a landfill. If you can't say why an entry is
worth re-reading, delete it.

---

*Delete this README once you have real content here.*
