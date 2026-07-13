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
