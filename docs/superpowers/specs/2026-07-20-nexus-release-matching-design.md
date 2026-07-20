# Nexus — Release matching: series aliases and episode-title contradiction

**Date:** 2026-07-20
**Status:** Approved (design), ready for implementation planning
**Branch:** `feat/release-matching`, off `master` `481dc7e`
**Scope:** SP-1 of 3 — see §9 for the sequenced follow-ons

## 1. Problem

Automation grabbed and imported episodes of the wrong TV show, twice, for a single
monitored series.

The user monitors **Pokémon (1997)**, TMDB `60572`, with three episodes of season 1
monitored. Live production history shows:

1. `Pokemon.Trainer.Tour.S01E01/02/03…-BurCyg` — grabbed and imported (TMDB `260604`,
   a different show).
2. After the fix in `481dc7e`, `Pokemon.S01E01.The.Pendant.That.Starts.It.All…-iVy` and
   `Pokemon.S01E03.For.Sure.Cause.Sprigatitos.with.Me` — grabbed and imported. These are
   **Pokémon Horizons** (TMDB `220150`); Sprigatito is a 2023 Gen-9 starter.

Six wrong episodes are now on disk across two shows.

### 1.1 Root cause

`searchEpisode` filtered candidates on season and episode number only:

```go
if c.Parsed.Season == e.SeasonNumber && containsInt(c.Parsed.Episodes, e.EpisodeNumber)
```

Nothing checked that the release was for the right *show*. Newznab matches its `q` term
loosely, and Nexus cannot scope a TV search server-side, so `q=Pokemon&season=1&ep=1`
returns S01E01 of every Pokémon-prefixed show and the highest-**scoring** release wins
regardless of which show it belongs to.

### 1.2 Why the first fix was insufficient

`481dc7e` added `releaseIsForSeries`: an exact match of the normalized parsed title
against the normalized series title. It correctly rejected `Pokemon Trainer Tour`, but on
real data it is **actively harmful**:

| Release (from live indexer results) | Actually | Exact-title check |
|---|---|---|
| `Pokemon.S01E01.The.Pendant…-iVy` | Horizons (wrong) | **accepted** |
| `Pokmon.Indigo.League.s01e01` | 1997 (correct) | **rejected** |
| `[EncoderAnon]Pocket Monsters (Pokemon) Episode 001…` | 1997 (correct) | **rejected** |

The check rejects the correct releases and admits the wrong one. Two different shows
share the scene name `Pokemon` and both have an S01E01; **no series-title comparison can
separate them.**

### 1.3 Evidence that `tvdbid` does not solve it

NZBGeek advertises `tv-search … supportedParams="q,rid,tvdbid,tvmazeid,season,ep"`, so a
tvdbid-scoped search is possible. It was probed directly:

`t=tvsearch&tvdbid=76703&season=1&ep=1` returns **20** results (vs 39 for the `q` search,
so it does reduce noise) but **the first result is the offending Horizons release**, and
the set still contains Pokémon Origins and other sibling shows. Id-scoping alone would
not have changed which release was grabbed.

Additionally, the returned items carry only `category`, `size` and `guid` newznab
attributes — **no per-item `tvdbid`** — so Sonarr's per-release id comparison is
unavailable on this indexer.

**Storing tvdbid is therefore out of scope.** It is not a fix.

## 2. Approach

Adopt Sonarr's architecture: never trust the indexer, resolve every release back to a
series, and reject anything that does not resolve to the series being searched
(`NzbDrone.Core/Parser/ParsingService.FindSeries`). Sonarr's matching is richer than
Nexus's in two ways — **alias titles** and **user-controlled release restrictions**. This
sub-project adopts the first, plus one signal specific to this failure mode. The second
becomes SP-3 (§9).

Two checks, applied in this order:

1. Series match (primary title **or alias**)
2. Episode-title contradiction

Both replace or extend the existing `releaseIsForSeries`. Backend only — no UI, no
`web/dist` rebuild.

## 3. Component: series aliases

### 3.1 Source

TMDB `GET /3/tv/{series_id}/alternative_titles`, verified against current TMDB docs.
Response shape:

```json
{"id":60572,"results":[{"iso_3166_1":"US","title":"Pokémon: Indigo League","type":"season 1"}]}
```

The real response for series `60572` contains the aliases needed:
`Pokémon: Indigo League` (US), `Pokemon` (US, "alternative spelling"),
`Pocket Monsters` (JP), `Pokémon: To Be a Pokémon Master` (US).

No TVDB API key is required, and Nexus already holds a TMDB key.

### 3.2 Storage

Migration `0009` adds:

```sql
CREATE TABLE series_aliases (
  id         INTEGER PRIMARY KEY,
  series_id  INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  country    TEXT NOT NULL DEFAULT '',
  type       TEXT NOT NULL DEFAULT '',
  UNIQUE(series_id, title)
);
```

Titles are stored **raw**; normalization happens at match time. All countries are stored:
non-Latin titles simply never match an ASCII scene name, so filtering them adds a rule
without removing a risk.

`type` is stored as **opaque metadata only**. It carries season hints
(`"season 1"`, `"seasons 10 - 13"`) but is free text — `"23th season in Catalan"`,
`"Hoopa shorts"` — and is deliberately **not parsed**. Recorded so a future change does
not have to re-derive why.

### 3.3 Population and backfill

Fetched inside `media.TVDetails` (`internal/media/tmdb.go:167`), which already calls
`/tv/{id}` and already runs both on series add and on the 12-hourly `MediaRefresh`.
Existing series therefore backfill on their next refresh with no separate migration step.

Alias fetch failure must **not** fail the refresh: log and continue with whatever aliases
are already stored.

### 3.4 Matching

Replaces `releaseIsForSeries` (`internal/automation/search.go`). A release matches when
its parsed title, normalized, equals the normalized primary title **or any stored alias**.

Normalization is the existing `normTitle`, which already folds diacritics — load-bearing
here, since `"Pokémon"` would otherwise normalize to `"pok mon"` and never equal a
release's `"pokemon"`.

Two guards carried over from the RSS path's `matchSeries`:

- **Ambiguity:** if a release resolves to more than one series in the library — by primary
  title or alias, in any combination — reject it rather than guess.
- **No aliases yet:** a series never refreshed since the migration has no aliases; fall
  back to primary-title matching rather than failing open (accepting everything) or
  closed (accepting nothing).

## 4. Component: episode-title contradiction

### 4.1 Parsing

`parsing.ParsedRelease` gains `EpisodeTitle string`: the segment after the `S##E##`
marker, terminated at the first recognised source / resolution / codec token. The parser
already recognises those tokens.

Worked examples from live indexer data:

| Release | Extracted | Signal |
|---|---|---|
| `Pokemon.S01E01.The.Pendant.That.Starts.It.All.Part.1.1080p.WEBRip…` | `The Pendant That Starts It All Part 1` | present |
| `Pokemon.S01E01.DVDRip.x264-QCF` | `` (stops at `DVDRip`) | absent |
| `Pokemon.S01E01.PDTV.HebDub.XviD-Sweet-Star` | `` (stops at `PDTV`) | absent |
| `Pokmon.Indigo.League.s01e01` | `` (nothing follows) | absent |

Two edge cases, made explicit so they are not decided by accident:

- **No terminating token** — the remainder of the title becomes the episode title. Any
  trailing release-group suffix (`-iVy`) is left in; the overlap comparison in §4.2
  tolerates it, since it only needs one side to contain the other.
- **No `S##E##` marker at all** — `EpisodeTitle` is empty. Such releases already fail the
  season/episode filter upstream, so this adds no new behaviour.

### 4.2 Rule: reject only on positive contradiction

Reject when the release carries an episode title **and** it does not overlap the stored
episode title. Absent or unrecognisable episode title means **no signal, no rejection** —
the check can only ever veto on positive evidence.

Comparison normalizes both sides (`normTitle`) and accepts when either contains the
other, so `"I Choose You"` matches stored `"Pokémon - I Choose You!"`.

Applied to the failure: stored episode `9209` is `"Pokémon - I Choose You!"`; the Horizons
release yields `"The Pendant That Starts It All Part 1"`; no overlap, rejected. The
`DVDRip` and `Indigo.League` releases yield nothing and are unaffected.

## 5. Application points

Both checks apply at the three TV grab paths. This project has been bitten three times by
fixing one site and missing the others, so the sites are enumerated explicitly:

| Site | File |
|---|---|
| `searchEpisode` | `internal/automation/search.go` |
| `searchSeason` pack branch | `internal/automation/search.go` |
| `upgradeEpisode` | `internal/automation/upgrade.go` |

`rssPlaceTV` (`internal/automation/rss.go`) already resolves the series via `matchSeries`
and needs neither check. It should, however, benefit from aliases: `matchSeries`'s
`seriesByTitle` index is built from primary titles only, so an aliased release does not
match there either. Extending that index to include aliases is **in scope** for this
sub-project, since it is the same data and the same defect.

## 6. Testing

- Each check gets its own test **at each path it guards**. A passing test on the search
  path proves nothing about RSS or upgrades — that is precisely how the three previous
  fixes each missed a site.
- Every gate test is mutation-verified: neuter the check, confirm the test fails, revert.
  Report any mutation that comes back green rather than papering over it.
- **The saga regression test**, seeded from real production data: series `Pokémon` with
  its real aliases, stored episode `"Pokémon - I Choose You!"`, and candidates
  `Pokemon.S01E01.The.Pendant…` (Horizons), `Pokemon.Trainer.Tour.S01E01`,
  `Pokmon.Indigo.League.s01e01`, `Pokemon.S01E01.DVDRip.x264-QCF`. Asserts exactly which
  survive. This is the guard for the entire incident.
- A fixture must make outcomes visibly differ: a series with several missing episodes, and
  candidates that differ in *which* check would reject them.

## 7. Out of scope for SP-1

- **Storing tvdbid** — probed and proven not to change the outcome (§1.3).
- **Anime absolute numbering.** `[EncoderAnon]Pocket Monsters (Pokemon) Episode 001…` and
  `[Anime.Time].Pokemon-001-…` carry no `S##E##`, so no season/episode is parsed and every
  automation path filters them out. Genuinely correct releases stay invisible. Its own
  sub-project; Sonarr needs scene mappings for it too.
- **Dub / language filtering.** `ParsedRelease.Languages` exists but nothing filters on it;
  `Pokemon.S01E01.PDTV.HebDub…` ranks alongside English releases. User has explicitly
  requested this for a later sub-project.
- **Alias `type`/season parsing** (§3.2).
- **Movie-side title verification** — `searchMovie` has no equivalent check, but movie
  searches use imdbid/tmdbid which newznab does honour for movies, so the risk is
  materially lower.

## 8. Known consequences

- Six wrong episodes are already imported. Those episodes have files, so a re-search skips
  them; the user must delete them with files and re-search. **This design prevents
  recurrence; it does not undo what landed.**
- A release whose scene name matches neither the primary title nor any alias is rejected.
  That is the intended trade: a missed grab is recoverable, a wrong grab is not.

## 9. Sequenced follow-ons (SP-2, SP-3)

Release restrictions were part of the original design discussion and are deliberately
deferred, because the part that fixes the live bug has no dependencies and should not wait
behind a subsystem. Both are user-approved in shape.

### SP-2 — Tags

A tags subsystem, matching Sonarr and Radarr: tag table, CRUD API, settings UI, and
attachment to **both series and movies**. Movies are explicitly included — Radarr has
release profiles for movies, so a series-only tag system would need revisiting
immediately.

Nexus has no tags today (migrations stop at `0008` before SP-1's `0009`).

### SP-3 — Release profiles *(depends on SP-2)*

Sonarr-placed at **Settings → Profiles → Release Profiles**
(`frontend/src/Settings/Profiles/Release/` in the Sonarr reference), as reusable named
profiles scoped to media by tag — not per-series fields on a detail page.

Carried forward from the SP-1 design discussion, already settled:

- **Required** terms with a `required_mode` of `any` (default, Sonarr's behaviour) or
  `all`. Sonarr offers only `any`, which cannot express a genuine conjunction such as
  requiring both `Indigo` and `1080p`; `all` is a deliberate addition. Any value other
  than `all` is treated as `any`, so a bad value fails to the permissive default rather
  than silently rejecting everything.
- **Ignored** terms: reject if the title contains any. No mode — an "all" variant would
  mean "reject only if it contains every bad word", which is never wanted.
- Case-insensitive **substring** match on the **raw release title**, not the parsed title,
  so tokens parsing strips (`HebDub`, `-BurCyg`) remain targetable.
- **Regex is out of scope.** Sonarr supports `/pattern/` via
  `PerlRegexFactory`/`TermMatcherService`; that is a second matching engine plus escaping
  rules. Additive later if substrings prove blunt.
- Applied at the three TV grab paths **and** `rssPlaceTV` — Sonarr applies restrictions to
  every download decision, not only searches — and at the movie paths once tags cover
  movies.
- **Testing note:** `required_mode` is only testable with **two terms and a candidate
  matching exactly one of them**. With a single term, or a candidate matching both, `any`
  and `all` produce identical results and the test passes against either mode — the same
  fixture trap that made three earlier tests on this project vacuous.
