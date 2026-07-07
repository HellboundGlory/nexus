import type { Season, Episode } from "./types"
import { StatusBadge } from "./StatusBadge"

export function SeasonTable({
  seasons, episodes, onToggleSeason, onToggleEpisode, onSearchSeason, onSearchEpisode,
}: {
  seasons: Season[]
  episodes: Episode[]
  seriesId: number
  onToggleSeason: (s: Season) => void
  onToggleEpisode: (e: Episode) => void
  onSearchSeason: (seasonNumber: number) => void
  onSearchEpisode: (e: Episode) => void
}) {
  const sorted = [...seasons].sort((a, b) => a.seasonNumber - b.seasonNumber)
  return (
    <div className="mt-6 flex flex-col gap-4">
      {sorted.map((s) => {
        const eps = episodes.filter((e) => e.seasonNumber === s.seasonNumber).sort((a, b) => a.episodeNumber - b.episodeNumber)
        const withFile = eps.filter((e) => e.hasFile).length
        return (
          <div key={s.id} className="overflow-hidden rounded-lg border border-[var(--color-border)]">
            <div className="flex items-center justify-between bg-[var(--color-panel-2)] px-4 py-2">
              <div className="flex items-center gap-3">
                <span className="font-semibold">Season {s.seasonNumber}</span>
                <StatusBadge tone={withFile >= eps.length && eps.length > 0 ? "ok" : "warn"} label={`${withFile} / ${eps.length}`} />
              </div>
              <div className="flex items-center gap-2">
                <button onClick={() => onSearchSeason(s.seasonNumber)} className="text-xs text-[var(--color-brand)]">Search season</button>
                <label className="flex items-center gap-1 text-xs text-[var(--color-muted)]">
                  <input type="checkbox" checked={s.monitored} onChange={() => onToggleSeason(s)} /> monitor
                </label>
              </div>
            </div>
            <ul>
              {eps.map((e) => (
                <li key={e.id} className="flex items-center gap-3 border-t border-[var(--color-border)] px-4 py-2 text-sm">
                  <span className="w-10 text-[var(--color-muted)]">{e.episodeNumber}</span>
                  <span className="min-w-0 flex-1 truncate">{e.title}</span>
                  <span className="text-xs text-[var(--color-muted)]">{e.airDate}</span>
                  <StatusBadge tone={e.hasFile ? "ok" : "muted"} label={e.hasFile ? "File" : "—"} />
                  <button aria-label={`Search episode ${e.episodeNumber}`} onClick={() => onSearchEpisode(e)} className="text-xs text-[var(--color-brand)]">Search episode</button>
                  <label className="flex items-center gap-1 text-xs text-[var(--color-muted)]">
                    <input type="checkbox" checked={e.monitored} onChange={() => onToggleEpisode(e)} /> mon
                  </label>
                </li>
              ))}
            </ul>
          </div>
        )
      })}
    </div>
  )
}
