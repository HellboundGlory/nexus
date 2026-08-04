---
id: normtitle-cannot-restore-dropped-letters
title: normTitle folds diacritics but cannot restore a dropped letter
type: memory
confidence: high
verified: 2026-07-21
source: incident-sp1-pokemon-probe
related: [automation-release-matching, adr-0003-fail-toward-silence]
summary: >
  `normTitle` maps é→e (so "Pokémon" and "Pokemon" match) but cannot bring back
  a letter that was dropped — "Pokmon" will never match "Pokémon". Do not
  "fix" this with fuzzy matching; it's a real, safe missed-grab direction.
---

# normTitle folds diacritics but cannot restore a dropped letter

## What happened

A spec fixture used the release title `Pokmon.Indigo.League` (missing the `e`).
`normTitle("Pokémon: Indigo League")` = `pokemon indigo league`, the TMDB alias
key — but `normTitle("Pokmon Indigo League")` = `pokmon indigo league`. The
accent fold turns é→e but cannot restore the dropped letter, so the alias
lookup missed and the release was rejected.

Probing the **real** NZBGeek feed showed the mangling genuinely occurs in the
wild: entries literally named
`[TESHI].Pokemon.Diamond.and.Pearl-071-Pokmon.Ranger.and.the.Kidnapped.Riolu!`.

## Why it matters

It defines what aliases can and cannot rescue. A release whose scene name
dropped a letter is **not** recoverable by alias matching, because TMDB
`alternative_titles` never returns the mangled spelling. Reaching for a fuzzy
`normTitle` to fix it would re-introduce wrong-show matches (the exact bug the
feature exists to stop) and still can't restore a genuinely dropped letter.

## What to do instead

Treat a dropped-letter release as a **safe missed-grab direction** bound by
[`fail-toward-silence`](../decisions/0003-release-matching-fail-toward-silence.md):
do not turn it into a rejection, but do not claim aliases will match it. If it
ever bites a real show, solve it with a scene-mapping layer (like Sonarr's XEM/
community mappings), not a fuzzy title comparison.

## General shape

Normalization folds *variation* (accent spelling, case, punctuation) but cannot
repair *loss* (a dropped character). Know which regime you're in before trusting
a normalization to match.
