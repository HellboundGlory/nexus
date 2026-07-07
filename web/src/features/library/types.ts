export type RootFolder = { id: number; path: string; createdAt: string }

export type QualityProfileItem = { qualityId: number; allowed: boolean }
export type QualityProfile = {
  id: number
  name: string
  cutoffQualityId: number
  upgradeAllowed: boolean
  items: QualityProfileItem[]
  createdAt: string
}

export type MetadataResult = {
  tmdbId: number
  title: string
  year: number
  overview: string
  posterUrl: string
  kind: string
}

export type Movie = {
  id: number
  tmdbId: number
  title: string
  sortTitle: string
  overview: string
  status: string
  year: number
  releaseDate: string
  runtime: number
  imdbId: string
  posterUrl: string
  fanartUrl: string
  rootFolderId: number | null
  qualityProfileId: number | null
  monitored: boolean
  addedAt: string
  lastRefreshedAt: string | null
  hasFile: boolean
}

export type Series = {
  id: number
  tmdbId: number
  title: string
  sortTitle: string
  overview: string
  status: string
  firstAired: string
  posterUrl: string
  fanartUrl: string
  rootFolderId: number | null
  qualityProfileId: number | null
  monitored: boolean
  addedAt: string
  lastRefreshedAt: string | null
  episodeCount: number
  episodeFileCount: number
}

export type Season = { id: number; seriesId: number; seasonNumber: number; monitored: boolean }

export type Episode = {
  id: number
  seriesId: number
  seasonNumber: number
  episodeNumber: number
  tmdbId: number
  title: string
  overview: string
  airDate: string
  monitored: boolean
  hasFile: boolean
}

export type SeriesDetail = Series & { seasons: Season[]; episodes: Episode[] }

export type AddMovieBody = { tmdbId: number; rootFolderId: number | null; monitored: boolean }
export type AddSeriesBody = { tmdbId: number; rootFolderId: number | null; monitorOption: "all" | "future" | "none" }

export type MediaKind = "movie" | "tv"
