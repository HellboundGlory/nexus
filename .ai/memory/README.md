# `memory/` — what has been learned

Accumulated observations that aren't durable enough for `knowledge/` but are too
useful to lose. This is the portable, reviewable replacement for vendor-specific
AI memory.

## The categories

| Directory | Who writes it | Trust | Expires |
|---|---|---|---|
| `inbox/` | **Agents** | Low — never authoritative | 30 days by default |
| `lessons/` | Humans | High — hard-won | never |
| `preferences/` | Humans | Authoritative for style only | never |
| `temporary/` | Either | Low | 30 days by default |
| `archived/` | Either | Historical only | never |

## The one rule that makes this work

**Agents write to `inbox/` and nowhere else.**

Not into `knowledge/`, not into `lessons/`, not into `conventions/`. Promotion
out of the inbox is a human decision.

This exists because the alternative fails predictably: agents append to memory
on every branch, every pull request fills with unrelated `.ai/` noise, reviewers
start rubber-stamping — and rubber-stamped review is *worse* than no review,
because it launders unverified output into content later agents treat as
human-approved.

## Most inbox content should be deleted

Curation is the job; promotion is the exception. A near-100% promotion rate
means nobody is actually reading it. If `inbox/` has grown past a few dozen live
entries, curation has stopped and this module has become the junk drawer it was
designed to prevent.

```
inbox/  ──►  lessons/     an experience worth keeping
        ──►  knowledge/   a durable claim about the system
        ──►  deleted      the common and correct outcome
```

## Setup

Add to your `.gitattributes` so concurrent agent writes don't conflict:

```gitattributes
.ai/memory/inbox/**   merge=union
```

And exclude `inbox/` from required reviewers / code owners. The point is to keep
agent output *out* of the review path.

---

*Reference: SPECIFICATION.md §17. Delete this README once you're comfortable
with the model.*
