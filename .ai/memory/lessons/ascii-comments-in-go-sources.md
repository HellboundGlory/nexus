---
id: ascii-comments-in-go-sources
title: Keep Go comments ASCII or cp1252 turns them into build errors
type: memory
confidence: high
verified: 2026-07-21
source: incident-sp2-emdash
related: [sdd-process, testing-policy]
summary: >
  A non-ASCII comment (e.g. an em-dash) in a Go source file breaks the build on
  this machine: Python's open() defaults to cp1252 and mangled the bytes. The
  build/vet can't catch it. But the ASCII rule is about comments — never use it
  to strip accents from string literals that are real data.
---

# Keep Go comments ASCII or cp1252 turns them into build errors

## What happened

In SP-2, em-dashes (`—`) in Go comments inside `internal/core/store/tag_store.go`
and its test were flagged. This machine's Python `open()` defaults to cp1252 and
mangled the non-ASCII bytes in Go sources into encoding/build errors. The root
cause was the plan's own brief: the em-dashes came from the brief's code blocks
and were transcribed faithfully.

## Why it matters

`go build` and `go vet` cannot catch this class of failure — there is no wired
linter for it, and it only surfaces as an opaque encoding error. It also means a
plan's code blocks must already be ASCII-clean before they are transcribed into
Go.

## What to do instead

- Keep `//` comments and code in Go sources **ASCII-only**; when copying a
  snippet that carries a non-ASCII dash/quote, transcribe it as ASCII in the
  source.
- **Never** apply the ASCII rule to string *literals* that are real data —
  `"Pokémon - I Choose You!"` is the actual stored episode title and its `é`
  must stay. The rule is about comments and identifiers, not literal data.
- This is Go-specific; the repo's own TSX already uses `…` and leaves it alone.

## General shape

Encoding hygiene is a toolchain constraint, not a style preference. Distinguish
"comment/identifier text" (must be ASCII) from "literal data" (must be exact).
