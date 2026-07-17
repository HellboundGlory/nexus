# Add Defaults Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user set a per-kind default root folder and default quality profile in Settings, pre-selected and overridable in the Add dialog, with the profile threaded through `AddMovie`/`AddSeries` so an added item is born with its profile.

**Architecture:** Purely additive. Four ids stored in the existing generic `settings` key-value table (no schema change, no root-folder "kind" column). A typed `GET`/`PUT /api/v1/config/media-defaults` on the media API whose GET validates each stored id against the live folders/profiles and returns `null` for a since-deleted one. `AddMovie`/`AddSeries` gain an optional `qualityProfileId`. Frontend: a new "Media Management" settings tab and a quality-profile dropdown in the Add dialog, both pre-seeded from the defaults.

**Tech Stack:** Go 1.26 (chi router, `database/sql` + SQLite), React 19 + TypeScript + TanStack Query + Radix UI + Tailwind, vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-07-17-nexus-add-defaults-design.md`

## Global Constraints

- **Purely additive.** `qualityProfileId` on the add path defaults to `nil`/absent; every existing add caller and test that omits it must create a profile-less item exactly as before. The separate `PUT /{id}/qualityprofile` assign endpoint is untouched.
- **Store the id, never the path/name.** A renamed folder/profile keeps working.
- **GET media-defaults never returns a dangling id.** Each stored id is validated against the live set; a since-deleted folder/profile → that field returns JSON `null`. `0` is never a valid id and must be distinguishable from `null` on the wire.
- **PUT media-defaults is all-or-nothing.** Validate every non-null id first; on any unknown id return `400` and write nothing.
- **`/config/media-defaults` must be a distinct static route** so it does not collide with importing's `/config/naming` on the shared `/api/v1` router.
- **This DB enforces foreign keys** (`_pragma=foreign_keys(ON)`). Go tests that seed rows must create parent rows first (e.g. a quality profile via `store.CreateQualityProfile`, a root folder via `store.CreateRootFolder`) before referencing them.
- **Ignore `gofmt -l` output** — it flags the whole repo on this CRLF Windows checkout. Keep your own new code gofmt-clean (watch inline comment alignment).
- **`web/dist` is committed and CI drift-checks it.** Every frontend task that changes app-bundled code rebuilds it (`cd web && npm run build`) and commits the result.
- Every task leaves the repo green: `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`, and for frontend tasks `cd web && npx vitest run && npx tsc -b`.

## Verified facts (established against source — trust these)

- `store.GetSetting(ctx, key) (value string, found bool, err error)` and `store.SetSetting(ctx, key, value string) error` — `internal/core/store/store.go:41,53`.
- `store.GetRootFolder(ctx, id) (*store.RootFolder, error)` and `store.GetQualityProfile(ctx, id) (store.QualityProfile, error)` both return `store.ErrNotFound` on a missing id.
- `store.CreateSeries(ctx, store.Series) (int64, error)` and `store.CreateMovie(ctx, store.Movie) (int64, error)`; both structs already have a `QualityProfileID *int64` field that the INSERT persists — it is simply never set at create time today.
- `media.Service.validateRootFolder(ctx, id *int64) error` (`media.go:111`) returns `ErrInvalidRootFolder` when `GetRootFolder` → `ErrNotFound`, `nil` when `id == nil`. The new `validateQualityProfile` mirrors it exactly.
- `writeMediaError` (`api.go:69`) already maps `ErrInvalidRootFolder` → `400 bad_request`.
- Settings tabs live in `web/src/features/settings/SettingsLayout.tsx` `TABS`; settings sub-routes in `web/src/app/routes.tsx:52-60`.
- The naming config GET/PUT hook pair in `web/src/features/settings/configApi.ts` (`useNamingConfig`/`useSaveNaming`) is the exact pattern for the media-defaults hooks; `NamingSection.tsx` is the form pattern.
- `configApi.useRootFolders()` (settings) → `RootFolder[]`; `qualityApi.useQualityProfiles()` (settings) → `QualityProfile[]`; library `api.ts` exports `useRootFolders`, `useQualityProfiles`, `useAddMovie`, `useAddSeries`.

## File Structure

**Backend — create:** `internal/media/defaults.go`, `internal/media/defaults_test.go`.
**Backend — modify:** `internal/media/errors.go` (new sentinel), `internal/media/api.go` (bodies, handlers, routes, error map), `internal/media/media.go` (request structs, `validateQualityProfile`, thread `QualityProfileID`), and the relevant `internal/media/*_test.go`.

**Frontend — create:** `web/src/features/settings/mediaDefaultsTypes.ts`, `web/src/features/settings/mediaDefaultsApi.ts`, `web/src/features/settings/MediaManagementSection.tsx`, `web/src/features/settings/MediaManagementSection.test.tsx`.
**Frontend — modify:** `web/src/features/settings/SettingsLayout.tsx` (TABS), `web/src/app/routes.tsx` (route), `web/src/features/library/types.ts` (add-body fields), `web/src/features/library/api.ts` (nothing — bodies flow through), `web/src/features/library/AddMediaDialog.tsx` (+ its test), `web/dist`.

## Task Order & Dependencies

```
T1 (profile-through-add, BE) ─┐
T2 (media-defaults config, BE) ┼─→ T3 (settings tab, FE) ─┐
                               └───────────────────────────┼─→ T4 (add dialog + dist, FE)
```

T1 and T2 are independent. T3 needs T2's endpoint. T4 needs T1's add-body field and T2's endpoint.

---

### Task 1: Thread `qualityProfileId` through Add

**Files:**
- Modify: `internal/media/errors.go`, `internal/media/media.go` (request structs ~36-46, add `validateQualityProfile`, set the field in `AddSeries`/`AddMovie`), `internal/media/api.go` (`addSeriesBody`/`addMovieBody`, handlers, `writeMediaError`)
- Test: `internal/media/service_test.go` (or `api_test.go` — use whichever already has the add tests)

**Interfaces:**
- Consumes: `store.GetQualityProfile`, `store.ErrNotFound`, existing `validateRootFolder`.
- Produces:
  - `var ErrInvalidQualityProfile = errors.New("media: invalid quality profile")`
  - `AddSeriesRequest.QualityProfileID *int64`, `AddMovieRequest.QualityProfileID *int64`
  - `func (s *Service) validateQualityProfile(ctx context.Context, id *int64) error`
  - `addSeriesBody`/`addMovieBody` gain `QualityProfileID *int64 \`json:"qualityProfileId"\``

- [ ] **Step 1: Write the failing tests**

Read the existing add tests first (`TestAddMovie` in `internal/media/service_test.go:107`, `TestAddSeriesInvalidRootFolder:98`) and reuse their exact fixture helpers (store setup, fake metadata provider, root-folder + quality-profile creation). Append tests mirroring that style:

```go
func TestAddMovieWithQualityProfile(t *testing.T) {
	// Build on the existing TestAddMovie fixture: a Service with a fake meta
	// provider, a created root folder, and a created quality profile.
	svc, st := newMediaTestService(t) // use the real helper name from this file
	ctx := context.Background()
	rootID := mustRootFolder(t, st)              // real helper / inline CreateRootFolder
	profID := mustQualityProfile(t, st)          // real helper / inline CreateQualityProfile

	m, err := svc.AddMovie(ctx, AddMovieRequest{
		TMDBID: 550, RootFolderID: &rootID, Monitored: true, QualityProfileID: &profID,
	})
	if err != nil {
		t.Fatalf("AddMovie: %v", err)
	}
	if m.QualityProfileID == nil || *m.QualityProfileID != profID {
		t.Fatalf("qualityProfileId = %v, want %d", m.QualityProfileID, profID)
	}
}

func TestAddMovieUnknownQualityProfileRejected(t *testing.T) {
	svc, st := newMediaTestService(t)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)
	bogus := int64(99999)

	_, err := svc.AddMovie(ctx, AddMovieRequest{
		TMDBID: 550, RootFolderID: &rootID, Monitored: true, QualityProfileID: &bogus,
	})
	if !errors.Is(err, ErrInvalidQualityProfile) {
		t.Fatalf("err = %v, want ErrInvalidQualityProfile", err)
	}
}

func TestAddMovieWithoutQualityProfileStaysNil(t *testing.T) {
	svc, st := newMediaTestService(t)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)

	m, err := svc.AddMovie(ctx, AddMovieRequest{
		TMDBID: 550, RootFolderID: &rootID, Monitored: true, // QualityProfileID omitted
	})
	if err != nil {
		t.Fatalf("AddMovie: %v", err)
	}
	if m.QualityProfileID != nil {
		t.Fatalf("qualityProfileId = %v, want nil (additive guarantee)", m.QualityProfileID)
	}
}
```

> Replace `newMediaTestService`/`mustRootFolder`/`mustQualityProfile` with the real helpers already in the test file. If the file constructs the service inline, copy that construction. Do **not** invent a parallel fixture. If the fake meta provider needs `TMDBID: 550` to resolve, use whatever tmdb id the existing `TestAddMovie` uses.

- [ ] **Step 2: Run to verify they fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && go test ./internal/media/ -run TestAddMovie -v`
Expected: FAIL — `unknown field QualityProfileID in struct literal of type AddMovieRequest`

- [ ] **Step 3: Implement**

`internal/media/errors.go` — add the sentinel:

```go
	ErrInvalidRootFolder     = errors.New("media: invalid root folder")
	ErrInvalidQualityProfile = errors.New("media: invalid quality profile")
```

`internal/media/media.go` — add the field to both request structs:

```go
type AddSeriesRequest struct {
	TMDBID           int
	RootFolderID     *int64
	MonitorOption    string
	QualityProfileID *int64
}

type AddMovieRequest struct {
	TMDBID           int
	RootFolderID     *int64
	Monitored        bool
	QualityProfileID *int64
}
```

Add `validateQualityProfile` beside `validateRootFolder`:

```go
// validateQualityProfile mirrors validateRootFolder: nil is allowed (no profile),
// an unknown id surfaces ErrInvalidQualityProfile (→400).
func (s *Service) validateQualityProfile(ctx context.Context, id *int64) error {
	if id == nil {
		return nil
	}
	if _, err := s.store.GetQualityProfile(ctx, *id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidQualityProfile
		}
		return err
	}
	return nil
}
```

In `AddSeries`, after the existing `validateRootFolder` guard, add the profile guard and set the field on the create struct:

```go
	if err := s.validateRootFolder(ctx, req.RootFolderID); err != nil {
		return store.Series{}, err
	}
	if err := s.validateQualityProfile(ctx, req.QualityProfileID); err != nil {
		return store.Series{}, err
	}
```

and add `QualityProfileID: req.QualityProfileID,` to the `store.CreateSeries(ctx, store.Series{...})` literal.

Do the same in `AddMovie`: the `validateQualityProfile` guard after `validateRootFolder`, and `QualityProfileID: req.QualityProfileID,` on the `store.CreateMovie(ctx, store.Movie{...})` literal.

`internal/media/api.go` — add the field to both bodies and pass it through:

```go
type addSeriesBody struct {
	TMDBID           int    `json:"tmdbId"`
	RootFolderID     *int64 `json:"rootFolderId"`
	MonitorOption    string `json:"monitorOption"`
	QualityProfileID *int64 `json:"qualityProfileId"`
}
```
```go
type addMovieBody struct {
	TMDBID           int    `json:"tmdbId"`
	RootFolderID     *int64 `json:"rootFolderId"`
	Monitored        bool   `json:"monitored"`
	QualityProfileID *int64 `json:"qualityProfileId"`
}
```

In `addSeries`, pass it: `AddSeriesRequest{TMDBID: b.TMDBID, RootFolderID: b.RootFolderID, MonitorOption: b.MonitorOption, QualityProfileID: b.QualityProfileID}`. In `addMovie`: `AddMovieRequest{TMDBID: b.TMDBID, RootFolderID: b.RootFolderID, Monitored: b.Monitored, QualityProfileID: b.QualityProfileID}`.

In `writeMediaError`, add a case beside `ErrInvalidRootFolder`:

```go
	case errors.Is(err, ErrInvalidQualityProfile):
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/media/ -v`
Expected: PASS — the three new tests plus every existing add test unchanged.

- [ ] **Step 5: Full suite**

Run: `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`
Expected: PASS (all packages).

- [ ] **Step 6: Commit**

```bash
git add internal/media/
git commit -m "feat(media): thread qualityProfileId through AddMovie/AddSeries

AddMovie/AddSeries set the root folder on create but never the quality
profile, so an added item is profile-less until a separate PUT assigns one
(the Wave B guard-toast exists for exactly this gap). Add an optional
QualityProfileID to both requests, validated like the root folder is, set
on the create row.

Purely additive: the field is a pointer defaulting to nil, so every existing
caller creates a profile-less item exactly as before. The separate
/{id}/qualityprofile assign path is untouched."
```

---

### Task 2: `media-defaults` config endpoint

**Files:**
- Create: `internal/media/defaults.go`, `internal/media/defaults_test.go`
- Modify: `internal/media/api.go` (routes in `Mount`, two handlers)

**Interfaces:**
- Consumes: `store.GetSetting`/`SetSetting`, `store.GetRootFolder`/`GetQualityProfile`, `store.ErrNotFound`; `validateRootFolder`, `validateQualityProfile` (T1).
- Produces:
  - `type KindDefaults struct { RootFolderID *int64 \`json:"rootFolderId"\`; QualityProfileID *int64 \`json:"qualityProfileId"\` }`
  - `type MediaDefaults struct { Movie KindDefaults \`json:"movie"\`; TV KindDefaults \`json:"tv"\` }`
  - `func (s *Service) GetMediaDefaults(ctx context.Context) (MediaDefaults, error)`
  - `func (s *Service) SetMediaDefaults(ctx context.Context, d MediaDefaults) error`
  - Routes: `GET /config/media-defaults`, `PUT /config/media-defaults`.

- [ ] **Step 1: Write the failing tests**

Create `internal/media/defaults_test.go`. Reuse the same service/store fixture the other media tests use (read `defaults`-adjacent tests for the helper name; the store is a real migrated SQLite temp DB).

```go
package media

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hellboundg/nexus/internal/core/store"
)

func TestMediaDefaultsRoundTrip(t *testing.T) {
	svc, st := newMediaTestService(t)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)
	profID := mustQualityProfile(t, st)

	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID, QualityProfileID: &profID},
		TV:    KindDefaults{RootFolderID: nil, QualityProfileID: &profID},
	}); err != nil {
		t.Fatalf("SetMediaDefaults: %v", err)
	}

	got, err := svc.GetMediaDefaults(ctx)
	if err != nil {
		t.Fatalf("GetMediaDefaults: %v", err)
	}
	if got.Movie.RootFolderID == nil || *got.Movie.RootFolderID != rootID {
		t.Fatalf("movie root = %v, want %d", got.Movie.RootFolderID, rootID)
	}
	if got.TV.RootFolderID != nil {
		t.Fatalf("tv root = %v, want nil", got.TV.RootFolderID)
	}
	if got.TV.QualityProfileID == nil || *got.TV.QualityProfileID != profID {
		t.Fatalf("tv profile = %v, want %d", got.TV.QualityProfileID, profID)
	}
}

func TestMediaDefaultsGetNullsStaleReference(t *testing.T) {
	svc, st := newMediaTestService(t)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)

	// set the movie root default, then delete the folder out from under it
	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteRootFolder(ctx, rootID); err != nil {
		t.Fatal(err)
	}

	got, err := svc.GetMediaDefaults(ctx)
	if err != nil {
		t.Fatalf("GetMediaDefaults: %v", err)
	}
	if got.Movie.RootFolderID != nil {
		t.Fatalf("stale movie root should be nulled, got %v", got.Movie.RootFolderID)
	}
}

func TestMediaDefaultsGetKeepsValidReference(t *testing.T) {
	svc, st := newMediaTestService(t)
	ctx := context.Background()
	profID := mustQualityProfile(t, st)

	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{QualityProfileID: &profID},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetMediaDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// regression guard: a VALID id must not be nulled
	if got.Movie.QualityProfileID == nil || *got.Movie.QualityProfileID != profID {
		t.Fatalf("valid profile default was dropped: %v", got.Movie.QualityProfileID)
	}
}

func TestSetMediaDefaultsUnknownProfileRejectedAndUnchanged(t *testing.T) {
	svc, st := newMediaTestService(t)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)
	profID := mustQualityProfile(t, st)

	// establish a good baseline
	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID, QualityProfileID: &profID},
	}); err != nil {
		t.Fatal(err)
	}

	bogus := int64(99999)
	err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID, QualityProfileID: &bogus},
	})
	if err == nil {
		t.Fatal("want an error for an unknown profile id")
	}
	// all-or-nothing: the baseline must be intact
	got, _ := svc.GetMediaDefaults(ctx)
	if got.Movie.QualityProfileID == nil || *got.Movie.QualityProfileID != profID {
		t.Fatalf("failed PUT must not overwrite; profile = %v", got.Movie.QualityProfileID)
	}
}

// Wire shape: an unset field is JSON null (not 0, not absent). 0 is never a
// valid id, so a typed round-trip could not tell "unset" from "id 0" — assert on
// the raw JSON.
func TestMediaDefaultsWireShapeNullNotZero(t *testing.T) {
	svc, st := newMediaTestService(t)
	ctx := context.Background()
	rootID := mustRootFolder(t, st)
	if err := svc.SetMediaDefaults(ctx, MediaDefaults{
		Movie: KindDefaults{RootFolderID: &rootID}, // tv left entirely unset
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetMediaDefaults(ctx)
	b, _ := json.Marshal(got)

	var raw struct {
		TV struct {
			RootFolderID     json.RawMessage `json:"rootFolderId"`
			QualityProfileID json.RawMessage `json:"qualityProfileId"`
		} `json:"tv"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw.TV.RootFolderID) != "null" {
		t.Errorf("tv rootFolderId = %s, want null", raw.TV.RootFolderID)
	}
	if string(raw.TV.QualityProfileID) != "null" {
		t.Errorf("tv qualityProfileId = %s, want null", raw.TV.QualityProfileID)
	}
}

var _ = store.ErrNotFound // keep the import if unused above
```

> Use the real fixture helpers from the media test files. `st.DeleteRootFolder` exists (`internal/core/store/media_store.go`). If `mustRootFolder`/`mustQualityProfile` don't already exist, add tiny local helpers that call `store.CreateRootFolder(ctx, "/tmp/x")` and `store.CreateQualityProfile(ctx, store.QualityProfile{Name:"p", Items:[]store.QualityProfileItem{{QualityID:7,Allowed:true}}, CutoffQualityID:7})` and return the ids — parent rows must exist because FKs are on.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/media/ -run TestMediaDefaults -v`
Expected: FAIL — `undefined: MediaDefaults`

- [ ] **Step 3: Implement `defaults.go`**

Create `internal/media/defaults.go`:

```go
package media

import (
	"context"
	"errors"
	"strconv"

	"github.com/hellboundg/nexus/internal/core/store"
)

const (
	keyDefaultMovieRoot    = "defaults.movie.rootFolderId"
	keyDefaultMovieProfile = "defaults.movie.qualityProfileId"
	keyDefaultTVRoot       = "defaults.tv.rootFolderId"
	keyDefaultTVProfile    = "defaults.tv.qualityProfileId"
)

// KindDefaults is the add-time default root folder and quality profile for one
// media kind. Each is nil when unset (JSON null on the wire).
type KindDefaults struct {
	RootFolderID     *int64 `json:"rootFolderId"`
	QualityProfileID *int64 `json:"qualityProfileId"`
}

// MediaDefaults is the per-kind add defaults, stored as four ids in the generic
// settings table.
type MediaDefaults struct {
	Movie KindDefaults `json:"movie"`
	TV    KindDefaults `json:"tv"`
}

// GetMediaDefaults reads the four stored ids and validates each against the live
// set. A stored id whose folder/profile has since been deleted is returned as nil
// (never a dangling id — a deleted default must not pre-select a phantom option).
func (s *Service) GetMediaDefaults(ctx context.Context) (MediaDefaults, error) {
	movieRoot, err := s.resolveDefault(ctx, keyDefaultMovieRoot, s.rootFolderExists)
	if err != nil {
		return MediaDefaults{}, err
	}
	movieProfile, err := s.resolveDefault(ctx, keyDefaultMovieProfile, s.qualityProfileExists)
	if err != nil {
		return MediaDefaults{}, err
	}
	tvRoot, err := s.resolveDefault(ctx, keyDefaultTVRoot, s.rootFolderExists)
	if err != nil {
		return MediaDefaults{}, err
	}
	tvProfile, err := s.resolveDefault(ctx, keyDefaultTVProfile, s.qualityProfileExists)
	if err != nil {
		return MediaDefaults{}, err
	}
	return MediaDefaults{
		Movie: KindDefaults{RootFolderID: movieRoot, QualityProfileID: movieProfile},
		TV:    KindDefaults{RootFolderID: tvRoot, QualityProfileID: tvProfile},
	}, nil
}

// resolveDefault reads one setting key and returns the stored id only if it
// parses AND still exists. Missing/empty/unparseable/deleted → nil (no default).
// A real store error (not "not found") propagates.
func (s *Service) resolveDefault(ctx context.Context, key string, exists func(context.Context, int64) (bool, error)) (*int64, error) {
	raw, found, err := s.store.GetSetting(ctx, key)
	if err != nil {
		return nil, err
	}
	if !found || raw == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, nil // corrupt value → treat as unset
	}
	ok, err := exists(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func (s *Service) rootFolderExists(ctx context.Context, id int64) (bool, error) {
	_, err := s.store.GetRootFolder(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) qualityProfileExists(ctx context.Context, id int64) (bool, error) {
	_, err := s.store.GetQualityProfile(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// SetMediaDefaults validates every non-nil id first, then writes all four keys.
// Validation-before-write makes it all-or-nothing: an unknown id fails the whole
// PUT and mutates nothing. A nil id is stored as "" (read back as unset).
func (s *Service) SetMediaDefaults(ctx context.Context, d MediaDefaults) error {
	if err := s.validateRootFolder(ctx, d.Movie.RootFolderID); err != nil {
		return err
	}
	if err := s.validateQualityProfile(ctx, d.Movie.QualityProfileID); err != nil {
		return err
	}
	if err := s.validateRootFolder(ctx, d.TV.RootFolderID); err != nil {
		return err
	}
	if err := s.validateQualityProfile(ctx, d.TV.QualityProfileID); err != nil {
		return err
	}
	for _, kv := range []struct {
		key string
		id  *int64
	}{
		{keyDefaultMovieRoot, d.Movie.RootFolderID},
		{keyDefaultMovieProfile, d.Movie.QualityProfileID},
		{keyDefaultTVRoot, d.TV.RootFolderID},
		{keyDefaultTVProfile, d.TV.QualityProfileID},
	} {
		v := ""
		if kv.id != nil {
			v = strconv.FormatInt(*kv.id, 10)
		}
		if err := s.store.SetSetting(ctx, kv.key, v); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/media/ -run TestMediaDefaults -v && go test ./internal/media/ -run TestSetMediaDefaults -v`
Expected: PASS — all five.

- [ ] **Step 5: Prove the stale-null guard and the wire-shape guard are live (mutation checks)**

1. In `resolveDefault`, temporarily change the `if !ok { return nil, nil }` to `if !ok { return &id, nil }` → run `TestMediaDefaultsGetNullsStaleReference` → must FAIL (`stale movie root should be nulled`). Restore → PASS.
2. Change `KindDefaults.RootFolderID`'s tag experiment is unnecessary; instead confirm the wire test bites by temporarily making `SetMediaDefaults` write `"0"` for a nil id (`v := "0"` when `kv.id == nil`) → `TestMediaDefaultsWireShapeNullNotZero` must FAIL (tv rootFolderId = 0, want null). Restore → PASS.

If either passes while mutated, the test is inert — fix it before continuing.

- [ ] **Step 6: Add the routes and handlers**

In `internal/media/api.go` `Mount`, add two routes (distinct static path — must not collide with importing's `/config/naming`):

```go
	r.Get("/config/media-defaults", a.getMediaDefaults)
	r.Put("/config/media-defaults", a.putMediaDefaults)
```

Append the handlers:

```go
func (a *API) getMediaDefaults(w http.ResponseWriter, r *http.Request) {
	d, err := a.svc.GetMediaDefaults(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load media defaults")
		return
	}
	api.WriteJSON(w, http.StatusOK, d)
}

func (a *API) putMediaDefaults(w http.ResponseWriter, r *http.Request) {
	var d MediaDefaults
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.svc.SetMediaDefaults(r.Context(), d); err != nil {
		writeMediaError(w, err) // ErrInvalidRootFolder / ErrInvalidQualityProfile → 400
		return
	}
	api.WriteJSON(w, http.StatusOK, d)
}
```

- [ ] **Step 7: Endpoint test**

Append to `internal/media/defaults_test.go` an HTTP-level test that mounts the API and exercises `PUT` with an unknown id → `400`, then `GET` → `200`. Model it on the existing media `api_test.go` router fixture (read it for the mount helper). Assert the `PUT` 400 body's `error.code == "bad_request"`.

- [ ] **Step 8: Full suite**

Run: `CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/media/
git commit -m "feat(media): media-defaults config endpoint

GET/PUT /config/media-defaults stores the per-kind default root folder and
quality profile as four ids in the settings table. GET validates each stored
id against the live folders/profiles and returns null for a since-deleted
one, so a stale default can never pre-select a phantom option in the Add
dialog. PUT validates every non-null id before writing anything, so a bad id
fails the whole request and mutates nothing.

Distinct static route so it does not collide with importing's /config/naming
on the shared /api/v1 router."
```

---

### Task 3: "Media Management" settings tab

**Files:**
- Create: `web/src/features/settings/mediaDefaultsTypes.ts`, `web/src/features/settings/mediaDefaultsApi.ts`, `web/src/features/settings/MediaManagementSection.tsx`, `web/src/features/settings/MediaManagementSection.test.tsx`
- Modify: `web/src/features/settings/SettingsLayout.tsx` (TABS), `web/src/app/routes.tsx` (route), `web/dist`

**Interfaces:**
- Consumes: `apiGet`/`apiPut` (`@/lib/api`); `useRootFolders` (`./configApi`); `useQualityProfiles` (`./qualityApi`); `useToast`.
- Produces:
  - `type KindDefaults = { rootFolderId: number | null; qualityProfileId: number | null }`, `type MediaDefaults = { movie: KindDefaults; tv: KindDefaults }`
  - `useMediaDefaults()` → `MediaDefaults`; `useSaveMediaDefaults()` mutation over `PUT /config/media-defaults`
  - `<MediaManagementSection />`

- [ ] **Step 1: Write the failing test**

Read `web/src/features/settings/NamingSection.test.tsx` first and reuse its harness (it mocks `./configApi` and wraps in `ToastProvider`). Create `web/src/features/settings/MediaManagementSection.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { MediaManagementSection } from "./MediaManagementSection"
import * as mdApi from "./mediaDefaultsApi"
import * as cfg from "./configApi"
import * as qual from "./qualityApi"

vi.mock("./mediaDefaultsApi")
vi.mock("./configApi")
vi.mock("./qualityApi")

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(cfg.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }, { id: 2, path: "/media/tv", createdAt: "" }] } as never)
  vi.mocked(qual.useQualityProfiles).mockReturnValue({ data: [{ id: 5, name: "HD-1080p", cutoffQualityId: 7, upgradeAllowed: true, items: [], createdAt: "" }] } as never)
  vi.mocked(mdApi.useSaveMediaDefaults).mockReturnValue(mut())
})

function renderSection() {
  render(<ToastProvider><MediaManagementSection /></ToastProvider>)
}

describe("MediaManagementSection", () => {
  it("seeds the four dropdowns from the saved defaults", () => {
    vi.mocked(mdApi.useMediaDefaults).mockReturnValue({
      data: { movie: { rootFolderId: 1, qualityProfileId: 5 }, tv: { rootFolderId: 2, qualityProfileId: null } },
      isLoading: false, isError: false,
    } as never)
    renderSection()
    expect((screen.getByLabelText("Default Movie Root Folder") as HTMLSelectElement).value).toBe("1")
    expect((screen.getByLabelText("Default TV Root Folder") as HTMLSelectElement).value).toBe("2")
    expect((screen.getByLabelText("Default TV Quality Profile") as HTMLSelectElement).value).toBe("")
  })

  it("saves the PUT body, sending null for a 'None' selection", async () => {
    const mutate = vi.fn()
    vi.mocked(mdApi.useSaveMediaDefaults).mockReturnValue(mut({ mutate }))
    vi.mocked(mdApi.useMediaDefaults).mockReturnValue({
      data: { movie: { rootFolderId: 1, qualityProfileId: 5 }, tv: { rootFolderId: 2, qualityProfileId: 5 } },
      isLoading: false, isError: false,
    } as never)
    renderSection()

    await userEvent.selectOptions(screen.getByLabelText("Default TV Quality Profile"), "") // choose None
    await userEvent.click(screen.getByRole("button", { name: /save/i }))

    expect(mutate).toHaveBeenCalledWith(
      { movie: { rootFolderId: 1, qualityProfileId: 5 }, tv: { rootFolderId: 2, qualityProfileId: null } },
      expect.anything(),
    )
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/features/settings/MediaManagementSection.test.tsx`
Expected: FAIL — cannot resolve `./MediaManagementSection`.

- [ ] **Step 3: Write `mediaDefaultsTypes.ts` and `mediaDefaultsApi.ts`**

`web/src/features/settings/mediaDefaultsTypes.ts`:

```ts
export type KindDefaults = { rootFolderId: number | null; qualityProfileId: number | null }
export type MediaDefaults = { movie: KindDefaults; tv: KindDefaults }
```

`web/src/features/settings/mediaDefaultsApi.ts` (mirrors `useNamingConfig`/`useSaveNaming`):

```ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPut } from "@/lib/api"
import type { MediaDefaults } from "./mediaDefaultsTypes"

export const mediaDefaultsKey = ["settings", "mediaDefaults"] as const

export function useMediaDefaults() {
  return useQuery({ queryKey: mediaDefaultsKey, queryFn: () => apiGet<MediaDefaults>("/config/media-defaults") })
}

export function useSaveMediaDefaults() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (d: MediaDefaults) => apiPut<MediaDefaults>("/config/media-defaults", d),
    onSuccess: () => qc.invalidateQueries({ queryKey: mediaDefaultsKey }),
  })
}
```

- [ ] **Step 4: Write `MediaManagementSection.tsx`**

Create `web/src/features/settings/MediaManagementSection.tsx`. A parseable-id helper keeps `""` → `null`:

```tsx
import { useState } from "react"
import { Select } from "@/components/ui/select"
import { useToast } from "@/lib/toast"
import { useRootFolders } from "./configApi"
import { useQualityProfiles } from "./qualityApi"
import { useMediaDefaults, useSaveMediaDefaults } from "./mediaDefaultsApi"
import type { MediaDefaults } from "./mediaDefaultsTypes"

const EMPTY: MediaDefaults = {
  movie: { rootFolderId: null, qualityProfileId: null },
  tv: { rootFolderId: null, qualityProfileId: null },
}

function toId(v: string): number | null {
  return v === "" ? null : Number(v)
}
function toStr(v: number | null): string {
  return v == null ? "" : String(v)
}

export function MediaManagementSection() {
  const { toast } = useToast()
  const defaults = useMediaDefaults()
  const roots = useRootFolders()
  const profiles = useQualityProfiles()
  const save = useSaveMediaDefaults()

  const [form, setForm] = useState<MediaDefaults | null>(null)
  const [initialized, setInitialized] = useState(false)
  if (!initialized && defaults.data) {
    setForm(defaults.data)
    setInitialized(true)
  }

  if (defaults.isLoading || !form) return <div className="p-6"><p className="text-sm text-[var(--color-muted)]">Loading…</p></div>
  if (defaults.isError) return <div className="p-6"><p className="text-sm text-[var(--color-warn)]">Failed to load.</p></div>

  const rootOptions = roots.data ?? []
  const profileOptions = profiles.data ?? []

  function setField(kind: "movie" | "tv", field: "rootFolderId" | "qualityProfileId", v: string) {
    setForm((f) => (f ? { ...f, [kind]: { ...f[kind], [field]: toId(v) } } : f))
  }

  const onSave = () => {
    save.mutate(form!, {
      onSuccess: () => toast("Saved"),
      onError: () => toast("Save failed", { variant: "error" }),
    })
  }

  const rows: { kind: "movie" | "tv"; label: string }[] = [
    { kind: "movie", label: "Movie" },
    { kind: "tv", label: "TV" },
  ]

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">Media Management</h2>
      <p className="mb-4 max-w-2xl text-sm text-[var(--color-muted)]">
        Defaults applied when adding a movie or show. You can still override them per add.
      </p>
      <div className="flex max-w-2xl flex-col gap-5">
        {rows.map((row) => (
          <div key={row.kind} className="flex flex-col gap-2">
            <div className="text-sm font-medium">{row.label}</div>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-xs text-[var(--color-muted)]">Default {row.label} Root Folder</span>
              <Select
                aria-label={`Default ${row.label} Root Folder`}
                value={toStr(form[row.kind].rootFolderId)}
                onChange={(v) => setField(row.kind, "rootFolderId", v)}
              >
                <option value="">None</option>
                {rootOptions.map((rf) => <option key={rf.id} value={rf.id}>{rf.path}</option>)}
              </Select>
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-xs text-[var(--color-muted)]">Default {row.label} Quality Profile</span>
              <Select
                aria-label={`Default ${row.label} Quality Profile`}
                value={toStr(form[row.kind].qualityProfileId)}
                onChange={(v) => setField(row.kind, "qualityProfileId", v)}
              >
                <option value="">None</option>
                {profileOptions.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </Select>
            </label>
          </div>
        ))}
        <div>
          <button
            onClick={onSave}
            disabled={save.isPending}
            className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
          >
            Save
          </button>
        </div>
      </div>
    </div>
  )
}
```

> `EMPTY` is exported-in-spirit only if a test needs it; it's fine to drop if unused. Confirm `Select`'s `onChange` passes the raw string value (it does — `AddMediaDialog` calls `onChange={setRootFolderId}` with a string).

- [ ] **Step 5: Wire the tab and route**

`web/src/features/settings/SettingsLayout.tsx` — add to `TABS` (after Naming, before General, matching Sonarr's ordering):

```tsx
  { to: "/settings/mediamanagement", label: "Media Management" },
```

`web/src/app/routes.tsx` — import and add the child route beside the others:

```tsx
import { MediaManagementSection } from "@/features/settings/MediaManagementSection"
```
```tsx
          { path: "mediamanagement", element: <MediaManagementSection /> },
```

- [ ] **Step 6: Run tests + typecheck**

Run: `cd web && npx vitest run src/features/settings/MediaManagementSection.test.tsx && npx tsc -b`
Expected: PASS, tsc 0.

- [ ] **Step 7: Rebuild dist**

Run: `cd web && npm run build`
Expected: succeeds; `git status web/dist` shows modified bundle (the new tab is app-bundled).

- [ ] **Step 8: Full FE suite**

Run: `cd web && npx vitest run`
Expected: PASS (existing + new). If a `SettingsLayout` test asserts the tab count, update it for the new tab.

- [ ] **Step 9: Commit**

```bash
git add web/src/features/settings/ web/src/app/routes.tsx web/dist
git commit -m "feat(webui): Media Management settings tab

A 7th settings tab with per-kind default root folder + quality profile
dropdowns, seeded from GET /config/media-defaults and saved via PUT. 'None'
sends null. Mirrors the naming-config form pattern.

Rebuilds web/dist (committed; CI drift-checks it)."
```

---

### Task 4: Add dialog — required root folder + profile, defaults pre-seed

**Files:**
- Modify: `web/src/features/library/types.ts` (add-body fields), `web/src/features/library/AddMediaDialog.tsx`, `web/src/features/library/AddMediaDialog.test.tsx`, `web/dist`

**Interfaces:**
- Consumes: `useMediaDefaults` (`@/features/settings/mediaDefaultsApi`, T3); `useQualityProfiles` (library `./api`); existing `useRootFolders`, `useAddMovie`, `useAddSeries`.
- Produces: `AddMovieBody` / `AddSeriesBody` gain `qualityProfileId: number | null`.

- [ ] **Step 1: Extend the add-body types**

`web/src/features/library/types.ts`:

```ts
export type AddMovieBody = { tmdbId: number; rootFolderId: number | null; monitored: boolean; qualityProfileId: number | null }
export type AddSeriesBody = { tmdbId: number; rootFolderId: number | null; monitorOption: "all" | "future" | "none"; qualityProfileId: number | null }
```

- [ ] **Step 2: Write the failing test**

Extend `web/src/features/library/AddMediaDialog.test.tsx`. The existing file mocks `@/features/library/api`; add `useQualityProfiles` to that mock and mock the settings defaults module. Update the top-of-file mock block and `stub()`:

```tsx
import * as md from "@/features/settings/mediaDefaultsApi"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return {
    ...actual,
    useLookup: vi.fn(),
    useRootFolders: vi.fn(),
    useQualityProfiles: vi.fn(),
    useAddMovie: vi.fn(),
    useAddSeries: vi.fn(),
  }
})
vi.mock("@/features/settings/mediaDefaultsApi")
```

In `stub()`, add the profile list and default:

```tsx
  vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [{ id: 5, name: "HD-1080p", cutoffQualityId: 7, upgradeAllowed: true, items: [], createdAt: "" }] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
  vi.mocked(md.useMediaDefaults).mockReturnValue({ data: { movie: { rootFolderId: 1, qualityProfileId: 5 }, tv: { rootFolderId: null, qualityProfileId: null } } } as unknown as ReturnType<typeof md.useMediaDefaults>)
```

The existing tests that call `useRootFolders` with `{ data: [] }` still work (no default resolvable, Add disabled by the no-roots guard). Add three new tests. The profile is **required** — no "None" option, Add disabled until a profile is selected.

```tsx
it("pre-selects the default root folder and profile, and sends them on add", async () => {
  stub()
  const mutateAsync = vi.fn().mockResolvedValue({})
  vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync, isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
  vi.mocked(lib.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }] } as unknown as ReturnType<typeof lib.useRootFolders>)
  render(
    <ToastProvider>
      <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
    </ToastProvider>,
  )
  await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
  await userEvent.click(await screen.findByText("Dune"))

  // defaults are pre-selected; with both set, Add is enabled
  expect((screen.getByLabelText("Root folder") as HTMLSelectElement).value).toBe("1")
  expect((screen.getByLabelText("Quality profile") as HTMLSelectElement).value).toBe("5")
  expect(screen.getByRole("button", { name: /add movie/i })).toBeEnabled()

  await userEvent.click(screen.getByRole("button", { name: /add movie/i }))
  expect(mutateAsync).toHaveBeenCalledWith({ tmdbId: 1, rootFolderId: 1, monitored: true, qualityProfileId: 5 })
})

it("requires a profile: Add is disabled until one is chosen when there is no default", async () => {
  stub()
  const mutateAsync = vi.fn().mockResolvedValue({})
  vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync, isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
  vi.mocked(lib.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }] } as unknown as ReturnType<typeof lib.useRootFolders>)
  // no default profile for movies
  vi.mocked(md.useMediaDefaults).mockReturnValue({ data: { movie: { rootFolderId: 1, qualityProfileId: null }, tv: { rootFolderId: null, qualityProfileId: null } } } as unknown as ReturnType<typeof md.useMediaDefaults>)
  render(
    <ToastProvider>
      <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
    </ToastProvider>,
  )
  await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
  await userEvent.click(await screen.findByText("Dune"))

  // no profile pre-selected → Add disabled
  expect(screen.getByRole("button", { name: /add movie/i })).toBeDisabled()

  await userEvent.selectOptions(screen.getByLabelText("Quality profile"), "5")
  expect(screen.getByRole("button", { name: /add movie/i })).toBeEnabled()
  await userEvent.click(screen.getByRole("button", { name: /add movie/i }))
  expect(mutateAsync).toHaveBeenCalledWith({ tmdbId: 1, rootFolderId: 1, monitored: true, qualityProfileId: 5 })
})

it("requires a root folder: Add is disabled until one is chosen when there is no default", async () => {
  stub()
  const mutateAsync = vi.fn().mockResolvedValue({})
  vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync, isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
  vi.mocked(lib.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }] } as unknown as ReturnType<typeof lib.useRootFolders>)
  // profile default present (5), but no root-folder default
  vi.mocked(md.useMediaDefaults).mockReturnValue({ data: { movie: { rootFolderId: null, qualityProfileId: 5 }, tv: { rootFolderId: null, qualityProfileId: null } } } as unknown as ReturnType<typeof md.useMediaDefaults>)
  render(
    <ToastProvider>
      <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
    </ToastProvider>,
  )
  await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
  await userEvent.click(await screen.findByText("Dune"))

  // profile satisfied by the default, but no root folder → Add disabled
  expect(screen.getByRole("button", { name: /add movie/i })).toBeDisabled()

  await userEvent.selectOptions(screen.getByLabelText("Root folder"), "1")
  expect(screen.getByRole("button", { name: /add movie/i })).toBeEnabled()
  await userEvent.click(screen.getByRole("button", { name: /add movie/i }))
  expect(mutateAsync).toHaveBeenCalledWith({ tmdbId: 1, rootFolderId: 1, monitored: true, qualityProfileId: 5 })
})

it("shows a hint and disables Add when no quality profiles are configured", async () => {
  stub()
  vi.mocked(lib.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }] } as unknown as ReturnType<typeof lib.useRootFolders>)
  vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
  vi.mocked(md.useMediaDefaults).mockReturnValue({ data: { movie: { rootFolderId: 1, qualityProfileId: null }, tv: { rootFolderId: null, qualityProfileId: null } } } as unknown as ReturnType<typeof md.useMediaDefaults>)
  render(
    <ToastProvider>
      <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
    </ToastProvider>,
  )
  await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
  await userEvent.click(await screen.findByText("Dune"))

  expect(screen.getByText(/no quality profile configured/i)).toBeInTheDocument()
  expect(screen.getByRole("button", { name: /add movie/i })).toBeDisabled()
})
```

> Note: the existing "blocks submit when no root folders" test uses `stub()` (movie default profile 5 pre-selected) but `useRootFolders {data: []}`, so Add stays disabled by the no-roots guard — that assertion is unaffected. The two tests that don't call `stub()` still need `useQualityProfiles`/`useMediaDefaults` mocked (they never reach the picked panel, so any non-throwing return works — add them to those tests or give the mocks safe defaults in `beforeEach`).

- [ ] **Step 3: Run to verify it fails**

Run: `cd web && npx vitest run src/features/library/AddMediaDialog.test.tsx`
Expected: FAIL — no "Quality profile" control / mutateAsync body lacks `qualityProfileId`.

- [ ] **Step 4: Implement the dialog changes**

In `web/src/features/library/AddMediaDialog.tsx`:

Add imports and hooks:

```tsx
import { useMediaDefaults } from "@/features/settings/mediaDefaultsApi"
```
```tsx
  const qualityProfiles = useQualityProfiles()
  const mediaDefaults = useMediaDefaults()
  const [qualityProfileId, setQualityProfileId] = useState("")
```

(`useQualityProfiles` is already exported from `./api` — add it to the existing import from `./api`.)

Pre-seed when a result is picked. Add a `useEffect` (import `useEffect` — already imported):

```tsx
  // Seed root folder + profile from the per-kind defaults each time a result is
  // picked. react-query hands back a stable data reference once loaded, so this
  // does not clobber a later manual override.
  useEffect(() => {
    if (!picked) return
    const d = kind === "movie" ? mediaDefaults.data?.movie : mediaDefaults.data?.tv
    setRootFolderId(d?.rootFolderId != null ? String(d.rootFolderId) : "")
    setQualityProfileId(d?.qualityProfileId != null ? String(d.qualityProfileId) : "")
  }, [picked, mediaDefaults.data, kind])
```

Add a `noProfiles` derived flag near the existing `noRoots` (`const noRoots = (rootFolders.data ?? []).length === 0`):

```tsx
  const noProfiles = (qualityProfiles.data ?? []).length === 0
```

Send both ids in `submit()`. The UI gates on **both** a root folder and a profile being selected, so both are always real ids here — send them as numbers:

```tsx
    const rfId = Number(rootFolderId) // gated non-empty by the Add button
    const qpId = Number(qualityProfileId) // gated non-empty by the Add button
    try {
      if (kind === "movie") {
        await addMovie.mutateAsync({ tmdbId: picked.tmdbId, rootFolderId: rfId, monitored, qualityProfileId: qpId })
      } else {
        await addSeries.mutateAsync({ tmdbId: picked.tmdbId, rootFolderId: rfId, monitorOption, qualityProfileId: qpId })
      }
```

Reset it in `reset()`: add `setQualityProfileId("")`.

**Root folder — remove the selectable "None".** The existing root-folder `Select` (inside the `noRoots ? hint : Select` conditional) has a blank `<option value="">Select…</option>`. Replace it with the same disabled-placeholder pattern so a root folder cannot be un-selected:

```tsx
          {noRoots ? (
            <p className="text-sm text-[var(--color-warn)]">No root folder configured — add one in Settings.</p>
          ) : (
            <Select aria-label="Root folder" value={rootFolderId} onChange={setRootFolderId}>
              {!rootFolderId && <option value="" disabled>Select a folder…</option>}
              {(rootFolders.data ?? []).map((rf) => (
                <option key={rf.id} value={rf.id}>{rf.path}</option>
              ))}
            </Select>
          )}
```

Render the Quality profile control beneath the root-folder select (before the monitor block). **No selectable "None"** — a disabled placeholder shows only when nothing is chosen, and the whole control is replaced by a hint when no profiles exist:

```tsx
          <label className="text-xs text-[var(--color-muted)]">Quality profile</label>
          {noProfiles ? (
            <p className="text-sm text-[var(--color-warn)]">No quality profile configured — add one in Settings.</p>
          ) : (
            <Select aria-label="Quality profile" value={qualityProfileId} onChange={setQualityProfileId}>
              {!qualityProfileId && <option value="" disabled>Select a profile…</option>}
              {(qualityProfiles.data ?? []).map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </Select>
          )}
```

Both placeholders are `disabled`, so once a value is chosen it cannot be re-selected back to empty — there is no way to return to "none" for either the root folder or the profile (that is the point of removing "None").

Gate the Add button on **both** a root folder and a profile being selected, alongside the existing `pending` and the no-options guards. Change the button's `disabled`:

```tsx
              disabled={noRoots || noProfiles || pending || !rootFolderId || !qualityProfileId}
```

> Cross-feature import note: the library dialog importing a config read hook from `@/features/settings/mediaDefaultsApi` is deliberate — media-defaults is settings-owned config that the add flow consumes. This mirrors the dialog already importing `ApiError` from `@/lib/api`.

- [ ] **Step 5: Run to verify it passes**

Run: `cd web && npx vitest run src/features/library/AddMediaDialog.test.tsx && npx tsc -b`
Expected: PASS (new test + the three existing ones), tsc 0.

> The three existing tests use `useRootFolders {data: []}`; with a resolvable movie default of `rootFolderId: 1` but no matching option, the Select value falls back to empty — those tests assert the no-roots guard and lookup/sort behavior, which are unaffected. If any existing test now fails because `useQualityProfiles`/`useMediaDefaults` is unmocked in it, add the mock (the two error/sort tests that don't call `stub()` need the same additions).

- [ ] **Step 6: Rebuild dist + full verify**

Run: `cd web && npm run build && npx vitest run && npx tsc -b`
Then: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go build ./...`
Expected: build succeeds; whole FE suite green; tsc 0; Go build green (it embeds `web/dist`). `git status web/dist` shows the rebuilt bundle.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/library/ web/dist
git commit -m "feat(webui): required root folder + quality profile in Add dialog

The Add dialog gains a required Quality profile dropdown (absent before) and
makes the root folder required too. Both are pre-selected from the per-kind
default, neither offers a 'None' option, and Add stays disabled until both
are selected: the user leaves the defaults or picks others. When a control
has no configured options, a hint replaces it and Add stays disabled. The
add request now carries qualityProfileId, so an added item is born with its
profile instead of needing a separate assignment. This removes the
folder-less / profile-less add the Wave B guard-toast exists to catch.

Rebuilds web/dist."
```

---

## Self-Review

**Spec coverage:**

| Spec § | Requirement | Task |
|--------|-------------|------|
| §1, §3 | Four settings keys, store the id | T2 (`keyDefault*` constants) |
| §2 | No root-folder "kind" column; only root folder + profile | T2/T3 (settings-only, no schema change) |
| §4.1 | Typed GET/PUT `/config/media-defaults`, `{movie,tv}` nullable-int shape | T2 |
| §4.2 | Stale-reference → `null` on GET; PUT all-or-nothing 400 | T2 Steps 3, 5 (mutation-checked), 7 |
| §4.3 | `qualityProfileId` threaded through Add, additive | T1 |
| §4.3 | `ErrInvalidQualityProfile` → 400 | T1 |
| §5.1 | Media Management tab, four dropdowns, "None" | T3 |
| §5.2 | Add dialog root folder + profile, **both required, no "None", pre-seeded** + send | T4 (Step 4 gating + both placeholders; Step 2 tests: pre-seed/enabled, no-default-root→disabled-until-picked, no-default-profile→disabled-until-picked, no-roots→hint, no-profiles→hint) |
| §6 | Error handling (stale → blank, unknown id → 400, no root/profile → Add disabled, no options → hint) | T1, T2, T4 |
| §7 | Go tests incl. wire-shape via RawMessage; FE tests; dist rebuild | T2 (wire-shape), T3, T4 |

**Placeholder scan:** The only deferred-to-implementer items are the real fixture-helper names in T1/T2 (`newMediaTestService`/`mustRootFolder`/`mustQualityProfile`) — each flagged with an instruction to use the file's existing helpers rather than invent, with a concrete fallback (`CreateRootFolder`/`CreateQualityProfile`) if none exists. No TBD/TODO/"add validation" placeholders.

**Type consistency:** `KindDefaults`/`MediaDefaults` (Go `*int64` ↔ TS `number | null`) are defined once in T2, mirrored in T3's `mediaDefaultsTypes.ts`, consumed identically in T4. `qualityProfileId` is the same JSON key across the add body (T1), the TS add-body types (T4), and the mutation call (T4). `useMediaDefaults`/`useSaveMediaDefaults` names match between T3 (definition) and T4 (consumption). `GET`/`PUT /config/media-defaults` path is identical in T2 (route), T3 (hooks), and matches the distinct-route constraint.

**One deliberate design choice to flag for the reviewer:** the Add dialog (library feature) imports `useMediaDefaults` from the settings feature. This is an intentional cross-feature *read* of settings-owned config, not a layering violation — noted inline in T4 Step 4.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-17-nexus-add-defaults.md`.
