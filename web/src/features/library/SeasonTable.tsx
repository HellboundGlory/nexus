import type { Season, Episode } from "./types"
import { StatusBadge } from "./StatusBadge"
import { SeasonSection } from "./SeasonSection"
import { seasonSections } from "./seasonSections"

export function SeasonTable({
  seasons, episodes, onToggleSeason, onToggleEpisode, onSearchSeason, onSearchEpisode,
  onInteractiveSeason, onInteractiveEpisode,
}: {
  seasons: Season[]
  episodes: Episode[]
  seriesId: number
  onToggleSeason: (s: Season) => void
  onToggleEpisode: (e: Episode) => void
  onSearchSeason: (seasonNumber: number) => void
  onSearchEpisode: (e: Episode) => void
  onInteractiveSeason: (seasonNumber: number) => void
  onInteractiveEpisode: (e: Episode) => void
}) {
  const sections = seasonSections(seasons, episodes)
  const seasonById = new Map(seasons.map((s) => [s.id, s]))
  return (
    <div className="mt-6 flex flex-col gap-4">
      {sections.map((sec) => {
        const season = seasonById.get(sec.id)!
        return (
          <SeasonSection
            key={sec.id}
            title={sec.title}
            withFile={sec.withFile}
            total={sec.eps.length}
            monitored={season.monitored}
            defaultOpen={sec.defaultOpen}
            onToggleMonitor={() => onToggleSeason(season)}
            onSearch={() => onSearchSeason(sec.seasonNumber)}
            onInteractive={() => onInteractiveSeason(sec.seasonNumber)}
          >
            {sec.eps.map((e) => (
              <li key={e.id} className="flex items-center gap-3 border-t border-[var(--color-border)] px-4 py-2 text-sm">
                <span className="w-10 text-[var(--color-muted)]">{e.episodeNumber}</span>
                <span className="min-w-0 flex-1 truncate">{e.title}</span>
                <span className="text-xs text-[var(--color-muted)]">{e.airDate}</span>
                <StatusBadge tone={e.hasFile ? "ok" : "muted"} label={e.hasFile ? "File" : "—"} />
                <button aria-label={`Search episode ${e.episodeNumber}`} onClick={() => onSearchEpisode(e)} className="text-xs text-[var(--color-brand)]">Search episode</button>
                <button aria-label={`Interactive search episode ${e.episodeNumber}`} onClick={() => onInteractiveEpisode(e)} className="text-xs text-[var(--color-brand)]">Interactive</button>
                <label className="flex items-center gap-1 text-xs text-[var(--color-muted)]">
                  <input type="checkbox" checked={e.monitored} onChange={() => onToggleEpisode(e)} /> mon
                </label>
              </li>
            ))}
          </SeasonSection>
        )
      })}
    </div>
  )
}
