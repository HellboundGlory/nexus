# Nexus — Interactive "Pick a Release" Search (Wave C3)

Date: 2026-07-17
Status: Approved (design)

## 1. Goal

Let the user run a search for one library item, see **every** release the indexers
returned — including the ones automation would silently discard — and grab any of
them, overriding the profile and blocklist when they choose to.

Nexus today has exactly one grab path, and it is fully automatic: `Decide` drops
releases the quality profile rejects, `enqueueBest` drops blocklisted releases, and
`Enqueue` refuses anything not accepted. When that machinery grabs nothing, the UI
says nothing — the user gets a fire-and-forget 202 and an unchanged page. Diagnosing
the accented-title search bug (Wave C-fixes) was hard for exactly this reason: the
releases existed on the indexer and Nexus rendered no evidence either way.

C3 makes the release list visible and the grab decision the user's.

## 2. Scope

**In scope** — interactive search on a **library item**: a movie, a season, or an
episode. Every grab has a known target, so it produces a tracked queue row + history
and participates in the C1/C2 pipeline exactly like an automatic grab.

**Out of scope (explicit non-goals):**

- **Free-text / item-less search.** A "type anything and grab it" box would have no
  media item to attach a queue row to, and would need the untracked
  `POST /api/v1/download` path (see §3) — a grab that never imports. Separate feature.
- **Series-level interactive search.** A whole-series list spans dozens of seasons
  with no coherent grab target. Sonarr omits it; so do we.
- **Client-side column sorting.** The server ranks best-first and that ordering *is*
  the information (§5.3). Sortable columns are deferred, not designed-around.
- **Changing automation's behavior.** `Decide` and the existing search endpoints are
  untouched. C3 adds paths; it does not alter the automatic ones.

## 3. Two corrections to prior assumptions

Both were found by verifying claims against source rather than trusting the notes.
Recorded here so the reasoning stays traceable.

### 3.1 `POST /api/v1/download` is the wrong grab primitive

Earlier notes recorded that C3 could reuse `GET /api/v1/search` +
`POST /api/v1/download`. **It cannot.** `downloadclient.Service.Grab` is
`route → fetchContent → client.Add` and writes **no queue row and no history** — a
release grabbed that way lands in SABnzbd/qBittorrent and Nexus never tracks,
imports, or blocklists it.

The tracked path is `importing.Service.Enqueue`. `/download` remains correct only for
a hypothetical item-less grab (an explicit non-goal, §2).

### 3.2 The grab endpoint already exists, and the T8 "sole caller" claim is overstated

**`POST /api/v1/queue` (`importing/api.go:86-105`) is already C3's grab endpoint.**
It accepts `downloadUrl`, `title`, `protocol`, `indexerId`, `clientId`, `mediaKind`,
`seriesId`, `episodeIds`, `movieId`, calls `Enqueue`, and returns the tracked queue
row as 201. The frontend has never called it.

This falsifies the rationale for an earlier draft of this design, which routed the
interactive grab through `enqueueBest` with a `force` flag to preserve C1/T8's
"`enqueueBest` is the **sole caller** of `enqueue.Enqueue`, giving compiler-enforced
blocklist coverage for future grab paths **including C3**".

That invariant does not exist. There are **two** callers of `Enqueue`:

| Caller | Path | Blocklist filtered? |
|--------|------|---------------------|
| `automation.enqueueBest` (search.go:128) | automatic grabs | **Yes** — `filterBlocklisted` |
| `importing.API.enqueue` (api.go:96) | `POST /queue`, manual grabs | **No** |

`filterBlocklisted` lives in `enqueueBest`, **not** in `Enqueue`, so `POST /queue`
already bypasses the blocklist. T8's lesson is true only *within* `automation`: it
guarantees no **automatic** grab path skips the filter. It never covered manual ones.

The correct reading is that `importing.API.enqueue` is the deliberate manual path —
"grab exactly this release the user pointed at" — and C3's interactive grab is
precisely that. Handing `enqueueBest` a one-element list plus a flag whose purpose is
to defeat the filtering `enqueueBest` exists to perform would be wrong-shaped, and
would buy no real enforcement while `POST /queue` remains open anyway.

**Not in scope:** "fixing" `POST /queue`'s blocklist bypass. It is the intended
manual path (§5.6).

## 4. The three gates, and why they matter

Three independent filters exist to enforce **automated** policy. C3's entire purpose
is to let the user override precisely these:

| # | Gate | Location | Applies to | C3 override |
|---|------|----------|-----------|-------------|
| 1 | Quality | `automation.Decide` (decide.go:27) | the **read** path | `DecideAll` annotates instead of dropping (§5.1) |
| 2 | Blocklist | `enqueueBest` → `filterBlocklisted` | **automatic** grabs only | n/a — C3 grabs via `POST /queue`, which never consults it (§3.2) |
| 3 | Accept | `importing.Enqueue` (enqueue.go:35) | **all** grabs | `force` skips the gate (§5.6) |

Note gate 2 does not sit on C3's grab path at all. Blocklisted releases are surfaced
as an annotation on the read path (§5.2) and the override is gated by the modal's
confirm (§6.4), not by the server. This is a deliberate, user-approved consequence of
§3.2 — a manual, admin-authed grab of a row the user explicitly clicked.

Two existing facts make this cheap to build on:

- **`quality.Decision` is already the right DTO.** It carries `Accepted`, `Quality`,
  `Score`, and `RejectionReason` with JSON tags, and it populates `Quality` **even
  when it rejects** (decision.go:73). No parallel type is needed.
- **`Resolve` never fails.** Unresolvable input falls back to `definitions[0]` =
  Unknown (ID 0), a real defined quality. A force-grabbed unparseable release
  therefore gets `QualityID: 0`, not a null or a crash. It displays as "Unknown".

## 5. Backend design

### 5.1 `DecideAll` — ranking that keeps the rejects

A new `DecideAll` sits beside `Decide` in `automation/decide.go`, sharing the same
parse and the same `compare` chain. `Decide` is **unchanged**, so automation cannot
regress.

```go
type ScoredCandidate struct {
    Candidate                    // Release + Parsed (existing)
    Decision   quality.Decision  // Accepted / Quality / Score / RejectionReason
    Rejections []string          // uniform reasons; empty == automation would grab it
}
```

### 5.2 One uniform rejection model

All three gates collapse into `Rejections`:

| Source | Example reason |
|--------|----------------|
| Quality profile | `"quality not in profile"` |
| C1 blocklist | `"blocklisted: Not on your server(s)"` |
| Coverage | `"does not cover S01E05"` |

**Empty `Rejections` means automation would have grabbed it.** One rule for the UI —
any reasons → greyed + confirm before grabbing — and it matches Sonarr's `rejections`
array. `Rejections` is **always a non-nil array** on the wire (§5.5).

The UI sends `force: true` for **any** row with rejections. Server-side, `force` is
only *load-bearing* for quality-rejected rows (the one gate on the grab path, §4); on
a blocklisted or non-covering row whose quality is fine, it is a harmless no-op —
`Enqueue` would have accepted that row anyway. Sending it uniformly keeps the client
rule simple and does not overstate what the server enforces.

### 5.3 Sort order, and a trap

Sort is **accepted-first, then `compare`**:

```go
sort.SliceStable(out, func(i, j int) bool {
    ci, cj := len(out[i].Rejections) == 0, len(out[j].Rejections) == 0
    if ci != cj { return ci }            // clean rows first
    return compare(out[i], out[j], profile) > 0
})
```

The accepted-first key **must be explicit**. It is tempting to assume rejected rows
sort last for free because `profileRank` returns `-1` for qualities absent from the
profile — but a quality that is **present in the profile and not allowed**
(`Allowed: false`) returns its **real index** (decision.go:47-54). Under a profile of
`[480p allowed, 1080p not-allowed]`, `quality.Compare` ranks the **rejected** 1080p
release *above* the **accepted** 480p one, floating a rejected row to the top.

This ordering buys a property worth protecting: **row 1 is exactly what auto-search
would have grabbed**, because "clean" is defined as passing the same three filters
automation applies — `Decide`'s quality gate, `enqueueBest`'s blocklist filter, and
the search strategy's coverage filter. §8 tests it.

### 5.4 API

Three **new** synchronous reads mirroring the existing `/automation/search/...`
shape. The grab reuses an **existing** endpoint (§3.2) — no new route:

```
GET  /automation/search/movie/{id}/interactive          (new)
GET  /automation/search/series/{id}/season/{n}/interactive  (new)
GET  /automation/search/episode/{id}/interactive        (new)
POST /queue      { ...existing enqueueBody, force: bool }   (existing + force)
```

The reads are **synchronous 200s**, deliberately unlike the existing fire-and-forget
202 — the modal must block on real results. (Wave-C prod testing also showed the
202 makes "nothing happened" ambiguous; a synchronous read removes that.)

The split is not arbitrary: it mirrors the backend's own module boundary —
**automation searches, importing enqueues**.

The grab **echoes the release fields back** from the modal row (`downloadUrl` /
`title` / `protocol` / `indexerId`), which is exactly what `enqueueBody` already
accepts. No server-side result cache: no eviction, no staleness, no TTL. The whole
`/api/v1` surface is admin-authed — the same trust assumption `GET /search` already
documents (indexer/api.go §10.1).

### 5.5 Response shape

Mirrors the existing `/search` response:

```json
{ "releases": [ ...ScoredRelease ], "indexerErrors": [ ... ] }
```

`indexerErrors` is **load-bearing, not decoration**: if 2 of 3 indexers failed, the
list is *partial*, and rendering a short list with no explanation reproduces the
invisibility this feature exists to remove. Both arrays are non-nil on the wire, as
`GET /search` already does.

Per-row wire shape:

- `rejections` — always an array (`[]` when clean), never absent
- `seeders` — pointer + `omitempty`; **absent** on usenet rows, present on torrents
  (including a real `0`)
- `quality` — always present; `"Unknown"` for unparseable (§4)

### 5.6 The grab path — reuse `POST /queue`, add `force`

Per §3.2, the tracked manual-grab endpoint already exists. The change is a `force`
flag on three structs and one conditional:

```go
type enqueueBody struct { ...; Force bool `json:"force"` }   // api.go:74
type EnqueueRequest struct { ...; Force bool }               // enqueue.go:14

// enqueue.go:35
if !decision.Accepted && !req.Force {
    return store.QueueItem{}, ErrRejected
}
```

**`enqueueBest` is not touched, and neither are its 8 call sites.** Nothing about the
automatic grab paths changes.

The change is **purely additive**, verified against source:

- `force` defaults to `false`, so every existing caller keeps its current behavior.
  The one existing `ErrRejected` assertion (`enqueue_test.go:121`) stays valid.
- `enqueueBody` is decoded in exactly one place (api.go:87).
- All 5 `EnqueueRequest` construction sites use **named** fields, so a new field
  breaks nothing.
- No frontend code calls `POST /queue` today.

`Enqueue` still parses and resolves `QualityID` when forced; `Force` skips only the
accept **gate**, not the quality **resolution** (§4).

Because gate 2 never sits on this path, `force` governs **quality only**. Blocklist
override is implicit on `POST /queue` and gated by the UI (§4, §6.4).

### 5.7 Two consequences, stated not buried

- **Force does not clear the blocklist row.** A forced grab of a blocklisted release
  that fails again is simply re-blocklisted by C1. Force means "this once", not
  "unblock forever". (User-confirmed.)
- **A forced non-covering grab downloads, then fails to import.** `matchEpisode`
  (importer.go:234) cross-checks the *downloaded file's* parsed season/episode
  against the queue row's episode IDs and returns no target on a mismatch, so the
  library cannot be corrupted — the cost is a wasted download and a failed queue row.
  The upside that justifies allowing it: a release Nexus cannot parse at all
  (`parsed.Episodes` empty, one episode ID) imports fine via `matchEpisode`'s
  single-episode trust path — the anime/odd-numbering override case.

## 6. Frontend design

### 6.1 Structure

A new feature directory `web/src/features/search/` — not more files in `library/`.
The entry points live in library, but the modal is shared across movie/season/episode
and owns its own hooks, types, and display logic. Mirrors `activity/`, including a
pure `resolve.ts` (the C2 pattern: pure resolver, unit-tested, no rendering).

```
search/api.ts                      useInteractiveSearch(target) → GET  /automation/search/…/interactive
                                   useInteractiveGrab()        → POST /queue  (§5.6)
search/types.ts                    ScoredRelease + response DTO
search/resolve.ts                  pure: row tone, rejection labels, size/age formatting
search/InteractiveSearchDialog.tsx
search/ReleaseRow.tsx
```

`useInteractiveGrab` invalidates the `queue` key (`activity/api.ts:10`) on success so
the Activity queue reflects the new grab immediately.

### 6.2 Entry points

An "Interactive Search" action alongside the **existing** auto-Search buttons, which
are untouched:

| Target | Location |
|--------|----------|
| Movie | `MovieDetail.tsx` |
| Season | `SeasonSection.tsx` header |
| Episode | episode row |

### 6.3 Columns

Title · Indexer · Size · Age · Seeders (torrents only) · Quality · Status · Grab.
Server order only (§2).

### 6.4 Force needs friction

Clean rows grab on click. Rows with rejections open a confirm listing the reasons
verbatim ("Rejected: quality not in profile. Grab anyway?"). This reuses the confirm
pattern already on the Blocklist tab's Remove, and is cheap insurance against a
misclick that costs a guaranteed-wasted download (§5.7).

### 6.5 No profile → no modal

`DecideAll` needs a profile to score against, and `Enqueue` returns `ErrNoProfile`
regardless — so a profile-less item could open a modal it could never grab from.
Guard at the entry point with the **existing Wave B toast** ("Assign a quality
profile before searching"), same wording, already implemented.

## 7. Error handling

| Case | Behavior |
|------|----------|
| All indexers fail | Error state inside the modal + retry |
| Some indexers fail | Banner naming them; results still shown (§5.5) |
| Grab → `ErrNoProfile` | Toast: assign a quality profile (§6.5) |
| Grab → network/client error | Toast with the reason |
| Grab → `ErrRejected` | Only reachable when `force=false` and the profile changed while the modal was open. Toast; user may retry with force. |

On grab success: toast, close the modal, invalidate the queue query.

## 8. Testing

**Go**

- `DecideAll` sort — **including a regression test for the present-but-not-allowed
  trap** (§5.3): a profile of `[480p allowed, 1080p not-allowed]` must rank the
  accepted 480p above the rejected 1080p. This is the bug the design would otherwise
  have shipped.
- `Rejections` populated correctly per source, and empty for a clean row.
- Row 1 == what `Decide` + `enqueueBest` would grab (the §5.3 property).
- `force: true` skips `Enqueue`'s `ErrRejected`; **`force: false` (and omitted) still
  returns it** — the additive guarantee of §5.6, and the case most likely to regress.
- `QualityID: 0` for a forced unparseable grab (§4).
- Forced grab still writes queue row + history — i.e. it is a *tracked* grab, the
  whole point of §3.1.
- **No test asserts `force` affects the blocklist** — it does not (§4 gate 2). A test
  claiming otherwise would encode the falsified §3.2 model.

**Wire shape (API tests)** — assert via `[]map[string]json.RawMessage`, **not** a
typed-struct round-trip. C2's lesson: Go collapses absent/null/zero, so a typed
unmarshal cannot tell "key absent" from "zero value" and the guard test goes inert
while still passing. Assert: `rejections` present as `[]` on clean rows; `seeders`
**absent** on usenet rows; `indexerErrors` present as `[]` when empty. Mutation-check
each: removing the guard must fail the test.

**Frontend**

- `resolve.ts` pure unit tests (row tone, rejection labels, formatting).
- Dialog: rejected rows render greyed with reasons; the confirm gate fires for
  rejected rows and not for clean ones; grab calls the mutation; the partial-indexer
  banner renders.
- `web/dist` rebuild (it is committed — drift check in CI).

## 9. Deferred

- Free-text / item-less search (§2).
- Client-side column sorting (§2).
- **Tiered id-then-`q` search** — the open follow-up from Wave C-fixes: Nexus mixes
  `q` with `tmdbid`/`imdbid` in one request where Radarr never does. Blocked on
  `caps.go` not parsing `supportedParams`. Unrelated to C3, but it shapes what the
  interactive list contains, so it is worth doing after.
- Force-grab clearing the blocklist row (§5.7) — deliberately not done.
