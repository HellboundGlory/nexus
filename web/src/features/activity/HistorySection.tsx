// web/src/features/activity/HistorySection.tsx
import { relativeTime } from "@/lib/time"
import { useMovies, useSeries } from "@/features/library/api"
import { useQualityDefinitions } from "@/features/settings/qualityApi"
import { useHistory } from "./api"
import { movieTitleMap, seriesTitleMap, resolveTitle, qualityName, eventLabel } from "./resolve"

export function HistorySection() {
  const history = useHistory()
  const movies = useMovies()
  const series = useSeries()
  const defs = useQualityDefinitions()

  if (history.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading history…</div>
  if (history.isError) return <div className="p-6 text-sm text-[var(--color-warn)]">Failed to load history.</div>

  const rows = history.data ?? []
  if (rows.length === 0) return <div className="p-6 text-sm text-[var(--color-muted)]">No history yet.</div>

  const movieMap = movieTitleMap(movies.data)
  const seriesMap = seriesTitleMap(series.data)

  return (
    <div className="p-6">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-[var(--color-border)] text-left text-xs uppercase tracking-wide text-[var(--color-muted)]">
            <th className="py-2 pr-4">Event</th>
            <th className="py-2 pr-4">Media</th>
            <th className="py-2 pr-4">Quality</th>
            <th className="py-2 pr-4">Message</th>
            <th className="py-2 pr-4">Time</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((h) => {
            const title = resolveTitle(h, movieMap, seriesMap)
            return (
              <tr key={h.id} className="border-b border-[var(--color-border)] align-top last:border-b-0">
                <td className={`py-2.5 pr-4 font-semibold ${h.eventType === "import_failed" ? "text-[var(--color-warn)]" : "text-[var(--color-fg)]"}`}>
                  {eventLabel(h.eventType)}
                </td>
                <td className="py-2.5 pr-4">
                  <div className="font-medium">{title}</div>
                  {h.sourceTitle && h.sourceTitle !== title ? (
                    <div className="truncate text-xs text-[var(--color-muted)]">{h.sourceTitle}</div>
                  ) : null}
                </td>
                <td className="py-2.5 pr-4">{qualityName(h.qualityId, defs.data)}</td>
                <td className="py-2.5 pr-4 text-[var(--color-muted)]">{h.message || "—"}</td>
                <td className="whitespace-nowrap py-2.5 pr-4 text-[var(--color-muted)]">
                  {relativeTime(new Date(h.createdAt).getTime())}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
