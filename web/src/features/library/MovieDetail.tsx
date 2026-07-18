import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { useToast } from "@/lib/toast"
import {
  useMovieDetail, useQualityProfiles, useSetMonitored, useAssignProfile,
  useRefresh, useDelete, useSearch, useDeleteMovieFile, libraryKeys,
} from "./api"
import { Select } from "@/components/ui/select"
import { StatusBadge, movieBadge } from "./StatusBadge"
import { DetailBanner } from "./DetailBanner"
import { InteractiveSearchDialog } from "@/features/search/InteractiveSearchDialog"
import type { SearchTarget } from "@/features/search/types"
import { formatSize } from "@/features/search/resolve"

export function MovieDetail({ id }: { id: number }) {
  const nav = useNavigate()
  const { toast } = useToast()
  const q = useMovieDetail(id)
  const profiles = useQualityProfiles()
  const setMon = useSetMonitored(libraryKeys.movie(id))
  const assign = useAssignProfile(libraryKeys.movie(id))
  const refresh = useRefresh(libraryKeys.movie(id))
  const del = useDelete()
  const search = useSearch()
  const delFile = useDeleteMovieFile()
  const [searchTarget, setSearchTarget] = useState<SearchTarget | null>(null)

  if (q.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading…</div>
  if (q.isError || !q.data) {
    return (
      <div className="p-6">
        <p className="text-sm text-[var(--color-muted)]">Not found.</p>
        <button onClick={() => nav("/movies")} className="mt-3 text-sm text-[var(--color-brand)] rounded hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-brand)]">← Back to Movies</button>
      </div>
    )
  }
  const m = q.data
  const badge = movieBadge(m)

  return (
    <div className="p-6">
      <DetailBanner
        fanartUrl={m.fanartUrl}
        posterUrl={m.posterUrl}
        title={m.title}
        back={<button onClick={() => nav("/movies")} className="text-sm text-[var(--color-brand)] rounded hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-brand)]">← Movies</button>}
      >
        <div className="flex items-center gap-3">
          <h2 className="text-2xl font-bold">{m.title}</h2>
          {m.year ? <span className="text-[var(--color-muted)]">{m.year}</span> : null}
          <StatusBadge tone={badge.tone} label={badge.label} />
        </div>
        <p className="mt-3 max-w-2xl text-sm text-[var(--color-muted)]">{m.overview}</p>
        <div className="mt-5 flex flex-wrap items-center gap-2">
          <button
            onClick={() => setMon.mutate({ target: { kind: "movie", id }, monitored: !m.monitored })}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            {m.monitored ? "Unmonitor" : "Monitor"}
          </button>
          <button
            onClick={() => {
              if (!m.qualityProfileId) { toast("Assign a quality profile before searching", { variant: "error" }); return }
              search.mutate({ kind: "movie", id }); toast(`Search started for ${m.title}`)
            }}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            Search
          </button>
          <button
            onClick={() => {
              // DecideAll needs a profile to score against and Enqueue would
              // reject the grab anyway, so a profile-less item must not open a
              // modal it could never grab from. Same guard, same wording as the
              // auto-Search button above.
              if (!m.qualityProfileId) { toast("Assign a quality profile before searching", { variant: "error" }); return }
              setSearchTarget({ kind: "movie", id })
            }}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            Interactive
          </button>
          <button
            onClick={() => { refresh.mutate({ kind: "movie", id }); toast("Refresh started") }}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            Refresh
          </button>
          <button
            onClick={() => {
              if (confirm(`Delete ${m.title}?`)) {
                del.mutate({ kind: "movie", id }, { onSuccess: () => { toast("Deleted"); nav("/movies") } })
              }
            }}
            className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
          >
            Delete
          </button>
          <div className="w-48">
            <Select
              aria-label="Quality profile"
              value={m.qualityProfileId ? String(m.qualityProfileId) : ""}
              disabled={(profiles.data ?? []).length === 0}
              onChange={(v) => v && assign.mutate({ kind: "movie", id, qualityProfileId: Number(v) })}
            >
              <option value="">{(profiles.data ?? []).length === 0 ? "No profiles" : "Quality profile…"}</option>
              {(profiles.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </Select>
          </div>
        </div>
      </DetailBanner>

      {m.file ? (
        <div className="mt-6 rounded-lg border border-[var(--color-border)] p-4">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
              <p className="truncate font-medium">{m.file.relativePath.split("/").pop()}</p>
              <p className="mt-1 text-sm text-[var(--color-muted)]">
                {m.file.quality ? <span>{m.file.quality} · </span> : null}
                {formatSize(m.file.size)} · added {new Date(m.file.addedAt).toLocaleDateString()}
              </p>
              <p className="mt-1 truncate text-xs text-[var(--color-muted)]">{m.file.relativePath}</p>
            </div>
            <button
              onClick={() => {
                if (confirm("Delete this file from disk?")) {
                  delFile.mutate(id, { onSuccess: () => toast("File deleted") })
                }
              }}
              className="shrink-0 rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
            >
              Delete file
            </button>
          </div>
        </div>
      ) : null}

      <InteractiveSearchDialog
        target={searchTarget}
        title={m.title}
        onOpenChange={(open) => { if (!open) setSearchTarget(null) }}
      />
    </div>
  )
}
