import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { useToast } from "@/lib/toast"
import {
  useSeriesDetail, useQualityProfiles, useSetMonitored, useAssignProfile,
  useRefresh, useDelete, useSearch, libraryKeys,
} from "./api"
import { Select } from "@/components/ui/select"
import { StatusBadge, seriesBadge } from "./StatusBadge"
import { SeasonTable } from "./SeasonTable"
import { DetailBanner } from "./DetailBanner"
import { InteractiveSearchDialog } from "@/features/search/InteractiveSearchDialog"
import type { SearchTarget } from "@/features/search/types"

export function SeriesDetail({ id }: { id: number }) {
  const nav = useNavigate()
  const { toast } = useToast()
  const q = useSeriesDetail(id)
  const profiles = useQualityProfiles()
  const setMon = useSetMonitored(libraryKeys.seriesDetail(id))
  const assign = useAssignProfile(libraryKeys.seriesDetail(id))
  const refresh = useRefresh(libraryKeys.seriesDetail(id))
  const del = useDelete()
  const search = useSearch()
  const [searchTarget, setSearchTarget] = useState<SearchTarget | null>(null)
  const [searchTitle, setSearchTitle] = useState("")

  if (q.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading…</div>
  if (q.isError || !q.data) {
    return (
      <div className="p-6">
        <p className="text-sm text-[var(--color-muted)]">Not found.</p>
        <button onClick={() => nav("/tv")} className="mt-3 text-sm text-[var(--color-brand)]">← Back to TV Shows</button>
      </div>
    )
  }
  const s = q.data
  const badge = seriesBadge(s)

  return (
    <div className="p-6">
      <DetailBanner
        fanartUrl={s.fanartUrl}
        posterUrl={s.posterUrl}
        title={s.title}
        back={<button onClick={() => nav("/tv")} className="text-sm text-[var(--color-brand)]">← TV Shows</button>}
      >
        <div className="flex items-center gap-3">
          <h2 className="text-2xl font-bold">{s.title}</h2>
          {s.firstAired ? <span className="text-[var(--color-muted)]">{s.firstAired.slice(0, 4)}</span> : null}
          <StatusBadge tone={badge.tone} label={badge.label} />
        </div>
        <p className="mt-3 max-w-2xl text-sm text-[var(--color-muted)]">{s.overview}</p>
        <div className="mt-5 flex flex-wrap items-center gap-2">
          <button onClick={() => setMon.mutate({ target: { kind: "series", id }, monitored: !s.monitored })} className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm">
            {s.monitored ? "Unmonitor" : "Monitor"}
          </button>
          <button
            onClick={() => {
              if (!s.qualityProfileId) { toast("Assign a quality profile before searching", { variant: "error" }); return }
              search.mutate({ kind: "series", id }); toast(`Search started for ${s.title}`)
            }}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            Search
          </button>
          <button onClick={() => { refresh.mutate({ kind: "series", id }); toast("Refresh started") }} className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm">Refresh</button>
          <button
            onClick={() => { if (confirm(`Delete ${s.title}?`)) del.mutate({ kind: "series", id }, { onSuccess: () => { toast("Deleted"); nav("/tv") } }) }}
            className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
          >
            Delete
          </button>
          <div className="w-48">
            <Select
              aria-label="Quality profile"
              value={s.qualityProfileId ? String(s.qualityProfileId) : ""}
              disabled={(profiles.data ?? []).length === 0}
              onChange={(v) => v && assign.mutate({ kind: "series", id, qualityProfileId: Number(v) })}
            >
              <option value="">{(profiles.data ?? []).length === 0 ? "No profiles" : "Quality profile…"}</option>
              {(profiles.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </Select>
          </div>
        </div>
      </DetailBanner>

      <SeasonTable
        seasons={s.seasons}
        episodes={s.episodes}
        seriesId={id}
        onToggleSeason={(sn) => setMon.mutate({ target: { kind: "season", id: sn.id }, monitored: !sn.monitored })}
        onToggleEpisode={(e) => setMon.mutate({ target: { kind: "episode", id: e.id }, monitored: !e.monitored })}
        onSearchSeason={(seasonNumber) => { search.mutate({ kind: "season", seriesId: id, seasonNumber }); toast(`Search started for season ${seasonNumber}`) }}
        onSearchEpisode={(e) => { search.mutate({ kind: "episode", id: e.id }); toast(`Search started for ${e.title}`) }}
        onInteractiveSeason={(seasonNumber) => {
          if (!s.qualityProfileId) { toast("Assign a quality profile before searching", { variant: "error" }); return }
          setSearchTitle(`${s.title} — season ${seasonNumber}`)
          // episodeIds attribute the grab: a season queue row with no episode ids
          // can never import. Send every monitored episode without a file, the
          // same set searchSeason enqueues a pack against.
          setSearchTarget({
            kind: "season",
            seriesId: id,
            seasonNumber,
            episodeIds: s.episodes.filter((e) => e.seasonNumber === seasonNumber && e.monitored && !e.hasFile).map((e) => e.id),
          })
        }}
        onInteractiveEpisode={(e) => {
          if (!s.qualityProfileId) { toast("Assign a quality profile before searching", { variant: "error" }); return }
          setSearchTitle(e.title)
          setSearchTarget({ kind: "episode", id: e.id, seriesId: id })
        }}
      />

      <InteractiveSearchDialog
        target={searchTarget}
        title={searchTitle}
        onOpenChange={(open) => { if (!open) setSearchTarget(null) }}
      />
    </div>
  )
}
