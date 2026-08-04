# `memory/lessons/`

Things learned the hard way. High trust — these are usually paid for with an
outage, a long debugging session, or a mistake nobody wants repeated.

Written by humans, or promoted from `../inbox/` by a human.

## What a good lesson looks like

The pattern is **symptom → cause → what to do instead**:

```markdown
---
title: Retries amplify load during partial outages
type: memory
confidence: high
verified: 2026-07-14
source: incident-2026-07-12
---

During the July partial outage, client retries turned a degraded dependency
into a full one. The retry policy had no jitter and no circuit breaker, so
every client retried in lockstep.

Use jittered backoff for anything crossing a service boundary, and fail fast
when the dependency is already known-degraded.
```

## What doesn't belong here

- Facts about how the system is built → `.ai/knowledge/`
- Rules everyone must follow → `.ai/conventions/`
- Raw agent observations → `../inbox/`

Lessons don't expire, but they can become obsolete. When one no longer applies,
move it to `../archived/` rather than deleting it — the reasoning is often still
instructive.

**Moving it is what stops it being taught.** Every lesson here gets a one-line
entry — title, tags, summary, link — in the vendor instruction files agents
actually read, which is how a promotion changes anyone's behavior. That index is
built from what is in this directory, not from whether a unit carries an
`expires` date, so an obsolete lesson left in place keeps being broadcast as
current. Give each one a `summary` worth reading in isolation: it is the only
part most agents will ever see.

---

*Delete this README once you have real lessons here.*
