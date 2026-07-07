type Tone = "ok" | "warn" | "muted"

export function movieBadge(m: { monitored: boolean; hasFile: boolean }): { label: string; tone: Tone } {
  if (m.hasFile) return { label: "Downloaded", tone: "ok" }
  if (m.monitored) return { label: "Missing", tone: "warn" }
  return { label: "Unmonitored", tone: "muted" }
}

export function seriesBadge(s: { monitored: boolean; episodeCount: number; episodeFileCount: number }): { label: string; tone: Tone } {
  const label = `${s.episodeFileCount} / ${s.episodeCount}`
  if (!s.monitored) return { label, tone: "muted" }
  if (s.episodeCount > 0 && s.episodeFileCount >= s.episodeCount) return { label, tone: "ok" }
  return { label, tone: "warn" }
}

const toneClass: Record<Tone, string> = {
  ok: "border-[var(--color-ok)] text-[var(--color-ok)]",
  warn: "border-[var(--color-warn)] text-[var(--color-warn)]",
  muted: "border-[var(--color-border)] text-[var(--color-muted)]",
}

export function StatusBadge({ tone, label }: { tone: Tone; label: string }) {
  return (
    <span className={`inline-block rounded-full border px-2 py-0.5 text-xs font-semibold ${toneClass[tone]}`}>
      {label}
    </span>
  )
}
