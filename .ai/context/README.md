# `context/` — what's happening right now

Ephemeral working state. Expected to churn, expected to go stale, and
**explicitly not durable truth**.

## What belongs here

- What's currently in flight
- The focus of this iteration
- A migration that's half-done and the plan for finishing it
- Temporary constraints ("staging is down until Thursday")

## What does not

Anything that will still be true in six months. If it's durable, it belongs in
`knowledge/`, `conventions/`, or `decisions/`.

## Why this module is separate

The classic failure of ad-hoc context files is mixing timescales — one file
holding both *"the ledger is append-only, permanently"* and *"we're mid-refactor
of login this week."* A reader can't tell which is which, so either the durable
facts get doubted or the stale notes get trusted.

Keeping ephemeral content in its own module makes the distinction structural
rather than a matter of careful reading.

## Use expiry dates

```yaml
---
title: Auth refactor in progress
type: context
expires: 2026-09-30
---
```

Past its date, an entry should be deleted or promoted. Expired context left in
place is exactly what this module exists to avoid.

## This module is off by default

`context: { enabled: false }` in the template manifest — that's why this
directory ships with a README and nothing else.

Many projects never need it. A good issue tracker covers the same ground with
better tooling. Turn it on only if you find yourself wanting to tell an agent
something about *this week*:

```yaml
modules:
  context: { enabled: true }
```

If you don't want it, delete this directory.

---

*Reference: SPECIFICATION.md §12.2. Delete this README once you have real
content here.*
