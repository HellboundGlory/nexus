// web/src/features/calendar/types.ts
// The `type` discriminator MUST match Go's calendarEntry.Type ("episode"|"movie").
export type CalendarEntry =
  | {
      type: "episode"
      date: string // "YYYY-MM-DD"
      hasFile: boolean
      seriesId: number
      seriesTitle: string
      seasonNumber: number
      episodeNumber: number
      episodeTitle: string
    }
  | {
      type: "movie"
      date: string // "YYYY-MM-DD"
      hasFile: boolean
      movieId: number
      movieTitle: string
      year: number
    }

export const CALENDAR_ENTRY_TYPES = ["episode", "movie"] as const
