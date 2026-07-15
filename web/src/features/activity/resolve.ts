// web/src/features/activity/resolve.ts
import type { Movie, Series } from "@/features/library/types"
import type { QualityDefinition } from "@/features/settings/qualityTypes"

export type TitleRow = {
  mediaKind: string
  movieId?: number
  seriesId?: number
  sourceTitle?: string
}

export function movieTitleMap(movies?: Movie[]): Map<number, string> {
  const m = new Map<number, string>()
  for (const mv of movies ?? []) {
    m.set(mv.id, mv.year > 0 ? `${mv.title} (${mv.year})` : mv.title)
  }
  return m
}

export function seriesTitleMap(series?: Series[]): Map<number, string> {
  const m = new Map<number, string>()
  for (const s of series ?? []) m.set(s.id, s.title)
  return m
}

export function resolveTitle(
  row: TitleRow,
  movieMap: Map<number, string>,
  seriesMap: Map<number, string>,
): string {
  if (row.mediaKind === "movie" && row.movieId != null) {
    const t = movieMap.get(row.movieId)
    if (t) return t
  }
  if (row.mediaKind === "tv" && row.seriesId != null) {
    const t = seriesMap.get(row.seriesId)
    if (t) return t
  }
  return row.sourceTitle && row.sourceTitle.length > 0 ? row.sourceTitle : "—"
}

export function qualityName(
  id: number | null | undefined,
  defs?: QualityDefinition[],
): string {
  if (id == null || id === 0) return "—"
  const d = (defs ?? []).find((q) => q.id === id)
  return d ? d.name : "—"
}

const EVENT_LABELS: Record<string, string> = {
  grabbed: "Grabbed",
  imported: "Imported",
  upgraded: "Upgraded",
  import_failed: "Import failed",
}
export function eventLabel(t: string): string {
  return EVENT_LABELS[t] ?? t
}

const STATUS_LABELS: Record<string, string> = {
  grabbed: "Grabbed",
  importing: "Importing",
  imported: "Imported",
  failed: "Failed",
}
export function statusLabel(s: string): string {
  return STATUS_LABELS[s] ?? s
}

export type Tone = "neutral" | "info" | "ok" | "error"
export function statusTone(s: string): Tone {
  switch (s) {
    case "imported":
      return "ok"
    case "importing":
      return "info"
    case "failed":
      return "error"
    default:
      return "neutral"
  }
}

const REFRESH_EVENTS = new Set(["queue.updated", "import.completed", "download.status", "download.failed"])
export function shouldRefresh(type: string): boolean {
  return REFRESH_EVENTS.has(type)
}

const LIVE_STATUS_LABELS: Record<string, string> = {
  downloading: "Downloading",
  queued: "Queued",
  paused: "Paused",
  warning: "Warning",
  completed: "Completed",
}
export function liveStatusLabel(s: string): string {
  return LIVE_STATUS_LABELS[s] ?? s
}

export type QueueDisplay =
  | { kind: "live"; percent: number; label: string; tone: Tone }
  | { kind: "status"; label: string; tone: Tone }

export function queueRowDisplay(row: {
  status: string
  progress?: number
  downloadStatus?: string
}): QueueDisplay {
  // Live progress overrides the grab-status label ONLY for grabbed rows that
  // have a live match. Presence of downloadStatus is the discriminator — never
  // the numeric progress (a genuine 0% row still has a downloadStatus).
  if (row.status === "grabbed" && row.downloadStatus != null) {
    return { kind: "live", percent: row.progress ?? 0, label: liveStatusLabel(row.downloadStatus), tone: "info" }
  }
  return { kind: "status", label: statusLabel(row.status), tone: statusTone(row.status) }
}
