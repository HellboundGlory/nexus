# `memory/preferences/`

How this team likes to work. Authoritative for **style**, never for
**correctness**.

Use this for the "we'd rather you did it this way" things that aren't strict
enough to be a convention and aren't facts about the system.

```markdown
---
title: Prefer small, focused pull requests
type: memory
confidence: high
---

We'd rather review four small changes than one large one. If a change is
touching more than a few concerns, split it — even when the total diff is the
same size.
```

## Preferences vs. conventions

| | Goes in |
|---|---|
| "Reviewers will ask you to change this" | `.ai/conventions/` |
| "We'd prefer it, but it's not a blocker" | here |

If you find a preference being enforced in review, promote it to a convention.
If you find a convention nobody enforces, demote it to a preference or delete it.

---

*Delete this README once you have real content here.*
