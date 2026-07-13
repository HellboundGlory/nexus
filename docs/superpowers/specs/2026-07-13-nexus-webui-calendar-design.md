# Nexus Web UI — Calendar Slice 5 (Upcoming agenda)

Status: approved 2026-07-13. Final slice of Sub-project 6 (Web UI).

This is the **only** Sub-6 slice that is not pure UI-over-existing-endpoints: it adds a
small backend query + endpoint because no cross-media, date-windowed query exists today.

## 1. Goal & scope

Give the user an **agenda-style Calendar**: a flat, date-sorted list of upcoming
**monitored** episodes and movies, grouped by day, so they can see what Nexus is about
to be chasing. Activity/History already answers "what already happened"; the Calendar
answers "what's coming".

### In scope

- A new cross-series episode-by-date-window store query + a movie-by-date-window query.
- A new `GET /api/v1/calendar?start=&end=` endpoint in the media package that merges both
  into one flat, date-sorted list enriched with a `hasFile` flag.
- A new `web/src/features/calendar/` module: types, a `useCalendar` query hook (with
  WS-driven invalidation), pure grouping/label helpers, and an agenda `CalendarView`.
- Swap the existing `<Placeholder title="Calendar" />` route for the real view (sidebar
  nav already exists).

### Explicitly out of scope (YAGNI / not in Nexus)

- Month-grid layout, prev/next month paging, per-day pop-out modal.
- iCal / RSS feed export, drag-to-reschedule, "add to calendar" links.
- A monitored on/off toggle or any other filter control (monitored-only, enforced
  server-side).
- Any multi-date movie axis. Nexus movies have a **single** `release_date` — this is
  deliberately **not** Radarr's inCinemas/digital/physical model (we have no such data).
- Pagination (project-wide small-library assumption; no endpoint paginates).

## 2. Existing backend surface (verified)

- **Dates are stored verbatim from TMDb as `"YYYY-MM-DD"` strings, or `""` when unknown**
  (`internal/media/tmdb.go` passes `air_date` / `release_date` through untouched;
  `internal/core/store/media_store.go` stores them as `string`). This is the key fact:
  window filtering is a **lexical string compare**, and empty dates sort below any real
  date so they are excluded from a `>= start` window for free.
- `store.Episode` (`media_store.go:104`): `id, seriesId, seasonNumber, episodeNumber,
  tmdbId, title, overview, airDate, monitored`. Episodes carry **no** parent series title
  — the calendar query must join `series` to get it.
- `store.Series` (`media_store.go:80`): has `title`, `sortTitle`, `monitored`.
- `store.Movie` (`media_store.go:289`): `id, ..., title, year, releaseDate, monitored`.
- `store.EpisodeFileIDs(ctx) map[int64]bool` and `store.MovieFileIDs(ctx) map[int64]bool`
  (`import_store.go:278` / `:260`) — the exact `hasFile` enrichment the list views already
  use. Reuse them; do not invent a new file query.
- `internal/media/api.go` already owns both `/series` and `/movies` sub-routers under the
  authed `/api/v1` group. The calendar endpoint mounts there — **no new top-level wiring,
  no new migration**.
- **"Monitored" predicate:** `internal/automation` treats an episode as actively wanted
  when series + season + episode are all monitored, and a movie when the movie is
  monitored (`search.go`). Season-monitor toggles cascade into `episode.monitored` in this
  codebase (`SetSeasonEpisodesMonitored`), so the calendar uses
  `series.monitored = 1 AND episode.monitored = 1` to capture that intent **without** a
  three-table join. (Accepted edge: a season row manually left `monitored=0` while its
  episodes stayed `monitored=1` would still show — rare given the cascade, and harmless for
  a read-only view.)

## 3. Backend design

### 3.1 Store queries (`internal/core/store/media_store.go`)

Mirror the whole-table aggregation precedent in `import_store.go` (`SeriesEpisodeStats`).

```go
// CalendarEpisode is an episode joined to its series title for the calendar view.
type CalendarEpisode struct {
    Episode
    SeriesTitle string `json:"seriesTitle"`
}

// CalendarEpisodes returns monitored episodes of monitored series whose air_date
// falls within [start, end] (inclusive, "YYYY-MM-DD" lexical compare), ordered by
// air_date then series sort_title. Empty air_date rows are excluded by the >= bound.
func (s *Store) CalendarEpisodes(ctx context.Context, start, end string) ([]CalendarEpisode, error)

// CalendarMovies returns monitored movies whose release_date falls within [start, end],
// ordered by release_date then sort_title. Empty release_date excluded by the >= bound.
func (s *Store) CalendarMovies(ctx context.Context, start, end string) ([]Movie, error)
```

Episode SQL:
```sql
SELECT e.id, e.series_id, e.season_number, e.episode_number, e.tmdb_id, e.title,
       e.overview, e.air_date, e.monitored, s.title
FROM episodes e JOIN series s ON e.series_id = s.id
WHERE s.monitored = 1 AND e.monitored = 1
  AND e.air_date >= ? AND e.air_date <= ?
ORDER BY e.air_date, s.sort_title;
```
Movie SQL: `... FROM movies WHERE monitored = 1 AND release_date >= ? AND release_date <= ? ORDER BY release_date, sort_title`.

### 3.2 Endpoint (`internal/media/api.go`)

`r.Get("/calendar", a.calendar)` inside `Mount`.

- Parse `start` / `end` query params. Both required, both must match `YYYY-MM-DD`
  (a strict `time.Parse("2006-01-02", …)` check). Missing or malformed → `400 bad_request`
  (reuse `api.WriteError`).
- Run `CalendarEpisodes` + `CalendarMovies`, load `EpisodeFileIDs` + `MovieFileIDs` once
  each, build the unified list, then **sort by `date`, then `type` ("episode" before
  "movie" on the same day), then the entry's display title (`seriesTitle` for episodes,
  `movieTitle` for movies)** so same-day ordering is deterministic across both types.
  Return `200` with the array (never `null` — emit `[]`).

### 3.3 Response wire shape (the recurring trap — pin it)

One flat array of entries with a `type` discriminator. `type` is a literal **I define
here** and MUST agree between Go and TS (this is the same class as the 3b `qualityId`
int-vs-string and 6-4 `mediaKind "tv"` bugs). A shared-literal test asserts the two `type`
values on both sides.

```jsonc
// type == "episode"
{ "type": "episode", "date": "2026-07-15", "hasFile": false,
  "seriesId": 12, "seriesTitle": "The Show",
  "seasonNumber": 2, "episodeNumber": 4, "episodeTitle": "Some Title" }

// type == "movie"
{ "type": "movie", "date": "2026-07-16", "hasFile": true,
  "movieId": 7, "movieTitle": "Movie Name", "year": 2026 }
```

Go DTO (media package, not store):
```go
type calendarEntry struct {
    Type    string `json:"type"`          // "episode" | "movie"
    Date    string `json:"date"`          // air_date or release_date, "YYYY-MM-DD"
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
```
No `monitored` field — monitored is enforced server-side, so it would always be `true`
(YAGNI; add it only if a toggle is ever wanted).

## 4. Frontend architecture (`web/src/features/calendar/`)

Mirror the Activity feature layout (`api.ts` hooks, pure `resolve.ts`, a view component).

- **`types.ts`** — `CalendarEntry` union matching §3.3 exactly. Model it as a discriminated
  union on `type` (`type: "episode"` branch vs `type: "movie"` branch) with **numeric** ids
  so a type mismatch is caught at compile time.
- **`api.ts`** — `calendarKeys`; `useCalendar(start, end)` = `useQuery` over
  `apiGet<CalendarEntry[]>(\`/calendar?start=${start}&end=${end}\`)`; a
  `useCalendarInvalidation()` that reuses the `useActivity()` ring buffer + `shouldRefresh`
  pattern from Activity so a completed import flips a `hasFile` dot without a manual reload.
- **`resolve.ts` (pure, unit-tested)**:
  - `groupByDay(entries)` → ordered array of `{ date, label, entries }` day buckets,
    preserving the server's sort. **Bucket by the raw `date` string** — never
    `new Date("YYYY-MM-DD")`, which parses as UTC midnight and reports the previous day in
    negative-offset timezones (invisible to a UTC-CI fixture test; must be covered by a
    non-UTC test case).
  - `dayLabel(date, today)` → `"Today"` / `"Tomorrow"` / else a formatted `"Fri Jul 18"`.
    `today` is injected (a `YYYY-MM-DD` string) so the helper stays pure and testable.
  - `entryLink(entry)` → `/tv/${seriesId}` for episodes, `/movies/${movieId}` for movies.
  - `windowRange(today, days)` → `{ start, end }` `YYYY-MM-DD` strings computed in **local**
    time (not UTC), `days` defaulting to 28.
- **`CalendarView.tsx`** — computes `{start, end}` via `windowRange`, calls `useCalendar`,
  renders day-header sections and per-entry rows: episodes show `SxxEyy` + series title +
  episode title; movies show title + `(year)`; each row a filled/empty dot for `hasFile`
  and links via `entryLink`. Loading and empty states ("Nothing scheduled in the next N
  days.").
- **`routes.tsx`** — replace `<Placeholder title="Calendar" />` with `<CalendarView />`.

## 5. Data flow & live refresh

1. `CalendarView` computes today (local) → `windowRange` → `start`/`end`.
2. `useCalendar(start, end)` fetches the flat list; `groupByDay` buckets it.
3. `useCalendarInvalidation()` watches the WS ring buffer; on an import/queue event it
   invalidates the calendar query so a newly-filed episode's dot fills in.

## 6. Testing

**Backend (store):** boundary fixtures —
- episode exactly on `start` and exactly on `end` (both included),
- episode just outside each bound (excluded),
- episode with **empty `air_date`** (excluded),
- monitored+unmonitored episode pair on the same date (unmonitored excluded),
- episode of an **unmonitored series** (excluded),
- movie in-window and out-of-window.
Assert ordering (air_date, then sort_title). Same shape of test for `CalendarMovies`.

**Backend (API):** `GET /calendar` returns the merged, date-then-type-sorted flat list
with correct `hasFile`; `400` on missing/malformed `start`/`end`; empty window → `[]`.

**Frontend (vitest):** pure `resolve.ts` — `groupByDay` bucketing/order, `dayLabel`
(Today/Tomorrow/date, including a **non-UTC timezone** case that would break a naive
`new Date(iso)`), `entryLink`, `windowRange`. A `CalendarView` render test (day headers +
episode/movie rows + hasFile dot + empty state). A shared-literal test asserting the two
`type` values agree Go↔TS.

**Verification:** full `CGO_ENABLED=0 go build/vet/test ./...`, `tsc -b`, vitest,
`web/dist` drift-guard, then a **live browser check** on a fresh instance before merge
(every prior slice did this).

## 7. Acceptance criteria

1. `GET /api/v1/calendar?start=YYYY-MM-DD&end=YYYY-MM-DD` returns a flat, date-sorted list
   of monitored episodes (with series title + `SxxEyy` fields) and monitored movies (title
   + year), each with `hasFile`; `400` on bad/missing params; `[]` on empty window.
2. Episodes/movies outside the window, unmonitored, or with empty dates never appear.
3. The Calendar nav item opens the agenda view: entries grouped under `Today` / `Tomorrow`
   / dated day headers, in date order, with a filled dot when a file already exists.
4. Rows link to the correct detail page (`/tv/:id` or `/movies/:id`).
5. A completed import updates a dot live (WS invalidation), no manual reload.
6. Empty library / empty window shows a clear empty state.
7. Day bucketing is correct in a negative-offset timezone (no off-by-one from UTC date
   construction).

## 8. Delivery

Built via the SDD loop (sonnet implementers + reviewers, opus final whole-branch review),
in an isolated worktree, then `finishing-a-development-branch`. **Standing rule holds: ASK
before pushing the default branch.**
