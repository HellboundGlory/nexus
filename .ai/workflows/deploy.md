---
title: Deploy / Publish
type: workflow
stability: evolving
command: deploy
triggers:
  - deploy
  - publish
  - push master
  - docker image
  - ghcr
  - production
destructive: false
idempotent: true
confirm: required
summary: >
  How a change reaches the user's production stack. Publishing to GitHub
  Container Registry is triggered by pushing `master` (the `docker-publish`
  workflow), and the user pulls the `ghcr.io/hellboundglory/nexus:latest` image
  on their LAN host. There is no tagged release — the production image tracks
  `master`.
---

# Deploy / Publish

## The one rule that matters

**Stop and ask before pushing `master`.** Pushing `master` triggers the
`docker-publish` GitHub Actions workflow, which builds and publishes the
`ghcr.io/hellboundglory/nexus:latest` image — an outward-facing action that
users then pull to production. Treat `git push origin master` as a
deploy-with-confirmation, every time, even when a merge was authorized. See
[`agent-policy`](../meta/agent-policy.md).

Pushing a **feature branch** is not publishing. Only `master` is load-bearing.

## Steps

1. Merge the feature branch to `master` — only after its whole-branch review is
   clean and the user has said "merge".
2. **Ask before pushing `master`.** That push runs `docker-publish` (no local
   Docker build needed) and publishes `ghcr.io/hellboundglory/nexus:latest`.
3. The user pulls the image on their host and recreates the container:
   `docker compose pull && docker compose up -d` on
   [`where-things-live`](../knowledge/where-things-live.md)'s production host.

## Validation

- `gh run list` / `gh run view` to confirm `docker-publish` succeeded after the
  push (unless it coincides with a GitHub API outage, see
  [`github-surfaces-fail-independently`](../memory/lessons/github-surfaces-fail-independently.md)).
- Confirm the running image on the host with `GET /api/v1/system/status` — it
  returns `{"version":"<git sha>"}` and settles "did the fix deploy?" in one
  call (see `where-things-live`).

## Expected Outputs

- A published `ghcr.io/hellboundglory/nexus:latest` image; no tag is created.

## Working notes

- **The whole-branch review gate happens before merge.** Every feature is
  reviewed (sdd reviewers + one opus whole-branch review) and the user signs
  off on the merge before any push to `master` is considered.
- **Prod access method:** the fastest reliable way to read prod state is to ask
  the user for the `NEXUS_API_KEY` (it lives in the host's `NEXUS_API_KEY` env
  in their deployed `docker-compose`) and `curl` the `/api/v1/...` API. Browser
  automation against this LAN host has proved unreliable — see
  [`where-things-live`](../knowledge/where-things-live.md).
