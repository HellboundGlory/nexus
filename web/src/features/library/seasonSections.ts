import type { Season, Episode } from "./types"

export type SeasonSectionData = {
  id: number
  seasonNumber: number
  title: string
  defaultOpen: boolean
  eps: Episode[]
  withFile: number
}

export function seasonSections(seasons: Season[], episodes: Episode[]): SeasonSectionData[] {
  const build = (s: Season): SeasonSectionData => {
    const eps = episodes
      .filter((e) => e.seasonNumber === s.seasonNumber)
      .sort((a, b) => a.episodeNumber - b.episodeNumber)
    return {
      id: s.id,
      seasonNumber: s.seasonNumber,
      title: s.seasonNumber === 0 ? "Specials" : `Season ${s.seasonNumber}`,
      defaultOpen: s.seasonNumber !== 0,
      eps,
      withFile: eps.filter((e) => e.hasFile).length,
    }
  }
  const regular = seasons
    .filter((s) => s.seasonNumber > 0)
    .sort((a, b) => a.seasonNumber - b.seasonNumber)
    .map(build)
  const specials = seasons.filter((s) => s.seasonNumber === 0).map(build)
  return [...regular, ...specials]
}
