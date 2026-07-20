export type RootFolder = { id: number; path: string; createdAt: string }

export type NamingConfig = {
  seriesFolder: string
  seasonFolder: string
  episodeFile: string
  movieFolder: string
  movieFile: string
}

export type AutomationConfig = {
  missingSearchIntervalHours: number
  missingSearchBatchSize: number
  rssSyncEnabled: boolean
  rssSyncIntervalMinutes: number
  upgradeSearchEnabled: boolean
  upgradeSearchIntervalHours: number
  upgradeSearchBatchSize: number
  upgradeGrabCooldownHours: number
  maxConcurrentPerSeries: number
}
