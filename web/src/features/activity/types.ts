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
  progress?: number
  downloadStatus?: string
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

export type Paged<T> = {
  items: T[]
  page: number
  pageSize: number
  total: number
}

export type ClientError = {
  clientId: string
  message: string
}

export type ClearResult = {
  removed: number
  clientErrors?: ClientError[]
}
