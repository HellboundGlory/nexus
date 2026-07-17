# Interactive "Pick a Release" Search (Wave C3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user search one library item (movie / season / episode), see **every** release the indexers returned — including the ones automation silently discards — and grab any of them, overriding the quality profile and blocklist when they choose to.

**Architecture:** Purely additive. `automation.Decide` and every automatic grab path are untouched. A new `DecideAll` annotates releases with rejection reasons instead of dropping them; three new synchronous `GET .../interactive` endpoints return the scored list; the grab reuses the **existing** `POST /api/v1/queue` with one new additive `force` flag. The frontend gets a new `web/src/features/search/` directory mirroring `activity/`'s pure-`resolve.ts` pattern.

**Tech Stack:** Go 1.26 (chi router, `database/sql` + SQLite), React 19 + TypeScript + TanStack Query + Radix UI + Tailwind, vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-07-17-nexus-interactive-search-design.md`

## Global Constraints

- **Never modify `automation.Decide`, `enqueueBest`, `filterBlocklisted`, or any of `enqueueBest`'s 8 call sites.** Spec §2, §5.6. Automation must not regress.
- **`force` defaults to `false`** and governs the **quality gate only**. It is never consulted for the blocklist. No test may assert that `force` affects the blocklist (spec §8) — it does not.
- **`internal/automation` may import only** `internal/core/*`, `internal/parsing`, `internal/quality`, `internal/importing` (package doc, `automation.go:1-6`). It must **not** import `internal/indexer`.
- **Wire-shape tests assert via `[]map[string]json.RawMessage`, never a typed-struct round-trip.** Go collapses absent/null/zero into the zero value, so a typed unmarshal cannot distinguish "key absent" from "zero value" and the guard test silently goes inert. (C2's key lesson.) Every such guard must be mutation-checked: remove the guard → test FAILS; restore → PASSES.
- **`rejections` and `indexerErrors` are always non-nil arrays on the wire** (`[]`, never absent/null). `seeders` is a pointer + `omitempty` — **absent** on usenet rows, present on torrents including a real `0`.
- **`web/dist` is committed** and CI runs a drift check. The final frontend task must rebuild it.
- Every task must leave the repo green: `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` (20 packages), and for frontend tasks `cd web && npx vitest run && npx tsc -b`.

---

## Two gaps found while verifying the spec against source

Both are **enablers for the approved design, not design changes.** They were found by reading the source the spec cites, and they are folded into the tasks below.

**Gap 1 — the `Searcher` interface throws away what §7's banner needs.** Spec §5.5/§7 require `indexerErrors: [{indexerId, message}]` so the modal can name the indexers that failed. But `automation.Searcher` is `Search(ctx, q) ([]provider.Release, error)`, and the adapter at `cmd/nexus/main.go:256-262` collapses every per-indexer error into one anonymous string:

```go
return res.Releases, fmt.Errorf("automation: %d indexer error(s)", len(res.IndexerErrors))
```

The `indexerId` and `message` are destroyed before automation ever sees them. **Task 2** widens the interface additively. (`indexer.SearchResult` already carries the structured `[]IndexerError` — only the adapter discards it.)

**Gap 2 — `DecideAll` must reproduce all three filters, not just quality.** Spec §5.1's sketch only shows the quality gate, but §5.3's load-bearing property — *"row 1 is exactly what auto-search would have grabbed"* — is defined by all three of automation's filters: quality (`Decide`), blocklist (`filterBlocklisted`), and coverage (each search strategy's own filter). A `DecideAll` that only annotates quality would rank a blocklisted or non-covering release at row 1 and break the property. **Task 3** threads the blocklist reasons and a coverage predicate in, and **Task 3 Step 7** tests the property against all three.

Gap 2 also needs a blocklist **reason** string for §5.2's `"blocklisted: Not on your server(s)"`, but `store.BlocklistedTitles` returns only `map[string]bool`. **Task 1** adds the reason-carrying sibling.

---

## File Structure

**Backend — create:**

| File | Responsibility |
|------|----------------|
| `internal/automation/interactive.go` | `ScoredCandidate`, `ScoredRelease`, `InteractiveResult`, `Coverage`, `DecideAll`, the 3 service entry points |
| `internal/automation/interactive_test.go` | `DecideAll` sort/rejection/property tests |
| `internal/automation/interactive_api_test.go` | endpoint + wire-shape tests |

**Backend — modify:**

| File | Change |
|------|--------|
| `internal/core/store/blocklist_store.go` | + `BlocklistedReasons` (T1) |
| `internal/automation/automation.go` | + `IndexerError`, + `Searcher.SearchDetailed` (T2) |
| `cmd/nexus/main.go:249-262` | adapter implements `SearchDetailed` (T2) |
| `internal/automation/search_test.go` | `fakeSearcher` gains `SearchDetailed` (T2) |
| `internal/importing/enqueue.go:14,35` | + `EnqueueRequest.Force`, gate (T4) |
| `internal/importing/api.go:74,96` | + `enqueueBody.Force`, pass through (T4) |
| `internal/automation/api.go:29-40` | + 3 interactive routes (T5) |

**Frontend — create** (`web/src/features/search/`): `types.ts`, `api.ts`, `resolve.ts`, `resolve.test.ts`, `InteractiveSearchDialog.tsx`, `InteractiveSearchDialog.test.tsx`, `ReleaseRow.tsx`.

**Frontend — modify:** `library/MovieDetail.tsx`, `library/SeriesDetail.tsx`, `library/SeasonTable.tsx`, `library/SeasonSection.tsx`.

---

## Task Order & Dependencies

```
T1 (store reasons) ─┐
T2 (SearchDetailed) ─┼─→ T3 (DecideAll) ─→ T5 (endpoints) ─→ T6 (FE api/resolve) ─→ T7 (FE dialog) ─→ T8 (FE entry + dist)
T4 (force flag) ────┘                          ↑
                     └─────────────────────────┘
```

T1, T2, T4 are independent of each other. T3 needs T1+T2. T5 needs T3+T4.

---

### Task 1: `store.BlocklistedReasons`

`DecideAll` must label a blocklisted row `"blocklisted: <reason>"` (spec §5.2). The existing `BlocklistedTitles` returns `map[string]bool` and drops the reason. This adds a sibling that keeps it. `BlocklistedTitles` is **left alone** — `filterBlocklisted` still uses it, and changing it would touch automatic grab paths.

**Files:**
- Modify: `internal/core/store/blocklist_store.go` (append after `BlocklistedTitles`, which ends at line 125)
- Test: `internal/core/store/blocklist_store_test.go`

**Interfaces:**
- Consumes: existing `blocklist` table (migration 0007), columns `norm_title`, `reason`, `movie_id`, `series_id`; `store.NormReleaseTitle`.
- Produces: `func (s *Store) BlocklistedReasons(ctx context.Context, movieID, seriesID *int64) (map[string]string, error)` — maps normalised release title → blocklist reason. Same scoping rules as `BlocklistedTitles`: `movieID` wins if both non-nil; both nil returns an empty (non-nil) map and no error.

- [ ] **Step 1: Write the failing test**

Append to `internal/core/store/blocklist_store_test.go`:

```go
func TestBlocklistedReasonsScopedByMovie(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	movieID := int64(7)
	otherID := int64(8)
	if _, err := st.AddBlocklist(ctx, Blocklist{
		MediaKind: "movie", MovieID: &movieID,
		SourceTitle: "Some.Movie.2019.1080p.WEB-DL", Protocol: "usenet",
		QualityID: 3, Reason: "Not on your server(s)",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBlocklist(ctx, Blocklist{
		MediaKind: "movie", MovieID: &otherID,
		SourceTitle: "Other.Movie.2020.1080p.WEB-DL", Protocol: "usenet",
		QualityID: 3, Reason: "Aborted, cannot be completed",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.BlocklistedReasons(ctx, &movieID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry scoped to movie %d, got %d: %v", movieID, len(got), got)
	}
	key := NormReleaseTitle("Some.Movie.2019.1080p.WEB-DL")
	if got[key] != "Not on your server(s)" {
		t.Fatalf("reason for %q = %q, want %q", key, got[key], "Not on your server(s)")
	}
}

func TestBlocklistedReasonsNoTargetReturnsEmptyNonNil(t *testing.T) {
	st := newTestStore(t)
	got, err := st.BlocklistedReasons(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("want a non-nil empty map, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want empty map, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/store/ -run TestBlocklistedReasons -v`
Expected: FAIL — `st.BlocklistedReasons undefined (type *Store has no field or method BlocklistedReasons)`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/core/store/blocklist_store.go`:

```go
// BlocklistedReasons is BlocklistedTitles carrying the reason text, for callers
// that must explain *why* a release is blocked rather than just drop it (the
// interactive search list). Scoping matches BlocklistedTitles exactly.
func (s *Store) BlocklistedReasons(ctx context.Context, movieID, seriesID *int64) (map[string]string, error) {
	out := map[string]string{}
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case movieID != nil:
		rows, err = s.db.QueryContext(ctx, `SELECT norm_title, reason FROM blocklist WHERE movie_id = ?`, *movieID)
	case seriesID != nil:
		rows, err = s.db.QueryContext(ctx, `SELECT norm_title, reason FROM blocklist WHERE series_id = ?`, *seriesID)
	default:
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t, reason string
		if err := rows.Scan(&t, &reason); err != nil {
			return nil, err
		}
		out[t] = reason
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/store/ -v -run TestBlocklisted`
Expected: PASS (both new tests, plus the existing `BlocklistedTitles` tests still green)

- [ ] **Step 5: Commit**

```bash
git add internal/core/store/blocklist_store.go internal/core/store/blocklist_store_test.go
git commit -m "feat(store): BlocklistedReasons — blocklist lookup that keeps the reason

BlocklistedTitles returns map[string]bool and drops the reason text. The
interactive search list must label a blocked row with why it was blocked,
so add a sibling that returns norm_title -> reason. BlocklistedTitles is
untouched; filterBlocklisted keeps using it."
```

---

### Task 2: Structured per-indexer errors reach automation

Closes **Gap 1**. `Searcher` gains a second method rather than changing `Search`, so all existing automatic search paths (`search.go`, `rss.go`, `upgrade.go`) and their error handling are untouched. Only **one** fake implements `Searcher` (`fakeSearcher`, `internal/automation/search_test.go:14`), so the compiler-enforced churn is one file.

`automation` cannot import `internal/indexer` (Global Constraints), so it declares its own `IndexerError` with the **same JSON tags** as `indexer.IndexerError` (`internal/indexer/search.go:20-23`). The adapter in `main.go` — which already imports both — does the mapping.

**Files:**
- Modify: `internal/automation/automation.go:17-21`
- Modify: `cmd/nexus/main.go:249-262`
- Modify: `internal/automation/search_test.go:14-24`
- Test: `internal/automation/search_test.go` (new test appended)

**Interfaces:**
- Consumes: `provider.Query`, `provider.Release`; `indexer.Service.Search(ctx, q) indexer.SearchResult` (fields `Releases []provider.Release`, `IndexerErrors []indexer.IndexerError{IndexerID, Message}`).
- Produces:
  - `type IndexerError struct { IndexerID string \`json:"indexerId"\`; Message string \`json:"message"\` }` in package `automation`.
  - `Searcher` interface gains `SearchDetailed(ctx context.Context, q provider.Query) ([]provider.Release, []IndexerError)` — returns releases from the indexers that succeeded plus one entry per indexer that failed. **Never returns an error**: a partial result is the normal case, and the caller renders the failures rather than aborting.
  - `fakeSearcher` gains an `indexerErrors []IndexerError` field that `SearchDetailed` returns.

- [ ] **Step 1: Write the failing test**

Append to `internal/automation/search_test.go`:

```go
func TestFakeSearcherSatisfiesDetailedSearcher(t *testing.T) {
	f := &fakeSearcher{
		releases:      []provider.Release{{Title: "Some.Movie.2019.1080p.WEB-DL", IndexerID: "1"}},
		indexerErrors: []IndexerError{{IndexerID: "3", Message: "timeout"}},
	}
	var s Searcher = f

	rel, errs := s.SearchDetailed(context.Background(), provider.Query{Term: "some movie"})
	if len(rel) != 1 || rel[0].IndexerID != "1" {
		t.Fatalf("releases = %+v, want the one succeeding indexer's release", rel)
	}
	if len(errs) != 1 || errs[0].IndexerID != "3" || errs[0].Message != "timeout" {
		t.Fatalf("indexerErrors = %+v, want the failing indexer named with its message", errs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/automation/ -run TestFakeSearcherSatisfiesDetailedSearcher -v`
Expected: FAIL — `unknown field indexerErrors in struct literal` and `undefined: IndexerError`

- [ ] **Step 3: Write minimal implementation**

In `internal/automation/automation.go`, replace lines 17-21 (the `Searcher` block) with:

```go
// IndexerError names one indexer that failed during an aggregated search. It
// mirrors indexer.IndexerError's wire shape; automation declares its own copy
// because its package contract forbids importing internal/indexer (see the
// package doc above). The adapter at the composition root maps between them.
type IndexerError struct {
	IndexerID string `json:"indexerId"`
	Message   string `json:"message"`
}

// Searcher runs an aggregated indexer search. Satisfied by an adapter over
// *indexer.Service.
//
// Search returns the releases plus a non-fatal aggregate error, and is what the
// automatic paths (missing sweep, RSS, upgrade) use — they only need to log that
// something failed.
//
// SearchDetailed additionally names each failing indexer. Interactive search
// needs this: rendering a short list with no explanation of which indexers were
// missing reproduces exactly the invisibility that feature exists to remove. It
// returns no error because a partial result is the normal case — the caller
// surfaces the failures rather than aborting.
type Searcher interface {
	Search(ctx context.Context, q provider.Query) ([]provider.Release, error)
	SearchDetailed(ctx context.Context, q provider.Query) ([]provider.Release, []IndexerError)
}
```

In `internal/automation/search_test.go`, replace the `fakeSearcher` struct and its `Search` method (lines 14-24) with:

```go
type fakeSearcher struct {
	lastQuery     provider.Query
	releases      []provider.Release
	err           error
	indexerErrors []IndexerError
}

func (f *fakeSearcher) Search(_ context.Context, q provider.Query) ([]provider.Release, error) {
	f.lastQuery = q
	return f.releases, f.err
}

func (f *fakeSearcher) SearchDetailed(_ context.Context, q provider.Query) ([]provider.Release, []IndexerError) {
	f.lastQuery = q
	return f.releases, f.indexerErrors
}
```

> Note: keep the rest of `fakeSearcher`'s existing method bodies exactly as they are. If `Search`'s body in the current file differs from the two lines above, preserve the current body verbatim and only add the new `SearchDetailed` method plus the `indexerErrors` field.

In `cmd/nexus/main.go`, replace lines 249-262 with:

```go
// autoSearchAdapter bridges indexer.Service.Search's SearchResult to the shapes
// automation.Searcher expects, without importing the indexer package into
// internal/automation.
//
// Search flattens per-indexer errors into a non-fatal aggregate error; the
// releases that succeeded are still returned. SearchDetailed preserves each
// failing indexer's id and message so interactive search can name them.
type autoSearchAdapter struct{ svc *indexer.Service }

func (a autoSearchAdapter) Search(ctx context.Context, q provider.Query) ([]provider.Release, error) {
	res := a.svc.Search(ctx, q)
	if len(res.IndexerErrors) > 0 {
		return res.Releases, fmt.Errorf("automation: %d indexer error(s)", len(res.IndexerErrors))
	}
	return res.Releases, nil
}

func (a autoSearchAdapter) SearchDetailed(ctx context.Context, q provider.Query) ([]provider.Release, []automation.IndexerError) {
	res := a.svc.Search(ctx, q)
	errs := make([]automation.IndexerError, 0, len(res.IndexerErrors))
	for _, e := range res.IndexerErrors {
		errs = append(errs, automation.IndexerError{IndexerID: e.IndexerID, Message: e.Message})
	}
	return res.Releases, errs
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/automation/ -run TestFakeSearcher -v && CGO_ENABLED=0 go build ./... && go vet ./...`
Expected: PASS, and the build succeeds — proving `autoSearchAdapter` still satisfies the widened `Searcher` at the composition root.

- [ ] **Step 5: Run the full automation suite to prove no regression**

Run: `go test ./internal/automation/ ./cmd/...`
Expected: PASS — every existing automatic-search test still green.

- [ ] **Step 6: Commit**

```bash
git add internal/automation/automation.go internal/automation/search_test.go cmd/nexus/main.go
git commit -m "feat(automation): SearchDetailed — per-indexer errors reach the caller

The interactive search modal must name the indexers that failed (design
§5.5, §7): a partial release list with no banner is the same invisibility
the feature exists to remove. But autoSearchAdapter collapsed every
per-indexer error into fmt.Errorf(\"%d indexer error(s)\"), destroying the
indexerId and message before automation saw them.

Widen Searcher additively with SearchDetailed rather than changing Search,
so the automatic paths (sweep, RSS, upgrade) and their error handling are
untouched. automation declares its own IndexerError with indexer's wire
tags because its package contract forbids importing internal/indexer; the
composition-root adapter maps between the two."
```

---

### Task 3: `DecideAll` — ranking that keeps the rejects

The heart of C3. Closes **Gap 2**: reproduces **all three** of automation's filters as annotations rather than drops, so that "empty `Rejections` == automation would have grabbed it" is actually true.

**Files:**
- Create: `internal/automation/interactive.go`
- Test: `internal/automation/interactive_test.go`

**Interfaces:**
- Consumes: `provider.Release`, `provider.MediaKind`, `store.QualityProfile`, `store.NormReleaseTitle`, `parsing.Parse`, `parsing.ParsedRelease`, `quality.Decide`, `quality.Decision`; the package-private `compare(a, b Candidate, profile store.QualityProfile) int` and `Candidate{Release, Parsed}` from `decide.go`; `store.BlocklistedReasons` (T1).
- Produces:
  - `type ScoredCandidate struct { Candidate; Decision quality.Decision; Rejections []string }`
  - `type Coverage func(p parsing.ParsedRelease) string` — returns `""` when the release covers the target, else the rejection reason. A `nil` Coverage means "no coverage constraint" (movies).
  - `func SeasonPackCoverage(season int) Coverage`
  - `func EpisodeCoverage(season, episode int) Coverage`
  - `func DecideAll(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile, blocked map[string]string, cover Coverage) []ScoredCandidate`

- [ ] **Step 1: Write the failing tests**

Create `internal/automation/interactive_test.go`:

```go
package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// profile480Only allows 480p (SDTV) and explicitly disallows 1080p WEB-DL while
// keeping it PRESENT in the profile. This is the §5.3 trap: profileRank returns
// -1 only for qualities ABSENT from the profile — a present-but-not-allowed
// quality returns its REAL index, so quality.Compare ranks the rejected 1080p
// ABOVE the accepted 480p. Accepted-first must therefore be an explicit sort key.
func profile480Only() store.QualityProfile {
	return store.QualityProfile{
		Name: "480p only",
		Items: []store.QualityProfileItem{
			{QualityID: qualityIDFor("SDTV"), Allowed: true},
			{QualityID: qualityIDFor("WEBDL-1080p"), Allowed: false},
		},
		CutoffQualityID: qualityIDFor("SDTV"),
	}
}

func TestDecideAllSortsAcceptedFirstDespiteHigherProfileRank(t *testing.T) {
	releases := []provider.Release{
		{Title: "Some.Movie.2019.1080p.WEB-DL.x264-GRP", IndexerID: "1"},
		{Title: "Some.Movie.2019.480p.HDTV.x264-GRP", IndexerID: "1"},
	}
	got := DecideAll(releases, provider.KindMovie, profile480Only(), nil, nil)

	if len(got) != 2 {
		t.Fatalf("want 2 scored candidates (rejects are kept, not dropped), got %d", len(got))
	}
	if len(got[0].Rejections) != 0 {
		t.Fatalf("row 1 must be the ACCEPTED 480p release, got %q with rejections %v",
			got[0].Release.Title, got[0].Rejections)
	}
	if got[1].Rejections[0] != "quality not in profile" {
		t.Fatalf("row 2 rejections = %v, want the not-allowed 1080p labelled", got[1].Rejections)
	}
}

func TestDecideAllLabelsBlocklistedWithReason(t *testing.T) {
	title := "Some.Movie.2019.480p.HDTV.x264-GRP"
	releases := []provider.Release{{Title: title, IndexerID: "1"}}
	blocked := map[string]string{store.NormReleaseTitle(title): "Not on your server(s)"}

	got := DecideAll(releases, provider.KindMovie, profile480Only(), blocked, nil)
	if len(got) != 1 {
		t.Fatalf("want the blocklisted release KEPT and labelled, got %d rows", len(got))
	}
	if got[0].Rejections[0] != "blocklisted: Not on your server(s)" {
		t.Fatalf("rejections = %v, want the blocklist reason surfaced", got[0].Rejections)
	}
}

func TestDecideAllLabelsNonCoveringEpisode(t *testing.T) {
	releases := []provider.Release{
		{Title: "Some.Show.S01E05.480p.HDTV.x264-GRP", IndexerID: "1"},
		{Title: "Some.Show.S01E09.480p.HDTV.x264-GRP", IndexerID: "1"},
	}
	got := DecideAll(releases, provider.KindTV, profile480Only(), nil, EpisodeCoverage(1, 5))
	if len(got) != 2 {
		t.Fatalf("want both releases kept, got %d", len(got))
	}
	if len(got[0].Rejections) != 0 || got[0].Parsed.Episodes[0] != 5 {
		t.Fatalf("row 1 must be the covering S01E05, got %q rejections=%v",
			got[0].Release.Title, got[0].Rejections)
	}
	if got[1].Rejections[0] != "does not cover S01E05" {
		t.Fatalf("row 2 rejections = %v, want the non-covering release labelled", got[1].Rejections)
	}
}

func TestDecideAllSeasonPackCoverageRejectsSingleEpisode(t *testing.T) {
	releases := []provider.Release{
		{Title: "Some.Show.S01.480p.HDTV.x264-GRP", IndexerID: "1"},
		{Title: "Some.Show.S01E03.480p.HDTV.x264-GRP", IndexerID: "1"},
	}
	got := DecideAll(releases, provider.KindTV, profile480Only(), nil, SeasonPackCoverage(1))
	if len(got[0].Rejections) != 0 {
		t.Fatalf("row 1 must be the season pack, got %q rejections=%v",
			got[0].Release.Title, got[0].Rejections)
	}
	if got[1].Rejections[0] != "not a season 1 pack" {
		t.Fatalf("row 2 rejections = %v, want the single episode labelled", got[1].Rejections)
	}
}

// The §5.3 property, tested against ALL THREE filters that define it. This is
// what makes "row 1 == what auto-search would have grabbed" real rather than
// aspirational: a candidate is clean only if it passes quality AND blocklist AND
// coverage — the same three filters Decide + enqueueBest + the search strategy
// apply on the automatic path.
func TestDecideAllRowOneMatchesWhatAutomationWouldGrab(t *testing.T) {
	blockedTitle := "Some.Show.S01E05.480p.HDTV.x264-BLOCKED"
	releases := []provider.Release{
		{Title: "Some.Show.S01E05.1080p.WEB-DL.x264-GRP", IndexerID: "1"},  // quality-rejected
		{Title: blockedTitle, IndexerID: "1"},                              // blocklisted
		{Title: "Some.Show.S01E09.480p.HDTV.x264-GRP", IndexerID: "1"},     // non-covering
		{Title: "Some.Show.S01E05.480p.HDTV.x264-GOOD", IndexerID: "1"},    // clean
	}
	blocked := map[string]string{store.NormReleaseTitle(blockedTitle): "Not on your server(s)"}

	got := DecideAll(releases, provider.KindTV, profile480Only(), blocked, EpisodeCoverage(1, 5))
	if len(got) != 4 {
		t.Fatalf("want all 4 releases kept, got %d", len(got))
	}
	if got[0].Release.Title != "Some.Show.S01E05.480p.HDTV.x264-GOOD" {
		t.Fatalf("row 1 = %q, want the only release passing all three filters", got[0].Release.Title)
	}
	if len(got[0].Rejections) != 0 {
		t.Fatalf("row 1 rejections = %v, want empty (== automation would grab it)", got[0].Rejections)
	}
	for _, c := range got[1:] {
		if len(c.Rejections) == 0 {
			t.Fatalf("row %q has no rejections but must have one", c.Release.Title)
		}
	}
}

func TestDecideAllRejectionsAlwaysNonNil(t *testing.T) {
	releases := []provider.Release{{Title: "Some.Movie.2019.480p.HDTV.x264-GRP", IndexerID: "1"}}
	got := DecideAll(releases, provider.KindMovie, profile480Only(), nil, nil)
	if got[0].Rejections == nil {
		t.Fatal("Rejections must be a non-nil empty slice so it serialises as [] not null")
	}
}
```

The tests need a helper to turn a quality name into its id. Add it to the same file:

```go
// qualityIDFor resolves a built-in quality definition id by name via the public
// quality API, so the tests do not hardcode ladder indices.
func qualityIDFor(name string) int {
	for _, d := range quality.Definitions() {
		if d.Name == name {
			return d.ID
		}
	}
	panic("unknown quality name in test: " + name)
}
```

and add `"github.com/hellboundg/nexus/internal/quality"` to the test file's imports.

> **Before writing the implementation, verify the two assumptions this helper makes**, and adapt if either is wrong:
> 1. `quality.Definitions()` exists and is exported — run `grep -rn "func Definitions" internal/quality/`. If it is not exported, use whatever the existing tests in `internal/automation/decide_test.go` and `internal/quality/decision_test.go` use to build a profile, and mirror that exactly.
> 2. The definition names are `"SDTV"` and `"WEBDL-1080p"` — run `grep -rn "Name:" internal/quality/definitions.go` and use the real names. `internal/quality/definitions.go` is the authority.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/automation/ -run TestDecideAll -v`
Expected: FAIL — `undefined: DecideAll`, `undefined: EpisodeCoverage`, `undefined: SeasonPackCoverage`

- [ ] **Step 3: Write minimal implementation**

Create `internal/automation/interactive.go`:

```go
package automation

import (
	"fmt"
	"sort"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/quality"
)

// ScoredCandidate is a release annotated with why automation would or would not
// have grabbed it. Unlike Candidate (which only ever holds releases that passed
// the accept gate), a ScoredCandidate is produced for EVERY release the indexers
// returned — the whole point of interactive search is to show the rejects.
//
// Rejections is the uniform reason model: an EMPTY Rejections means automation
// would have grabbed this release. That gives the UI one rule (any reasons →
// grey the row + confirm before grabbing) and mirrors Sonarr's rejections array.
type ScoredCandidate struct {
	Candidate
	Decision   quality.Decision
	Rejections []string
}

// Coverage reports why a release does not cover the search target, or "" if it
// does. It is the interactive-search mirror of the per-strategy filters in
// search.go (searchSeason's pack filter, searchEpisode's covering filter) — but
// it annotates instead of dropping. A nil Coverage means no coverage constraint,
// which is the movie case.
type Coverage func(p parsing.ParsedRelease) string

// SeasonPackCoverage accepts only a full pack for the given season: the same
// predicate searchSeason applies at search.go:245 (right season, no episode
// numbers == a pack rather than a single episode).
func SeasonPackCoverage(season int) Coverage {
	return func(p parsing.ParsedRelease) string {
		if p.Season == season && len(p.Episodes) == 0 {
			return ""
		}
		return fmt.Sprintf("not a season %d pack", season)
	}
}

// EpisodeCoverage accepts only releases containing the given episode: the same
// predicate searchEpisode applies at search.go:326.
func EpisodeCoverage(season, episode int) Coverage {
	return func(p parsing.ParsedRelease) string {
		if p.Season == season && containsInt(p.Episodes, episode) {
			return ""
		}
		return fmt.Sprintf("does not cover S%02dE%02d", season, episode)
	}
}

// DecideAll is Decide's interactive sibling: it parses and ranks every release
// exactly as Decide does, but ANNOTATES the ones automation would discard rather
// than dropping them. Decide itself is deliberately untouched so the automatic
// paths cannot regress.
//
// It reproduces all three of automation's filters as annotations — quality
// (Decide's gate), blocklist (enqueueBest's filterBlocklisted), and coverage
// (the search strategy's own filter) — because the guarantee this function sells
// is that row 1 is exactly what auto-search would have grabbed. Annotating only
// some of the three would float a release automation would never take to the top.
//
// blocked maps normalised release title -> blocklist reason (store.BlocklistedReasons);
// nil means nothing is blocked. cover may be nil (no coverage constraint).
func DecideAll(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile, blocked map[string]string, cover Coverage) []ScoredCandidate {
	out := make([]ScoredCandidate, 0, len(releases))
	for _, r := range releases {
		p := parsing.Parse(r.Title, kind)
		decision := quality.Decide(p, profile)

		// Non-nil so the DTO serialises [] rather than null (design §5.5).
		rejections := []string{}
		if !decision.Accepted {
			reason := decision.RejectionReason
			if reason == "" {
				reason = "quality not in profile"
			}
			rejections = append(rejections, reason)
		}
		if reason, ok := blocked[store.NormReleaseTitle(r.Title)]; ok {
			rejections = append(rejections, "blocklisted: "+reason)
		}
		if cover != nil {
			if reason := cover(p); reason != "" {
				rejections = append(rejections, reason)
			}
		}

		out = append(out, ScoredCandidate{
			Candidate:  Candidate{Release: r, Parsed: p},
			Decision:   decision,
			Rejections: rejections,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := len(out[i].Rejections) == 0, len(out[j].Rejections) == 0
		// Accepted-first MUST be an explicit key. It is tempting to assume the
		// rejects sink for free because profileRank returns -1 for qualities
		// absent from the profile — but a quality PRESENT and not allowed returns
		// its REAL index (decision.go:47-54), so under [480p allowed, 1080p
		// not-allowed] quality.Compare ranks the rejected 1080p ABOVE the
		// accepted 480p. Without this key, row 1 would be a release automation
		// would never grab.
		if ci != cj {
			return ci
		}
		return compare(out[i].Candidate, out[j].Candidate, profile) > 0
	})
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/automation/ -run TestDecideAll -v`
Expected: PASS — all 6 tests.

- [ ] **Step 5: Prove the sort-trap test actually guards the trap (mutation check)**

Temporarily delete the `if ci != cj { return ci }` lines from the sort comparator, then run:

Run: `go test ./internal/automation/ -run TestDecideAllSortsAcceptedFirstDespiteHigherProfileRank -v`
Expected: **FAIL** — `row 1 must be the ACCEPTED 480p release, got "Some.Movie.2019.1080p.WEB-DL.x264-GRP"`. This is the bug the design would otherwise have shipped.

Then restore the two lines and re-run:
Expected: PASS.

If the test passes with the key removed, the test is inert — fix the test before continuing.

- [ ] **Step 6: Run the full suite to prove Decide did not regress**

Run: `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`
Expected: PASS (20 packages) — in particular every existing `decide_test.go`, `search_test.go`, `rss_test.go`, and `upgrade_test.go` case.

- [ ] **Step 7: Commit**

```bash
git add internal/automation/interactive.go internal/automation/interactive_test.go
git commit -m "feat(automation): DecideAll — rank releases without dropping the rejects

Decide drops every release the quality profile rejects, so the automatic
path can never explain itself. DecideAll parses and ranks identically but
annotates instead, with a uniform Rejections model: empty Rejections means
automation would have grabbed this release. Decide is untouched.

Two things worth keeping:

  - Rejections covers all three of automation's filters (quality, blocklist,
    coverage), not just quality. The property being sold is 'row 1 is what
    auto-search would have grabbed'; annotating only one filter would float
    a blocklisted or non-covering release to the top and quietly break it.

  - Accepted-first is an explicit sort key, and has a regression test.
    profileRank returns -1 only for qualities ABSENT from the profile — a
    quality present-but-not-allowed returns its real index, so under a
    profile of [480p allowed, 1080p not-allowed] quality.Compare ranks the
    REJECTED 1080p above the ACCEPTED 480p."
```

---

### Task 4: `force` on the grab path

Independent of T1-T3. The smallest change in the plan: one field on two structs, one conditional, and pass-through in the handler. Verified against source as **purely additive**: `force` defaults to `false`; `enqueueBody` is decoded in exactly one place (`api.go:87`); all 5 `EnqueueRequest` construction sites use **named** fields; no frontend calls `POST /queue` today; the only existing `ErrRejected` assertion (`enqueue_test.go:121`) stays valid.

`force` skips only the accept **gate** — not quality **resolution**. A forced unparseable release still resolves to Unknown (ID 0) because `quality.Resolve` never fails, falling back to `definitions[0]`.

**Files:**
- Modify: `internal/importing/enqueue.go:14-24` (struct), `:35` (gate)
- Modify: `internal/importing/api.go:74-84` (body), `:96-99` (pass-through)
- Test: `internal/importing/enqueue_test.go`, `internal/importing/api_test.go`

**Interfaces:**
- Consumes: existing `Service.Enqueue`, `ErrRejected`, `ErrNoProfile`, `quality.Decide`, `quality.Resolve`.
- Produces:
  - `EnqueueRequest` gains `Force bool` (no JSON tag — it is a Go-side struct).
  - `enqueueBody` gains `Force bool \`json:"force"\``.
  - Behaviour: `Enqueue` returns `ErrRejected` iff `!decision.Accepted && !req.Force`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/importing/enqueue_test.go`. These mirror the setup of the existing `ErrRejected` test at line 121 — **read that test first and reuse its exact fixture helpers** (store setup, fake grabber, profile construction); the bodies below assume helpers named `newTestService` and a profile that rejects 1080p. Adapt names to whatever that file already uses.

```go
func TestEnqueueForceSkipsRejectedGate(t *testing.T) {
	svc, movieID := newTestServiceWithRejectingProfile(t)

	got, err := svc.Enqueue(context.Background(), EnqueueRequest{
		DownloadURL: "http://indexer.test/nzb/1",
		Title:       "Some.Movie.2019.1080p.WEB-DL.x264-GRP", // not allowed by the profile
		Protocol:    provider.ProtocolUsenet,
		IndexerID:   "1",
		MediaKind:   provider.KindMovie,
		MovieID:     movieID,
		Force:       true,
	})
	if err != nil {
		t.Fatalf("force=true must skip the accept gate, got %v", err)
	}
	if got.ID == 0 {
		t.Fatal("force grab must write a tracked queue row")
	}
	if got.Status != store.QueueGrabbed {
		t.Fatalf("status = %q, want grabbed", got.Status)
	}
}

// The additive guarantee: every existing caller omits Force, and must keep its
// current behaviour exactly. This is the case most likely to regress.
func TestEnqueueWithoutForceStillRejects(t *testing.T) {
	svc, movieID := newTestServiceWithRejectingProfile(t)

	_, err := svc.Enqueue(context.Background(), EnqueueRequest{
		DownloadURL: "http://indexer.test/nzb/1",
		Title:       "Some.Movie.2019.1080p.WEB-DL.x264-GRP",
		Protocol:    provider.ProtocolUsenet,
		IndexerID:   "1",
		MediaKind:   provider.KindMovie,
		MovieID:     movieID,
		// Force omitted — defaults false
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected when force is omitted", err)
	}
}

// quality.Resolve never fails: unresolvable input falls back to definitions[0] =
// Unknown (ID 0). So a forced grab of a title nothing can parse gets QualityID 0
// — a real defined quality — not a null or a crash. Force skips the accept GATE,
// never the quality RESOLUTION.
func TestEnqueueForceUnparseableGetsQualityIDZero(t *testing.T) {
	svc, movieID := newTestServiceWithRejectingProfile(t)

	got, err := svc.Enqueue(context.Background(), EnqueueRequest{
		DownloadURL: "http://indexer.test/nzb/1",
		Title:       "zzzz",
		Protocol:    provider.ProtocolUsenet,
		IndexerID:   "1",
		MediaKind:   provider.KindMovie,
		MovieID:     movieID,
		Force:       true,
	})
	if err != nil {
		t.Fatalf("force grab of an unparseable title must succeed, got %v", err)
	}
	if got.QualityID != 0 {
		t.Fatalf("QualityID = %d, want 0 (Unknown)", got.QualityID)
	}
}

// A forced grab must be a TRACKED grab — queue row AND history. That is the
// whole reason C3 uses importing.Enqueue rather than downloadclient.Grab, which
// writes neither and would never import.
func TestEnqueueForceWritesHistory(t *testing.T) {
	svc, movieID := newTestServiceWithRejectingProfile(t)
	ctx := context.Background()

	if _, err := svc.Enqueue(ctx, EnqueueRequest{
		DownloadURL: "http://indexer.test/nzb/1",
		Title:       "Some.Movie.2019.1080p.WEB-DL.x264-GRP",
		Protocol:    provider.ProtocolUsenet,
		IndexerID:   "1",
		MediaKind:   provider.KindMovie,
		MovieID:     movieID,
		Force:       true,
	}); err != nil {
		t.Fatal(err)
	}

	hist, err := svc.store.ListHistory(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].EventType != "grabbed" {
		t.Fatalf("history = %+v, want one grabbed event", hist)
	}
}
```

Add a fixture helper to the same file if one does not already exist:

```go
// newTestServiceWithRejectingProfile builds a Service over a temp store with one
// movie assigned a profile that allows 480p and NOT 1080p, so a 1080p release is
// rejected unless forced.
func newTestServiceWithRejectingProfile(t *testing.T) (*Service, int64) {
	t.Helper()
	// Build on whatever fixture enqueue_test.go already uses for the existing
	// ErrRejected test (enqueue_test.go:121) — reuse its store/grabber setup
	// verbatim rather than inventing a parallel one.
	panic("implement using the existing fixtures in this file")
}
```

> Replace that `panic` with the real setup, copied from the existing `ErrRejected` test's fixture. **Do not invent a parallel fixture** — if this file already has a helper that builds a service with a rejecting profile, use it and delete this one.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/importing/ -run "TestEnqueueForce|TestEnqueueWithoutForce" -v`
Expected: FAIL — `unknown field Force in struct literal of type EnqueueRequest`

- [ ] **Step 3: Write minimal implementation**

In `internal/importing/enqueue.go`, replace the `EnqueueRequest` struct (lines 12-24) with:

```go
// EnqueueRequest grabs one release for one library item (single episode, a set
// of episodes for a pack, or a movie) and records the tracking row.
type EnqueueRequest struct {
	DownloadURL string
	Title       string
	Protocol    provider.Protocol
	IndexerID   string
	ClientID    string // optional client override
	MediaKind   provider.MediaKind
	SeriesID    int64
	EpisodeIDs  []int64
	MovieID     int64
	// Force skips the quality accept gate for a release the user explicitly
	// picked in interactive search. It governs QUALITY ONLY: the blocklist is
	// not consulted on this path at all (filterBlocklisted lives in
	// automation.enqueueBest, not here), so force is a no-op for a blocklisted
	// release whose quality is fine. It never skips quality RESOLUTION — a
	// forced unparseable release still resolves to Unknown (ID 0).
	Force bool
}
```

In the same file, replace line 35's gate:

```go
	if !decision.Accepted && !req.Force {
		return store.QueueItem{}, ErrRejected
	}
```

In `internal/importing/api.go`, replace `enqueueBody` (lines 74-84) with:

```go
type enqueueBody struct {
	DownloadURL string             `json:"downloadUrl"`
	Title       string             `json:"title"`
	Protocol    provider.Protocol  `json:"protocol"`
	IndexerID   string             `json:"indexerId"`
	ClientID    string             `json:"clientId"`
	MediaKind   provider.MediaKind `json:"mediaKind"`
	SeriesID    int64              `json:"seriesId"`
	EpisodeIDs  []int64            `json:"episodeIds"`
	MovieID     int64              `json:"movieId"`
	Force       bool               `json:"force"`
}
```

and the `Enqueue` call (lines 96-99):

```go
	q, err := a.svc.Enqueue(r.Context(), EnqueueRequest{
		DownloadURL: b.DownloadURL, Title: b.Title, Protocol: b.Protocol, IndexerID: b.IndexerID,
		ClientID: b.ClientID, MediaKind: b.MediaKind, SeriesID: b.SeriesID, EpisodeIDs: b.EpisodeIDs,
		MovieID: b.MovieID, Force: b.Force,
	})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/importing/ -v`
Expected: PASS — including the pre-existing `ErrRejected` assertion at `enqueue_test.go:121`, unchanged.

- [ ] **Step 5: Run the full suite**

Run: `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`
Expected: PASS (20 packages).

- [ ] **Step 6: Commit**

```bash
git add internal/importing/enqueue.go internal/importing/api.go internal/importing/enqueue_test.go
git commit -m "feat(importing): additive force flag on the grab path

POST /queue is already the tracked manual-grab endpoint (queue row +
history + QueueUpdated); the frontend has simply never called it. C3's
interactive grab is exactly that path, so it reuses it rather than adding
a route — and the only thing missing was a way to say 'I know the profile
rejects this, grab it anyway'.

Force governs QUALITY ONLY. The blocklist is not consulted here at all:
filterBlocklisted lives in automation.enqueueBest, so POST /queue already
bypasses it. On a blocklisted row whose quality is fine, force is a no-op.
The override is gated by the modal's confirm, not by the server.

Force skips the accept GATE, never quality RESOLUTION — a forced
unparseable release resolves to Unknown (ID 0), not null.

Purely additive: force defaults false, so every existing caller keeps its
behaviour and the existing ErrRejected assertion stays valid."
```

---

### Task 5: The three interactive endpoints

**Files:**
- Modify: `internal/automation/interactive.go` (append: DTOs + service entry points)
- Modify: `internal/automation/api.go:29-40` (routes) + handlers
- Test: `internal/automation/interactive_api_test.go`

**Interfaces:**
- Consumes: `DecideAll`, `Coverage`, `SeasonPackCoverage`, `EpisodeCoverage`, `ScoredCandidate` (T3); `IndexerError`, `Searcher.SearchDetailed` (T2); `store.BlocklistedReasons` (T1); existing `Service.profileFor(ctx, *int64) (store.QualityProfile, bool, error)`, `movieQuery`, `tvQuery`, `s.store.GetMovie/GetSeries/GetEpisode`; `api.WriteJSON`, `api.WriteError`, `pathInt64`.
- Produces:
  - `type ScoredRelease struct{...}` — the per-row wire DTO (below).
  - `type InteractiveResult struct { Releases []ScoredRelease \`json:"releases"\`; IndexerErrors []IndexerError \`json:"indexerErrors"\` }`
  - `func (s *Service) InteractiveSearchMovie(ctx context.Context, movieID int64) (InteractiveResult, error)`
  - `func (s *Service) InteractiveSearchSeason(ctx context.Context, seriesID int64, seasonNumber int) (InteractiveResult, error)`
  - `func (s *Service) InteractiveSearchEpisode(ctx context.Context, episodeID int64) (InteractiveResult, error)`
  - Routes: `GET /automation/search/movie/{id}/interactive`, `GET /automation/search/series/{id}/season/{n}/interactive`, `GET /automation/search/episode/{id}/interactive`.
  - Errors: `ErrNoProfile` (automation-local sentinel) → 400 `no_profile`; `store.ErrNotFound` → 404; else 500.

**Why synchronous 200s and not the existing fire-and-forget 202:** the modal must block on real results. Wave-C prod testing also showed the 202 makes "nothing happened" ambiguous — a Minecraft search click showed no grab for 40s and the only way to tell was an explicit POST.

**Why no server-side result cache:** the grab echoes `downloadUrl`/`title`/`protocol`/`indexerId` back from the modal row, which is exactly what `enqueueBody` already accepts. No cache means no eviction, no staleness, no TTL. The whole `/api/v1` surface is admin-authed — the same trust assumption `GET /search` already documents (`indexer/api.go:258-262`).

- [ ] **Step 1: Write the failing tests**

Create `internal/automation/interactive_api_test.go`. Model the store/service/router fixture on the existing `internal/automation/api_test.go` — **read it first and reuse its helpers verbatim**.

```go
package automation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestInteractiveMovieReturnsScoredReleasesAndIndexerErrors(t *testing.T) {
	// Fixture: one movie with the 480p-only profile assigned; searcher returns a
	// rejected 1080p + an accepted 480p, and reports indexer "3" as failing.
	fx := newInteractiveFixture(t)
	fx.searcher.releases = []provider.Release{
		{Title: "Some.Movie.2019.1080p.WEB-DL.x264-GRP", IndexerID: "1", DownloadURL: "http://x/1", Protocol: provider.ProtocolUsenet},
		{Title: "Some.Movie.2019.480p.HDTV.x264-GRP", IndexerID: "1", DownloadURL: "http://x/2", Protocol: provider.ProtocolUsenet},
	}
	fx.searcher.indexerErrors = []IndexerError{{IndexerID: "3", Message: "timeout"}}

	rec := fx.get(t, "/automation/search/movie/"+fx.movieIDStr+"/interactive")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var res InteractiveResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Releases) != 2 {
		t.Fatalf("want both releases incl. the quality-rejected one, got %d", len(res.Releases))
	}
	if len(res.Releases[0].Rejections) != 0 {
		t.Fatalf("row 1 must be the accepted 480p, got %+v", res.Releases[0])
	}
	if len(res.Releases[1].Rejections) == 0 {
		t.Fatal("row 2 (1080p) must carry a rejection reason, not be dropped")
	}
	// indexerErrors is load-bearing: a partial list with no banner reproduces the
	// invisibility this feature exists to remove.
	if len(res.IndexerErrors) != 1 || res.IndexerErrors[0].IndexerID != "3" {
		t.Fatalf("indexerErrors = %+v, want the failing indexer named", res.IndexerErrors)
	}
}

func TestInteractiveMovieNoProfileReturns400(t *testing.T) {
	fx := newInteractiveFixture(t)
	fx.clearMovieProfile(t)

	rec := fx.get(t, "/automation/search/movie/"+fx.movieIDStr+"/interactive")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when the item has no quality profile", rec.Code)
	}
}

func TestInteractiveMovieNotFoundReturns404(t *testing.T) {
	fx := newInteractiveFixture(t)
	rec := fx.get(t, "/automation/search/movie/99999/interactive")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// WIRE SHAPE — assert on the raw JSON map, never a typed round-trip. Go collapses
// absent/null/zero into the zero value, so a typed unmarshal cannot tell "key
// absent" from "zero value" and this guard would pass regardless of omitempty,
// going silently inert. (C2's lesson: the enrichment guard test did exactly that.)
func TestInteractiveWireShape(t *testing.T) {
	fx := newInteractiveFixture(t)
	seeders := 0
	fx.searcher.releases = []provider.Release{
		// usenet: no seeders → the key must be ABSENT
		{Title: "Some.Movie.2019.480p.HDTV.x264-GRP", IndexerID: "1", DownloadURL: "http://x/1", Protocol: provider.ProtocolUsenet},
		// torrent with a REAL 0 seeders → the key must be PRESENT with value 0
		{Title: "Some.Movie.2019.480p.HDTV.x264-OTHER", IndexerID: "2", DownloadURL: "http://x/2", Protocol: provider.ProtocolTorrent, Seeders: &seeders},
	}

	rec := fx.get(t, "/automation/search/movie/"+fx.movieIDStr+"/interactive")

	var raw struct {
		Releases      []map[string]json.RawMessage `json:"releases"`
		IndexerErrors json.RawMessage              `json:"indexerErrors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}

	var usenet, torrent map[string]json.RawMessage
	for _, r := range raw.Releases {
		if string(r["protocol"]) == `"usenet"` {
			usenet = r
		} else {
			torrent = r
		}
	}

	if _, present := usenet["seeders"]; present {
		t.Error("usenet row must OMIT seeders entirely")
	}
	v, present := torrent["seeders"]
	if !present {
		t.Fatal("torrent row must PRESENT seeders even at a real 0")
	}
	if string(v) != "0" {
		t.Errorf("torrent seeders = %s, want 0", v)
	}

	// rejections is always [], never absent and never null
	rj, present := usenet["rejections"]
	if !present {
		t.Fatal("rejections key must always be present")
	}
	if string(rj) != "[]" {
		t.Errorf("rejections = %s, want [] for a clean row", rj)
	}

	// quality is always present, even for an unparseable title
	if _, present := usenet["quality"]; !present {
		t.Error("quality key must always be present")
	}

	// indexerErrors is [] when empty, never null
	if string(raw.IndexerErrors) != "[]" {
		t.Errorf("indexerErrors = %s, want []", raw.IndexerErrors)
	}
}
```

Add the fixture helper to the same file:

```go
type interactiveFixture struct {
	searcher   *fakeSearcher
	router     http.Handler
	movieIDStr string
	seriesID   int64
}

// newInteractiveFixture builds a store with one movie (480p-only profile) and one
// series, an automation Service over a fakeSearcher, and a mounted router.
func newInteractiveFixture(t *testing.T) *interactiveFixture {
	t.Helper()
	// Build on the existing fixtures in internal/automation/api_test.go — reuse
	// its store setup, its Dispatcher fake, and its router mounting verbatim.
	panic("implement using the existing fixtures in api_test.go")
}

func (f *interactiveFixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func (f *interactiveFixture) clearMovieProfile(t *testing.T) {
	t.Helper()
	panic("implement: null out the movie's quality_profile_id via the store")
}
```

> Replace both `panic`s with real setup built from `internal/automation/api_test.go`'s existing helpers. Do not invent a parallel fixture.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/automation/ -run TestInteractive -v`
Expected: FAIL — `undefined: InteractiveResult`

- [ ] **Step 3: Write the DTOs and service entry points**

Append to `internal/automation/interactive.go`:

```go
// ErrNoProfile is returned by the interactive entry points when the target item
// has no quality profile assigned. DecideAll needs a profile to score against,
// and importing.Enqueue would reject the grab anyway, so a profile-less item
// could otherwise open a modal it could never grab from.
var ErrNoProfile = errors.New("automation: item has no quality profile")

// ScoredRelease is one row of the interactive list.
//
// provider.Release carries NO json tags, so this DTO spells every field out
// rather than embedding it — the wire shape must not depend on Go field names.
type ScoredRelease struct {
	Title       string                    `json:"title"`
	DownloadURL string                    `json:"downloadUrl"`
	InfoURL     string                    `json:"infoUrl,omitempty"`
	Size        int64                     `json:"size"`
	IndexerID   string                    `json:"indexerId"`
	Protocol    provider.Protocol         `json:"protocol"`
	PublishDate time.Time                 `json:"publishDate"`
	// Seeders is a POINTER + omitempty: ABSENT on usenet rows, PRESENT on
	// torrents including a real 0. Never use the numeric value as a
	// presence discriminator (the C2 wire-shape trap).
	Seeders *int `json:"seeders,omitempty"`
	// Quality is always present — "Unknown" (ID 0) for an unparseable title,
	// because quality.Resolve never fails.
	Quality  quality.QualityDefinition `json:"quality"`
	Score    int                       `json:"score"`
	Accepted bool                      `json:"accepted"`
	// Rejections is always a non-nil array. Empty means automation would have
	// grabbed this release.
	Rejections []string `json:"rejections"`
}

// InteractiveResult mirrors indexer.SearchResult's shape. Both arrays are
// non-nil on the wire.
type InteractiveResult struct {
	Releases      []ScoredRelease `json:"releases"`
	IndexerErrors []IndexerError  `json:"indexerErrors"`
}

func toScoredReleases(cands []ScoredCandidate) []ScoredRelease {
	out := make([]ScoredRelease, 0, len(cands))
	for _, c := range cands {
		out = append(out, ScoredRelease{
			Title:       c.Release.Title,
			DownloadURL: c.Release.DownloadURL,
			InfoURL:     c.Release.InfoURL,
			Size:        c.Release.Size,
			IndexerID:   c.Release.IndexerID,
			Protocol:    c.Release.Protocol,
			PublishDate: c.Release.PublishDate,
			Seeders:     c.Release.Seeders,
			Quality:     c.Decision.Quality,
			Score:       c.Decision.Score,
			Accepted:    c.Decision.Accepted,
			Rejections:  c.Rejections,
		})
	}
	return out
}

func result(cands []ScoredCandidate, errs []IndexerError) InteractiveResult {
	if errs == nil {
		errs = []IndexerError{}
	}
	return InteractiveResult{Releases: toScoredReleases(cands), IndexerErrors: errs}
}

// InteractiveSearchMovie returns every release the indexers hold for a movie,
// each annotated with why automation would or would not grab it. Unlike
// SearchMovie it grabs nothing, and it deliberately does NOT skip unmonitored or
// already-filed items — the user asked for this list explicitly.
func (s *Service) InteractiveSearchMovie(ctx context.Context, movieID int64) (InteractiveResult, error) {
	m, err := s.store.GetMovie(ctx, movieID)
	if err != nil {
		return InteractiveResult{}, err
	}
	profile, ok, err := s.profileFor(ctx, m.QualityProfileID)
	if err != nil {
		return InteractiveResult{}, err
	}
	if !ok {
		return InteractiveResult{}, ErrNoProfile
	}
	releases, idxErrs := s.search.SearchDetailed(ctx, movieQuery(m))
	blocked, err := s.store.BlocklistedReasons(ctx, &m.ID, nil)
	if err != nil {
		slog.Warn("automation: interactive blocklist lookup failed", "movieId", m.ID, "err", err)
	}
	return result(DecideAll(releases, provider.KindMovie, profile, blocked, nil), idxErrs), nil
}

// InteractiveSearchSeason lists releases for a whole season. Coverage is the
// season-pack predicate — the same one searchSeason applies — so a single
// episode is shown but labelled rather than silently dropped.
func (s *Service) InteractiveSearchSeason(ctx context.Context, seriesID int64, seasonNumber int) (InteractiveResult, error) {
	se, err := s.store.GetSeries(ctx, seriesID)
	if err != nil {
		return InteractiveResult{}, err
	}
	profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
	if err != nil {
		return InteractiveResult{}, err
	}
	if !ok {
		return InteractiveResult{}, ErrNoProfile
	}
	releases, idxErrs := s.search.SearchDetailed(ctx, tvQuery(se, seasonNumber, nil))
	blocked, err := s.store.BlocklistedReasons(ctx, nil, &se.ID)
	if err != nil {
		slog.Warn("automation: interactive blocklist lookup failed", "seriesId", se.ID, "err", err)
	}
	cands := DecideAll(releases, provider.KindTV, profile, blocked, SeasonPackCoverage(seasonNumber))
	return result(cands, idxErrs), nil
}

// InteractiveSearchEpisode lists releases for one episode.
func (s *Service) InteractiveSearchEpisode(ctx context.Context, episodeID int64) (InteractiveResult, error) {
	e, err := s.store.GetEpisode(ctx, episodeID)
	if err != nil {
		return InteractiveResult{}, err
	}
	se, err := s.store.GetSeries(ctx, e.SeriesID)
	if err != nil {
		return InteractiveResult{}, err
	}
	profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
	if err != nil {
		return InteractiveResult{}, err
	}
	if !ok {
		return InteractiveResult{}, ErrNoProfile
	}
	ep := e.EpisodeNumber
	releases, idxErrs := s.search.SearchDetailed(ctx, tvQuery(se, e.SeasonNumber, &ep))
	blocked, err := s.store.BlocklistedReasons(ctx, nil, &e.SeriesID)
	if err != nil {
		slog.Warn("automation: interactive blocklist lookup failed", "episodeId", e.ID, "err", err)
	}
	cands := DecideAll(releases, provider.KindTV, profile, blocked, EpisodeCoverage(e.SeasonNumber, e.EpisodeNumber))
	return result(cands, idxErrs), nil
}
```

Add to `interactive.go`'s import block: `"context"`, `"errors"`, `"log/slog"`, `"time"`.

- [ ] **Step 4: Add the routes and handlers**

In `internal/automation/api.go`, replace the `Mount` body (lines 29-40) with:

```go
func (a *API) Mount(r chi.Router) {
	r.Route("/automation", func(r chi.Router) {
		r.Post("/search/movie/{id}", a.searchMovie)
		r.Post("/search/series/{id}", a.searchSeries)
		r.Post("/search/series/{id}/season/{n}", a.searchSeason)
		r.Post("/search/episode/{id}", a.searchEpisode)
		// Interactive reads are synchronous 200s, deliberately unlike the
		// fire-and-forget 202s above: the modal must block on real results, and
		// prod testing showed a 202 makes "nothing happened" ambiguous.
		r.Get("/search/movie/{id}/interactive", a.interactiveMovie)
		r.Get("/search/series/{id}/season/{n}/interactive", a.interactiveSeason)
		r.Get("/search/episode/{id}/interactive", a.interactiveEpisode)
		r.Route("/config", func(r chi.Router) {
			r.Get("/", a.getConfig)
			r.Put("/", a.putConfig)
		})
	})
}
```

Append the handlers to `internal/automation/api.go`:

```go
// writeInteractive maps the interactive entry points' errors onto the API's
// error vocabulary and writes the result.
func (a *API) writeInteractive(w http.ResponseWriter, res InteractiveResult, err error) {
	switch {
	case err == nil:
		api.WriteJSON(w, http.StatusOK, res)
	case errors.Is(err, ErrNoProfile):
		api.WriteError(w, http.StatusBadRequest, "no_profile", "assign a quality profile before searching")
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "search failed")
	}
}

func (a *API) interactiveMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	res, err := a.svc.InteractiveSearchMovie(r.Context(), id)
	a.writeInteractive(w, res, err)
}

func (a *API) interactiveSeason(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	n, okN := pathInt64(r, "n")
	if !ok || !okN {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id or season")
		return
	}
	res, err := a.svc.InteractiveSearchSeason(r.Context(), id, int(n))
	a.writeInteractive(w, res, err)
}

func (a *API) interactiveEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	res, err := a.svc.InteractiveSearchEpisode(r.Context(), id)
	a.writeInteractive(w, res, err)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/automation/ -run TestInteractive -v`
Expected: PASS — all 4 tests.

- [ ] **Step 6: Prove the wire-shape guards are live (mutation check)**

For each guard, break it, confirm the test FAILS, then restore and confirm it PASSES:

1. Remove `,omitempty` from `ScoredRelease.Seeders` → `TestInteractiveWireShape` must FAIL with `usenet row must OMIT seeders entirely`.
2. In `DecideAll`, change `rejections := []string{}` to `var rejections []string` → must FAIL with `rejections = null, want []`.
3. In `result`, delete the `if errs == nil { errs = []IndexerError{} }` lines → must FAIL with `indexerErrors = null, want []`.

If any mutation leaves the test passing, that guard is inert — fix the test before continuing.

- [ ] **Step 7: Run the full suite**

Run: `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`
Expected: PASS (20 packages).

- [ ] **Step 8: Commit**

```bash
git add internal/automation/interactive.go internal/automation/api.go internal/automation/interactive_api_test.go
git commit -m "feat(automation): synchronous interactive search endpoints

Three new GETs returning {releases, indexerErrors} for a movie, a season,
or an episode. They grab nothing; the grab is POST /queue with force.

Synchronous 200s, deliberately unlike the sibling 202s: the modal must
block on real results, and prod testing showed fire-and-forget makes
'nothing happened' ambiguous — a search click that appeared to do nothing
for 40s could only be diagnosed with an explicit POST.

indexerErrors is load-bearing, not decoration: if 2 of 3 indexers failed,
the list is partial, and rendering a short list with no explanation
reproduces exactly the invisibility this feature exists to remove.

ScoredRelease spells out every field rather than embedding provider.Release,
which carries no json tags — the wire shape must not depend on Go field
names. Wire-shape tests assert on []map[string]json.RawMessage and are
mutation-checked, because a typed round-trip cannot tell absent from zero."
```

---

### Task 6: Frontend types, API hooks, and the pure resolver

Mirrors `activity/`'s split: `types.ts` (wire shapes), `api.ts` (hooks), `resolve.ts` (pure, unit-tested, no rendering).

**Files:**
- Create: `web/src/features/search/types.ts`, `web/src/features/search/api.ts`, `web/src/features/search/resolve.ts`, `web/src/features/search/resolve.test.ts`

**Interfaces:**
- Consumes: `apiGet`, `apiPost` from `@/lib/api`; `useQuery`, `useMutation`, `useQueryClient` from `@tanstack/react-query`; `activityKeys` from `@/features/activity/api`.
- Produces:
  - `type SearchTarget = { kind: "movie"; id: number } | { kind: "season"; seriesId: number; seasonNumber: number } | { kind: "episode"; id: number }`
  - `type ScoredRelease`, `type InteractiveResult`, `type GrabRequest`
  - `useInteractiveSearch(target: SearchTarget | null)` — disabled when `target` is null
  - `useInteractiveGrab()` — mutation over `POST /queue`, invalidates `activityKeys.queue`
  - Pure: `interactivePath(t)`, `formatSize(bytes)`, `formatAge(iso, now?)`, `rowTone(r)`, `rejectionSummary(r)`, `needsConfirm(r)`, `grabBody(r, target)`

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/search/resolve.test.ts`:

```ts
import { describe, it, expect } from "vitest"
import {
  interactivePath, formatSize, formatAge, rowTone,
  rejectionSummary, needsConfirm, grabBody,
} from "./resolve"
import type { ScoredRelease } from "./types"

function release(over: Partial<ScoredRelease> = {}): ScoredRelease {
  return {
    title: "Some.Movie.2019.480p.HDTV.x264-GRP",
    downloadUrl: "http://x/1",
    size: 1_500_000_000,
    indexerId: "1",
    protocol: "usenet",
    publishDate: "2026-07-15T00:00:00Z",
    quality: { id: 1, name: "SDTV", source: "hdtv", resolution: "480p", rank: 1 },
    score: 10,
    accepted: true,
    rejections: [],
    ...over,
  }
}

describe("interactivePath", () => {
  it("builds the movie path", () => {
    expect(interactivePath({ kind: "movie", id: 7 })).toBe("/automation/search/movie/7/interactive")
  })
  it("builds the season path", () => {
    expect(interactivePath({ kind: "season", seriesId: 3, seasonNumber: 2 }))
      .toBe("/automation/search/series/3/season/2/interactive")
  })
  it("builds the episode path", () => {
    expect(interactivePath({ kind: "episode", id: 42 })).toBe("/automation/search/episode/42/interactive")
  })
})

describe("needsConfirm", () => {
  // The single UI rule: any rejections → confirm. Empty rejections means
  // automation would have grabbed it, so it grabs on click.
  it("is false for a clean row", () => {
    expect(needsConfirm(release())).toBe(false)
  })
  it("is true for any rejected row", () => {
    expect(needsConfirm(release({ rejections: ["quality not in profile"] }))).toBe(true)
  })
})

describe("rowTone", () => {
  it("is neutral for a clean row", () => {
    expect(rowTone(release())).toBe("neutral")
  })
  it("is muted for a rejected row", () => {
    expect(rowTone(release({ rejections: ["blocklisted: Not on your server(s)"] }))).toBe("muted")
  })
})

describe("rejectionSummary", () => {
  it("is empty for a clean row", () => {
    expect(rejectionSummary(release())).toBe("")
  })
  it("joins reasons verbatim", () => {
    expect(rejectionSummary(release({ rejections: ["quality not in profile", "does not cover S01E05"] })))
      .toBe("quality not in profile. does not cover S01E05")
  })
})

describe("formatSize", () => {
  it("formats GB", () => expect(formatSize(1_500_000_000)).toBe("1.5 GB"))
  it("formats MB", () => expect(formatSize(350_000_000)).toBe("350 MB"))
  it("handles zero", () => expect(formatSize(0)).toBe("—"))
})

describe("formatAge", () => {
  const now = new Date("2026-07-17T00:00:00Z")
  it("formats days", () => expect(formatAge("2026-07-15T00:00:00Z", now)).toBe("2d"))
  it("formats hours", () => expect(formatAge("2026-07-16T20:00:00Z", now)).toBe("4h"))
  it("handles an empty date", () => expect(formatAge("", now)).toBe("—"))
})

describe("grabBody", () => {
  // force is sent for ANY rejected row. Server-side it is only load-bearing for
  // quality-rejected rows — on a blocklisted or non-covering row whose quality is
  // fine it is a harmless no-op, because Enqueue would have accepted it anyway.
  // Sending it uniformly keeps one client rule and does not overstate what the
  // server enforces.
  it("sends force=false for a clean movie row", () => {
    expect(grabBody(release(), { kind: "movie", id: 7 })).toEqual({
      downloadUrl: "http://x/1",
      title: "Some.Movie.2019.480p.HDTV.x264-GRP",
      protocol: "usenet",
      indexerId: "1",
      mediaKind: "movie",
      movieId: 7,
      force: false,
    })
  })
  it("sends force=true for a rejected row", () => {
    const b = grabBody(release({ rejections: ["quality not in profile"] }), { kind: "movie", id: 7 })
    expect(b.force).toBe(true)
  })
  it("sends seriesId + episodeIds for an episode target", () => {
    expect(grabBody(release(), { kind: "episode", id: 42, seriesId: 3 })).toMatchObject({
      mediaKind: "tv",
      seriesId: 3,
      episodeIds: [42],
    })
  })
  it("sends seriesId + all missing episodeIds for a season target", () => {
    expect(grabBody(release(), { kind: "season", seriesId: 3, seasonNumber: 2, episodeIds: [10, 11] }))
      .toMatchObject({ mediaKind: "tv", seriesId: 3, episodeIds: [10, 11] })
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run src/features/search/resolve.test.ts`
Expected: FAIL — `Failed to resolve import "./resolve"`

- [ ] **Step 3: Write `types.ts`**

Create `web/src/features/search/types.ts`:

```ts
// web/src/features/search/types.ts

// Mirrors internal/quality.QualityDefinition.
export type QualityDef = {
  id: number
  name: string
  source: string
  resolution: string
  rank: number
}

// Mirrors automation.ScoredRelease. Field-by-field with the Go json tags.
export type ScoredRelease = {
  title: string
  downloadUrl: string
  infoUrl?: string
  size: number
  indexerId: string
  protocol: string
  publishDate: string
  // ABSENT on usenet rows; present on torrents including a real 0. Never use the
  // value as a presence check — `seeders != null` is the discriminator.
  seeders?: number
  // Always present; "Unknown" (id 0) for an unparseable title.
  quality: QualityDef
  score: number
  accepted: boolean
  // Always an array. EMPTY means automation would have grabbed this release —
  // that is the single rule the UI keys off.
  rejections: string[]
}

export type IndexerErrorEntry = {
  indexerId: string
  message: string
}

export type InteractiveResult = {
  releases: ScoredRelease[]
  indexerErrors: IndexerErrorEntry[]
}

// The item the search is for. Season/episode targets carry the episode ids the
// grab must be attributed to — a queue row with no episode ids can never import.
export type SearchTarget =
  | { kind: "movie"; id: number }
  | { kind: "season"; seriesId: number; seasonNumber: number; episodeIds?: number[] }
  | { kind: "episode"; id: number; seriesId?: number }

// Mirrors importing.enqueueBody.
export type GrabRequest = {
  downloadUrl: string
  title: string
  protocol: string
  indexerId: string
  mediaKind: string
  seriesId?: number
  episodeIds?: number[]
  movieId?: number
  force: boolean
}
```

- [ ] **Step 4: Write `resolve.ts`**

Create `web/src/features/search/resolve.ts`:

```ts
// web/src/features/search/resolve.ts
// Pure display + request-shaping logic for interactive search. No rendering, no
// I/O — mirrors activity/resolve.ts so the rules are unit-testable in isolation.
import type { ScoredRelease, SearchTarget, GrabRequest } from "./types"

export function interactivePath(t: SearchTarget): string {
  switch (t.kind) {
    case "movie":
      return `/automation/search/movie/${t.id}/interactive`
    case "season":
      return `/automation/search/series/${t.seriesId}/season/${t.seasonNumber}/interactive`
    case "episode":
      return `/automation/search/episode/${t.id}/interactive`
  }
}

// Empty rejections == automation would have grabbed it. Any reasons → grey the
// row and confirm before grabbing. One rule, uniformly applied.
export function needsConfirm(r: ScoredRelease): boolean {
  return r.rejections.length > 0
}

export type RowTone = "neutral" | "muted"
export function rowTone(r: ScoredRelease): RowTone {
  return r.rejections.length > 0 ? "muted" : "neutral"
}

// Reasons are shown verbatim — they come from the server and are the only
// explanation the user gets for why automation passed this release over.
export function rejectionSummary(r: ScoredRelease): string {
  return r.rejections.join(". ")
}

export function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return "—"
  const gb = bytes / 1_000_000_000
  if (gb >= 1) return `${gb.toFixed(1)} GB`
  return `${Math.round(bytes / 1_000_000)} MB`
}

export function formatAge(iso: string, now: Date = new Date()): string {
  if (!iso) return "—"
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return "—"
  const mins = Math.floor((now.getTime() - t) / 60_000)
  if (mins < 60) return `${Math.max(mins, 0)}m`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}

// force is sent for ANY rejected row. Server-side it is only load-bearing for
// quality-rejected rows: the blocklist is not consulted on POST /queue at all, so
// on a blocklisted or non-covering row whose quality is fine, force is a no-op —
// Enqueue would have accepted it anyway. Sending it uniformly keeps the client
// rule simple without overstating what the server enforces.
export function grabBody(r: ScoredRelease, target: SearchTarget): GrabRequest {
  const base = {
    downloadUrl: r.downloadUrl,
    title: r.title,
    protocol: r.protocol,
    indexerId: r.indexerId,
    force: needsConfirm(r),
  }
  switch (target.kind) {
    case "movie":
      return { ...base, mediaKind: "movie", movieId: target.id }
    case "season":
      return { ...base, mediaKind: "tv", seriesId: target.seriesId, episodeIds: target.episodeIds ?? [] }
    case "episode":
      return { ...base, mediaKind: "tv", seriesId: target.seriesId ?? 0, episodeIds: [target.id] }
  }
}
```

- [ ] **Step 5: Write `api.ts`**

Create `web/src/features/search/api.ts`:

```ts
// web/src/features/search/api.ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost } from "@/lib/api"
import { activityKeys } from "@/features/activity/api"
import { interactivePath, grabBody } from "./resolve"
import type { InteractiveResult, ScoredRelease, SearchTarget } from "./types"

export const searchKeys = {
  interactive: (t: SearchTarget) => ["interactive-search", interactivePath(t)] as const,
}

// Interactive search is an explicit user action against live indexers, so it does
// not poll and does not serve stale results — refetching on mount/focus would fire
// real indexer queries the user did not ask for.
export function useInteractiveSearch(target: SearchTarget | null) {
  return useQuery({
    queryKey: target ? searchKeys.interactive(target) : ["interactive-search", "idle"],
    queryFn: () => apiGet<InteractiveResult>(interactivePath(target!)),
    enabled: target !== null,
    staleTime: Infinity,
    gcTime: 0,
    refetchOnWindowFocus: false,
    retry: false,
  })
}

// The grab reuses POST /queue — the pre-existing tracked manual-grab endpoint
// (queue row + history + QueueUpdated). downloadclient's /download would NOT
// track it and the release would never import.
export function useInteractiveGrab() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ release, target }: { release: ScoredRelease; target: SearchTarget }) =>
      apiPost<{ id: number }>("/queue", grabBody(release, target)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: activityKeys.queue })
      qc.invalidateQueries({ queryKey: activityKeys.history })
    },
  })
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd web && npx vitest run src/features/search/resolve.test.ts && npx tsc -b`
Expected: PASS (all resolve tests) and tsc exits 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/search/types.ts web/src/features/search/api.ts web/src/features/search/resolve.ts web/src/features/search/resolve.test.ts
git commit -m "feat(webui): interactive search types, hooks, and pure resolver

New web/src/features/search/ rather than more files in library/: the entry
points live in library, but the modal is shared across movie/season/episode
and owns its own hooks, types, and display logic. Mirrors activity/,
including the pure resolve.ts pattern.

The grab targets POST /queue, the pre-existing tracked manual-grab endpoint.
grabBody sends force for any rejected row — server-side it is only
load-bearing for quality-rejected ones, but sending it uniformly keeps one
client rule without overstating what the server enforces.

The query does not poll or refetch on focus: interactive search is an
explicit user action that hits live indexers."
```

---

### Task 7: The modal

**Files:**
- Create: `web/src/features/search/ReleaseRow.tsx`, `web/src/features/search/InteractiveSearchDialog.tsx`, `web/src/features/search/InteractiveSearchDialog.test.tsx`

**Interfaces:**
- Consumes: `Dialog`, `DialogTitle` from `@/components/ui/dialog` (Dialog takes an optional `className` for width — default `w-[32rem]`, so this passes `w-[64rem]`); `useToast` from `@/lib/toast`; `useInteractiveSearch`, `useInteractiveGrab` (T6); `rowTone`, `rejectionSummary`, `needsConfirm`, `formatSize`, `formatAge` (T6).
- Produces:
  - `ReleaseRow({ release, onGrab, grabbing })` — one `<tr>`; columns Title · Indexer · Size · Age · Seeders · Quality · Status · Grab.
  - `InteractiveSearchDialog({ target, title, onOpenChange })` — renders when `target !== null`.

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/search/InteractiveSearchDialog.test.tsx`. **Read an existing feature test first** (e.g. `web/src/features/activity/*.test.tsx`) and reuse its QueryClientProvider + ToastProvider wrapper verbatim rather than inventing one.

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { InteractiveSearchDialog } from "./InteractiveSearchDialog"
import { renderWithProviders } from "@/test/utils" // reuse the existing helper; adapt the import

const clean = {
  title: "Some.Movie.2019.480p.HDTV.x264-GOOD",
  downloadUrl: "http://x/good",
  size: 1_500_000_000,
  indexerId: "nzbgeek",
  protocol: "usenet",
  publishDate: "2026-07-15T00:00:00Z",
  quality: { id: 1, name: "SDTV", source: "hdtv", resolution: "480p", rank: 1 },
  score: 10,
  accepted: true,
  rejections: [] as string[],
}
const rejected = {
  ...clean,
  title: "Some.Movie.2019.1080p.WEB-DL.x264-GRP",
  downloadUrl: "http://x/rejected",
  quality: { id: 5, name: "WEBDL-1080p", source: "webdl", resolution: "1080p", rank: 5 },
  accepted: false,
  rejections: ["quality not in profile"],
}

function mockSearch(body: unknown, status = 200) {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
    if (String(url).includes("/interactive")) {
      return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
    }
    return new Response(JSON.stringify({ id: 1 }), { status: 201, headers: { "Content-Type": "application/json" } })
  })
}

beforeEach(() => vi.restoreAllMocks())
afterEach(() => vi.restoreAllMocks())

describe("InteractiveSearchDialog", () => {
  it("renders rejected rows with their reason instead of hiding them", async () => {
    mockSearch({ releases: [clean, rejected], indexerErrors: [] })
    renderWithProviders(
      <InteractiveSearchDialog target={{ kind: "movie", id: 7 }} title="Some Movie" onOpenChange={() => {}} />,
    )
    expect(await screen.findByText(clean.title)).toBeInTheDocument()
    expect(screen.getByText(rejected.title)).toBeInTheDocument()
    expect(screen.getByText(/quality not in profile/)).toBeInTheDocument()
  })

  it("grabs a clean row on click without a confirm", async () => {
    mockSearch({ releases: [clean], indexerErrors: [] })
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true)
    renderWithProviders(
      <InteractiveSearchDialog target={{ kind: "movie", id: 7 }} title="Some Movie" onOpenChange={() => {}} />,
    )
    await screen.findByText(clean.title)
    await userEvent.click(screen.getByRole("button", { name: /grab .*GOOD/i }))

    expect(confirmSpy).not.toHaveBeenCalled()
    await waitFor(() => {
      const calls = vi.mocked(globalThis.fetch).mock.calls.map((c) => String(c[0]))
      expect(calls.some((u) => u.endsWith("/queue"))).toBe(true)
    })
  })

  it("confirms before grabbing a rejected row, and does not grab when declined", async () => {
    mockSearch({ releases: [rejected], indexerErrors: [] })
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false)
    renderWithProviders(
      <InteractiveSearchDialog target={{ kind: "movie", id: 7 }} title="Some Movie" onOpenChange={() => {}} />,
    )
    await screen.findByText(rejected.title)
    await userEvent.click(screen.getByRole("button", { name: /grab .*GRP/i }))

    expect(confirmSpy).toHaveBeenCalledWith(expect.stringContaining("quality not in profile"))
    const calls = vi.mocked(globalThis.fetch).mock.calls.map((c) => String(c[0]))
    expect(calls.some((u) => u.endsWith("/queue"))).toBe(false)
  })

  it("renders the partial-indexer banner naming the failures", async () => {
    mockSearch({ releases: [clean], indexerErrors: [{ indexerId: "nzbplanet", message: "timeout" }] })
    renderWithProviders(
      <InteractiveSearchDialog target={{ kind: "movie", id: 7 }} title="Some Movie" onOpenChange={() => {}} />,
    )
    expect(await screen.findByRole("alert")).toHaveTextContent(/nzbplanet/)
  })

  it("shows an error state when the search itself fails", async () => {
    mockSearch({ error: { code: "internal", message: "search failed" } }, 500)
    renderWithProviders(
      <InteractiveSearchDialog target={{ kind: "movie", id: 7 }} title="Some Movie" onOpenChange={() => {}} />,
    )
    expect(await screen.findByText(/couldn't search/i)).toBeInTheDocument()
  })

  it("omits the seeders cell for usenet rows", async () => {
    mockSearch({ releases: [clean], indexerErrors: [] })
    renderWithProviders(
      <InteractiveSearchDialog target={{ kind: "movie", id: 7 }} title="Some Movie" onOpenChange={() => {}} />,
    )
    await screen.findByText(clean.title)
    expect(screen.getByTestId("seeders-cell")).toHaveTextContent("—")
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run src/features/search/InteractiveSearchDialog.test.tsx`
Expected: FAIL — `Failed to resolve import "./InteractiveSearchDialog"`

- [ ] **Step 3: Write `ReleaseRow.tsx`**

Create `web/src/features/search/ReleaseRow.tsx`:

```tsx
import { rowTone, rejectionSummary, needsConfirm, formatSize, formatAge } from "./resolve"
import type { ScoredRelease } from "./types"

export function ReleaseRow({
  release, onGrab, grabbing,
}: {
  release: ScoredRelease
  onGrab: (r: ScoredRelease) => void
  grabbing: boolean
}) {
  const muted = rowTone(release) === "muted"
  const reasons = rejectionSummary(release)
  return (
    <tr className={`border-t border-[var(--color-border)] text-sm ${muted ? "opacity-60" : ""}`}>
      <td className="max-w-0 px-3 py-2">
        <div className="truncate" title={release.title}>{release.title}</div>
        {reasons ? <div className="mt-0.5 truncate text-xs text-[var(--color-warn)]" title={reasons}>{reasons}</div> : null}
      </td>
      <td className="px-3 py-2 text-xs text-[var(--color-muted)]">{release.indexerId}</td>
      <td className="px-3 py-2 text-xs">{formatSize(release.size)}</td>
      <td className="px-3 py-2 text-xs">{formatAge(release.publishDate)}</td>
      {/* seeders is ABSENT on usenet rows and present on torrents even at 0, so
          the null check is the discriminator — never the numeric value. */}
      <td data-testid="seeders-cell" className="px-3 py-2 text-xs">
        {release.seeders != null ? release.seeders : "—"}
      </td>
      <td className="px-3 py-2 text-xs">{release.quality.name}</td>
      <td className="px-3 py-2 text-xs">
        {needsConfirm(release)
          ? <span className="text-[var(--color-warn)]">Rejected</span>
          : <span className="text-[var(--color-ok)]">OK</span>}
      </td>
      <td className="px-3 py-2 text-right">
        <button
          type="button"
          aria-label={`Grab ${release.title}`}
          disabled={grabbing}
          onClick={() => onGrab(release)}
          className="rounded-md border border-[var(--color-border)] px-2 py-1 text-xs disabled:opacity-50"
        >
          Grab
        </button>
      </td>
    </tr>
  )
}
```

- [ ] **Step 4: Write `InteractiveSearchDialog.tsx`**

Create `web/src/features/search/InteractiveSearchDialog.tsx`:

```tsx
import { Dialog, DialogTitle } from "@/components/ui/dialog"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { useInteractiveSearch, useInteractiveGrab } from "./api"
import { ReleaseRow } from "./ReleaseRow"
import { needsConfirm, rejectionSummary } from "./resolve"
import type { ScoredRelease, SearchTarget } from "./types"

export function InteractiveSearchDialog({
  target, title, onOpenChange,
}: {
  target: SearchTarget | null
  title: string
  onOpenChange: (open: boolean) => void
}) {
  const { toast } = useToast()
  const q = useInteractiveSearch(target)
  const grab = useInteractiveGrab()

  function onGrab(r: ScoredRelease) {
    if (!target) return
    // Force needs friction: a rejected row costs a guaranteed-wasted download
    // (a non-covering grab downloads then fails to import by design), so a
    // misclick should not be free. Clean rows grab straight away.
    if (needsConfirm(r) && !confirm(`Rejected: ${rejectionSummary(r)}. Grab anyway?`)) return

    grab.mutate(
      { release: r, target },
      {
        onSuccess: () => {
          toast(`Grabbed ${r.title}`)
          onOpenChange(false)
        },
        onError: (err) => {
          const msg =
            err instanceof ApiError && err.code === "no_profile"
              ? "Assign a quality profile before searching"
              : err instanceof Error
                ? err.message
                : "Grab failed"
          toast(msg, { variant: "error" })
        },
      },
    )
  }

  const releases = q.data?.releases ?? []
  const indexerErrors = q.data?.indexerErrors ?? []

  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange} className="w-[64rem]">
      <DialogTitle>Interactive search — {title}</DialogTitle>

      {q.isLoading ? <p className="py-8 text-center text-sm text-[var(--color-muted)]">Searching indexers…</p> : null}

      {q.isError ? (
        <div className="py-8 text-center">
          <p className="text-sm text-[var(--color-warn)]">Couldn't search the indexers.</p>
          <button onClick={() => q.refetch()} className="mt-2 text-sm text-[var(--color-brand)]">Retry</button>
        </div>
      ) : null}

      {/* A partial list with no banner is the same invisibility this feature
          exists to remove, so name the indexers that failed. */}
      {indexerErrors.length > 0 ? (
        <div role="alert" className="mb-3 rounded-md border border-[var(--color-warn)] px-3 py-2 text-xs text-[var(--color-warn)]">
          Some indexers failed, so this list may be incomplete:{" "}
          {indexerErrors.map((e) => `${e.indexerId} (${e.message})`).join(", ")}
        </div>
      ) : null}

      {!q.isLoading && !q.isError && releases.length === 0 ? (
        <p className="py-8 text-center text-sm text-[var(--color-muted)]">No releases found.</p>
      ) : null}

      {releases.length > 0 ? (
        <div className="max-h-[60vh] overflow-auto">
          <table className="w-full table-fixed">
            <thead className="sticky top-0 bg-[var(--color-panel)] text-left text-xs text-[var(--color-muted)]">
              <tr>
                <th className="px-3 py-2 font-medium">Title</th>
                <th className="w-28 px-3 py-2 font-medium">Indexer</th>
                <th className="w-20 px-3 py-2 font-medium">Size</th>
                <th className="w-16 px-3 py-2 font-medium">Age</th>
                <th className="w-16 px-3 py-2 font-medium">Seeders</th>
                <th className="w-28 px-3 py-2 font-medium">Quality</th>
                <th className="w-20 px-3 py-2 font-medium">Status</th>
                <th className="w-20 px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {/* Server order only — the ranking IS the information, and row 1 is
                  exactly what auto-search would have grabbed. */}
              {releases.map((r) => (
                <ReleaseRow key={`${r.indexerId}:${r.downloadUrl}`} release={r} onGrab={onGrab} grabbing={grab.isPending} />
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </Dialog>
  )
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd web && npx vitest run src/features/search/ && npx tsc -b`
Expected: PASS (all dialog + resolve tests), tsc exits 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/search/ReleaseRow.tsx web/src/features/search/InteractiveSearchDialog.tsx web/src/features/search/InteractiveSearchDialog.test.tsx
git commit -m "feat(webui): interactive search modal

Shows every release the indexers returned, rejected ones included, greyed
with their reason verbatim rather than vanishing. Server order only — the
ranking is the information, and row 1 is exactly what auto-search would
have grabbed.

Force needs friction: a rejected row opens a confirm listing the reasons,
reusing the pattern already on the Blocklist tab's Remove. A rejected grab
costs a guaranteed-wasted download (a non-covering grab downloads then
fails to import by design), so a misclick should not be free.

The partial-indexer banner names the failures: a short list with no
explanation is the same invisibility this feature exists to remove."
```

---

### Task 8: Entry points and `web/dist`

Adds an "Interactive" action **alongside** the existing auto-Search buttons, which are untouched. Guarded by the **existing Wave B toast** — same wording, already implemented at `MovieDetail.tsx:57` and `SeriesDetail.tsx:55`. A profile-less item must not open a modal it could never grab from: `DecideAll` needs a profile to score against, and `Enqueue` returns `ErrNoProfile` regardless.

**Files:**
- Modify: `web/src/features/library/MovieDetail.tsx`
- Modify: `web/src/features/library/SeriesDetail.tsx:83-91`
- Modify: `web/src/features/library/SeasonTable.tsx`
- Modify: `web/src/features/library/SeasonSection.tsx:5-16,31-36`
- Modify: `web/dist/**` (rebuild)

**Interfaces:**
- Consumes: `InteractiveSearchDialog` (T7), `SearchTarget` (T6); existing `useToast`, `useQualityProfiles`.
- Produces: `SeasonSection` gains a required `onInteractive: () => void` prop; `SeasonTable` gains required `onInteractiveSeason: (seasonNumber: number) => void` and `onInteractiveEpisode: (e: Episode) => void` props.

- [ ] **Step 1: Add the prop to `SeasonSection`**

In `web/src/features/library/SeasonSection.tsx`, replace the signature (lines 5-16) and the button group (lines 31-36):

```tsx
export function SeasonSection({
  title, withFile, total, monitored, defaultOpen, onToggleMonitor, onSearch, onInteractive, children,
}: {
  title: string
  withFile: number
  total: number
  monitored: boolean
  defaultOpen: boolean
  onToggleMonitor: () => void
  onSearch: () => void
  onInteractive: () => void
  children: React.ReactNode
}) {
```

```tsx
        <div className="flex items-center gap-2">
          <button onClick={onSearch} className="text-xs text-[var(--color-brand)]">Search season</button>
          <button onClick={onInteractive} className="text-xs text-[var(--color-brand)]">Interactive</button>
          <label className="flex items-center gap-1 text-xs text-[var(--color-muted)]">
            <input type="checkbox" checked={monitored} onChange={onToggleMonitor} /> monitor
          </label>
        </div>
```

- [ ] **Step 2: Thread the props through `SeasonTable`**

In `web/src/features/library/SeasonTable.tsx`, replace the signature (lines 6-16):

```tsx
export function SeasonTable({
  seasons, episodes, onToggleSeason, onToggleEpisode, onSearchSeason, onSearchEpisode,
  onInteractiveSeason, onInteractiveEpisode,
}: {
  seasons: Season[]
  episodes: Episode[]
  seriesId: number
  onToggleSeason: (s: Season) => void
  onToggleEpisode: (e: Episode) => void
  onSearchSeason: (seasonNumber: number) => void
  onSearchEpisode: (e: Episode) => void
  onInteractiveSeason: (seasonNumber: number) => void
  onInteractiveEpisode: (e: Episode) => void
}) {
```

Pass it to `SeasonSection` (after the existing `onSearch` prop at line 32):

```tsx
            onSearch={() => onSearchSeason(sec.seasonNumber)}
            onInteractive={() => onInteractiveSeason(sec.seasonNumber)}
```

And add the episode button, immediately after the existing `Search episode` button (line 40):

```tsx
                <button aria-label={`Search episode ${e.episodeNumber}`} onClick={() => onSearchEpisode(e)} className="text-xs text-[var(--color-brand)]">Search episode</button>
                <button aria-label={`Interactive search episode ${e.episodeNumber}`} onClick={() => onInteractiveEpisode(e)} className="text-xs text-[var(--color-brand)]">Interactive</button>
```

- [ ] **Step 3: Wire `MovieDetail`**

In `web/src/features/library/MovieDetail.tsx`: add the imports

```tsx
import { useState } from "react"
import { InteractiveSearchDialog } from "@/features/search/InteractiveSearchDialog"
import type { SearchTarget } from "@/features/search/types"
```

add the state inside the component, next to the other hooks (after `const search = useSearch()`):

```tsx
  const [searchTarget, setSearchTarget] = useState<SearchTarget | null>(null)
```

add the button immediately after the existing `Search` button (which closes at line 63):

```tsx
          <button
            onClick={() => {
              // DecideAll needs a profile to score against and Enqueue would
              // reject the grab anyway, so a profile-less item must not open a
              // modal it could never grab from. Same guard, same wording as the
              // auto-Search button above.
              if (!m.qualityProfileId) { toast("Assign a quality profile before searching", { variant: "error" }); return }
              setSearchTarget({ kind: "movie", id })
            }}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            Interactive
          </button>
```

and render the dialog just before the component's closing `</div>` (after `</DetailBanner>`, line 92):

```tsx
      <InteractiveSearchDialog
        target={searchTarget}
        title={m.title}
        onOpenChange={(open) => { if (!open) setSearchTarget(null) }}
      />
```

- [ ] **Step 4: Wire `SeriesDetail`**

In `web/src/features/library/SeriesDetail.tsx`: add the same three imports as Step 3, add the state next to the other hooks (after `const search = useSearch()`, line 21):

```tsx
  const [searchTarget, setSearchTarget] = useState<SearchTarget | null>(null)
  const [searchTitle, setSearchTitle] = useState("")
```

then extend the `SeasonTable` element (lines 83-91) with the two new props:

```tsx
        onInteractiveSeason={(seasonNumber) => {
          if (!s.qualityProfileId) { toast("Assign a quality profile before searching", { variant: "error" }); return }
          setSearchTitle(`${s.title} — season ${seasonNumber}`)
          // episodeIds attribute the grab: a season queue row with no episode ids
          // can never import. Send every monitored episode without a file, the
          // same set searchSeason enqueues a pack against.
          setSearchTarget({
            kind: "season",
            seriesId: id,
            seasonNumber,
            episodeIds: s.episodes.filter((e) => e.seasonNumber === seasonNumber && e.monitored && !e.hasFile).map((e) => e.id),
          })
        }}
        onInteractiveEpisode={(e) => {
          if (!s.qualityProfileId) { toast("Assign a quality profile before searching", { variant: "error" }); return }
          setSearchTitle(e.title)
          setSearchTarget({ kind: "episode", id: e.id, seriesId: id })
        }}
```

and render the dialog just before the component's closing `</div>` (after the `<SeasonTable ... />` element):

```tsx
      <InteractiveSearchDialog
        target={searchTarget}
        title={searchTitle}
        onOpenChange={(open) => { if (!open) setSearchTarget(null) }}
      />
```

> Verify `s.episodes` items expose `seasonNumber`, `monitored`, and `hasFile` — `web/src/features/library/types.ts` is the authority (`SeasonTable.tsx:35-43` already reads `episodeNumber`, `title`, `airDate`, `hasFile`, `monitored`). Adapt the filter to the real field names.

- [ ] **Step 5: Run the full frontend suite**

Run: `cd web && npx vitest run && npx tsc -b`
Expected: PASS — every existing test plus the new ones (the suite was 182/182 before C3). If any existing `SeasonTable`/`SeasonSection` test fails to compile, it is missing the new required props: add them to those tests.

- [ ] **Step 6: Rebuild `web/dist`**

`web/dist` is committed and CI runs a drift check, so a stale dist fails the merge.

Run: `cd web && npm run build`
Expected: build succeeds; `git status web/dist` shows modified assets.

- [ ] **Step 7: Verify the whole repo is green**

Run: `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./... && cd web && npx vitest run && npx tsc -b`
Expected: 20 Go packages PASS, frontend suite PASS, tsc 0.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/library/ web/dist
git commit -m "feat(webui): interactive search entry points on movie, season, episode

An Interactive action alongside the existing auto-Search buttons, which are
untouched. Guarded by the existing Wave B profile toast, same wording: a
profile-less item must not open a modal it could never grab from, since
DecideAll needs a profile to score against and Enqueue would reject the
grab regardless.

The season target carries the ids of every monitored episode without a
file — the same set searchSeason enqueues a pack against. A season queue
row with no episode ids could never import.

Rebuilds web/dist (committed; CI drift-checks it)."
```

---

## Self-Review

**Spec coverage:**

| Spec § | Requirement | Task |
|--------|-------------|------|
| §2 | Library items only; no free-text; no series-level; no client sorting | T5 (3 endpoints only), T7 (server order only), T8 (3 entry points) |
| §2 | Automation untouched | Global Constraints; T3 Step 6, T4 Step 5 verify |
| §4 | `Resolve` never fails → QualityID 0 | T4 Step 1 (`TestEnqueueForceUnparseableGetsQualityIDZero`) |
| §5.1 | `ScoredCandidate` | T3 |
| §5.2 | Uniform `Rejections`, all three sources | T3 (Gap 2) |
| §5.3 | Accepted-first sort + the present-but-not-allowed trap | T3 Steps 1, 5 (mutation-checked) |
| §5.3 | "Row 1 == what automation would grab" | T3 Step 1 (`TestDecideAllRowOneMatchesWhatAutomationWouldGrab`, all 3 filters) |
| §5.4 | 3 synchronous GETs; grab reuses POST /queue | T5 |
| §5.5 | Response shape; `rejections` `[]`; `seeders` absent on usenet; `quality` always present | T5 Steps 3, 6 (mutation-checked) |
| §5.5/§7 | `indexerErrors` load-bearing + banner naming them | **T2 (Gap 1)**, T5, T7 |
| §5.6 | `force` on 3 structs + 1 conditional; additive | T4 |
| §5.7 | Force does not clear the blocklist | T4 (no such code); §8's "no test asserts force affects the blocklist" honoured |
| §6.1 | `features/search/` w/ pure resolve.ts | T6 |
| §6.2 | Entry points movie/season/episode | T8 |
| §6.3 | Columns | T7 (`ReleaseRow`) |
| §6.4 | Force needs friction (confirm) | T7 Steps 1, 4 |
| §6.5 | No profile → no modal, existing toast | T8 Steps 3, 4 |
| §7 | Error handling table | T5 (`writeInteractive`), T7 (error state, retry, toasts) |
| §8 | Go, wire-shape, FE tests | T3, T4, T5, T6, T7 |

**Placeholder scan:** Three deliberate `panic("implement using the existing fixtures…")` markers exist (T4 Step 1, T5 Step 1). They are **not** TBDs — they mark the one thing the plan must not guess: test fixtures whose exact helper names live in files the implementer will have open (`enqueue_test.go`, `api_test.go`). Each carries an explicit instruction to reuse the existing helpers rather than invent parallel ones, and the surrounding test bodies are complete. Two `>` callouts (T3 Step 1, T8 Step 4) instruct verification of `quality.Definitions()`/definition names and the `Episode` field names against source before implementing, with the authoritative file named.

**Type consistency:** `IndexerError{IndexerID, Message}` — T2 defines, T5 embeds in `InteractiveResult`, T6 mirrors as `IndexerErrorEntry`, T7 reads `e.indexerId`/`e.message`. ✓ `ScoredCandidate{Candidate, Decision, Rejections}` — T3 defines, T5's `toScoredReleases` reads `c.Release.*`, `c.Decision.Quality/Score/Accepted`, `c.Rejections`. ✓ `Coverage`/`SeasonPackCoverage`/`EpisodeCoverage` — T3 defines, T5 consumes. ✓ `blocked map[string]string` — T1 produces, T3 consumes, T5 passes. ✓ `Force` — T4 on both `EnqueueRequest` (Go) and `enqueueBody` (`json:"force"`); T6's `GrabRequest.force`. ✓ `SearchTarget` — T6 defines with the optional `episodeIds`/`seriesId` that T8 supplies and `grabBody` reads. ✓ `activityKeys` imported from `@/features/activity/api` (T6) matches its real export at `activity/api.ts:9`. ✓

**One known asymmetry, deliberate:** `automation.ErrNoProfile` (T5) is a *new* sentinel distinct from the existing `importing.ErrNoProfile`. `automation.profileFor` signals "no profile" via `ok=false`, not an error, so automation has no such sentinel today. Both map to the same 400 `no_profile` code and the same frontend toast, so the user-visible contract is single. Do not try to share the importing one — automation importing it would be legal, but the two mean different things (`importing`'s is returned by a grab; this one by a read).

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-17-nexus-interactive-search.md`.
