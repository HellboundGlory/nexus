# Nexus Parsing & Quality (Sub-project 4b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Nexus a pure release-name parser, a fixed ranked set of quality definitions, user-created quality profiles, and a stateless quality-based decision/scoring engine — plus a REST surface (profile CRUD, profile assignment, a `/parse` preview) — with no live consumer yet (search is sub-5, upgrade-vs-file is 4c).

**Architecture:** Two new feature packages: `internal/parsing` (pure `Parse(title, kind) → ParsedRelease`, depends only on `internal/core/provider`) and `internal/quality` (built-in quality registry + stateless `Resolve`/`Decide`/`Compare` + profiles service + REST API, depends on `internal/parsing` + `internal/core/*`). The `quality_profiles` table and CRUD live in `internal/core/store` (migration `0005`); profile *assignment* lives in the `media` package so `media` still imports only `internal/core/*`.

**Tech Stack:** Go 1.26, `go-chi/chi/v5`, `modernc.org/sqlite` (pure Go, `CGO_ENABLED=0`), stdlib `net/http` + `net/http/httptest` + `regexp`, `log/slog`.

## Global Constraints

- Module path is `github.com/hellboundg/nexus`.
- Module boundaries (verify with `go list -deps` in Task 9): `internal/parsing` imports **only** `internal/core/*`; `internal/quality` imports **only** `internal/parsing` + `internal/core/*`; `internal/media` imports **only** `internal/core/*` (never `internal/quality`). None import `internal/indexer`, `internal/downloadclient`, or `internal/automation`.
- Go is not on PATH in the dev environment: prefix every Go command with `export PATH="/c/Program Files/Go/bin:$PATH"`.
- `-race` is unavailable (no C compiler / `CGO_ENABLED=0`). Verify with `-count=N`, never `-race`.
- All tests are offline, deterministic, CGO-free: no network, pure functions and `httptest`. Test DBs use `database.Open(t.TempDir()+"/t.db")`, never `:memory:`.
- Reuse existing store helpers (`boolToInt`, `rowScanner`, sentinel `store.ErrNotFound`) — do NOT redefine them.
- REST error responses go through `api.WriteError(w, status, code, msg)`; success through `api.WriteJSON(w, status, v)`. Sub-routers expose `Mount(r chi.Router)` and are mounted into the authed `/api/v1` group in `cmd/nexus/main.go` (variadic mounts on `api.NewRouter`).
- No new credentials or external services are introduced in 4b.
- Language is parsed but never influences a decision in 4b.
- `command.Reporter` is `interface{ Progress(pct int, msg string) }`; there is no exported `NopReporter` (tests define a local one if needed — 4b introduces no commands, so not required here).

---

### Task 1: Parsing package — types + quality attributes

**Files:**
- Create: `internal/parsing/parsing.go`
- Create: `internal/parsing/parser.go`
- Create: `internal/parsing/parser_test.go`

**Interfaces:**
- Produces: `parsing.Source` (+ consts `SourceUnknown/CAM/TS/DVD/HDTV/WEBRip/WEBDL/Bluray`), `parsing.Resolution` (+ consts `ResUnknown/Res480p/Res720p/Res1080p/Res2160p`), `parsing.Revision{Version int; IsRepack bool}`, `parsing.ParsedRelease` (full struct), and `parsing.Parse(title string, kind provider.MediaKind) ParsedRelease`. This task fills `Source`, `Resolution`, `Codec`, `Revision`, and a first-pass `Title`; identity/group/language fields stay at their zero values until Task 2.
- Consumes: `provider.MediaKind` (`provider.KindTV`, `provider.KindMovie`) from `internal/core/provider`.

- [ ] **Step 1: Write the failing test**

Create `internal/parsing/parser_test.go`:
```go
package parsing

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestParseQualityAttributes(t *testing.T) {
	cases := []struct {
		title string
		src   Source
		res   Resolution
		codec string
		rev   Revision
	}{
		{"The.Show.S01E01.1080p.BluRay.x264-GRP", SourceBluray, Res1080p, "x264", Revision{Version: 1}},
		{"Movie.Title.2019.2160p.WEB-DL.x265-GRP", SourceWEBDL, Res2160p, "x265", Revision{Version: 1}},
		{"Some.Show.S02E03.720p.HDTV.x264-GRP", SourceHDTV, Res720p, "x264", Revision{Version: 1}},
		{"Film.1998.480p.DVDRip.XviD-GRP", SourceDVD, Res480p, "xvid", Revision{Version: 1}},
		{"Show.S01E05.1080p.WEBRip.PROPER.x264-GRP", SourceWEBRip, Res1080p, "x264", Revision{Version: 2, IsRepack: false}},
		{"Show.S01E06.1080p.BluRay.REPACK.x265-GRP", SourceBluray, Res1080p, "x265", Revision{Version: 2, IsRepack: true}},
	}
	for _, c := range cases {
		got := Parse(c.title, provider.KindTV)
		if got.Source != c.src || got.Resolution != c.res || got.Codec != c.codec || got.Revision != c.rev {
			t.Errorf("Parse(%q) = {src:%v res:%v codec:%q rev:%+v}, want {src:%v res:%v codec:%q rev:%+v}",
				c.title, got.Source, got.Resolution, got.Codec, got.Revision, c.src, c.res, c.codec, c.rev)
		}
	}
}

func TestParseUnknownIsBestEffort(t *testing.T) {
	got := Parse("just some random text", provider.KindMovie)
	if got.Source != SourceUnknown || got.Resolution != ResUnknown {
		t.Fatalf("expected unknown src/res, got src:%v res:%v", got.Source, got.Resolution)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/parsing/ -run TestParse`
Expected: FAIL — `undefined: Parse` / undefined types.

- [ ] **Step 3: Create the types**

Create `internal/parsing/parsing.go`:
```go
// Package parsing turns a release title into structured fields (quality,
// identity, release group, proper/repack, language). Pure — no I/O.
package parsing

// Source is the release medium/source, ranked loosely low→high by convention.
type Source int

const (
	SourceUnknown Source = iota
	SourceCAM
	SourceTS
	SourceDVD
	SourceHDTV
	SourceWEBRip
	SourceWEBDL
	SourceBluray
)

// Resolution is the vertical pixel resolution bucket.
type Resolution int

const (
	ResUnknown Resolution = iota
	Res480p
	Res720p
	Res1080p
	Res2160p
)

// Revision captures proper/repack. Version starts at 1; a PROPER or REPACK
// marker bumps it to 2. IsRepack is true only for REPACK.
type Revision struct {
	Version  int
	IsRepack bool
}

// ParsedRelease is the structured result of Parse. Identity fields use zero
// values when absent: Season and Year are 0, Episodes/Languages are nil,
// Edition/ReleaseGroup are "".
type ParsedRelease struct {
	Title        string     `json:"title"`
	Year         int        `json:"year"`
	Season       int        `json:"season"`
	Episodes     []int      `json:"episodes"`
	Edition      string     `json:"edition"`
	Source       Source     `json:"source"`
	Resolution   Resolution `json:"resolution"`
	Codec        string     `json:"codec"`
	ReleaseGroup string     `json:"releaseGroup"`
	Revision     Revision   `json:"revision"`
	Languages    []string   `json:"languages"`
}
```

- [ ] **Step 4: Create the parser (quality attributes)**

Create `internal/parsing/parser.go`:
```go
package parsing

import (
	"regexp"
	"strings"

	"github.com/hellboundg/nexus/internal/core/provider"
)

var (
	reResolution = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p)\b`)
	reCodec      = regexp.MustCompile(`(?i)\b(x264|h\.?264|avc|x265|h\.?265|hevc|xvid|divx)\b`)
	reProper     = regexp.MustCompile(`(?i)\bproper\b`)
	reRepack     = regexp.MustCompile(`(?i)\brepack\b`)
	// Source patterns, checked in priority order (first match wins).
	sourcePatterns = []struct {
		re  *regexp.Regexp
		src Source
	}{
		{regexp.MustCompile(`(?i)\b(bluray|blu-ray|bdrip|brrip|bd25|bd50)\b`), SourceBluray},
		{regexp.MustCompile(`(?i)\b(web-?dl|webdl)\b`), SourceWEBDL},
		{regexp.MustCompile(`(?i)\bweb-?rip\b`), SourceWEBRip},
		{regexp.MustCompile(`(?i)\bhdtv\b`), SourceHDTV},
		{regexp.MustCompile(`(?i)\b(dvdrip|dvd-?r|dvd)\b`), SourceDVD},
		{regexp.MustCompile(`(?i)\b(hdts|telesync|ts)\b`), SourceTS},
		{regexp.MustCompile(`(?i)\b(hdcam|cam)\b`), SourceCAM},
	}
)

// Parse extracts structured fields from a release title. It never errors: an
// unrecognizable title yields a best-effort ParsedRelease with Unknown
// source/resolution. kind selects TV vs movie identity parsing (Task 2).
func Parse(title string, kind provider.MediaKind) ParsedRelease {
	p := ParsedRelease{Season: 0, Revision: Revision{Version: 1}}

	if m := reResolution.FindString(title); m != "" {
		switch strings.ToLower(m) {
		case "2160p":
			p.Resolution = Res2160p
		case "1080p":
			p.Resolution = Res1080p
		case "720p":
			p.Resolution = Res720p
		case "480p":
			p.Resolution = Res480p
		}
	}
	for _, sp := range sourcePatterns {
		if sp.re.MatchString(title) {
			p.Source = sp.src
			break
		}
	}
	if m := reCodec.FindString(title); m != "" {
		p.Codec = normalizeCodec(m)
	}
	if reRepack.MatchString(title) {
		p.Revision = Revision{Version: 2, IsRepack: true}
	} else if reProper.MatchString(title) {
		p.Revision = Revision{Version: 2, IsRepack: false}
	}

	p.Title = cleanTitle(title)
	_ = kind // identity parsing added in Task 2
	return p
}

func normalizeCodec(m string) string {
	s := strings.ToLower(strings.ReplaceAll(m, ".", ""))
	switch s {
	case "h264", "avc":
		return "h264"
	case "h265", "hevc":
		return "x265"
	case "divx":
		return "xvid"
	}
	return s
}

// cleanTitle returns the title text up to the first quality/identity marker,
// with separators normalized to spaces. Task 2 extends the marker set.
func cleanTitle(title string) string {
	cut := len(title)
	if loc := reResolution.FindStringIndex(title); loc != nil && loc[0] < cut {
		cut = loc[0]
	}
	for _, sp := range sourcePatterns {
		if loc := sp.re.FindStringIndex(title); loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}
	name := title[:cut]
	name = strings.NewReplacer(".", " ", "_", " ").Replace(name)
	return strings.TrimSpace(name)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/parsing/ -run TestParse -count=1`
Expected: PASS. (If a case fails, adjust the regex/ordering — do not change the asserted expectations.)

- [ ] **Step 6: Commit**

```bash
git add internal/parsing/parsing.go internal/parsing/parser.go internal/parsing/parser_test.go
git commit -m "feat: add release-name parser with quality attribute extraction"
```

---

### Task 2: Parser — identity, release group, language

**Files:**
- Modify: `internal/parsing/parser.go`
- Modify: `internal/parsing/parser_test.go`

**Interfaces:**
- Produces: `Parse` now fills `Season`, `Episodes`, `Year`, `Edition`, `ReleaseGroup`, `Languages`, and refines `Title` (cuts at season/year markers too). Signatures unchanged.
- Consumes: everything from Task 1.

- [ ] **Step 1: Add the failing tests**

Append to `internal/parsing/parser_test.go`:
```go
func TestParseIdentityTV(t *testing.T) {
	cases := []struct {
		title    string
		wantS    int
		wantEps  []int
		wantName string
	}{
		{"The.Show.S02E05.1080p.BluRay.x264-GRP", 2, []int{5}, "The Show"},
		{"The.Show.S01E01E02.720p.HDTV.x264-GRP", 1, []int{1, 2}, "The Show"},
		{"The.Show.S03E10-E12.1080p.WEB-DL-GRP", 3, []int{10, 11, 12}, "The Show"},
	}
	for _, c := range cases {
		got := Parse(c.title, provider.KindTV)
		if got.Season != c.wantS || !equalInts(got.Episodes, c.wantEps) || got.Title != c.wantName {
			t.Errorf("Parse(%q) season=%d eps=%v title=%q; want season=%d eps=%v title=%q",
				c.title, got.Season, got.Episodes, got.Title, c.wantS, c.wantEps, c.wantName)
		}
	}
}

func TestParseIdentityMovie(t *testing.T) {
	got := Parse("Movie.Title.2019.Extended.1080p.BluRay.x264-GRP", provider.KindMovie)
	if got.Year != 2019 || got.Title != "Movie Title" || got.Edition != "Extended" {
		t.Fatalf("year=%d title=%q edition=%q", got.Year, got.Title, got.Edition)
	}
}

func TestParseGroupAndLanguage(t *testing.T) {
	got := Parse("Movie.Title.2019.MULTi.1080p.BluRay.x264-SOMEGRP", provider.KindMovie)
	if got.ReleaseGroup != "SOMEGRP" {
		t.Errorf("group=%q want SOMEGRP", got.ReleaseGroup)
	}
	if len(got.Languages) == 0 || got.Languages[0] != "multi" {
		t.Errorf("languages=%v want [multi]", got.Languages)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/parsing/ -run TestParseIdentity -count=1`
Expected: FAIL — season/episodes/year/edition/group/language all zero.

- [ ] **Step 3: Add the extraction regexes and logic**

In `internal/parsing/parser.go`, add these vars to the existing `var (...)` block:
```go
	reSeasonEp   = regexp.MustCompile(`(?i)\bS(\d{1,2})((?:E\d{1,2})+)(?:-?E?(\d{1,2}))?\b`)
	reEpNums     = regexp.MustCompile(`(?i)E(\d{1,2})`)
	reYear       = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	reGroup      = regexp.MustCompile(`-(\w+)$`)
	reEdition    = regexp.MustCompile(`(?i)\b(director'?s cut|extended|unrated|remastered|imax|theatrical)\b`)
	reLanguage   = regexp.MustCompile(`(?i)\b(multi|english|french|german|spanish|italian|japanese|korean|dutch)\b`)
)
```
Then replace the identity section of `Parse` (the `_ = kind` line and the `p.Title = cleanTitle(title)` call) with:
```go
	if kind == provider.KindTV {
		if m := reSeasonEp.FindStringSubmatch(title); m != nil {
			p.Season = atoi(m[1])
			for _, em := range reEpNums.FindAllStringSubmatch(m[2], -1) {
				p.Episodes = append(p.Episodes, atoi(em[1]))
			}
			if m[3] != "" { // range end, e.g. E10-E12
				end := atoi(m[3])
				for e := p.Episodes[len(p.Episodes)-1] + 1; e <= end; e++ {
					p.Episodes = append(p.Episodes, e)
				}
			}
		}
	} else {
		if m := reYear.FindString(title); m != "" {
			p.Year = atoi(m)
		}
		if m := reEdition.FindString(title); m != "" {
			p.Edition = canonicalEdition(m)
		}
	}
	if m := reGroup.FindStringSubmatch(title); m != nil {
		p.ReleaseGroup = m[1]
	}
	for _, lm := range reLanguage.FindAllStringSubmatch(title, -1) {
		p.Languages = append(p.Languages, strings.ToLower(lm[1]))
	}
	p.Title = cleanTitle(title, kind)
	return p
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func canonicalEdition(m string) string {
	s := strings.ToLower(m)
	switch {
	case strings.HasPrefix(s, "director"):
		return "Director's Cut"
	case s == "extended":
		return "Extended"
	case s == "unrated":
		return "Unrated"
	case s == "remastered":
		return "Remastered"
	case s == "imax":
		return "IMAX"
	case s == "theatrical":
		return "Theatrical"
	}
	return m
}
```
Update `cleanTitle` to take `kind` and cut at season/year markers too:
```go
func cleanTitle(title string, kind provider.MediaKind) string {
	cut := len(title)
	consider := func(loc []int) {
		if loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}
	consider(reResolution.FindStringIndex(title))
	for _, sp := range sourcePatterns {
		consider(sp.re.FindStringIndex(title))
	}
	if kind == provider.KindTV {
		consider(reSeasonEp.FindStringIndex(title))
	} else {
		consider(reYear.FindStringIndex(title))
	}
	name := title[:cut]
	name = strings.NewReplacer(".", " ", "_", " ").Replace(name)
	return strings.TrimSpace(name)
}
```
Delete the old one-arg `cleanTitle` and the now-unused `_ = kind`.

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/parsing/ -count=1`
Expected: PASS (both Task 1 and Task 2 tests). Adjust regexes if a case fails; do not change asserted expectations.

- [ ] **Step 5: Commit**

```bash
git add internal/parsing/parser.go internal/parsing/parser_test.go
git commit -m "feat: parse TV/movie identity, release group, and language"
```

---

### Task 3: Quality definitions + Resolve

**Files:**
- Create: `internal/quality/definitions.go`
- Create: `internal/quality/decision.go`
- Create: `internal/quality/definitions_test.go`

**Interfaces:**
- Produces: `quality.QualityDefinition{ID int; Name string; Source parsing.Source; Resolution parsing.Resolution; Rank int}`; `quality.Definitions() []QualityDefinition` (ranked, stable order); `quality.DefinitionByID(id int) (QualityDefinition, bool)`; `quality.Resolve(p parsing.ParsedRelease) QualityDefinition`. Quality id `0` is `Unknown`.
- Consumes: `parsing.Source`, `parsing.Resolution`, `parsing.ParsedRelease` from Task 1.

- [ ] **Step 1: Write the failing test**

Create `internal/quality/definitions_test.go`:
```go
package quality

import (
	"testing"

	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestResolveKnown(t *testing.T) {
	cases := []struct {
		title    string
		wantName string
	}{
		{"Show.S01E01.1080p.BluRay.x264-GRP", "Bluray-1080p"},
		{"Show.S01E01.720p.WEB-DL.x264-GRP", "WEBDL-720p"},
		{"Show.S01E01.2160p.HDTV.x265-GRP", "HDTV-2160p"},
		{"Show.S01E01.480p.SDTV-GRP", "SDTV"},
	}
	for _, c := range cases {
		p := parsing.Parse(c.title, provider.KindTV)
		got := Resolve(p)
		if got.Name != c.wantName {
			t.Errorf("Resolve(%q) = %q, want %q", c.title, got.Name, c.wantName)
		}
	}
}

func TestResolveUnknown(t *testing.T) {
	got := Resolve(parsing.Parse("random text with no quality", provider.KindMovie))
	if got.ID != 0 || got.Name != "Unknown" {
		t.Fatalf("expected Unknown, got id=%d name=%q", got.ID, got.Name)
	}
}

func TestDefinitionsRankedAndLookup(t *testing.T) {
	defs := Definitions()
	if len(defs) == 0 {
		t.Fatal("no definitions")
	}
	for i := 1; i < len(defs); i++ {
		if defs[i].Rank < defs[i-1].Rank {
			t.Fatalf("definitions not rank-ordered at %d", i)
		}
	}
	d, ok := DefinitionByID(defs[len(defs)-1].ID)
	if !ok || d.Name != defs[len(defs)-1].Name {
		t.Fatal("DefinitionByID mismatch")
	}
}
```

Note: `SDTV` has no resolution token in the title; Resolve treats source-unknown + `480p` OR an explicit SDTV as `SDTV`. To keep the `480p.SDTV` case deterministic, add an `SDTV` source alias in the test corpus handling — see Step 3 (the `SDTV` definition maps `SourceUnknown/HDTV + Res480p` → SDTV when no better source matched). If the `480p.SDTV-GRP` case is awkward, the implementer may change that one title to `Show.S01E01.480p.HDTV.x264-GRP` expecting `HDTV` — but keep the other three assertions.

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -run TestResolve -count=1`
Expected: FAIL — `undefined: Resolve` / `Definitions`.

- [ ] **Step 3: Create the definitions registry**

Create `internal/quality/definitions.go`:
```go
// Package quality defines the built-in ranked quality set, the stateless
// decision engine, quality profiles, and their REST surface.
package quality

import "github.com/hellboundg/nexus/internal/parsing"

// QualityDefinition is one entry in the fixed, code-defined quality ladder.
// IDs and ranks are stable; profiles reference definitions by ID.
type QualityDefinition struct {
	ID         int                `json:"id"`
	Name       string             `json:"name"`
	Source     parsing.Source     `json:"source"`
	Resolution parsing.Resolution `json:"resolution"`
	Rank       int                `json:"rank"`
}

// definitions is the global ladder, low→high. Rank == slice index.
var definitions = buildDefinitions()

func buildDefinitions() []QualityDefinition {
	rows := []struct {
		id   int
		name string
		src  parsing.Source
		res  parsing.Resolution
	}{
		{0, "Unknown", parsing.SourceUnknown, parsing.ResUnknown},
		{1, "SDTV", parsing.SourceHDTV, parsing.Res480p},
		{2, "WEBDL-480p", parsing.SourceWEBDL, parsing.Res480p},
		{3, "Bluray-480p", parsing.SourceBluray, parsing.Res480p},
		{4, "HDTV-720p", parsing.SourceHDTV, parsing.Res720p},
		{5, "HDTV-1080p", parsing.SourceHDTV, parsing.Res1080p},
		{6, "WEBDL-720p", parsing.SourceWEBDL, parsing.Res720p},
		{7, "WEBDL-1080p", parsing.SourceWEBDL, parsing.Res1080p},
		{8, "Bluray-720p", parsing.SourceBluray, parsing.Res720p},
		{9, "Bluray-1080p", parsing.SourceBluray, parsing.Res1080p},
		{10, "HDTV-2160p", parsing.SourceHDTV, parsing.Res2160p},
		{11, "WEBDL-2160p", parsing.SourceWEBDL, parsing.Res2160p},
		{12, "Bluray-2160p", parsing.SourceBluray, parsing.Res2160p},
	}
	defs := make([]QualityDefinition, len(rows))
	for i, r := range rows {
		defs[i] = QualityDefinition{ID: r.id, Name: r.name, Source: r.src, Resolution: r.res, Rank: i}
	}
	return defs
}

// Definitions returns the ranked ladder (low→high).
func Definitions() []QualityDefinition {
	out := make([]QualityDefinition, len(definitions))
	copy(out, definitions)
	return out
}

// DefinitionByID looks up a definition by its stable ID.
func DefinitionByID(id int) (QualityDefinition, bool) {
	for _, d := range definitions {
		if d.ID == id {
			return d, true
		}
	}
	return QualityDefinition{}, false
}
```

Create `internal/quality/decision.go`:
```go
package quality

import "github.com/hellboundg/nexus/internal/parsing"

// Resolve maps a parsed release's (Source, Resolution) to a built-in quality
// definition. WEBRip is treated as WEBDL; unknown source with a known
// resolution falls back to the HDTV-tier of that resolution; anything
// unresolvable is Unknown (ID 0).
func Resolve(p parsing.ParsedRelease) QualityDefinition {
	src := p.Source
	if src == parsing.SourceWEBRip {
		src = parsing.SourceWEBDL
	}
	// exact source+resolution match
	for _, d := range definitions {
		if d.ID == 0 {
			continue
		}
		if d.Source == src && d.Resolution == p.Resolution {
			return d
		}
	}
	// unknown/other source but known resolution → HDTV-tier fallback
	if p.Resolution != parsing.ResUnknown {
		for _, d := range definitions {
			if d.Source == parsing.SourceHDTV && d.Resolution == p.Resolution {
				return d
			}
		}
	}
	return definitions[0] // Unknown
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -count=1`
Expected: PASS. (The `480p.SDTV` title resolves to SDTV via the HDTV-480p mapping since no explicit source matches and 480p falls back to the HDTV-tier which is `SDTV`.)

- [ ] **Step 5: Commit**

```bash
git add internal/quality/definitions.go internal/quality/decision.go internal/quality/definitions_test.go
git commit -m "feat: add built-in quality definitions and Resolve"
```

---

### Task 4: quality_profiles store + migration 0005

**Files:**
- Create: `internal/core/database/migrations/0005_quality.sql`
- Create: `internal/core/store/quality_store.go`
- Create: `internal/core/store/quality_store_test.go`

**Interfaces:**
- Produces: `store.QualityProfileItem{QualityID int; Allowed bool}`; `store.QualityProfile{ID int64; Name string; CutoffQualityID int; UpgradeAllowed bool; Items []QualityProfileItem; CreatedAt time.Time}`; `store.ErrProfileInUse`; methods `CreateQualityProfile`, `GetQualityProfile`, `ListQualityProfiles`, `UpdateQualityProfile`, `DeleteQualityProfile`.
- Consumes: existing `store.Store`, `store.ErrNotFound`, migration-runner convention (numbered `.sql` files embedded and applied by `database.Migrate`).

- [ ] **Step 1: Write the failing test**

Create `internal/core/store/quality_store_test.go`:
```go
package store

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
)

func newQualityTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestQualityProfileCRUD(t *testing.T) {
	st := newQualityTestStore(t)
	ctx := context.Background()

	p := QualityProfile{
		Name:            "HD",
		CutoffQualityID: 9,
		UpgradeAllowed:  true,
		Items:           []QualityProfileItem{{QualityID: 7, Allowed: true}, {QualityID: 9, Allowed: true}},
	}
	created, err := st.CreateQualityProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() {
		t.Fatalf("bad created: %+v", created)
	}
	got, err := st.GetQualityProfile(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "HD" || len(got.Items) != 2 || got.Items[1].QualityID != 9 || !got.UpgradeAllowed {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	got.Name = "HD-updated"
	got.Items = append(got.Items, QualityProfileItem{QualityID: 12, Allowed: false})
	if err := st.UpdateQualityProfile(ctx, got); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := st.GetQualityProfile(ctx, created.ID)
	if reloaded.Name != "HD-updated" || len(reloaded.Items) != 3 {
		t.Fatalf("update not persisted: %+v", reloaded)
	}

	list, err := st.ListQualityProfiles(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v (err %v)", list, err)
	}

	if err := st.DeleteQualityProfile(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetQualityProfile(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteQualityProfileInUse(t *testing.T) {
	st := newQualityTestStore(t)
	ctx := context.Background()
	created, err := st.CreateQualityProfile(ctx, QualityProfile{Name: "P", CutoffQualityID: 9, Items: []QualityProfileItem{{QualityID: 9, Allowed: true}}})
	if err != nil {
		t.Fatal(err)
	}
	// Reference it from a series (root folder nullable, quality profile set).
	if _, err := st.CreateSeries(ctx, Series{TMDBID: 1, Title: "S"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesQualityProfileID(ctx, 1, &created.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteQualityProfile(ctx, created.ID); !errors.Is(err, ErrProfileInUse) {
		t.Fatalf("expected ErrProfileInUse, got %v", err)
	}
}
```

Note: `SetSeriesQualityProfileID` and `CreateSeries` already exist / are added in Task 8; this test also compiles once Task 8's setter exists. Since store tests compile per-package, add a minimal `SetSeriesQualityProfileID` stub in this task's store file (Task 8 keeps the same signature) — see Step 3. `CreateSeries` already exists from 4a.

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run TestQualityProfile -count=1`
Expected: FAIL — `undefined: QualityProfile` / migration table missing.

- [ ] **Step 3: Create the migration and store**

Create `internal/core/database/migrations/0005_quality.sql`:
```sql
CREATE TABLE quality_profiles (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT NOT NULL UNIQUE,
    cutoff_quality_id INTEGER NOT NULL,
    upgrade_allowed   INTEGER NOT NULL DEFAULT 1,
    items             TEXT NOT NULL,
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

Create `internal/core/store/quality_store.go`:
```go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ErrProfileInUse is returned by DeleteQualityProfile when a series or movie
// still references the profile.
var ErrProfileInUse = errors.New("store: quality profile in use")

// QualityProfileItem is one quality's membership/ordering in a profile.
type QualityProfileItem struct {
	QualityID int  `json:"qualityId"`
	Allowed   bool `json:"allowed"`
}

// QualityProfile is a user-defined quality selection. Items is stored as a
// JSON array capturing both the allowed set and the ordering.
type QualityProfile struct {
	ID              int64                `json:"id"`
	Name            string               `json:"name"`
	CutoffQualityID int                  `json:"cutoffQualityId"`
	UpgradeAllowed  bool                 `json:"upgradeAllowed"`
	Items           []QualityProfileItem `json:"items"`
	CreatedAt       time.Time            `json:"createdAt"`
}

func (s *Store) CreateQualityProfile(ctx context.Context, p QualityProfile) (QualityProfile, error) {
	itemsJSON, err := json.Marshal(p.Items)
	if err != nil {
		return QualityProfile{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO quality_profiles (name, cutoff_quality_id, upgrade_allowed, items) VALUES (?, ?, ?, ?)`,
		p.Name, p.CutoffQualityID, boolToInt(p.UpgradeAllowed), string(itemsJSON))
	if err != nil {
		return QualityProfile{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetQualityProfile(ctx, id)
}

func (s *Store) GetQualityProfile(ctx context.Context, id int64) (QualityProfile, error) {
	var (
		p         QualityProfile
		upgrade   int
		itemsJSON string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, cutoff_quality_id, upgrade_allowed, items, created_at FROM quality_profiles WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.CutoffQualityID, &upgrade, &itemsJSON, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return QualityProfile{}, ErrNotFound
	}
	if err != nil {
		return QualityProfile{}, err
	}
	p.UpgradeAllowed = upgrade != 0
	if err := json.Unmarshal([]byte(itemsJSON), &p.Items); err != nil {
		return QualityProfile{}, err
	}
	return p, nil
}

func (s *Store) ListQualityProfiles(ctx context.Context) ([]QualityProfile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, cutoff_quality_id, upgrade_allowed, items, created_at FROM quality_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QualityProfile
	for rows.Next() {
		var (
			p         QualityProfile
			upgrade   int
			itemsJSON string
		)
		if err := rows.Scan(&p.ID, &p.Name, &p.CutoffQualityID, &upgrade, &itemsJSON, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.UpgradeAllowed = upgrade != 0
		if err := json.Unmarshal([]byte(itemsJSON), &p.Items); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateQualityProfile(ctx context.Context, p QualityProfile) error {
	itemsJSON, err := json.Marshal(p.Items)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE quality_profiles SET name = ?, cutoff_quality_id = ?, upgrade_allowed = ?, items = ? WHERE id = ?`,
		p.Name, p.CutoffQualityID, boolToInt(p.UpgradeAllowed), string(itemsJSON), p.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteQualityProfile(ctx context.Context, id int64) error {
	var refs int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM series WHERE quality_profile_id = ?) +
		        (SELECT COUNT(*) FROM movies WHERE quality_profile_id = ?)`, id, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrProfileInUse
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM quality_profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

Note: `SetSeriesQualityProfileID` used by the in-use test is defined in Task 8's `media_store.go` changes. To keep this package compiling now, Task 8 is where the setter lands; if running tasks strictly in order, move the `TestDeleteQualityProfileInUse` body's `SetSeriesQualityProfileID` call to Task 8 OR add the setter here. **Decision: add the two setters here** (they are store methods on series/movies but small), and Task 8 consumes them. Append to `quality_store.go`:
```go
// SetSeriesQualityProfileID sets or clears a series' quality profile (nil clears).
func (s *Store) SetSeriesQualityProfileID(ctx context.Context, seriesID int64, profileID *int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE series SET quality_profile_id = ? WHERE id = ?`, profileID, seriesID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetMovieQualityProfileID sets or clears a movie's quality profile (nil clears).
func (s *Store) SetMovieQualityProfileID(ctx context.Context, movieID int64, profileID *int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE movies SET quality_profile_id = ? WHERE id = ?`, profileID, movieID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/core/store/ -run 'TestQualityProfile|TestDeleteQualityProfile' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/database/migrations/0005_quality.sql internal/core/store/quality_store.go internal/core/store/quality_store_test.go
git commit -m "feat: add quality_profiles store, migration 0005, and profile-id setters"
```

---

### Task 5: Decision engine — Decide + Compare

**Files:**
- Modify: `internal/quality/decision.go`
- Create: `internal/quality/decision_test.go`

**Interfaces:**
- Produces: `quality.Decision{Accepted bool; Quality QualityDefinition; Score int; RejectionReason string}`; `quality.Decide(p parsing.ParsedRelease, profile store.QualityProfile) Decision`; `quality.Compare(a, b parsing.ParsedRelease, profile store.QualityProfile) int`.
- Consumes: `Resolve`, `DefinitionByID` (Task 3); `store.QualityProfile`, `store.QualityProfileItem` (Task 4); `parsing.ParsedRelease` (Task 1).

- [ ] **Step 1: Write the failing test**

Create `internal/quality/decision_test.go`:
```go
package quality

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

// profile allows WEBDL-720p(7) and Bluray-1080p(9), ranked in that order.
func hdProfile() store.QualityProfile {
	return store.QualityProfile{
		Name:            "HD",
		CutoffQualityID: 9,
		UpgradeAllowed:  true,
		Items: []store.QualityProfileItem{
			{QualityID: 7, Allowed: true},
			{QualityID: 9, Allowed: true},
			{QualityID: 12, Allowed: false},
		},
	}
}

func TestDecideAcceptAndReject(t *testing.T) {
	prof := hdProfile()

	accept := Decide(parsing.Parse("Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV), prof)
	if !accept.Accepted || accept.Quality.Name != "Bluray-1080p" {
		t.Fatalf("expected accept Bluray-1080p, got %+v", accept)
	}

	reject := Decide(parsing.Parse("Show.S01E01.2160p.BluRay.x265-GRP", provider.KindTV), prof)
	if reject.Accepted || reject.RejectionReason == "" {
		t.Fatalf("expected reject for Bluray-2160p (not allowed), got %+v", reject)
	}

	unknown := Decide(parsing.Parse("random", provider.KindMovie), prof)
	if unknown.Accepted {
		t.Fatalf("expected reject for Unknown, got %+v", unknown)
	}
}

func TestDecideScoreReflectsProfileOrder(t *testing.T) {
	prof := hdProfile()
	web := Decide(parsing.Parse("Show.S01E01.720p.WEB-DL.x264-GRP", provider.KindTV), prof)
	blu := Decide(parsing.Parse("Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV), prof)
	if !(blu.Score > web.Score) {
		t.Fatalf("Bluray-1080p (%d) should outscore WEBDL-720p (%d) per profile order", blu.Score, web.Score)
	}
}

func TestCompare(t *testing.T) {
	prof := hdProfile()
	web := parsing.Parse("Show.S01E01.720p.WEB-DL.x264-GRP", provider.KindTV)
	blu := parsing.Parse("Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV)
	if Compare(blu, web, prof) != 1 {
		t.Fatal("Bluray should beat WEBDL")
	}
	if Compare(web, blu, prof) != -1 {
		t.Fatal("WEBDL should lose to Bluray")
	}

	// same quality, one is a REPACK → repack wins on revision tiebreak
	plain := parsing.Parse("Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV)
	repack := parsing.Parse("Show.S01E01.1080p.BluRay.REPACK.x264-GRP", provider.KindTV)
	if Compare(repack, plain, prof) != 1 {
		t.Fatal("REPACK should beat plain on revision tiebreak")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -run 'TestDecide|TestCompare' -count=1`
Expected: FAIL — `undefined: Decide` / `Compare` / `Decision`.

- [ ] **Step 3: Add Decide and Compare**

Append to `internal/quality/decision.go`:
```go
import (
	// keep the existing parsing import; add:
	"github.com/hellboundg/nexus/internal/core/store"
)

// Decision is the result of evaluating one release against a profile.
type Decision struct {
	Accepted        bool              `json:"accepted"`
	Quality         QualityDefinition `json:"quality"`
	Score           int               `json:"score"`
	RejectionReason string            `json:"rejectionReason,omitempty"`
}

// profileRank returns the 0-based position of a quality within the profile's
// allowed items (higher = more preferred) and whether it is allowed at all.
func profileRank(profile store.QualityProfile, qualityID int) (rank int, allowed bool) {
	for i, it := range profile.Items {
		if it.QualityID == qualityID {
			return i, it.Allowed
		}
	}
	return -1, false
}

// revisionBonus is a small additive score so a PROPER/REPACK outscores the
// same quality without one, but never enough to jump a quality tier.
func revisionBonus(rev parsing.Revision) int {
	if rev.Version > 1 {
		return 1
	}
	return 0
}

// Decide evaluates a parsed release against a profile. A release is accepted
// only if its resolved quality is present AND allowed in the profile. Score is
// driven by the quality's rank within the profile (×10 to leave room for the
// revision bonus), so a user's ordering — not the global ladder — governs.
func Decide(p parsing.ParsedRelease, profile store.QualityProfile) Decision {
	q := Resolve(p)
	rank, allowed := profileRank(profile, q.ID)
	if !allowed {
		return Decision{Accepted: false, Quality: q, RejectionReason: "quality not in profile"}
	}
	return Decision{Accepted: true, Quality: q, Score: rank*10 + revisionBonus(p.Revision)}
}

// Compare orders two releases for a profile: +1 if a is better, -1 if b is
// better, 0 if indistinguishable. Higher profile-ranked quality wins; ties
// break on revision version. Releases whose quality is absent from the profile
// rank below any present quality.
func Compare(a, b parsing.ParsedRelease, profile store.QualityProfile) int {
	ra, _ := profileRank(profile, Resolve(a).ID)
	rb, _ := profileRank(profile, Resolve(b).ID)
	switch {
	case ra != rb && ra > rb:
		return 1
	case ra != rb && ra < rb:
		return -1
	}
	switch {
	case a.Revision.Version > b.Revision.Version:
		return 1
	case a.Revision.Version < b.Revision.Version:
		return -1
	}
	return 0
}
```
Note: merge the new `store` import into the file's existing import block (Task 3 created it with only the `parsing` import — change it to a grouped import of both).

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -count=1`
Expected: PASS (Task 3 + Task 5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/quality/decision.go internal/quality/decision_test.go
git commit -m "feat: add stateless Decide and Compare decision engine"
```

---

### Task 6: Quality profiles service (CRUD + validation)

**Files:**
- Create: `internal/quality/service.go`
- Create: `internal/quality/service_test.go`

**Interfaces:**
- Produces: `quality.Service`; `quality.NewService(st *store.Store) *Service`; methods `CreateProfile(ctx, store.QualityProfile) (store.QualityProfile, error)`, `GetProfile`, `ListProfiles`, `UpdateProfile`, `DeleteProfile`; validation error `quality.ErrInvalidProfile`.
- Consumes: `store.*` profile methods (Task 4), `DefinitionByID` (Task 3).

- [ ] **Step 1: Write the failing test**

Create `internal/quality/service_test.go`:
```go
package quality

import (
	"context"
	"errors"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newQualityService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	return NewService(st), st
}

func validProfile() store.QualityProfile {
	return store.QualityProfile{
		Name:            "HD",
		CutoffQualityID: 9,
		UpgradeAllowed:  true,
		Items:           []store.QualityProfileItem{{QualityID: 7, Allowed: true}, {QualityID: 9, Allowed: true}},
	}
}

func TestServiceCreateValidatesName(t *testing.T) {
	svc, _ := newQualityService(t)
	p := validProfile()
	p.Name = ""
	if _, err := svc.CreateProfile(context.Background(), p); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("empty name should be invalid, got %v", err)
	}
}

func TestServiceCreateValidatesCutoffInAllowedSet(t *testing.T) {
	svc, _ := newQualityService(t)
	p := validProfile()
	p.CutoffQualityID = 12 // not in items
	if _, err := svc.CreateProfile(context.Background(), p); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("cutoff outside allowed set should be invalid, got %v", err)
	}
}

func TestServiceCreateValidatesRealDefinitions(t *testing.T) {
	svc, _ := newQualityService(t)
	p := validProfile()
	p.Items = append(p.Items, store.QualityProfileItem{QualityID: 999, Allowed: true})
	if _, err := svc.CreateProfile(context.Background(), p); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("unknown quality id should be invalid, got %v", err)
	}
}

func TestServiceCreateAndGet(t *testing.T) {
	svc, _ := newQualityService(t)
	created, err := svc.CreateProfile(context.Background(), validProfile())
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetProfile(context.Background(), created.ID)
	if err != nil || got.Name != "HD" {
		t.Fatalf("get mismatch: %+v err=%v", got, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -run TestService -count=1`
Expected: FAIL — `undefined: NewService` / `ErrInvalidProfile`.

- [ ] **Step 3: Create the service**

Create `internal/quality/service.go`:
```go
package quality

import (
	"context"
	"errors"
	"strings"

	"github.com/hellboundg/nexus/internal/core/store"
)

// ErrInvalidProfile is returned when a profile fails validation.
var ErrInvalidProfile = errors.New("quality: invalid profile")

// Service owns quality-profile CRUD with validation over the store.
type Service struct {
	store *store.Store
}

func NewService(st *store.Store) *Service { return &Service{store: st} }

func validateProfile(p store.QualityProfile) error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrInvalidProfile
	}
	if len(p.Items) == 0 {
		return ErrInvalidProfile
	}
	allowed := map[int]bool{}
	for _, it := range p.Items {
		if _, ok := DefinitionByID(it.QualityID); !ok {
			return ErrInvalidProfile
		}
		if it.Allowed {
			allowed[it.QualityID] = true
		}
	}
	if _, ok := DefinitionByID(p.CutoffQualityID); !ok {
		return ErrInvalidProfile
	}
	if !allowed[p.CutoffQualityID] {
		return ErrInvalidProfile
	}
	return nil
}

func (s *Service) CreateProfile(ctx context.Context, p store.QualityProfile) (store.QualityProfile, error) {
	if err := validateProfile(p); err != nil {
		return store.QualityProfile{}, err
	}
	return s.store.CreateQualityProfile(ctx, p)
}

func (s *Service) GetProfile(ctx context.Context, id int64) (store.QualityProfile, error) {
	return s.store.GetQualityProfile(ctx, id)
}

func (s *Service) ListProfiles(ctx context.Context) ([]store.QualityProfile, error) {
	return s.store.ListQualityProfiles(ctx)
}

func (s *Service) UpdateProfile(ctx context.Context, p store.QualityProfile) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	return s.store.UpdateQualityProfile(ctx, p)
}

func (s *Service) DeleteProfile(ctx context.Context, id int64) error {
	return s.store.DeleteQualityProfile(ctx, id)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/quality/service.go internal/quality/service_test.go
git commit -m "feat: add quality profile service with validation"
```

---

### Task 7: Quality REST API (definitions, profiles, /parse)

**Files:**
- Create: `internal/quality/api.go`
- Create: `internal/quality/api_test.go`

**Interfaces:**
- Produces: `quality.API`; `quality.NewAPI(svc *Service) *API`; `(*API).Mount(r chi.Router)` registering `GET /quality/definitions`, `GET|POST /qualityprofile`, `GET|PUT|DELETE /qualityprofile/{id}`, `POST /parse`.
- Consumes: `Service` (Task 6), `Definitions` (Task 3), `Parse` (Task 1), `Resolve`/`Decide` (Task 3/5), `store.QualityProfile` (Task 4), `provider.MediaKind`, `api.WriteJSON`/`api.WriteError`.

- [ ] **Step 1: Write the failing test**

Create `internal/quality/api_test.go`:
```go
package quality

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/store"
)

func newTestAPI(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	r := chi.NewRouter()
	NewAPI(NewService(st)).Mount(r)
	return r, st
}

func TestAPIDefinitions(t *testing.T) {
	r, _ := newTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/quality/definitions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Bluray-1080p") {
		t.Fatalf("definitions status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIProfileCRUDAndValidation(t *testing.T) {
	r, _ := newTestAPI(t)

	// invalid (empty items) → 400
	bad := `{"name":"X","cutoffQualityId":9,"upgradeAllowed":true,"items":[]}`
	req := httptest.NewRequest(http.MethodPost, "/qualityprofile", strings.NewReader(bad))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d want 400", w.Code)
	}

	// valid → 201
	good := `{"name":"HD","cutoffQualityId":9,"upgradeAllowed":true,"items":[{"qualityId":7,"allowed":true},{"qualityId":9,"allowed":true}]}`
	req = httptest.NewRequest(http.MethodPost, "/qualityprofile", strings.NewReader(good))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("valid create status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIParsePreview(t *testing.T) {
	r, _ := newTestAPI(t)
	body := `{"title":"Show.S01E01.1080p.BluRay.x264-GRP","kind":"tv"}`
	req := httptest.NewRequest(http.MethodPost, "/parse", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("parse status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Bluray-1080p") {
		t.Fatalf("parse body missing resolved quality: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -run TestAPI -count=1`
Expected: FAIL — `undefined: NewAPI`.

- [ ] **Step 3: Create the API**

Create `internal/quality/api.go`:
```go
package quality

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Mount(r chi.Router) {
	r.Get("/quality/definitions", a.definitions)
	r.Route("/qualityprofile", func(r chi.Router) {
		r.Get("/", a.listProfiles)
		r.Post("/", a.createProfile)
		r.Get("/{id}", a.getProfile)
		r.Put("/{id}", a.updateProfile)
		r.Delete("/{id}", a.deleteProfile)
	})
	r.Post("/parse", a.parse)
}

func profileID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func writeProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidProfile):
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "profile not found")
	case errors.Is(err, store.ErrProfileInUse):
		api.WriteError(w, http.StatusConflict, "conflict", "profile is in use")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (a *API) definitions(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, Definitions())
}

func (a *API) listProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.ListProfiles(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list profiles")
		return
	}
	if rows == nil {
		rows = []store.QualityProfile{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) createProfile(w http.ResponseWriter, r *http.Request) {
	var p store.QualityProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	created, err := a.svc.CreateProfile(r.Context(), p)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, created)
}

func (a *API) getProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	p, err := a.svc.GetProfile(r.Context(), id)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, p)
}

func (a *API) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	var p store.QualityProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	p.ID = id
	if err := a.svc.UpdateProfile(r.Context(), p); err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	if err := a.svc.DeleteProfile(r.Context(), id); err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type parseBody struct {
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	ProfileID *int64 `json:"profileId"`
}

func (a *API) parse(w http.ResponseWriter, r *http.Request) {
	var b parseBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if b.Title == "" {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "title is required")
		return
	}
	kind := provider.KindMovie
	if b.Kind == "tv" {
		kind = provider.KindTV
	}
	parsed := parsing.Parse(b.Title, kind)
	resolved := Resolve(parsed)

	resp := map[string]any{"parsed": parsed, "quality": resolved}
	if b.ProfileID != nil {
		prof, err := a.svc.GetProfile(r.Context(), *b.ProfileID)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		d := Decide(parsed, prof)
		resp["decision"] = d
	}
	api.WriteJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/quality/ -count=1`
Expected: PASS (all quality tests).

- [ ] **Step 5: Commit**

```bash
git add internal/quality/api.go internal/quality/api_test.go
git commit -m "feat: add quality REST API (definitions, profile CRUD, parse preview)"
```

---

### Task 8: Media profile assignment + RowsAffected fix

**Files:**
- Modify: `internal/media/media.go`
- Modify: `internal/media/api.go`
- Modify: `internal/media/service_test.go` (or a new `assign_test.go`)
- Modify: `internal/media/api_test.go`

**Interfaces:**
- Produces: media `Service` methods `SetSeriesQualityProfile(ctx, seriesID, profileID int64) error` and `SetMovieQualityProfile(ctx, movieID, profileID int64) error` (validate the profile exists via `store.GetQualityProfile`, then call `store.SetSeriesQualityProfileID`/`SetMovieQualityProfileID`); new routes `PUT /series/{id}/qualityprofile` and `PUT /movies/{id}/qualityprofile`. Also hardens `SetSeriesMonitored`/`SetMovieMonitored` to return `ErrNotFound` (→404) and skip the WS emit when the id is missing.
- Consumes: `store.GetQualityProfile`, `store.SetSeriesQualityProfileID`, `store.SetMovieQualityProfileID` (Task 4); existing media `Service`/`API`/error-mapping (`writeMediaError`).

- [ ] **Step 1: Write the failing tests**

Confirm the existing `SetSeriesMonitored` signature in `internal/media/media.go` and whether it currently returns nil on a missing id (the 4a backlog says it does). Add to `internal/media/api_test.go`:
```go
func TestAPIAssignQualityProfile(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	r, st := newTestAPI(t, fp)

	// create a profile directly in the store
	prof, err := st.CreateQualityProfile(context.Background(), store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// add a series to assign to
	addReq := httptest.NewRequest(http.MethodPost, "/series", strings.NewReader(`{"tmdbId":100,"monitorOption":"all"}`))
	aw := httptest.NewRecorder()
	r.ServeHTTP(aw, addReq)
	if aw.Code != http.StatusCreated {
		t.Fatalf("add series status=%d", aw.Code)
	}
	var se store.Series
	_ = json.Unmarshal(aw.Body.Bytes(), &se)

	// assign
	body := `{"qualityProfileId":` + strconv.FormatInt(prof.ID, 10) + `}`
	req := httptest.NewRequest(http.MethodPut, "/series/"+strconv.FormatInt(se.ID, 10)+"/qualityprofile", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assign status=%d body=%s", w.Code, w.Body.String())
	}

	// assign to a missing series → 404
	req = httptest.NewRequest(http.MethodPut, "/series/9999/qualityprofile", strings.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("assign to missing series status=%d want 404", w.Code)
	}
}
```
Ensure `api_test.go` imports `context`, `encoding/json`, `strconv`, and `github.com/hellboundg/nexus/internal/core/store` (add any missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ -run TestAPIAssignQualityProfile -count=1`
Expected: FAIL — route 404 (not registered) / undefined service method.

- [ ] **Step 3: Add the service methods + monitor hardening**

In `internal/media/media.go`, add:
```go
// SetSeriesQualityProfile validates the profile exists, then assigns it.
func (s *Service) SetSeriesQualityProfile(ctx context.Context, seriesID, profileID int64) error {
	if _, err := s.store.GetQualityProfile(ctx, profileID); err != nil {
		return err // store.ErrNotFound → 404 via writeMediaError's store.ErrNotFound path
	}
	pid := profileID
	if err := s.store.SetSeriesQualityProfileID(ctx, seriesID, &pid); err != nil {
		return err
	}
	s.emitSeries(ctx, seriesID)
	return nil
}

// SetMovieQualityProfile validates the profile exists, then assigns it.
func (s *Service) SetMovieQualityProfile(ctx context.Context, movieID, profileID int64) error {
	if _, err := s.store.GetQualityProfile(ctx, profileID); err != nil {
		return err
	}
	pid := profileID
	if err := s.store.SetMovieQualityProfileID(ctx, movieID, &pid); err != nil {
		return err
	}
	s.emitMovie(ctx, movieID)
	return nil
}
```
Harden the existing monitor setters (4a backlog item a). Change `SetSeriesMonitored` and `SetMovieMonitored` so the underlying store update's `RowsAffected == 0` returns `store.ErrNotFound` and NO event is emitted. If the current store setter (`SetSeriesMonitoredFlag` or equivalent) does not surface RowsAffected, update it to return `ErrNotFound` on zero rows (mirror `SetSeriesQualityProfileID`), and have the service return before `emitSeries`. Verify the exact current setter names in `media.go`/`media_store.go` before editing.

In `internal/media/api.go`, register the routes inside the existing `series` and `movies` route groups:
```go
		r.Put("/{id}/qualityprofile", a.assignSeriesProfile)
```
(and the movie equivalent `a.assignMovieProfile`), then add the handlers:
```go
type assignProfileBody struct {
	QualityProfileID int64 `json:"qualityProfileId"`
}

func (a *API) assignSeriesProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	var b assignProfileBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.svc.SetSeriesQualityProfile(r.Context(), id, b.QualityProfileID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "series or profile not found")
			return
		}
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) assignMovieProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	var b assignProfileBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.svc.SetMovieQualityProfile(r.Context(), id, b.QualityProfileID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "movie or profile not found")
			return
		}
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```
Confirm `internal/media/api.go` already imports `errors`, `encoding/json`, `store`, and `api` (it does from 4a).

- [ ] **Step 4: Run tests to verify they pass**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./internal/media/ -count=1`
Expected: PASS (existing 4a media tests + the new assignment test; the hardened monitor setters must not break existing monitor tests — if an existing test asserts 200 on a missing id, update it to expect 404, noting the 4a-backlog behavior change).

- [ ] **Step 5: Commit**

```bash
git add internal/media/media.go internal/media/api.go internal/media/api_test.go
git commit -m "feat: assign quality profiles to series/movies; harden monitor setters"
```

---

### Task 9: Composition wiring + full sweep

**Files:**
- Modify: `cmd/nexus/main.go`
- Modify: `cmd/nexus/main_test.go`

**Interfaces:**
- Consumes: `quality.NewService`, `quality.NewAPI`; existing `store`, `api.NewRouter` variadic mounts.
- Produces: a running server that also mounts `/api/v1/quality/definitions`, `/api/v1/qualityprofile`, and `/api/v1/parse`.

- [ ] **Step 1: Extend the run test**

Add to `cmd/nexus/main_test.go`:
```go
func TestRunMountsQualityRoutes(t *testing.T) {
	t.Setenv("NEXUS_DATA_DIR", t.TempDir())
	t.Setenv("NEXUS_PORT", "9598")
	t.Setenv("NEXUS_API_KEY", "testkey")
	t.Setenv("NEXUS_ADMIN_PASSWORD", "adminpw")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()
	defer func() { cancel(); <-errCh }()

	deadline := time.Now().Add(5 * time.Second)
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9598/api/v1/quality/definitions", nil)
		req.Header.Set("X-Api-Key", "testkey")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/quality/definitions status = %d want 200", status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./cmd/nexus/ -run TestRunMountsQualityRoutes -count=1`
Expected: FAIL — 404 (quality not wired).

- [ ] **Step 3: Wire the module into `main.go`**

Add the import `"github.com/hellboundg/nexus/internal/quality"`. After the media construction block (`mediaRefresh := media.NewRefresh(mediaSvc)`), add:
```go
	qualitySvc := quality.NewService(st)
	qualityAPI := quality.NewAPI(qualitySvc)
```
Append `qualityAPI.Mount` to the router's variadic mounts:
```go
	}, web.Handler(), idxAPI.Mount, dlAPI.Mount, mediaAPI.Mount, qualityAPI.Mount)
```
(No new scheduler jobs and no WSForward changes — quality emits no events; profile assignment reuses media's existing events.)

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH"; go test ./cmd/nexus/ -run TestRunMountsQualityRoutes -count=1`
Expected: PASS.

- [ ] **Step 5: Full build + vet + test sweep**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build ./cmd/nexus
go vet ./...
go test ./... -count=1
```
Expected: build succeeds; vet clean; all packages PASS. Remove any built binary (`rm -f nexus nexus.exe`).

- [ ] **Step 6: Verify module boundaries**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
go list -deps ./internal/parsing | grep hellboundg
go list -deps ./internal/quality | grep hellboundg
go list -deps ./internal/media | grep hellboundg
```
Expected: `internal/parsing` → only `internal/core/*`; `internal/quality` → only `internal/parsing` + `internal/core/*`; `internal/media` → only `internal/core/*` (NO `internal/quality`). Any `internal/indexer`/`internal/downloadclient`/`internal/automation` in any of the three is a violation.

- [ ] **Step 7: Commit**

```bash
git add cmd/nexus/main.go cmd/nexus/main_test.go
git commit -m "feat: wire quality engine into composition root"
```

---

## Notes for the executor

- **Model:** use `sonnet` (not `haiku`) for every implementer and reviewer in this repo — haiku hallucinated an unrelated task on a prior sub-project.
- **Reference reading (Task 1-3):** extract *which* attributes and the quality-resolution rules from *arr's `NzbDrone.Core/Parser/QualityParser.cs` and `NzbDrone.Core/Qualities/` (Sonarr and Radarr) at `C:\Users\James\Downloads\Projects\_arr-reference\`. Do NOT transcribe the `Parser.cs` regex mass.
- **Parser tests are the bulk of coverage.** Tasks 1-2 ship a representative corpus; the implementer/reviewer should expand it with additional real titles where cheap, but the asserted cases in this plan must pass unchanged.
- **Task 8 is the one place** that both extends 4a's media package and applies a behavior change (monitor setters now 404 on a missing id). Confirm the exact current setter names in `media.go`/`media_store.go` before editing; update any existing test that asserted the old 200-on-missing behavior.
