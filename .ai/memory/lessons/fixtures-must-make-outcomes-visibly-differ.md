---
id: fixtures-must-make-outcomes-visibly-differ
title: A regression test whose fixtures can't show the guarded outcome pins nothing
type: memory
confidence: high
verified: 2026-07-21
source: incident-sp1-t7-saga
related: [adr-0005-fixtures-must-make-outcomes-visibly-differ, sdd-process]
summary: >
  If removing the code under test doesn't change what the fixture grabs, the
  test is silently vacuous. Make the guard the sole differentiator, and confirm
  with a mutation that fails for the discriminating reason.
---

# A regression test whose fixtures can't show the guarded outcome pins nothing

## What happened

The SP-1 saga test for `episodeTitleContradicts` was originally written with the
prod profile `hdProfile()`, which admits only WEBDL-1080p and BluRay-1080p. The
right-show release in the fixture was DVDRip (SD), so it was filtered on
**quality** before the episode-title check ever ran. The only quality-eligible
release left was the wrong show — deleting the guard would still change what
gets grabbed, but for a reason unrelated to what the test claimed to pin, and a
later profile tweak would silently flip it. The same trap had appeared as
SP-A's `DownloadClientID` and SP-B's one-vs-several-missing-episodes fixtures.

## Why it matters

A "green with the guard deleted" test gives false confidence: it looks like
coverage but is a vacuous pin. The whole project's mutation-verification
discipline (see [`sdd-process`](../conventions/sdd-process.md)) depends on a
red mutation meaning the test really observes the behavior.

## What to do instead

Make the fixture such that **removing the code under test changes an observable
outcome in exactly the way the test asserts** — e.g. the saga's wrong- and
right-show releases are both quality-eligible, so the episode-title
contradiction is the sole discriminator; a mutation (guard always false) fails
by grabbing the *wrong* release, not by grabbing zero. When a fixture must
reach around a real production constraint to do this (e.g. giving the right
release a profile-eligible quality while keeping the real episode-title text),
say so in a comment and keep the real discriminating signal intact.

## General shape

Give the guarded and unguarded paths **visibly different** outcomes in the
fixture. If they're not visibly different, the test proves nothing.
