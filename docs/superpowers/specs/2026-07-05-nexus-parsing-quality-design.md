# Nexus — Parsing & Quality Design Spec (Sub-project 4b)

**Date:** 2026-07-05
**Status:** Approved (design), pending implementation plan
**Program context:** Sub-project 4 (Media management) from the foundation roadmap
(`2026-07-01-nexus-foundation-design.md` §4) is decomposed into three slices, each
with its own spec → plan → build cycle:

- **4a — Library core (done, merged):** media data model, TMDb metadata provider,
  root folders, add/refresh/monitor, REST + WS.
- **4b — Parsing & quality (this spec):** release-name parser, quality definitions,
  quality profiles, and a quality-based release decision/scoring engine.
- **4c — Import & rename:** link completed downloads to library items, rename via
  templates, move into root folders, file tracking, history.

Slices are built in order. 4b depends on 4a (fills its nullable `quality_profile_id`
placeholder); 4c depends on 4a + 4b + sub-3. This spec covers **4b only**.

---

## 1. Purpose

Give Nexus the ability to understand a release name and judge a release against a
user's quality preferences. Two pieces:

1. A pure **release-name parser** turning a title like
   `The.Show.S02E05.1080p.BluRay.x265-GRP` into structured fields (quality, identity,
   release group, proper/repack, language).
2. A **quality system** — a fixed ranked set of quality definitions, user-created
   quality **profiles** (allowed set + cutoff + upgrade flag), and a stateless
   **decision engine** that evaluates and compares parsed releases against a profile.

4b builds these as tested libraries plus a thin REST surface (profile CRUD, profile
assignment, a parse/preview tool). It has no live consumer yet: search/RSS (sub-5) and
existing-file upgrade decisions (4c) will call into these libraries later. 4b performs
no disk, no network, and no grabbing.

## 2. Scope decisions (settled during brainstorming)

1. **Custom formats are deferred.** The *arr power-user layer (user-defined
   regex/attribute rules with per-profile scores) is the largest complexity sink in
   the ecosystem. Quality profiles *without* it are the functional *arr MVP. Custom
   formats become a later slice. This keeps 4b a single spec/plan.
2. **Full *arr-style parse.** The parser extracts quality (source + resolution +
   codec), proper/repack revision, release group, cleaned title, identity fields (TV
   season/episode, movie year/edition), and detected language(s). The parser is the
   natural home for identity/group fields so sub-5 need not build its own parser.
3. **Language is parsed but not a decision axis in 4b.** `Languages` is carried on the
   parse result for sub-5/UI and a future language-profile feature; the 4b decision
   engine keys on quality only.
4. **Quality definitions are a fixed, seeded, code-defined ranked set** (not a table,
   not user-editable). Profiles are user-created selections on top of them.
5. **Standard quality profile:** an ordered/allowed subset of the built-in qualities +
   a cutoff quality + an `upgradeAllowed` flag. **No** per-quality size limits and
   **no** quality grouping (both deferred).
6. **The decision engine is a stateless library** (`Resolve` / `Decide` / `Compare`),
   evaluating a release against a profile *in isolation*. "Is this an upgrade over the
   file I already have?" requires an existing file (4c) and is out of scope; `Compare`
   provides the ordering primitive 4c/sub-5 will use.
7. **REST surface beyond profiles:** a `POST /parse` test/preview endpoint that runs
   the whole pipeline (parse → resolve → optional decide). Mirrors *arr's manual-parse
   tool; invaluable for the future UI and debugging.

## 3. Architecture & module boundaries

Two new feature packages plus a new store file and migration. Every feature module
depends only on `internal/core/*` (and, for `quality`, on `internal/parsing`) — never
on `internal/indexer`, `internal/downloadclient`, or `internal/automation`.

- **`internal/parsing/`** — the pure release-name parser. `Parse(title string, kind
  provider.MediaKind) ParsedRelease`. No DB, no I/O. Depends only on
  `internal/core/provider` (for `MediaKind`). Independently testable with an enormous
  table-driven corpus.
- **`internal/quality/`** — the built-in ranked quality-definition registry (Go
  constants), the stateless decision engine (`Resolve` / `Decide` / `Compare`), the
  profiles service (CRUD over the store), and the REST API (profiles CRUD, definitions
  list, parse/preview). Depends on `internal/parsing` + `internal/core/*`.
- **`internal/core/store/quality_store.go`** + migration **`0005_quality.sql`** — the
  `quality_profiles` table and its CRUD, plus setters for
  `series.quality_profile_id` / `movies.quality_profile_id`. Reuse existing store
  helpers (`boolToInt`, `rowScanner`, `ErrNotFound`) — do not redefine them.
- **Profile assignment** lives in the **media** package as new routes (`PUT
  /api/v1/series/{id}/qualityprofile`, same for movies). Media validates the profile
  exists via `core/store` and sets the column, so media still imports only
  `internal/core/*` — never `internal/quality`. No dependency cycle.

**Alternatives rejected:** (a) folding the parser into `internal/quality` — worse
isolation, the parser deserves independent testing; (b) putting profile CRUD inside
the media package — bloats media and couples two domains.

### 3.1 Foundation / cross-package touch-ups this sub-project requires

- **`core/store`:** add `quality_store.go` (QualityProfile CRUD + assignment setters)
  and migration `0005_quality.sql`.
- **`internal/media`:** add `SetSeriesQualityProfile` / `SetMovieQualityProfile`
  (service + store setters) and the two assignment routes. **Opportunistic fix:** while
  touching the series/movie monitor + assignment setters, apply the 4a-backlog
  `RowsAffected` fix — a toggle/assignment on a missing id returns 404 and emits no
  event (today `SetSeriesMonitored`/`SetMovieMonitored` return 200 + a phantom
  `media.*.updated`).
- **`cmd/nexus/main.go`:** construct `quality.NewService` and `quality.NewAPI`; append
  `qualityAPI.Mount` to the router's variadic mounts. No new scheduled jobs, no new
  bootstrap env keys.

### 3.2 Package layout

```
internal/parsing/
  parsing.go       // ParsedRelease, Source, Resolution, Revision types
  parser.go        // Parse(title, kind) + attribute extractors
  parser_test.go   // large table-driven TV + movie corpus
internal/quality/
  definitions.go   // built-in ranked QualityDefinition registry
  decision.go      // Resolve / Decide / Compare (pure)
  service.go       // profiles CRUD service over the store
  api.go           // REST: definitions, profile CRUD, /parse
  *_test.go
internal/core/store/
  quality_store.go // quality_profiles CRUD + assignment setters
  migrations/0005_quality.sql
```

## 4. Data model & contracts

### 4.1 Table (migration `0005_quality.sql`)

```sql
CREATE TABLE quality_profiles (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT NOT NULL UNIQUE,
    cutoff_quality_id INTEGER NOT NULL,          -- a built-in quality definition id
    upgrade_allowed   INTEGER NOT NULL DEFAULT 1, -- bool
    items             TEXT NOT NULL,             -- JSON: ordered [{qualityId, allowed}]
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

`items` is an ordered JSON array of `{qualityId int, allowed bool}` capturing both the
allowed set and the user's ranking in one column (same JSON-column style sub-3 used for
routing). `series.quality_profile_id` and `movies.quality_profile_id` already exist
(4a, nullable INTEGER); 4b adds setters and validates against `quality_profiles.id`.
The FK has no `ON DELETE` clause — deleting an in-use profile returns a friendly **409**
(same pattern as root folders).

### 4.2 Store types

```go
type QualityProfileItem struct {
    QualityID int  `json:"qualityId"`
    Allowed   bool `json:"allowed"`
}
type QualityProfile struct {
    ID              int64                `json:"id"`
    Name            string               `json:"name"`
    CutoffQualityID int                  `json:"cutoffQualityId"`
    UpgradeAllowed  bool                 `json:"upgradeAllowed"`
    Items           []QualityProfileItem `json:"items"`
    CreatedAt       time.Time            `json:"createdAt"`
}
```

Store methods: `CreateQualityProfile`, `GetQualityProfile`, `ListQualityProfiles`,
`UpdateQualityProfile`, `DeleteQualityProfile` (returns a distinct in-use error when a
series/movie still references it), `SetSeriesQualityProfileID`,
`SetMovieQualityProfileID`.

### 4.3 Built-in quality definitions (`internal/quality`, code-defined)

Stable int ids with a global rank, seeded not stored:

`Unknown(0)`, `SDTV`, `WEBDL-480p`, `Bluray-480p`, `HDTV-720p`, `HDTV-1080p`,
`WEBDL-720p`, `WEBDL-1080p`, `Bluray-720p`, `Bluray-1080p`, `HDTV-2160p`,
`WEBDL-2160p`, `Bluray-2160p`.

```go
type QualityDefinition struct {
    ID         int        `json:"id"`
    Name       string     `json:"name"`
    Source     Source     `json:"source"`
    Resolution Resolution `json:"resolution"`
    Rank       int        `json:"rank"`
}
```

Remux and other exotic tiers are deferred. Exposed read-only via
`GET /api/v1/quality/definitions`.

### 4.4 `parsing.ParsedRelease`

```go
type Source int      // Unknown, CAM, TS, DVD, HDTV, WEBRip, WEBDL, Bluray, ...
type Resolution int  // Unknown, R480p, R720p, R1080p, R2160p
type Revision struct { Version int; IsRepack bool } // proper/repack

type ParsedRelease struct {
    Title        string   `json:"title"`        // cleaned title
    Year         int      `json:"year"`         // 0 if none
    Season       int      `json:"season"`       // TV; -1 if none
    Episodes     []int    `json:"episodes"`     // TV; multi-episode supported
    Edition      string   `json:"edition"`      // movie; "" if none
    Source       Source   `json:"source"`
    Resolution   Resolution `json:"resolution"`
    Codec        string   `json:"codec"`        // informational
    ReleaseGroup string   `json:"releaseGroup"` // "" if none
    Revision     Revision `json:"revision"`
    Languages    []string `json:"languages"`    // detected; informational in 4b
}
```

`quality.Resolve(parsed)` maps `(Source, Resolution)` to a built-in
`QualityDefinition`; an unresolvable pair → `Unknown`.

## 5. Runtime behavior

### 5.1 Parser

Pure function. Extracts, from a release title: cleaned title, year, TV
season/episode(s), movie edition, source, resolution, codec, release group,
proper/repack revision, and language(s). Robust to missing fields (a plain
`Movie.Title.2019` yields title + year, everything else zero/empty). Never errors —
an unrecognizable title yields a best-effort `ParsedRelease` with `Unknown`
source/resolution. Reference the *arr `QualityParser` / `Qualities` logic for *which*
attributes and resolution rules to implement; do not transcribe the accreted regex mass
of `Parser.cs`.

### 5.2 Decision engine (pure)

- **`Resolve(parsed) QualityDefinition`** — deterministic `(Source, Resolution)`
  lookup; unknown → `Unknown` (rank 0).
- **`Decide(parsed, profile) Decision`** where
  `Decision = { Accepted bool; Quality QualityDefinition; Score int; RejectionReason string }`:
  resolve the quality; if its id is not in the profile's *allowed* set →
  `Accepted=false`, reason `"quality not in profile"`; otherwise `Accepted=true` with
  `Score` = the quality's position in the profile's ordered items (higher = more
  preferred), plus a small increment for `Revision`.
- **`Compare(a, b, profile) int`** (−1/0/+1): higher profile-ranked quality wins; tie →
  higher `Revision.Version` wins; still tied → `0` (caller may fall back to
  seeders/size/age — not 4b's concern).

**Boundary:** `Decide`/`Compare` never consult an existing file or a live feed.
`cutoff_quality_id` and `upgrade_allowed` are persisted on the profile now and consumed
by 4c's upgrade logic later.

### 5.3 Profile assignment

`PUT /series/{id}/qualityprofile` / `PUT /movies/{id}/qualityprofile` with body
`{qualityProfileId}`. Validates the profile exists (→404 if not, →404 if the
series/movie id is missing), sets the column, and reuses media's existing
`media.series.updated` / `media.movie.updated` WS emit.

### 5.4 Events

Quality-profile CRUD emits no events. Assignment reuses media's existing series/movie
updated events. No new WSForward entries.

## 6. API surface (all auth-guarded, under `/api/v1`)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/quality/definitions` | list the built-in ranked quality set (read-only) |
| GET | `/qualityprofile` | list profiles |
| POST | `/qualityprofile` | create a profile (validated) |
| GET | `/qualityprofile/{id}` | get one profile |
| PUT | `/qualityprofile/{id}` | update a profile (validated) |
| DELETE | `/qualityprofile/{id}` | delete; **409** if in use |
| PUT | `/series/{id}/qualityprofile` | assign a profile to a series (media pkg) |
| PUT | `/movies/{id}/qualityprofile` | assign a profile to a movie (media pkg) |
| POST | `/parse` | test/preview: `{title, kind, profileId?}` → `{parsed, quality, decision?}` |

Profile validation (create/update): non-empty unique name; `cutoffQualityId` is a real
definition id **and** is within the allowed set; every `items[].qualityId` references a
real definition. `/parse` populates `decision` only when `profileId` is supplied.

## 7. Testing (all offline, CGO-free, deterministic)

- **Parser** — large table-driven suites of real TV + movie titles asserting every
  field: resolution, source, codec, release group, proper/repack, season, multi-episode
  ranges, year, edition, language. This is the bulk of 4b's test volume.
- **Decision engine** — `Resolve` / `Decide` / `Compare` unit tables including Unknown,
  not-allowed rejection, revision tiebreak, and profile-order scoring.
- **Store** — `quality_profiles` CRUD, assignment column setters, and the in-use-delete
  guard (uses `database.Open(t.TempDir()+"/t.db")`, never `:memory:`).
- **API** — CRUD happy/error paths, 409 on in-use delete, 404 on bad assignment,
  `/parse` with and without a profile.

## 8. Acceptance criteria

1. Parser extracts all §4.4 fields across a representative TV + movie corpus; unknown
   titles yield a best-effort parse without erroring.
2. `quality.Resolve` maps source+resolution to the correct built-in definition;
   unresolvable → `Unknown`.
3. Quality-profile CRUD works with full validation; DELETE of an in-use profile → 409.
4. Assigning a profile fills `series.quality_profile_id` / `movies.quality_profile_id`;
   missing series/movie or profile → 404; no phantom event on failure.
5. `Decide` accepts/rejects and scores per §5.2; `Compare` orders per §5.2.
6. `POST /parse` returns parsed fields + resolved quality, plus a decision when a
   profileId is given.
7. `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./... -count=1` all green.
8. Module boundaries hold: `internal/parsing` → `internal/core/*` only;
   `internal/quality` → `internal/parsing` + `internal/core/*` only; `internal/media`
   → `internal/core/*` only (no import of `internal/quality`).

## 9. Out of scope (explicit — later slices / sub-projects)

- Custom formats and custom-format scoring (later slice).
- Language *profiles* / language-based decisions (4b parses language but never decides
  on it).
- Per-quality size limits (min/max/preferred) and size-based rejection.
- Quality *grouping* (equal-rank tiers).
- Remux and other exotic quality tiers.
- Upgrade-vs-existing-file logic (needs files → 4c) and live search/RSS consumption of
  the decision engine (sub-5).

## 10. Notes & deviations

- **Reference reading:** extract *which* attributes and the quality-resolution rules
  from *arr's `NzbDrone.Core/Parser/QualityParser.cs` and `NzbDrone.Core/Qualities/`
  (Sonarr *and* Radarr), not the `Parser.cs` regex mass. Nexus needs one unified parser
  covering both TV (season/episode) and movie (year/edition) forms.
- **Credential handling:** none — 4b introduces no secrets or external services.
- **Scoring today is quality-only.** With custom formats deferred, `Decision.Score` is
  driven by the quality's profile rank plus the revision increment. Custom-format score
  contributions slot into the same `Score` field when that slice lands.
