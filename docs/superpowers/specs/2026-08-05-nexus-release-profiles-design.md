# Nexus Release Profiles (SP-3) — Design

Date: 2026-08-05
Status: Approved
Sub-project: SP-3 of the SP-2 → SP-3 → SP-C batch

## 1. Purpose and scope

Nexus has quality profiles (which *qualities* are allowed) but no way to
restrict *which releases* are acceptable. SP-3 adds **release profiles**:
reusable named rules, scoped to media by tag, that filter and score releases by
substring terms on the raw release title. This is Sonarr's
Settings → Profiles → Release Profiles, adapted to Nexus's tag model.

The behavioural decisions are already settled in
`docs/superpowers/specs/2026-07-20-nexus-release-matching-design.md` §9 and
`docs/superpowers/specs/2026-07-25-nexus-tags-design.md` §8. This spec records
them in full and adds the implementation detail. **Do not re-brainstorm the
settled decisions.**

### 1.1 In scope

- A `release_profiles` table plus a `release_profile_tags` junction table.
- Store CRUD for release profiles, and a batch reader returning the whole
  library's profile associations in one query.
- A `releaseprofile` API package: list / create / update / delete.
- A matching engine that applies release profiles to a candidate release,
  filtering by required/ignored terms and scoring by preferred terms.
- Wiring into the automation grab paths (search, RSS, upgrade) for both TV and
  movies.
- A Settings → Release Profiles page.

### 1.2 Explicitly out of scope

- **Regex terms.** Sonarr supports `/pattern/` via `PerlRegexFactory`; that is a
  second matching engine plus escaping rules. Additive later if substrings prove
  blunt.
- **Per-series/per-movie release profile fields.** Profiles are scoped by tag,
  not by a detail-page field. This is the settled design.
- **Auto-tagging rules** (Sonarr's `AutoTagging`).
- **Delay profiles** (Sonarr's separate concept).
- **Language / dub filtering.** `ParsedRelease.Languages` exists but nothing
  filters on it; that is a separate queued follow-up (anime support), not part
  of SP-3.

## 2. Data model

Migration `internal/core/database/migrations/0011_release_profiles.sql`,
following the style of `0010_tags.sql`.

```sql
CREATE TABLE release_profiles (
  id            INTEGER PRIMARY KEY,
  name          TEXT NOT NULL,
  required_mode TEXT NOT NULL DEFAULT 'any',
  required_any  TEXT NOT NULL DEFAULT '[]',   -- JSON array of strings
  required_all  TEXT NOT NULL DEFAULT '[]',   -- JSON array of strings
  ignored       TEXT NOT NULL DEFAULT '[]',   -- JSON array of strings
  preferred     TEXT NOT NULL DEFAULT '[]',   -- JSON array of strings
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE release_profile_tags (
  release_profile_id INTEGER NOT NULL REFERENCES release_profiles(id) ON DELETE CASCADE,
  tag_id             INTEGER NOT NULL REFERENCES tags(id)             ON DELETE CASCADE,
  PRIMARY KEY(release_profile_id, tag_id)
);

CREATE INDEX idx_release_profile_tags_tag ON release_profile_tags(tag_id);
```

### 2.1 Term lists stored as JSON arrays, not a separate table

Each profile has four term lists: `required_any`, `required_all`, `ignored`,
`preferred`. Storing them as JSON text columns (like `quality_profiles.items`)
keeps the read path a single row scan and matches the existing quality-profile
pattern. A separate `release_profile_terms` table would add a join for no
benefit — the lists are always read and written whole.

### 2.2 Junction table, not a JSON column on the profile

Scoping is by tag, and SP-3's hot-path query is "given this series/movie, which
release profiles apply?" — that is `series → series_tags → release_profile_tags`
(or `movies → movie_tags → release_profile_tags`), a plain join. This mirrors
the tags spec §2.2 decision: a JSON column on the profile would force a scan and
decode of every profile row instead of a join.

### 2.3 Cascade is real here

`internal/core/database/database.go:17` opens the DSN with
`_pragma=foreign_keys(ON)`, so `ON DELETE CASCADE` is honoured. Deleting a tag
removes its `release_profile_tags` rows. Deleting a release profile removes its
junction rows.

**Deleting a tag that scopes a release profile is refused** (tags spec §4.3):
a tag deletion would silently un-scope a release profile with no warning. The
tags API already refuses in-use tag deletion, so this is already handled — a
release profile referencing a tag makes that tag "in use" via the junction
table, and the existing `DeleteTag` in-use check must count
`release_profile_tags` rows too.

## 3. Store surface

New file `internal/core/store/release_profile_store.go`.

```go
type ReleaseProfile struct {
    ID           int64     `json:"id"`
    Name         string    `json:"name"`
    RequiredMode string    `json:"requiredMode"`
    RequiredAny  []string  `json:"requiredAny"`
    RequiredAll  []string  `json:"requiredAll"`
    Ignored      []string  `json:"ignored"`
    Preferred    []string  `json:"preferred"`
    TagIDs       []int64   `json:"tagIds"`
    CreatedAt    time.Time `json:"createdAt"`
}

ListReleaseProfiles(ctx) ([]ReleaseProfile, error)          // TagIDs populated
CreateReleaseProfile(ctx, p ReleaseProfile) (ReleaseProfile, error)
GetReleaseProfile(ctx, id int64) (ReleaseProfile, error)
UpdateReleaseProfile(ctx, p ReleaseProfile) error
DeleteReleaseProfile(ctx, id int64) error                   // ErrNotFound

// Batch readers for the RSS hot path — built once per sweep, not per release.
SeriesReleaseProfileIDs(ctx) (map[int64][]int64, error)     // seriesID -> profileIDs
MovieReleaseProfileIDs(ctx) (map[int64][]int64, error)      // movieID -> profileIDs
```

New sentinel error `ErrReleaseProfileInUse` is **not** needed — release profiles
are not referenced by series/movies (scoping is by tag, not by a profile id
column), so delete is always safe. `DeleteReleaseProfile` returns `ErrNotFound`
for a missing id.

`TagIDs` is populated by `ListReleaseProfiles` and `GetReleaseProfile` via a
join; `CreateReleaseProfile`/`UpdateReleaseProfile` write the junction rows in a
transaction. An unknown tag id in the input returns `ErrTagNotFound` and rolls
back (mirroring `SetSeriesTags`).

`SeriesReleaseProfileIDs` / `MovieReleaseProfileIDs` return an empty map for an
empty library, and omit entities with no profiles. They are the SP-3 analogue of
`SeriesTagIDs` / `MovieTagIDs` — built once per RSS sweep, not per release.

## 4. Matching engine

New file `internal/quality/release_profile.go` (the quality package already owns
the decision engine; release-profile matching is a sibling concern).

```go
// ReleaseProfileMatch is the result of evaluating one release against one
// release profile.
type ReleaseProfileMatch struct {
    Accepted bool
    Score    int
    Reason   string // rejection reason, when not accepted
}

// MatchReleaseProfile evaluates a raw release title against a release profile.
// Matching is case-insensitive substring on the RAW title, not the parsed title.
func MatchReleaseProfile(rawTitle string, p store.ReleaseProfile) ReleaseProfileMatch
```

### 4.1 Term semantics (settled in release-matching spec §9)

- **Required terms** with a `required_mode` of `any` (default, Sonarr's
  behaviour) or `all`. Sonarr offers only `any`, which cannot express a genuine
  conjunction such as requiring both `Indigo` and `1080p`; `all` is a deliberate
  addition. Any value other than `all` is treated as `any`, so a bad value fails
  to the permissive default rather than silently rejecting everything.
- **Ignored terms**: reject if the title contains any. No mode — an "all"
  variant would mean "reject only if it contains every bad word", which is never
  wanted.
- Case-insensitive **substring** match on the **raw release title**, not the
  parsed title, so tokens parsing strips (`HebDub`, `-BurCyg`) remain
  targetable.
- **Regex is out of scope.**

### 4.2 The `required_mode` field

The `ReleaseProfile` struct carries `RequiredMode string` (`"any"` or `"all"`).
It is stored as a column `required_mode TEXT NOT NULL DEFAULT 'any'`. Any value
other than `"all"` is treated as `"any"` at match time.

### 4.3 Scoring

A release is **accepted** if it passes the required and ignored checks. It is
then **scored** by the number of preferred terms it contains (one point each).
The score is used to rank candidates: a release matching more preferred terms
ranks above one matching fewer, all else equal. Preferred terms do not gate
acceptance — they only boost ranking.

### 4.4 Combining with quality profiles

Release profiles are **orthogonal** to quality profiles. A release must pass
both:
1. The quality profile's accept gate (existing `quality.Decide`).
2. Every applicable release profile's required/ignored checks.

The release-profile score is folded into the candidate ranking as a tiebreaker
after the quality comparison, before the torrent-seeder/usenet-age/size
tiebreakers.

## 5. API

### 5.1 New package `internal/releaseprofile`

`internal/releaseprofile/api.go` with `func (a *API) Mount(r chi.Router)`,
mounted through the existing `NewRouter(..., mounts...)` varargs in
`internal/core/api/api.go`. The shape follows `internal/quality/api.go`.

| Method | Path | Body | Success | Errors |
|---|---|---|---|---|
| GET | `/api/v1/releaseprofile` | — | 200 `[ReleaseProfile]` | — |
| POST | `/api/v1/releaseprofile` | `ReleaseProfile` | 201 `ReleaseProfile` | 400 invalid, 400 unknown tag |
| GET | `/api/v1/releaseprofile/{id}` | — | 200 `ReleaseProfile` | 404 |
| PUT | `/api/v1/releaseprofile/{id}` | `ReleaseProfile` | 200 `{ok:true}` | 400, 404 |
| DELETE | `/api/v1/releaseprofile/{id}` | — | 200 `{ok:true}` | 404 |

Validation (in the service layer, mirroring `internal/quality/service.go`):
- `Name` must be non-empty after trimming.
- At least one of `RequiredAny`, `RequiredAll`, `Ignored`, `Preferred` must be
  non-empty (a profile with no terms is meaningless).
- `RequiredMode` must be `"any"` or `"all"` (anything else → 400 at write time,
  even though match time treats non-`"all"` as `"any"`).
- Every `TagID` must exist (else `ErrTagNotFound` → 400).

### 5.2 No per-media assignment endpoints

Release profiles are scoped by tag, and tags are already assigned via the
existing `PUT /series/{id}/tags` and `PUT /movies/{id}/tags` endpoints. No new
media routes are needed.

## 6. Automation wiring

Release profiles apply at **every** grab path, for both TV and movies — Sonarr
applies restrictions to every download decision, not only searches. The four TV
grab paths from the release-matching spec §5, plus the movie paths:

| Site | File | Release profiles |
|---|---|---|
| `searchEpisode` | `internal/automation/search.go` | yes |
| `searchSeason` pack branch | `internal/automation/search.go` | yes |
| `upgradeEpisode` | `internal/automation/upgrade.go` | yes |
| `rssPlaceTV` | `internal/automation/rss.go` | yes |
| `searchMovie` | `internal/automation/search.go` | yes |
| `upgradeMovie` | `internal/automation/upgrade.go` | yes |
| RSS movie path | `internal/automation/rss.go` | yes |

### 6.1 Resolving applicable profiles

For a given series/movie, the applicable release profiles are those whose
`TagIDs` intersect the item's tag set, **plus** any profile with no tags (a
profile with no tags applies to everything, matching Sonarr).

The RSS hot path builds `SeriesReleaseProfileIDs` / `MovieReleaseProfileIDs`
once per sweep (via the batch readers in §3), then resolves each item's
applicable profiles by intersecting with the item's tag ids. The search/upgrade
paths load the item's tags and the profile list per item — these are not hot
paths, so a per-item lookup is acceptable there.

### 6.2 Where the filter runs

The release-profile filter runs **after** `quality.Decide` accepts a release and
**before** the candidate is added to the covering/pack list. A release rejected
by any applicable release profile is dropped. The release-profile score is
folded into `compare` as a tiebreaker.

### 6.3 The `Decide` signature change

`Decide` currently takes `(releases, kind, profile)`. It gains a release-profile
evaluation. To keep the change surgical, the automation service resolves the
applicable release profiles per item and passes them into `Decide` as a new
parameter:

```go
func Decide(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile, rps []store.ReleaseProfile) []Candidate
```

`Candidate` gains a `ReleaseProfileScore int` field, populated by `Decide` and
used by `compare` as a tiebreaker.

## 7. Frontend

### 7.1 Settings → Release Profiles

A new entry in the `TABS` array in `web/src/features/settings/SettingsLayout.tsx`,
placed after **Quality Profiles** and before **Tags** (matching Sonarr's
Settings → Profiles ordering).

`ReleaseProfilesSection` lists each profile with its name, term counts, and
assigned tags, with add / edit / delete affordances. The dialog (`ProfileDialog`
analogue) edits the name, the four term lists, the required mode, and the
assigned tags (multi-select from existing tags, reusing the `TagInput`-style
pattern from the tags feature).

### 7.2 Types and API hooks

- `web/src/features/settings/releaseProfileTypes.ts` — the `ReleaseProfile` type.
- `web/src/features/settings/releaseProfileApi.ts` — query/mutation hooks for the
  CRUD endpoints, mirroring `qualityApi.ts`.

## 8. Testing

### 8.1 Go

`release_profile_store_test.go`:

- CRUD round-trip; `ListReleaseProfiles` populates `TagIDs`.
- Unknown tag id in create/update → `ErrTagNotFound`, no partial write.
- `SeriesReleaseProfileIDs` / `MovieReleaseProfileIDs` with ≥2 series each
  holding ≥2 profiles, so a "returns only the first row per entity" bug is
  visible.
- **Fixture rule:** series and movies have independent rowid sequences (tags
  spec §6.1). Use different tag ids and different media ids on the two sides.

`release_profile_test.go` (matching engine):

- Required `any`: candidate matching one of two terms accepted.
- Required `all`: candidate matching only one of two terms rejected.
- **The `required_mode` fixture trap** (release-matching spec §9): `any` and
  `all` are only distinguishable with **two terms and a candidate matching
  exactly one of them**. A single term, or a candidate matching both, makes the
  test pass against either mode. Every `required_mode` test must use two terms
  and a candidate matching exactly one.
- Ignored: candidate containing an ignored term rejected.
- Preferred: candidate matching more preferred terms scores higher.
- Case-insensitive substring on the raw title: `HebDub` in the raw title is
  targetable even though parsing strips it.

`automation` tests: each grab path filters by release profile. A passing test on
one path proves nothing about the others (the four-grab-path lesson). Every gate
test is mutation-verified.

### 8.2 Frontend (vitest)

- `ReleaseProfilesSection`: list renders, add/edit/delete round-trip through
  mocked hooks, 400 surfaces as a toast.
- The dialog: term list editing, required-mode toggle, tag multi-select.

### 8.3 Process

Every task mutation-verified before it is reported done, with the named
mutations written into the task brief up front — the controller-addendum pattern
from SP-B.

## 9. Build and verification

- `web/dist` **must** be rebuilt, committed, and verified reproducible. SP-3
  ships UI.
- Frontend typecheck is `cd web && npx tsc -p tsconfig.app.json --noEmit`. A
  bare `npx tsc --noEmit` in `web/` typechecks nothing and always exits 0.
- `gofmt -l` is useless in this repo (line-ending noise lists nearly every
  file). Trust `go build` and `go vet`.
- The database migration count assertion in
  `internal/core/database/database_test.go` is a hardcoded "expected N applied
  migrations" — **adding migration 0011 must bump it**, or the database suite
  fails.
