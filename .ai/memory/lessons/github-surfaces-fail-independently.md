---
id: github-surfaces-fail-independently
title: GitHub's surfaces and git push fail independently — read the Error line, not the warning above it
type: memory
confidence: high
verified: 2026-07-17
source: incident-ci-unicorn-outage
related: [where-things-live, deploy]
summary: >
  api.github.com, github.com, raw.githubusercontent.com, and git push fail on
  their own schedules. The "Unicorn" HTML page is a GitHub REST 5xx in disguise,
  and the punycode DEP0040 warning is a red herring. Read the actual Error line.
---

# GitHub's surfaces and git push fail independently — read the Error line, not the warning above it

## What happened

On 2026-07-16/17 the GitHub **REST API** went down while the web UI and raw
content stayed up. An Actions step (`docker/metadata-action`) got GitHub's HTML
error page where JSON was expected and threw while parsing; locally the same
outage presented as `gh` failing with `invalid character '<' looking for
beginning of value` — that string *is* the Unicorn page. Conflating the surfaces
wasted debugging time on a non-existent code bug.

## What to do instead

Three GitHub surfaces are independent, **plus** `git push` uses the git protocol
and can succeed while `gh` is fully broken:

- `api.github.com` — the REST API (`gh`, MCP server).
- `github.com` — the web UI (read Action logs here during an API outage).
- `raw.githubusercontent.com` — raw file fetches.
- `git push`/`fetch` — the git protocol, independent of the REST API.

Before touching code on a mystery failure, check the **Error:** line, and the
GitHub status page. **The `(node:NNNN) [DEP0040] DeprecationWarning: The
'punycode' module is deprecated` warning is a red herring** — it's informational
stderr that sets no exit code, appears in green runs too, and merely sits
adjacent to the real error. Read the line that starts `Error:`, not the warning
above it.

## General shape

When an external dependency has multiple independent transport surfaces, a
failure on one is not a failure of the whole — and an adjacent informational
warning is not the cause.
