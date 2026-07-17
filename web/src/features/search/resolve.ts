// web/src/features/search/resolve.ts
// Pure display + request-shaping logic for interactive search. No rendering, no
// I/O — mirrors activity/resolve.ts so the rules are unit-testable in isolation.
import type { ScoredRelease, SearchTarget, GrabRequest } from "./types"

export function interactivePath(t: SearchTarget): string {
  switch (t.kind) {
    case "movie":
      return `/automation/search/movie/${t.id}/interactive`
    case "season":
      return `/automation/search/series/${t.seriesId}/season/${t.seasonNumber}/interactive`
    case "episode":
      return `/automation/search/episode/${t.id}/interactive`
  }
}

// Empty rejections == automation would have grabbed it. Any reasons → grey the
// row and confirm before grabbing. One rule, uniformly applied.
export function needsConfirm(r: ScoredRelease): boolean {
  return r.rejections.length > 0
}

export type RowTone = "neutral" | "muted"
export function rowTone(r: ScoredRelease): RowTone {
  return r.rejections.length > 0 ? "muted" : "neutral"
}

// Reasons are shown verbatim — they come from the server and are the only
// explanation the user gets for why automation passed this release over.
export function rejectionSummary(r: ScoredRelease): string {
  return r.rejections.join(". ")
}

export function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return "—"
  const gb = bytes / 1_000_000_000
  if (gb >= 1) return `${gb.toFixed(1)} GB`
  return `${Math.round(bytes / 1_000_000)} MB`
}

export function formatAge(iso: string, now: Date = new Date()): string {
  if (!iso) return "—"
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return "—"
  const mins = Math.floor((now.getTime() - t) / 60_000)
  if (mins < 60) return `${Math.max(mins, 0)}m`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}

// force is sent for ANY rejected row. Server-side it is only load-bearing for
// quality-rejected rows: the blocklist is not consulted on POST /queue at all, so
// on a blocklisted or non-covering row whose quality is fine, force is a no-op —
// Enqueue would have accepted it anyway. Sending it uniformly keeps the client
// rule simple without overstating what the server enforces.
export function grabBody(r: ScoredRelease, target: SearchTarget): GrabRequest {
  const base = {
    downloadUrl: r.downloadUrl,
    title: r.title,
    protocol: r.protocol,
    indexerId: r.indexerId,
    force: needsConfirm(r),
  }
  switch (target.kind) {
    case "movie":
      return { ...base, mediaKind: "movie", movieId: target.id }
    case "season":
      return { ...base, mediaKind: "tv", seriesId: target.seriesId, episodeIds: target.episodeIds ?? [] }
    case "episode":
      return { ...base, mediaKind: "tv", seriesId: target.seriesId ?? 0, episodeIds: [target.id] }
  }
}
