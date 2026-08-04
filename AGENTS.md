# Nexus

A single, self-hosted binary that replaces Prowlarr + Sonarr + Radarr with one
unified engine and interface — indexers, download clients, TV/movie library,
and automation behind one REST + WebSocket API and an embedded React web UI on
one port.

> This repository uses **RepoOS** — a portable, repository-local knowledge layer
> for AI-assisted development. Structured knowledge lives in [`.ai/`](.ai/).
> Spec: `1.2.0`.

---

## Start here

| If you need… | Read |
|---|---|
| How to build, test, or publish | [`.ai/workflows/`](.ai/workflows/) |
| Repo layout, prod stack, *arr reference source | [`where-things-live`](.ai/knowledge/where-things-live.md) |
| What's built next / current state / where to resume | [`roadmap`](.ai/knowledge/roadmap.md) |
| How the system fits together | [`architecture`](.ai/knowledge/architecture.md) |
| How release matching / grabbing decides | [`automation-release-matching`](.ai/knowledge/automation-release-matching.md) |
| In-progress tags work | [`tags`](.ai/knowledge/tags.md) |
| How we write and review code | [`.ai/conventions/`](.ai/conventions/) |
| Why something is the way it is | [`.ai/decisions/`](.ai/decisions/) |
| What agents may and may not do | [`.ai/meta/agent-policy.md`](.ai/meta/agent-policy.md) |
| **You edited `.ai/`** | `make repoos-generate` before committing |

Machine-readable index: [`.ai/repoos.yaml`](.ai/repoos.yaml)

---

## Working in this repository

- **Ask before pushing `master`.** It triggers the `docker-publish` workflow and
  publishes the production image (`ghcr.io/hellboundglory/nexus:latest`). See
  [`.ai/workflows/deploy.md`](.ai/workflows/deploy.md).
- **Features are built through the SDD loop, Subagent-Driven** — never inline.
  Follow [`.ai/workflows/sdd.md`](.ai/workflows/sdd.md) and
  [`.ai/conventions/sdd-process.md`](.ai/conventions/sdd-process.md).
- Run the `test` workflow (`make test web-test`) before opening a pull request.
- `web/dist` is generated and committed — never edit it by hand. If a change
  touches the frontend, run `make web` and keep the bundle in sync (CI's
  `verify-web` checks this).
- **Go comments stay ASCII** (non-ASCII breaks the build on this machine); never
  strip accents from string literals that are real data.
- Build straight from source with `make build`; there is no tagged release yet.
- **Edit anything under `.ai/`? Regenerate in the same change** —
  `make repoos-generate`. The vendor instruction files are generated from
  `.ai/` and are never hand-edited.

## Common tasks

These have defined procedures. Follow them rather than improvising:

| Task | Workflow |
|---|---|
| Build | [`.ai/workflows/build.md`](.ai/workflows/build.md) |
| Test | [`.ai/workflows/test.md`](.ai/workflows/test.md) |
| Publish / deploy | [`.ai/workflows/deploy.md`](.ai/workflows/deploy.md) |
| Feature (SDD) loop | [`.ai/workflows/sdd.md`](.ai/workflows/sdd.md) |

---

## Recording what you learn

If work here turns up something **useful, non-obvious, and specific to this
repository**, write it down. Not into your own memory, which stays on one
machine with one tool — into [`.ai/memory/inbox/`](.ai/memory/inbox/), which
travels with the repository.

**Worth recording:**

- an undocumented operational constraint
- a build, generation, or migration step that is not visible in the file tree
- a failure mode that has now happened more than once
- a workaround whose *cause* matters
- an approach that was tried and rejected, and why it failed
- a deployment or release rule you had to learn
- an architectural invariant the code does not make obvious
- a mismatch between how something appears to behave and how it does

**Not worth recording:** anything readable from the source tree · debugging
output · guesses without evidence · general programming knowledge · secrets,
credentials, customer or personal data · a log of what you did · a preference no
human has stated.

Write one file, with evidence:

```markdown
---
title: Enter a one-line title a reviewer can judge without opening the file
type: memory
confidence: high | medium | low
source: agent:<tool>/<session>
summary: >
  State what you found, in a sentence.
x-repoos-proposes:
  action: promote
  target: .ai/memory/lessons/<slug>.md
  reason: >
    State why it is worth keeping. Becomes the commit message.
---

What was discovered · why it matters · what you inspected to establish it ·
what a future agent should do differently.
```

`target` may be `.ai/memory/lessons/` (hard-won experience) or `.ai/knowledge/`
(a durable claim about the system). Those are the only two, and the file must
not already exist. Omit the `x-repoos-proposes` block if you just want the note
on record without suggesting it become permanent.

**Then a human decides.** That is the whole design — you propose, review
disposes:

```
your observation → .ai/memory/inbox/ → repoos propose --apply
   → a branch and a commit → human review → merged into lessons/ or knowledge/
   → regenerated into the vendor instruction files → the next agent starts ahead
```

Nothing you write is authoritative until that merge lands. Deleting an inbox
entry is the normal outcome and is not a failure — if nearly everything gets
promoted, the inbox has stopped being curated.

---

<!--
  Keep this file short. It exists to orient a reader in under a minute and
  point them at the right place — not to hold the knowledge itself.
  If a section here grows past a few lines, move it into .ai/ and link to it.
-->
