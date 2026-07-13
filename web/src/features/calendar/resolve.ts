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
