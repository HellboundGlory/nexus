# `memory/inbox/`

**Agents write here. Only here.**

Low trust by default. Nothing in this directory is authoritative, and nothing
here may override `knowledge/`, `conventions/`, or `decisions/`.

Entries expire after 30 days unless given an explicit `expires` date.

What belongs here and what does not is in the root
[`AGENTS.md`](../../../AGENTS.md#recording-what-you-learn). The short version:
useful, non-obvious, and specific to this repository — not a log of what an
agent did, and never a credential.

## Shape of an entry

```markdown
---
title: Ledger replay walks the full event log
type: memory
confidence: medium
source: <pr-number, issue, or agent identifier>
applies_to: [src/ledger/replay.*]
expires: 2026-09-15
summary: >
  There is no snapshot; replay is linear in event count. Above roughly 2M
  events this exceeds the job timeout.
---

Noticed while investigating the nightly reconciliation timeout. Measured on the
2026-08 production snapshot: 2.1M events, 47 minutes, job timeout is 45.
```

The body carries the evidence — what was inspected, what was measured, what a
future agent should do differently. An observation a reviewer cannot check is an
observation a reviewer has to take on faith, and taking agent output on faith is
the thing this directory exists to avoid.

## Proposing a promotion

An agent that thinks an observation should become durable adds a proposal block:

```yaml
x-repoos-proposes:
  action: promote
  target: .ai/memory/lessons/replay-has-no-snapshot.md
  reason: >
    Why this is worth keeping. Ends up in the commit message.
```

The bare `proposes:` spelling still works and warns (`RO-608`) — extensions live
behind `x-`.

You then review the queue and promote one onto its own branch:

```bash
repoos propose .                        # what's pending
repoos propose . --apply=<name>         # promote it, on a branch
```

That writes the durable unit, deletes the inbox entry, and commits both. Push
and open a pull request when you want it reviewed — the tool never pushes.

Promotions can only land in `.ai/knowledge/` or `../lessons/`, and can never
overwrite an existing file, so a proposal cannot reach your code, your CI, or
the agent policy. Nothing is authoritative until you merge it.

## Curating this directory

Periodically — monthly is plenty — go through and for each entry:

- **Durable claim about the system?** → move to `.ai/knowledge/`
- **Hard-won experience?** → move to `../lessons/`
- **Wrong, trivial, or already obsolete?** → **delete it**

Deleting is the expected outcome for most entries — it needs no tooling, and
Git keeps the history if you ever want it back. If you find yourself promoting
nearly everything, the inbox is not being curated, it is being archived.

---

*Keep this README — it's the only thing marking the directory as the agent write
path once the directory is otherwise empty.*
