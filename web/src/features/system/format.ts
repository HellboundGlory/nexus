export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  return [h, m, sec].map((n) => String(n).padStart(2, "0")).join(":")
}

export function humanizeInterval(seconds: number): string {
  const plural = (n: number, u: string) => `${n} ${u}${n === 1 ? "" : "s"}`
  if (seconds % 86400 === 0) return plural(seconds / 86400, "day")
  if (seconds % 3600 === 0) return plural(seconds / 3600, "hour")
  if (seconds % 60 === 0) return plural(seconds / 60, "minute")
  return plural(seconds, "second")
}

export function humanizeName(name: string): string {
  return name
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
}

export function relativePast(iso: string, now = Date.now()): string {
  const s = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000))
  if (s < 60) return "a few seconds ago"
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} minute${m === 1 ? "" : "s"} ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} hour${h === 1 ? "" : "s"} ago`
  const d = Math.floor(h / 24)
  return `${d} day${d === 1 ? "" : "s"} ago`
}

export function relativeFuture(iso: string, now = Date.now()): string {
  const diff = new Date(iso).getTime() - now
  if (diff <= 0) return "now"
  const s = Math.floor(diff / 1000)
  if (s < 60) return `in ${s} second${s === 1 ? "" : "s"}`
  const m = Math.floor(s / 60)
  if (m < 60) return `in ${m} minute${m === 1 ? "" : "s"}`
  const h = Math.floor(m / 60)
  if (h < 24) return `in ${h} hour${h === 1 ? "" : "s"}`
  const d = Math.floor(h / 24)
  return `in ${d} day${d === 1 ? "" : "s"}`
}
