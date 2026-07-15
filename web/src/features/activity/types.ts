// web/src/features/activity/types.ts
export type QueueItem = {
  id: number
  downloadClientId: string
  clientItemId: string
  protocol: string
  sourceTitle: string
  mediaKind: string
  seriesId?: number
  movieId?: number
  episodeIds: number[]
  qualityId: number
  status: string
  error?: string
  createdAt: string
  updatedAt: string
}

export type BlocklistEntry = {
  id: number
  mediaKind: string
  movieId?: number
  seriesId?: number
  sourceTitle: string
  protocol: string
  qualityId: number
  reason: string
  createdAt: string
  title: string
}

export type HistoryEvent = {
  id: number
  eventType: string
  mediaKind: string
  seriesId?: number
  episodeId?: number
  movieId?: number
  sourceTitle?: string
  qualityId?: number | null
  message?: string
  createdAt: string
}
