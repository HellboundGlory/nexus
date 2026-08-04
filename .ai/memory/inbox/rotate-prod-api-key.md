---
title: Prod NEXUS_API_KEY may need rotating; it is never committed
type: memory
confidence: medium
source: agent:prod-access-setup
expires: 2026-09-04
summary: >
  The host sets a stable NEXUS_API_KEY in its deployed docker-compose so prod
  reads work via header auth, but the key has been stored in plaintext in the
  developer's machine memory at their request. If that ever matters, rotate it.
---

The frontend host sets `NEXUS_API_KEY` in its deployed `docker-compose` (without
it, Nexus generates a random key at every boot and never logs it). Prod reads
are done with that key via an `X-Api-Key` header against the `/api/v1/...` API;
the key must be quoted in bash (it contains `@` and `#`).

The key value is **not** in this document and must never be — the agent policy
forbids secrets in `.ai/`. It lives in the developer's private machine memory
(kept plaintext at their explicit request). Rotation is: edit `NEXUS_API_KEY` in
the host's compose, then `docker compose up -d`, and update wherever the key is
recorded outside the repo. Flag this to the user if the plaintext ever
matters; do not store the value here.
