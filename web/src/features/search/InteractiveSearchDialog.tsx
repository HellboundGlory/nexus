import { Dialog, DialogTitle } from "@/components/ui/dialog"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { useInteractiveSearch, useInteractiveGrab } from "./api"
import { ReleaseRow } from "./ReleaseRow"
import { needsConfirm, rejectionSummary } from "./resolve"
import type { ScoredRelease, SearchTarget } from "./types"

export function InteractiveSearchDialog({
  target, title, onOpenChange,
}: {
  target: SearchTarget | null
  title: string
  onOpenChange: (open: boolean) => void
}) {
  const { toast } = useToast()
  const q = useInteractiveSearch(target)
  const grab = useInteractiveGrab()

  function onGrab(r: ScoredRelease) {
    if (!target) return
    // Force needs friction: a rejected row costs a guaranteed-wasted download
    // (a non-covering grab downloads then fails to import by design), so a
    // misclick should not be free. Clean rows grab straight away.
    if (needsConfirm(r) && !confirm(`Rejected: ${rejectionSummary(r)}. Grab anyway?`)) return

    grab.mutate(
      { release: r, target },
      {
        onSuccess: () => {
          toast(`Grabbed ${r.title}`)
          onOpenChange(false)
        },
        onError: (err) => {
          const msg =
            err instanceof ApiError && err.code === "no_profile"
              ? "Assign a quality profile before searching"
              : err instanceof Error
                ? err.message
                : "Grab failed"
          toast(msg, { variant: "error" })
        },
      },
    )
  }

  const releases = q.data?.releases ?? []
  const indexerErrors = q.data?.indexerErrors ?? []

  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange} className="w-[64rem]">
      <DialogTitle>Interactive search — {title}</DialogTitle>

      {q.isLoading ? <p className="py-8 text-center text-sm text-[var(--color-muted)]">Searching indexers…</p> : null}

      {q.isError ? (
        <div className="py-8 text-center">
          <p className="text-sm text-[var(--color-warn)]">Couldn't search the indexers.</p>
          <button onClick={() => q.refetch()} className="mt-2 text-sm text-[var(--color-brand)]">Retry</button>
        </div>
      ) : null}

      {/* A partial list with no banner is the same invisibility this feature
          exists to remove, so name the indexers that failed. */}
      {indexerErrors.length > 0 ? (
        <div role="alert" className="mb-3 rounded-md border border-[var(--color-warn)] px-3 py-2 text-xs text-[var(--color-warn)]">
          Some indexers failed, so this list may be incomplete:{" "}
          {indexerErrors.map((e) => `${e.indexerId} (${e.message})`).join(", ")}
        </div>
      ) : null}

      {!q.isLoading && !q.isError && releases.length === 0 ? (
        <p className="py-8 text-center text-sm text-[var(--color-muted)]">No releases found.</p>
      ) : null}

      {releases.length > 0 ? (
        <div className="max-h-[60vh] overflow-auto">
          <table className="w-full table-fixed">
            <thead className="sticky top-0 bg-[var(--color-panel)] text-left text-xs text-[var(--color-muted)]">
              <tr>
                <th className="px-3 py-2 font-medium">Title</th>
                <th className="w-28 px-3 py-2 font-medium">Indexer</th>
                <th className="w-20 px-3 py-2 font-medium">Size</th>
                <th className="w-16 px-3 py-2 font-medium">Age</th>
                <th className="w-16 px-3 py-2 font-medium">Seeders</th>
                <th className="w-28 px-3 py-2 font-medium">Quality</th>
                <th className="w-20 px-3 py-2 font-medium">Status</th>
                <th className="w-20 px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {/* Server order only — the ranking IS the information, and row 1 is
                  exactly what auto-search would have grabbed. */}
              {releases.map((r) => (
                <ReleaseRow key={`${r.indexerId}:${r.downloadUrl}`} release={r} onGrab={onGrab} grabbing={grab.isPending} />
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </Dialog>
  )
}
