// web/src/features/search/types.ts

// Mirrors internal/quality.QualityDefinition.
export type QualityDef = {
  id: number
  name: string
  source: string
  resolution: string
  rank: number
}

// Mirrors automation.ScoredRelease. Field-by-field with the Go json tags.
export type ScoredRelease = {
  title: string
  downloadUrl: string
  infoUrl?: string
  size: number
  indexerId: string
  protocol: string
  publishDate: string
  // ABSENT on usenet rows; present on torrents including a real 0. Never use the
  // value as a presence check — `seeders != null` is the discriminator.
  seeders?: number
  // Always present; "Unknown" (id 0) for an unparseable title.
  quality: QualityDef
  score: number
  accepted: boolean
  // Always an array. EMPTY means automation would have grabbed this release —
  // that is the single rule the UI keys off.
  rejections: string[]
}

export type IndexerErrorEntry = {
  indexerId: string
  message: string
}

export type InteractiveResult = {
  releases: ScoredRelease[]
  indexerErrors: IndexerErrorEntry[]
}

// The item the search is for. Season/episode targets carry the episode ids the
// grab must be attributed to — a queue row with no episode ids can never import.
export type SearchTarget =
  | { kind: "movie"; id: number }
  | { kind: "season"; seriesId: number; seasonNumber: number; episodeIds?: number[] }
  | { kind: "episode"; id: number; seriesId?: number }

// Mirrors importing.enqueueBody.
export type GrabRequest = {
  downloadUrl: string
  title: string
  protocol: string
  indexerId: string
  mediaKind: string
  seriesId?: number
  episodeIds?: number[]
  movieId?: number
  force: boolean
}
