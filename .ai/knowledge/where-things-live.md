---
id: where-things-live
title: Where Things Live (and What Isn't Committed)
type: knowledge
stability: evolving
summary: >
  Repo layout, the design/session history under docs/superpowers and
  .superpowers, the reference arr-family source clones, and the live production
  stack — plus the access method for prod (which never embeds the secret).
---

# Where Things Live

## The repository

`github.com/HellboundGlory/nexus` (module `github.com/hellboundg/nexus`) — a
single binary replacing Prowlarr + Sonarr + Radarr. Integration branch is
`master`; **pushing `master` publishes the production image** (see
[`deploy`](../workflows/deploy.md)).

| Path | What |
|---|---|
| `cmd/nexus/` | Composition root: wires modules, starts the server |
| `internal/core/*` | Shared infrastructure: `api`, `auth`, `command`, `config`, `database` (SQLite migrations 0001–0010), `events`, `provider`, `scheduler`, `store`, `version` |
| `internal/{indexer,downloadclient,media,automation,importing}` | Feature modules |
| `internal/{parsing,quality,naming}` | Leaf packages (release-name parser, quality decision, naming tokens) |
| `web/` | React SPA; source under `src/`, committed build under `dist/` (embedded via `web/embed.go`) |
| `docs/superpowers/plans/`, `docs/superpowers/specs/` | **Design history** — every sub-project's plan (TDD tasks) and design spec, e.g. `2026-07-25-nexus-tags-plan/spec`, `2026-07-20-nexus-release-matching-*` |
| `.superpowers/sdd/` | Session artifacts of the old SDD workflow (ledgers, briefs, `review-*.diff`). Kept for reference; the durable knowledge has been migrated into `.ai/` |

## Reference *arr source (read, never commit)

Nexus is a from-scratch reimplementation. Upstream application source lives
**outside the repo** at `C:\Users\James\Downloads\Projects\_arr-reference\`
(`Prowlarr/`, `Sonarr/`, `Radarr/`, and `docker/` — which holds only container
config, not app logic). All three share the `NzbDrone.Core` lineage; the deep
pieces to study are `NzbDrone.Core/Parser/Parser.cs` (release-name parsing) and
Prowlarr's Cardigann indexer definitions.

## Production stack (user's LAN host `192.168.1.247`)

| Service | URL | What |
|---|---|---|
| **Nexus (prod)** | `http://192.168.1.247:9494/` | Runs `ghcr.io/hellboundglory/nexus:latest`; user pulls + `docker compose up -d` after each master push |
| **SABnzbd (prod)** | `http://192.168.1.247:8080/` | The usenet download client Nexus grabs to |

### Reading prod state (the reliable path)

The fastest way to verify a deployed fix is the **API directly**, not the
browser — browser automation against this LAN host has repeatedly failed. The
host sets `NEXUS_API_KEY` in its deployed `docker-compose`, so prod has a
stable key.

- The key is **not committed and never goes in `.ai/`** (policy: no secrets).
  Ask the user for it, or read it from the host's compose. Append `X-Api-Key`
  as a header — and **quote it in bash** (it contains `@` and `#`).
- Base path is **`/api/v1/...`** — a bare `/api/...` returns JSON 404 `no such
  endpoint`, which misleadingly looks like the host is wrong.
- `GET /api/v1/system/status` → `{"version":"<git sha>"}` — use it to confirm
  which image is actually running before debugging anything.
- Keep data out of query strings (a safety filter blocks them); header auth
  sidesteps it.

## Hosting / CI notes

- GitHub account `HellboundGlory`; `gh` CLI and the GitHub MCP server are
  available (git credential manager supplies push creds).
- Pushing `master` runs `docker-publish` → `ghcr.io/hellboundglory/nexus:latest`
  **only when build-relevant files change**; docs-only pushes are skipped by the
  workflow's path filters (see [`deploy`](../workflows/deploy.md)). **Ask before
  pushing master when the push is build-relevant.**
- The three GitHub surfaces (REST API, web UI, raw content) and `git push` fail
  **independently** — see
  [`github-surfaces-fail-independently`](../memory/lessons/github-surfaces-fail-independently.md).
