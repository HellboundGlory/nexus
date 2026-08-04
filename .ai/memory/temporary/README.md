# `memory/temporary/`

Speculative, unverified, or in-flight notes. Low trust.

Entries **must** carry an `expires` date; if omitted it defaults to 30 days from
creation.

```markdown
---
title: Suspect the cache is the source of the stale reads
type: memory
confidence: low
expires: 2026-09-01
---

Not confirmed. If this turns out to be right, it belongs in lessons/ with the
evidence attached. If it turns out to be wrong, delete it.
```

## Why it expires

An unverified guess that sits around long enough starts to read like an
established fact. The expiry date is what stops "we think maybe" from quietly
becoming "it is known that".

When an entry expires: verify and promote it, or delete it. Leaving expired
entries in place is the failure mode this directory exists to prevent.

---

*Delete this README once you have real content here.*
