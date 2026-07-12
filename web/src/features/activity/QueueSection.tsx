// web/src/features/activity/QueueSection.tsx
import { ApiError } from "@/lib/api"
import { useToast } from "@/lib/toast"
import { relativeTime } from "@/lib/time"
import { useMovies, useSeries } from "@/features/library/api"
import { useQualityDefinitions } from "@/features/settings/qualityApi"
import { useQueue, useImportItem, useRemoveQueueItem } from "./api"
import {
  movieTitleMap, seriesTitleMap, resolveTitle, qualityName, statusLabel, statusTone, type Tone,
} from "./resolve"

const toneClass: Record<Tone, string> = {
  ok: "text-[var(--color-ok)]",
  info: "text-[var(--color-brand)]",
  error: "text-[var(--color-warn)]",
  neutral: "text-[var(--color-muted)]",
}

export function QueueSection() {
  const queue = useQueue()
  const movies = useMovies()
  const series = useSeries()
  const defs = useQualityDefinitions()
  const importItem = useImportItem()
  const removeItem = useRemoveQueueItem()
  const { toast } = useToast()

  if (queue.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading queue…</div>
  if (queue.isError) return <div className="p-6 text-sm text-[var(--color-warn)]">Failed to load queue.</div>

  const rows = queue.data ?? []
  if (rows.length === 0) return <div className="p-6 text-sm text-[var(--color-muted)]">Queue is empty.</div>

  const movieMap = movieTitleMap(movies.data)
  const seriesMap = seriesTitleMap(series.data)

  const onImport = (id: number) =>
    importItem.mutate(id, {
      onSuccess: () => toast("Import started"),
      onError: (e) => toast(e instanceof ApiError ? e.message : "Import failed", { variant: "error" }),
    })

  const onRemove = (id: number) => {
    if (!window.confirm("Remove this item from the queue?")) return
    removeItem.mutate(id, {
      onSuccess: () => toast("Removed from queue"),
      onError: (e) => toast(e instanceof ApiError ? e.message : "Remove failed", { variant: "error" }),
    })
  }

  return (
    <div className="p-6">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-[var(--color-border)] text-left text-xs uppercase tracking-wide text-[var(--color-muted)]">
            <th className="py-2 pr-4">Media</th>
            <th className="py-2 pr-4">Kind</th>
            <th className="py-2 pr-4">Quality</th>
            <th className="py-2 pr-4">Protocol</th>
            <th className="py-2 pr-4">Status</th>
            <th className="py-2 pr-4">Added</th>
            <th className="py-2 pr-4 text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const title = resolveTitle(r, movieMap, seriesMap)
            return (
              <tr key={r.id} className="border-b border-[var(--color-border)] align-top last:border-b-0">
                <td className="py-2.5 pr-4">
                  <div className="font-medium">{title}</div>
                  {r.sourceTitle && r.sourceTitle !== title ? (
                    <div className="truncate text-xs text-[var(--color-muted)]">{r.sourceTitle}</div>
                  ) : null}
                  {r.status === "failed" && r.error ? (
                    <div className="text-xs text-[var(--color-warn)]">{r.error}</div>
                  ) : null}
                </td>
                <td className="py-2.5 pr-4 text-[var(--color-muted)]">{r.mediaKind}</td>
                <td className="py-2.5 pr-4">{qualityName(r.qualityId, defs.data)}</td>
                <td className="py-2.5 pr-4 text-[var(--color-muted)]">{r.protocol}</td>
                <td className={`py-2.5 pr-4 font-semibold ${toneClass[statusTone(r.status)]}`}>{statusLabel(r.status)}</td>
                <td className="whitespace-nowrap py-2.5 pr-4 text-[var(--color-muted)]">
                  {relativeTime(new Date(r.createdAt).getTime())}
                </td>
                <td className="whitespace-nowrap py-2.5 pr-4 text-right">
                  {(r.status === "failed" || r.status === "grabbed") && (
                    <button
                      onClick={() => onImport(r.id)}
                      className="mr-2 rounded border border-[var(--color-border)] px-2 py-1 text-xs hover:border-[var(--color-brand)]"
                    >
                      Import
                    </button>
                  )}
                  <button
                    onClick={() => onRemove(r.id)}
                    className="rounded border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-warn)] hover:border-[var(--color-warn)]"
                  >
                    Remove
                  </button>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
