# Nexus Web UI — Calendar Slice 5 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an agenda-style Calendar to the Nexus Web UI that lists upcoming monitored episodes and movies grouped by day.

**Architecture:** A small backend addition (two date-window store queries + one `GET /api/v1/calendar` endpoint in the existing media package) feeds a new `web/src/features/calendar/` module: pure grouping/label helpers, a TanStack Query hook with WS-driven invalidation, and an agenda view that replaces the current Calendar placeholder route. No migration, no new top-level wiring.

**Tech Stack:** Go 1.x + chi + SQLite (backend); React 19 + TypeScript + TanStack Query v5 + react-router v6 + Tailwind v4 + Vitest (frontend). Design spec: `docs/superpowers/specs/2026-07-13-nexus-webui-calendar-design.md`.

## Global Constraints

- **Go is not on PATH by default** — prefix every Go command with `export PATH="/c/Program Files/Go/bin:$PATH"`.
- **Always build/test with `CGO_ENABLED=0`.** `-race` is unavailable (no C toolchain) — use `-count=N` for concurrency.
- **Dates are `"YYYY-MM-DD"` strings or `""`.** Window filtering is a lexical string compare; empty dates sort below any real date and are excluded by a `>= start` bound. Never sort or compare dates as numbers.
- **Wire-shape rule (recurring bug class):** the calendar entry `type` discriminator is the literal pair `"episode"` / `"movie"` and MUST agree between Go (`calendarEntry.Type`) and TS (`CalendarEntry` union + `CALENDAR_ENTRY_TYPES`). This is the same class as the 3b `qualityId` int-vs-string and 6-4 `mediaKind "tv"` bugs — pin it with tests on both sides.
- **Timezone rule:** never construct a JS `Date` from a `"YYYY-MM-DD"` string (`new Date("2026-07-15")` is parsed as UTC midnight and reports the previous day in negative-offset zones). Bucket/compare by the raw string; when a `Date` is unavoidable use the numeric constructor `new Date(y, m-1, d)` (local).
- **Monitored-only, server-enforced:** episodes require `series.monitored = 1 AND episode.monitored = 1`; movies require `monitored = 1`.
- **SDD models:** use `sonnet` for implementers AND reviewers (never haiku). Final whole-branch review on opus.
- **Standing rule: ASK before pushing the default branch.**

---

### Task 1: Backend store — date-window queries

**Files:**
- Modify: `internal/core/store/media_store.go` (add `CalendarEpisode` type + `CalendarEpisodes` + `CalendarMovies`, after `SetSeriesEpisodesMonitored` ~line 287 and after `ListMovies` ~line 373 respectively — placement flexible, keep near related code)
- Test: `internal/core/store/media_store_test.go` (add `TestCalendarQueries`)

**Interfaces:**
- Consumes: existing `Store`, `Episode`, `Series`, `Movie`, `movieSelect`, `scanMovieRow`, `rowScanner`.
- Produces:
  - `type CalendarEpisode struct { Episode; SeriesTitle string \`json:"seriesTitle"\` }`
  - `func (s *Store) CalendarEpisodes(ctx context.Context, start, end string) ([]CalendarEpisode, error)`
  - `func (s *Store) CalendarMovies(ctx context.Context, start, end string) ([]Movie, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/core/store/media_store_test.go`:

```go
func TestCalendarQueries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sid, err := s.CreateSeries(ctx, Series{TMDBID: 1, Title: "Show", SortTitle: "show", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	unmon, err := s.CreateSeries(ctx, Series{TMDBID: 2, Title: "Hidden", SortTitle: "hidden", Monitored: false})
	if err != nil {
		t.Fatal(err)
	}

	eps := []Episode{
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "StartEdge", AirDate: "2026-07-10", Monitored: true},
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 2, Title: "EndEdge", AirDate: "2026-07-31", Monitored: true},
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 3, Title: "AfterEnd", AirDate: "2026-08-01", Monitored: true},
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 4, Title: "NoDate", AirDate: "", Monitored: true},
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 5, Title: "Unmon", AirDate: "2026-07-15", Monitored: false},
		{SeriesID: unmon, SeasonNumber: 1, EpisodeNumber: 1, Title: "HiddenSeries", AirDate: "2026-07-15", Monitored: true},
	}
	for _, e := range eps {
		if err := s.UpsertEpisode(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.CalendarEpisodes(ctx, "2026-07-10", "2026-07-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 episodes got %d: %+v", len(got), got)
	}
	if got[0].Title != "StartEdge" || got[1].Title != "EndEdge" {
		t.Fatalf("order/content: %+v", got)
	}
	if got[0].SeriesTitle != "Show" {
		t.Fatalf("series title join: %+v", got[0])
	}

	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 10, Title: "In", SortTitle: "in", Year: 2026, ReleaseDate: "2026-07-20", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 11, Title: "Out", SortTitle: "out", Year: 2026, ReleaseDate: "2026-08-15", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 12, Title: "Unmon", SortTitle: "unmon", Year: 2026, ReleaseDate: "2026-07-20", Monitored: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 13, Title: "NoDate", SortTitle: "nodate", Year: 2026, ReleaseDate: "", Monitored: true}); err != nil {
		t.Fatal(err)
	}

	gm, err := s.CalendarMovies(ctx, "2026-07-10", "2026-07-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(gm) != 1 || gm[0].Title != "In" {
		t.Fatalf("want 1 movie In got %+v", gm)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/core/store/ -run TestCalendarQueries`
Expected: FAIL — `s.CalendarEpisodes` / `s.CalendarMovies` undefined (compile error).

- [ ] **Step 3: Write minimal implementation**

Add to `internal/core/store/media_store.go`:

```go
// CalendarEpisode is an episode joined to its parent series title, for the
// calendar view (episodes carry no series title of their own).
type CalendarEpisode struct {
	Episode
	SeriesTitle string `json:"seriesTitle"`
}

// CalendarEpisodes returns monitored episodes of monitored series whose air_date
// falls within [start, end] inclusive. Dates are "YYYY-MM-DD" strings compared
// lexically; empty air_date rows fall below any start bound and are excluded.
func (s *Store) CalendarEpisodes(ctx context.Context, start, end string) ([]CalendarEpisode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.series_id, e.season_number, e.episode_number, e.tmdb_id,
		       e.title, e.overview, e.air_date, e.monitored, s.title
		FROM episodes e JOIN series s ON e.series_id = s.id
		WHERE s.monitored = 1 AND e.monitored = 1
		  AND e.air_date >= ? AND e.air_date <= ?
		ORDER BY e.air_date, s.sort_title`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarEpisode
	for rows.Next() {
		var c CalendarEpisode
		var m int
		if err := rows.Scan(&c.ID, &c.SeriesID, &c.SeasonNumber, &c.EpisodeNumber, &c.TMDBID,
			&c.Title, &c.Overview, &c.AirDate, &m, &c.SeriesTitle); err != nil {
			return nil, err
		}
		c.Monitored = m != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// CalendarMovies returns monitored movies whose release_date falls within
// [start, end] inclusive, ordered by release_date then sort_title. Empty
// release_date rows are excluded by the >= start bound.
func (s *Store) CalendarMovies(ctx context.Context, start, end string) ([]Movie, error) {
	rows, err := s.db.QueryContext(ctx, movieSelect+`
		WHERE monitored = 1 AND release_date >= ? AND release_date <= ?
		ORDER BY release_date, sort_title`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movie
	for rows.Next() {
		m, err := scanMovieRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/core/store/ -run TestCalendarQueries -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/store/media_store.go internal/core/store/media_store_test.go
git commit -m "feat(6-5): calendar date-window store queries"
```

---

### Task 2: Backend API — `GET /calendar` endpoint

**Files:**
- Modify: `internal/media/api.go` (register route in `Mount`; add `calendar` handler + `calendarEntry` DTO + `validDate`/`entryTitle` helpers; add `sort` and `time` imports)
- Test: `internal/media/api_test.go` (add `TestAPICalendar`)

**Interfaces:**
- Consumes: `a.store.CalendarEpisodes`, `a.store.CalendarMovies`, `a.store.EpisodeFileIDs`, `a.store.MovieFileIDs` (all existing), `api.WriteJSON`, `api.WriteError`.
- Produces: `GET /calendar?start=&end=` → `200` JSON array of `calendarEntry`, `400` on bad/missing dates, `[]` on empty window.

- [ ] **Step 1: Write the failing test**

Add to `internal/media/api_test.go` (`store` and `strings` are already imported; add nothing):

```go
func TestAPICalendar(t *testing.T) {
	fp := &fakeProvider{}
	r, st := newTestAPI(t, fp)
	ctx := context.Background()

	sid, err := st.CreateSeries(ctx, store.Series{TMDBID: 1, Title: "Show", SortTitle: "show", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 2, Title: "Two", AirDate: "2026-07-15", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 9, Title: "Out", AirDate: "2026-09-01", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateMovie(ctx, store.Movie{TMDBID: 2, Title: "Aaa Film", SortTitle: "aaa film", Year: 2026, ReleaseDate: "2026-07-15", Monitored: true}); err != nil {
		t.Fatal(err)
	}

	// happy path: two entries in window, episode sorts before movie on the same date
	req := httptest.NewRequest(http.MethodGet, "/calendar?start=2026-07-10&end=2026-07-31", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries got %d: %s", len(got), w.Body.String())
	}
	if got[0]["type"] != "episode" || got[1]["type"] != "movie" {
		t.Fatalf("same-day order want episode,movie got %v,%v", got[0]["type"], got[1]["type"])
	}
	if got[0]["seriesTitle"] != "Show" {
		t.Fatalf("seriesTitle: %v", got[0])
	}

	// bad date → 400
	req = httptest.NewRequest(http.MethodGet, "/calendar?start=nope&end=2026-07-31", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad start status=%d want 400", w.Code)
	}

	// empty window → [] (never null)
	req = httptest.NewRequest(http.MethodGet, "/calendar?start=2030-01-01&end=2030-01-02", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty window body=%q want []", w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/media/ -run TestAPICalendar`
Expected: FAIL — route not registered (`/calendar` 404) / handler undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/media/api.go`, add `"sort"` and `"time"` to the import block. Register the route inside `Mount`, next to the lookup route:

```go
	r.Get("/media/lookup", a.lookup)
	r.Get("/calendar", a.calendar)
```

Add the handler + helpers (anywhere in the file, e.g. after `lookup`):

```go
type calendarEntry struct {
	Type    string `json:"type"` // "episode" | "movie" — MUST match TS CalendarEntry
	Date    string `json:"date"` // air_date or release_date, "YYYY-MM-DD"
	HasFile bool   `json:"hasFile"`
	// episode-only (zero for movies)
	SeriesID      int64  `json:"seriesId,omitempty"`
	SeriesTitle   string `json:"seriesTitle,omitempty"`
	SeasonNumber  int    `json:"seasonNumber,omitempty"`
	EpisodeNumber int    `json:"episodeNumber,omitempty"`
	EpisodeTitle  string `json:"episodeTitle,omitempty"`
	// movie-only (zero for episodes)
	MovieID    int64  `json:"movieId,omitempty"`
	MovieTitle string `json:"movieTitle,omitempty"`
	Year       int    `json:"year,omitempty"`
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func entryTitle(e calendarEntry) string {
	if e.Type == "episode" {
		return e.SeriesTitle
	}
	return e.MovieTitle
}

func (a *API) calendar(w http.ResponseWriter, r *http.Request) {
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	if !validDate(start) || !validDate(end) {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "start and end must be YYYY-MM-DD")
		return
	}
	eps, err := a.store.CalendarEpisodes(r.Context(), start, end)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load calendar")
		return
	}
	movies, err := a.store.CalendarMovies(r.Context(), start, end)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load calendar")
		return
	}
	epFiles, err := a.store.EpisodeFileIDs(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load calendar")
		return
	}
	movFiles, err := a.store.MovieFileIDs(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load calendar")
		return
	}
	out := make([]calendarEntry, 0, len(eps)+len(movies))
	for _, e := range eps {
		out = append(out, calendarEntry{
			Type: "episode", Date: e.AirDate, HasFile: epFiles[e.ID],
			SeriesID: e.SeriesID, SeriesTitle: e.SeriesTitle,
			SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, EpisodeTitle: e.Title,
		})
	}
	for _, m := range movies {
		out = append(out, calendarEntry{
			Type: "movie", Date: m.ReleaseDate, HasFile: movFiles[m.ID],
			MovieID: m.ID, MovieTitle: m.Title, Year: m.Year,
		})
	}
	// date, then type ("episode" < "movie"), then display title — deterministic
	// same-day order across both entry kinds.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return entryTitle(out[i]) < entryTitle(out[j])
	})
	api.WriteJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && CGO_ENABLED=0 go test ./internal/media/ -run TestAPICalendar -count=1`
Expected: PASS. Then `CGO_ENABLED=0 go vet ./internal/media/` — clean.

- [ ] **Step 5: Commit**

```bash
git add internal/media/api.go internal/media/api_test.go
git commit -m "feat(6-5): GET /calendar endpoint (merged, date-sorted, hasFile)"
```

---

### Task 3: Frontend — types + pure resolve helpers

**Files:**
- Create: `web/src/features/calendar/types.ts`
- Create: `web/src/features/calendar/resolve.ts`
- Test: `web/src/features/calendar/resolve.test.ts`

**Interfaces:**
- Produces (types.ts): `CalendarEntry` (discriminated union on `type`), `CALENDAR_ENTRY_TYPES`.
- Produces (resolve.ts): `DayBucket`, `toLocalISODate(d: Date): string`, `windowRange(today: Date, days?: number): {start,end}`, `addDays(date: string, n: number): string`, `dayLabel(date: string, today: string): string`, `groupByDay(entries: CalendarEntry[], today: string): DayBucket[]`, `entryLink(e: CalendarEntry): string`, `shouldRefresh(type: string): boolean`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/calendar/resolve.test.ts`:

```ts
import { describe, it, expect } from "vitest"
import type { CalendarEntry } from "./types"
import { CALENDAR_ENTRY_TYPES } from "./types"
import { groupByDay, dayLabel, entryLink, windowRange, toLocalISODate, addDays, shouldRefresh } from "./resolve"

const ep = (over: Partial<Extract<CalendarEntry, { type: "episode" }>> = {}): CalendarEntry => ({
  type: "episode", date: "2026-07-15", hasFile: false,
  seriesId: 1, seriesTitle: "Show", seasonNumber: 2, episodeNumber: 4, episodeTitle: "T", ...over,
})
const mv = (over: Partial<Extract<CalendarEntry, { type: "movie" }>> = {}): CalendarEntry => ({
  type: "movie", date: "2026-07-16", hasFile: true, movieId: 7, movieTitle: "Film", year: 2026, ...over,
})

describe("wire-shape literals", () => {
  it("pins the two discriminator values (must match Go calendarEntry.Type)", () => {
    expect(CALENDAR_ENTRY_TYPES).toEqual(["episode", "movie"])
  })
})

describe("toLocalISODate / windowRange / addDays (local, no UTC drift)", () => {
  it("formats a local date without UTC conversion", () => {
    expect(toLocalISODate(new Date(2026, 0, 5))).toBe("2026-01-05")
  })
  it("computes a forward window across a month boundary", () => {
    expect(windowRange(new Date(2026, 6, 20), 28)).toEqual({ start: "2026-07-20", end: "2026-08-17" })
  })
  it("adds days across a year boundary", () => {
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01")
  })
})

describe("dayLabel", () => {
  it("labels today and tomorrow relative to a given today string", () => {
    expect(dayLabel("2026-07-15", "2026-07-15")).toBe("Today")
    expect(dayLabel("2026-07-16", "2026-07-15")).toBe("Tomorrow")
  })
  it("labels other days as weekday month day", () => {
    expect(dayLabel("2026-07-17", "2026-07-15")).toBe("Fri Jul 17")
  })
})

describe("groupByDay", () => {
  it("buckets a pre-sorted list by date string, preserving order", () => {
    const entries = [ep({ date: "2026-07-15" }), mv({ date: "2026-07-15" }), ep({ date: "2026-07-16", seriesId: 2 })]
    const days = groupByDay(entries, "2026-07-15")
    expect(days.map((d) => d.date)).toEqual(["2026-07-15", "2026-07-16"])
    expect(days[0].label).toBe("Today")
    expect(days[0].entries).toHaveLength(2)
    expect(days[1].entries).toHaveLength(1)
  })
  it("buckets purely by string (no Date parsing) so it is timezone-independent", () => {
    // new Date("2026-07-15") is UTC-midnight and could read as the 14th in a
    // negative-offset TZ; groupByDay must keep it under 2026-07-15.
    const days = groupByDay([ep({ date: "2026-07-15" })], "2026-07-10")
    expect(days[0].date).toBe("2026-07-15")
    expect(days[0].label).toBe("Wed Jul 15")
  })
})

describe("entryLink", () => {
  it("links episodes to the series and movies to the movie", () => {
    expect(entryLink(ep())).toBe("/tv/1")
    expect(entryLink(mv())).toBe("/movies/7")
  })
})

describe("shouldRefresh", () => {
  it("refreshes on import/queue events only", () => {
    expect(shouldRefresh("import.completed")).toBe(true)
    expect(shouldRefresh("indexer.status")).toBe(false)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/calendar/resolve.test.ts`
Expected: FAIL — cannot resolve `./types` / `./resolve`.

- [ ] **Step 3: Write minimal implementation**

Create `web/src/features/calendar/types.ts`:

```ts
// web/src/features/calendar/types.ts
// The `type` discriminator MUST match Go's calendarEntry.Type ("episode"|"movie").
export type CalendarEntry =
  | {
      type: "episode"
      date: string // "YYYY-MM-DD"
      hasFile: boolean
      seriesId: number
      seriesTitle: string
      seasonNumber: number
      episodeNumber: number
      episodeTitle: string
    }
  | {
      type: "movie"
      date: string // "YYYY-MM-DD"
      hasFile: boolean
      movieId: number
      movieTitle: string
      year: number
    }

export const CALENDAR_ENTRY_TYPES = ["episode", "movie"] as const
```

Create `web/src/features/calendar/resolve.ts`:

```ts
// web/src/features/calendar/resolve.ts
import type { CalendarEntry } from "./types"

export type DayBucket = { date: string; label: string; entries: CalendarEntry[] }

function pad2(n: number): string {
  return String(n).padStart(2, "0")
}

// Format a local Date as YYYY-MM-DD (local getters, never toISOString/UTC).
export function toLocalISODate(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

// Inclusive [start, end] date strings for a forward window of `days` days from
// `today` (a local Date), computed in local time.
export function windowRange(today: Date, days = 28): { start: string; end: string } {
  const start = toLocalISODate(today)
  const end = new Date(today.getFullYear(), today.getMonth(), today.getDate() + days)
  return { start, end: toLocalISODate(end) }
}

// Add n days to a YYYY-MM-DD string via local numeric-constructor math.
export function addDays(date: string, n: number): string {
  const [y, m, d] = date.split("-").map(Number)
  return toLocalISODate(new Date(y, m - 1, d + n))
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]

// Human label for a YYYY-MM-DD date relative to `today` (also YYYY-MM-DD).
// today/tomorrow are pure string compares; the fallback parses fields with the
// numeric Date constructor (local), never new Date(isoString).
export function dayLabel(date: string, today: string): string {
  if (date === today) return "Today"
  if (date === addDays(today, 1)) return "Tomorrow"
  const [y, m, d] = date.split("-").map(Number)
  const wd = WEEKDAYS[new Date(y, m - 1, d).getDay()]
  return `${wd} ${MONTHS[m - 1]} ${d}`
}

// Bucket a pre-sorted entry list by its raw date string, preserving order
// within and across buckets. No Date parsing → timezone-independent.
export function groupByDay(entries: CalendarEntry[], today: string): DayBucket[] {
  const buckets: DayBucket[] = []
  let current: DayBucket | undefined
  for (const e of entries) {
    if (!current || current.date !== e.date) {
      current = { date: e.date, label: dayLabel(e.date, today), entries: [] }
      buckets.push(current)
    }
    current.entries.push(e)
  }
  return buckets
}

export function entryLink(e: CalendarEntry): string {
  return e.type === "episode" ? `/tv/${e.seriesId}` : `/movies/${e.movieId}`
}

const REFRESH_EVENTS = new Set(["queue.updated", "import.completed", "download.status"])
export function shouldRefresh(type: string): boolean {
  return REFRESH_EVENTS.has(type)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/calendar/resolve.test.ts`
Expected: PASS (all cases).

- [ ] **Step 5: Commit**

```bash
git add web/src/features/calendar/types.ts web/src/features/calendar/resolve.ts web/src/features/calendar/resolve.test.ts
git commit -m "feat(6-5): calendar FE types + pure resolve helpers"
```

---

### Task 4: Frontend — query hooks, agenda view, route swap

**Files:**
- Create: `web/src/features/calendar/api.ts`
- Create: `web/src/features/calendar/CalendarView.tsx`
- Create: `web/src/features/calendar/CalendarView.test.tsx`
- Modify: `web/src/app/routes.tsx` (swap the Calendar placeholder for `<CalendarView />`)

**Interfaces:**
- Consumes: `apiGet` (`@/lib/api`), `useActivity` (`@/lib/activity`), `shouldRefresh`/`groupByDay`/`entryLink`/`toLocalISODate`/`windowRange` (`./resolve`), `CalendarEntry` (`./types`).
- Produces: `calendarKeys`, `useCalendar(start, end)`, `useCalendarInvalidation()`, `CalendarView`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/calendar/CalendarView.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { CalendarView } from "./CalendarView"
import * as api from "./api"
import type { CalendarEntry } from "./types"

vi.mock("./api")

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.useCalendarInvalidation).mockReturnValue(undefined)
})

function renderView() {
  render(
    <MemoryRouter>
      <CalendarView />
    </MemoryRouter>,
  )
}

describe("CalendarView", () => {
  it("shows an empty state when nothing is scheduled", () => {
    vi.mocked(api.useCalendar).mockReturnValue({ data: [], isLoading: false } as never)
    renderView()
    expect(screen.getByText(/nothing scheduled/i)).toBeInTheDocument()
  })

  it("renders episode and movie rows grouped under Today", () => {
    const now = new Date()
    const iso = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`
    const data: CalendarEntry[] = [
      { type: "episode", date: iso, hasFile: false, seriesId: 1, seriesTitle: "The Show", seasonNumber: 2, episodeNumber: 4, episodeTitle: "Deep" },
      { type: "movie", date: iso, hasFile: true, movieId: 7, movieTitle: "The Film", year: 2026 },
    ]
    vi.mocked(api.useCalendar).mockReturnValue({ data, isLoading: false } as never)
    renderView()
    expect(screen.getByText("Today")).toBeInTheDocument()
    expect(screen.getByText("The Show")).toBeInTheDocument()
    expect(screen.getByText("S02E04")).toBeInTheDocument()
    expect(screen.getByText("The Film (2026)")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/calendar/CalendarView.test.tsx`
Expected: FAIL — cannot resolve `./CalendarView` / `./api`.

- [ ] **Step 3: Write minimal implementation**

Create `web/src/features/calendar/api.ts`:

```ts
// web/src/features/calendar/api.ts
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect } from "react"
import { apiGet } from "@/lib/api"
import { useActivity } from "@/lib/activity"
import { shouldRefresh } from "./resolve"
import type { CalendarEntry } from "./types"

export const calendarKeys = {
  all: ["calendar"] as const,
  range: (start: string, end: string) => ["calendar", start, end] as const,
}

export function useCalendar(start: string, end: string) {
  return useQuery({
    queryKey: calendarKeys.range(start, end),
    queryFn: () => apiGet<CalendarEntry[]>(`/calendar?start=${start}&end=${end}`),
  })
}

export function useCalendarInvalidation(): void {
  const events = useActivity()
  const qc = useQueryClient()
  const latest = events[0]
  useEffect(() => {
    if (latest && shouldRefresh(latest.type)) {
      qc.invalidateQueries({ queryKey: calendarKeys.all })
    }
    // keyed on the latest event id so it fires once per new event
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [latest?.id])
}
```

Create `web/src/features/calendar/CalendarView.tsx`:

```tsx
// web/src/features/calendar/CalendarView.tsx
import { useMemo } from "react"
import { Link } from "react-router-dom"
import { useCalendar, useCalendarInvalidation } from "./api"
import { groupByDay, entryLink, toLocalISODate, windowRange } from "./resolve"
import type { CalendarEntry } from "./types"

const WINDOW_DAYS = 28

function pad2(n: number): string {
  return String(n).padStart(2, "0")
}

function EntryRow({ e }: { e: CalendarEntry }) {
  const movieLabel = e.type === "movie" ? (e.year ? `${e.movieTitle} (${e.year})` : e.movieTitle) : ""
  return (
    <Link
      to={entryLink(e)}
      className="flex items-center gap-3 rounded-md px-3 py-2 hover:bg-[rgba(124,92,255,0.10)]"
    >
      <span
        className={
          e.hasFile
            ? "h-2 w-2 shrink-0 rounded-full bg-[var(--color-brand)]"
            : "h-2 w-2 shrink-0 rounded-full border border-[var(--color-muted)]"
        }
      />
      {e.type === "episode" ? (
        <>
          <span className="w-14 shrink-0 text-xs text-[var(--color-muted)]">
            S{pad2(e.seasonNumber)}E{pad2(e.episodeNumber)}
          </span>
          <span className="font-medium">{e.seriesTitle}</span>
          <span className="truncate text-[var(--color-muted)]">{e.episodeTitle}</span>
        </>
      ) : (
        <span className="font-medium">{movieLabel}</span>
      )}
    </Link>
  )
}

export function CalendarView() {
  useCalendarInvalidation()
  const { start, end } = useMemo(() => windowRange(new Date(), WINDOW_DAYS), [])
  const today = toLocalISODate(new Date())
  const q = useCalendar(start, end)
  const days = useMemo(() => groupByDay(q.data ?? [], today), [q.data, today])

  return (
    <div className="p-6">
      <h1 className="mb-4 text-2xl font-bold">Calendar</h1>
      {q.isLoading && <p className="text-[var(--color-muted)]">Loading…</p>}
      {!q.isLoading && days.length === 0 && (
        <p className="text-[var(--color-muted)]">Nothing scheduled in the next {WINDOW_DAYS} days.</p>
      )}
      <div className="flex flex-col gap-6">
        {days.map((d) => (
          <section key={d.date}>
            <h2 className="mb-1 text-sm font-semibold text-[var(--color-muted)]">{d.label}</h2>
            <div className="flex flex-col">
              {d.entries.map((e) => (
                <EntryRow
                  key={
                    e.type === "episode"
                      ? `e-${e.seriesId}-${e.seasonNumber}-${e.episodeNumber}`
                      : `m-${e.movieId}`
                  }
                  e={e}
                />
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  )
}
```

In `web/src/app/routes.tsx`, add the import and swap the route element. Add near the other feature imports:

```tsx
import { CalendarView } from "@/features/calendar/CalendarView"
```

Change:

```tsx
      { path: "calendar", element: <Placeholder title="Calendar" /> },
```

to:

```tsx
      { path: "calendar", element: <CalendarView /> },
```

(Leave the `Placeholder` import in place — it is still used by the `system` route.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/features/calendar/` then `npx tsc -b`
Expected: both PASS / exit 0.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/calendar/api.ts web/src/features/calendar/CalendarView.tsx web/src/features/calendar/CalendarView.test.tsx web/src/app/routes.tsx
git commit -m "feat(6-5): calendar agenda view + query hooks + route"
```

---

### Task 5: Rebuild embedded web bundle + full verify

**Files:**
- Modify: `web/dist/**` (committed build output — drift-guarded)

**Interfaces:** none (build + verification only).

- [ ] **Step 1: Rebuild the embedded bundle**

Run: `cd web && npm run build`
Expected: Vite build succeeds, `web/dist/` regenerated.

- [ ] **Step 2: Run the full frontend suite**

Run: `cd web && npx vitest run && npx tsc -b`
Expected: all vitest suites PASS (including the new calendar suites), `tsc -b` exit 0.

- [ ] **Step 3: Run the full backend suite + drift guard**

Run:
```bash
export PATH="/c/Program Files/Go/bin:$PATH"
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./...
git diff --exit-code web/dist
```
Expected: build/vet/test all PASS across all packages (incl. `internal/core/store`, `internal/media`, `web/spa_test.go`); `git diff --exit-code web/dist` exits 0 (any staged dist changes already committed in this step's commit — if it reports drift, `git add web/dist` and amend/commit).

- [ ] **Step 4: Commit the rebuilt bundle**

```bash
git add web/dist
git commit -m "build(6-5): rebuild embedded web bundle for calendar slice"
```

(If `git diff --cached web/dist` is empty because the bundle was byte-identical, skip the commit — nothing to record.)

---

## Post-plan: whole-branch review, live check, finish

After all tasks: run the SDD final whole-branch review (opus, `review-package MERGE_BASE HEAD`, dist excluded), triage findings, then a **live browser check** on a fresh instance (`CGO_ENABLED=0 go build -o nexus.exe ./cmd/nexus`, fresh `NEXUS_DATA_DIR`, add a monitored series/movie with near-term air/release dates via TMDb, confirm the Calendar tab groups them by day with correct hasFile dots and detail-page links). Then `superpowers:finishing-a-development-branch`. **ASK before pushing.**

## Self-Review notes (author)

- **Spec coverage:** §2 dates/monitored predicate → T1 query WHERE clause + tests; §3.1 store queries → T1; §3.2 endpoint + validation + sort → T2; §3.3 wire shape → T2 DTO + `CALENDAR_ENTRY_TYPES` pin (T3) + Go `type` assertion (T2 test); §4 FE module (types/api/resolve/view/route) → T3+T4; §4 timezone rule → T3 helpers + timezone test; §5 window default → `WINDOW_DAYS=28` + `windowRange` (T3/T4); §6 testing (boundary/400/empty/non-UTC/render) → T1/T2/T3/T4; §6 dist+verify → T5. No gaps.
- **Type consistency:** `CalendarEntry`, `calendarEntry`, `CalendarEpisode`, `CalendarEpisodes`, `CalendarMovies`, `useCalendar`, `useCalendarInvalidation`, `groupByDay`, `dayLabel`, `entryLink`, `windowRange`, `toLocalISODate`, `addDays`, `shouldRefresh`, `calendarKeys` used identically across tasks.
- **No placeholders:** every code step shows full code; every run step shows the command + expected result.
