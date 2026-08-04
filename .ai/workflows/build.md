---
title: Build the Project
type: workflow
stability: evolving
command: build
triggers:
  - build
  - compile
  - make a release binary
  - rebuild
destructive: false
idempotent: true
summary: >
  Builds the single self-contained binary. `build` depends on `web`, so the
  React UI is installed, built, and embedded into the Go binary before the Go
  compile runs — you cannot build the binary without first building the web
  bundle (web/dist is embedded via embed.go).
---

# Build the Project

## Prerequisites

- Go toolchain on PATH
- Node.js and npm (needed for the frontend, pulled by `make build`)

## Steps

1. Build everything (frontend install + bundle, then the Go binary). This is
   the single command to use — it is idempotent and rebuilds the frontend first.

   ```bash
   make build
   ```

2. The `build` target ran `cd web && npm ci && npm run build` (web/dist),
   then `go build -o nexus ./cmd/nexus`. If you only changed Go code, you can
   skip the frontend with the raw compile:

   ```bash
   go build -o nexus ./cmd/nexus
   ```

## Validation

Confirm before reporting success:

- `./nexus` exists and is non-empty
- `git diff --exit-code web/dist` is empty — the committed SPA bundle must not
  drift; run `make verify-web` to confirm

## Expected Outputs

- `nexus` — the standalone binary (back-end + embedded web UI); `web/dist` —
  the generated SPA bundle (committed)

<!--
  TWO RULES THAT MATTER (SPECIFICATION.md §14.3, §14.4):

  1. Steps live HERE, in the body. Never duplicate them into the frontmatter.
     Two copies of a procedure will drift, and a drifted procedure is worse
     than no procedure at all.

  2. DELEGATE. If your project already has a build script, call it:
         npm run build      ✓
         make build         ✓
     Do NOT paste the underlying toolchain invocation:
         tsc -p ... && esbuild --bundle ...   ✗  will drift from the real one

  A workflow's value is knowing WHICH command, WHEN, IN WHAT ORDER, and WHAT
  MUST NOT BE SKIPPED — not restating what the command does internally.
-->
