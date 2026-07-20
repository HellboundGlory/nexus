// web/src/features/activity/QueueSection.tsx
import { useState } from "react"
import { ApiError } from "@/lib/api"
import { useToast } from "@/lib/toast"
import { relativeTime } from "@/lib/time"
import { useMovies, useSeries } from "@/features/library/api"
import { useQualityDefinitions } from "@/features/settings/qualityApi"
import { useQueue, useImportItem, useRemoveQueueItem, useClearQueue } from "./api"
import { RemoveQueueItemDialog } from "./RemoveQueueItemDialog"
import { ClearConfirmDialog } from "./ClearConfirmDialog"
import {
  movieTitleMap, seriesTitleMap, resolveTitle, qualityName, queueRowDisplay, type Tone,
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
  const clearQueue = useClearQueue()
  const { toast } = useToast()

  const [removeTarget, setRemoveTarget] = useState<{ id: number; title: string } | null>(null)
  const [clearOpen, setClearOpen] = useState(false)
  // Set when a non-forced clear is refused; showing it turns the dialog into
  // the "Clear anyway" state. Force is never offered until it is the answer.
  const [clearWarning, setClearWarning] = useState<string | null>(null)

  if (queue.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading queue…</div>
  if (queue.isError) return <div className="p-6 text-sm text-[var(--color-warn)]">Failed to load queue.</div>

  const rows = queue.data ?? []

  const movieMap = movieTitleMap(movies.data)
  const seriesMap = seriesTitleMap(series.data)

  const onImport = (id: number) =>
    importItem.mutate(id, {
      onSuccess: () => toast("Import started"),
      onError: (e) => toast(e instanceof ApiError ? e.message : "Import failed", { variant: "error" }),
    })

  const onRemove = (opts: { removeFromClient: boolean; blocklist: boolean }) => {
    if (!removeTarget) return
    removeItem.mutate(
      { id: removeTarget.id, ...opts },
      {
        onSuccess: () => {
          setRemoveTarget(null)
          toast("Removed from queue")
        },
        onError: (e) => toast(e instanceof ApiError ? e.message : "Remove failed", { variant: "error" }),
      },
    )
  }

  const onClear = (force: boolean) => {
    clearQueue.mutate(
      { force },
      {
        onSuccess: (res) => {
          setClearOpen(false)
          setClearWarning(null)
          const failed = res.clientErrors?.length ?? 0
          toast(
            failed > 0
              ? `Queue cleared (${res.removed} items). ${failed} download client(s) could not be reached; their downloads may still be running.`
              : `Queue cleared (${res.removed} items)`,
            failed > 0 ? { variant: "error" } : undefined,
          )
        },
        onError: (e) => {
          // A refused clear keeps the dialog open and offers the override.
          if (e instanceof ApiError && e.status === 503) {
            setClearWarning(e.message)
            return
          }
          setClearOpen(false)
          toast(e instanceof ApiError ? e.message : "Clear failed", { variant: "error" })
        },
      },
    )
  }

  return (
    <div className="p-6">
      <div className="mb-3 flex items-center justify-between">
        <span className="text-xs text-[var(--color-muted)]">{rows.length} items</span>
        {rows.length > 0 ? (
          <button
            onClick={() => { setClearWarning(null); setClearOpen(true) }}
            className="rounded border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-warn)] hover:border-[var(--color-warn)]"
          >
            Clear queue
          </button>
        ) : null}
      </div>

      {rows.length === 0 ? (
        <div className="text-sm text-[var(--color-muted)]">Queue is empty.</div>
      ) : (
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
            const disp = queueRowDisplay(r)
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
                <td className="py-2.5 pr-4">
                  {disp.kind === "live" ? (
                    <div className="min-w-[7rem]">
                      <div className="mb-1 flex items-center justify-between gap-2">
                        <span className={`text-xs font-semibold ${toneClass[disp.tone]}`}>{disp.label}</span>
                        <span className="text-xs tabular-nums text-[var(--color-muted)]">{Math.round(disp.percent)}%</span>
                      </div>
                      <div className="h-1.5 w-full overflow-hidden rounded bg-[var(--color-border)]">
                        <div
                          role="progressbar"
                          aria-valuenow={Math.round(disp.percent)}
                          aria-valuemin={0}
                          aria-valuemax={100}
                          className="h-full rounded bg-[var(--color-brand)]"
                          style={{ width: `${Math.round(disp.percent)}%` }}
                        />
                      </div>
                    </div>
                  ) : (
                    <span className={`font-semibold ${toneClass[disp.tone]}`}>{disp.label}</span>
                  )}
                </td>
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
                    onClick={() => setRemoveTarget({ id: r.id, title: r.sourceTitle || title })}
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
      )}

      <RemoveQueueItemDialog
        open={removeTarget !== null}
        onOpenChange={(o) => { if (!o) setRemoveTarget(null) }}
        title={removeTarget?.title ?? ""}
        onConfirm={onRemove}
      />
      <ClearConfirmDialog
        open={clearOpen}
        onOpenChange={(o) => { setClearOpen(o); if (!o) setClearWarning(null) }}
        title="Clear queue?"
        body={`This removes all ${rows.length} queued items and cancels their downloads.`}
        warning={clearWarning}
        confirmLabel={clearWarning ? "Clear anyway" : "Clear"}
        onConfirm={() => onClear(clearWarning !== null)}
      />
    </div>
  )
}
