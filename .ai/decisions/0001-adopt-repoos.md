---
id: adr-0001-adopt-repoos
title: Adopt RepoOS for repository-local AI knowledge
type: decision
stability: stable
summary: >
  Nexus adopted RepoOS (spec 1.2.0) as its repository-local knowledge layer so
  architecture, convention, and workflow knowledge travels with the repo and is
  reviewable like code; vendor files are generated from .ai/.
---

# 0001 — Adopt RepoOS for repository-local AI knowledge

## Status

Accepted — 2026-08-04

## Context

Knowledge about this project — its architecture, its build and release
procedures, the reasoning behind past decisions — was living in three places
that don't travel: people's heads, chat history, and per-machine AI assistant
memory. None of it survives a new contributor, a new machine, or a change of
tooling.

The concrete trigger for Nexus: this is a **single-maintainer, single-binary
project** whose critical operating knowledge was real but scattered. The module
boundary rule (`internal/<feature>` never imports a sibling feature), the
`web/dist` drift guard (`make verify-web`), the pure-Go/`CGO_ENABLED=0` build
constraint, and "there is no tagged release — run from source" all mattered but
lived in the README, the Makefile, chat threads, and per-machine auto-memory.
Two agents working from stale per-vendor memory could drift apart because
CLAUDE.md and any other vendor file had no single committed source to agree on.
The cost of this knowledge not traveling is high: a contributor who edits
`web/dist` by hand, or breaks the module boundary, produces a subtle, costly bug
that nothing in the file tree warns against.

## Decision

Adopt RepoOS (spec `1.2.0`): a committed `.ai/` directory plus a root
`AGENTS.md`, holding knowledge, conventions, workflows, and decisions in plain
Markdown.

Vendor-specific instruction files are treated as **generated** from this layer
rather than maintained by hand. Nexus configures generation via
`x-repoos-generate` (targeting `CLAUDE.md`) so the repo keeps one source of
truth in `.ai/` and regenerates the vendor config from it.

## Consequences

**Gained**

- Project knowledge travels with the repository — any machine, any contributor,
  any AI tool.
- Changes to what agents are told are reviewable diffs rather than silent
  updates to a hidden store.
- One source of truth instead of several drifting vendor files.
- The `web/dist` and module-boundary rules become reviewable, code-adjacent
  policy instead of tribal knowledge.

**Cost**

- The layer needs maintenance. Stale documentation is worse than none, because
  it gets trusted.
- Some upfront writing before any benefit appears.

**Accepted trade-off**

We're starting at the `core` profile with the workflows that already exist
(`build`, `test`) plus conventions and a knowledge overview. The `context`
module is disabled, `memory` is enabled as the agent write path (inbox → human
review), and we are not attempting exhaustive architecture documentation beyond
the overview until it is actually needed.

## Alternatives considered

- **Keep using per-tool memory.** Rejected: doesn't survive machine changes and
  can't be reviewed.
- **A wiki or external docs site.** Rejected: drifts from the code because it
  isn't in the same change as the thing it describes.
- **Keep everything in hand-maintained vendor files (CLAUDE.md, .cursorrules).**
  Rejected: the files drift apart from each other and from the code; Nexus
  chose to generate them from `.ai/` instead so there is one editable source.
