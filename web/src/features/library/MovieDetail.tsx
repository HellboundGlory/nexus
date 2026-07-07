import { useNavigate } from "react-router-dom"
import { useToast } from "@/lib/toast"
import {
  useMovieDetail, useQualityProfiles, useSetMonitored, useAssignProfile,
  useRefresh, useDelete, useSearch, libraryKeys,
} from "./api"
import { Select } from "@/components/ui/select"
import { StatusBadge, movieBadge } from "./StatusBadge"

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

  if (q.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading…</div>
  if (q.isError || !q.data) {
    return (
      <div className="p-6">
        <p className="text-sm text-[var(--color-muted)]">Not found.</p>
        <button onClick={() => nav("/movies")} className="mt-3 text-sm text-[var(--color-brand)]">← Back to Movies</button>
      </div>
    )
  }
  const m = q.data
  const badge = movieBadge(m)

  return (
    <div className="p-6">
      <button onClick={() => nav("/movies")} className="mb-4 text-sm text-[var(--color-brand)]">← Movies</button>
      <div className="flex gap-6">
        <div className="aspect-[2/3] w-40 shrink-0 overflow-hidden rounded-lg bg-[var(--color-panel-2)]">
          {m.posterUrl ? <img src={m.posterUrl} alt={m.title} className="h-full w-full object-cover" /> : null}
        </div>
        <div className="min-w-0 flex-1">
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
              onClick={() => { search.mutate({ kind: "movie", id }); toast(`Search started for ${m.title}`) }}
              className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
            >
              Search
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
        </div>
      </div>
    </div>
  )
}
