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
