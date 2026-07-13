import { useState, useEffect } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"
import { Select } from "@/components/ui/select"
import { ApiError } from "@/lib/api"
import { useToast } from "@/lib/toast"
import {
  useLookup, useRootFolders, useAddMovie, useAddSeries,
} from "./api"
import type { MetadataResult, MediaKind } from "./types"

export function AddMediaDialog({
  kind, open, onOpenChange,
}: {
  kind: MediaKind
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const { toast } = useToast()
  const [term, setTerm] = useState("")
  const [debounced, setDebounced] = useState("")
  const [picked, setPicked] = useState<MetadataResult | null>(null)
  const [rootFolderId, setRootFolderId] = useState("")
  const [monitorOption, setMonitorOption] = useState<"all" | "future" | "none">("all")
  const [monitored, setMonitored] = useState(true)

  // simple debounce
  useDebounce(term, 300, setDebounced)

  const lookup = useLookup(debounced, kind)
  const rootFolders = useRootFolders()
  const addMovie = useAddMovie()
  const addSeries = useAddSeries()

  const noRoots = (rootFolders.data ?? []).length === 0
  const pending = addMovie.isPending || addSeries.isPending

  async function submit() {
    if (!picked) return
    const rfId = rootFolderId ? Number(rootFolderId) : null
    try {
      if (kind === "movie") {
        await addMovie.mutateAsync({ tmdbId: picked.tmdbId, rootFolderId: rfId, monitored })
      } else {
        await addSeries.mutateAsync({ tmdbId: picked.tmdbId, rootFolderId: rfId, monitorOption })
      }
      toast(`Added ${picked.title}`, { variant: "ok" })
      reset()
      onOpenChange(false)
    } catch (e) {
      toast(e instanceof Error ? e.message : "Failed to add", { variant: "error" })
    }
  }

  function reset() {
    setTerm(""); setDebounced(""); setPicked(null); setRootFolderId("")
    setMonitorOption("all"); setMonitored(true)
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) reset(); onOpenChange(o) }}>
      <DialogTitle>Add {kind === "movie" ? "Movie" : "TV Show"}</DialogTitle>

      {!picked ? (
        <div>
          <input
            autoFocus
            value={term}
            onChange={(e) => setTerm(e.target.value)}
            placeholder="Search TMDb…"
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-3 py-2 text-sm"
          />
          {debounced.trim() && lookup.isError && (
            <p className="mt-3 text-sm text-[var(--color-warn)]">
              {lookup.error instanceof ApiError && lookup.error.code === "not_configured"
                ? "Metadata provider not configured — set a TMDb API key on the server."
                : lookup.error instanceof Error
                  ? lookup.error.message
                  : "Search failed."}
            </p>
          )}
          {debounced.trim() && lookup.isLoading && (
            <p className="mt-3 text-sm text-[var(--color-muted)]">Searching…</p>
          )}
          {debounced.trim() && !lookup.isLoading && !lookup.isError && (lookup.data ?? []).length === 0 && (
            <p className="mt-3 text-sm text-[var(--color-muted)]">No results.</p>
          )}
          <ul className="mt-3 max-h-72 overflow-auto">
            {(lookup.data ?? []).map((r) => (
              <li key={r.tmdbId}>
                <button
                  onClick={() => setPicked(r)}
                  className="flex w-full items-start gap-3 rounded-md p-2 text-left hover:bg-[var(--color-panel-2)]"
                >
                  <span className="font-medium">{r.title}</span>
                  {r.year ? <span className="text-xs text-[var(--color-muted)]">{r.year}</span> : null}
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <div className="text-sm font-semibold">{picked.title}{picked.year ? ` (${picked.year})` : ""}</div>

          <label className="text-xs text-[var(--color-muted)]">Root folder</label>
          {noRoots ? (
            <p className="text-sm text-[var(--color-warn)]">No root folder configured — add one in Settings.</p>
          ) : (
            <Select aria-label="Root folder" value={rootFolderId} onChange={setRootFolderId}>
              <option value="">Select…</option>
              {(rootFolders.data ?? []).map((rf) => (
                <option key={rf.id} value={rf.id}>{rf.path}</option>
              ))}
            </Select>
          )}

          {kind === "tv" ? (
            <>
              <label className="text-xs text-[var(--color-muted)]">Monitor</label>
              <Select aria-label="Monitor" value={monitorOption} onChange={(v) => setMonitorOption(v as "all" | "future" | "none")}>
                <option value="all">All episodes</option>
                <option value="future">Future episodes</option>
                <option value="none">None</option>
              </Select>
            </>
          ) : (
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={monitored} onChange={(e) => setMonitored(e.target.checked)} />
              Monitored
            </label>
          )}

          <div className="mt-2 flex justify-end gap-2">
            <button onClick={() => setPicked(null)} className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm">Back</button>
            <button
              onClick={submit}
              disabled={noRoots || pending}
              className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
            >
              {pending ? "Adding…" : `Add ${kind === "movie" ? "Movie" : "Show"}`}
            </button>
          </div>
        </div>
      )}
    </Dialog>
  )
}

function useDebounce(value: string, ms: number, setter: (v: string) => void) {
  useEffect(() => {
    const t = setTimeout(() => setter(value), ms)
    return () => clearTimeout(t)
  }, [value, ms, setter])
}
