import { Link } from "react-router-dom"
import { StatusBadge } from "./StatusBadge"

export function MediaCard({
  to, title, subtitle, posterUrl, badge,
}: {
  to: string
  title: string
  subtitle: string
  posterUrl: string
  badge: { tone: "ok" | "warn" | "muted"; label: string }
}) {
  return (
    <Link
      to={to}
      className="group flex flex-col overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] transition-colors hover:border-[var(--color-brand)]"
    >
      <div className="aspect-[2/3] w-full bg-[var(--color-panel-2)]">
        {posterUrl ? (
          <img src={posterUrl} alt={title} className="h-full w-full object-cover" loading="lazy" />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-[var(--color-muted)]">No poster</div>
        )}
      </div>
      <div className="flex flex-1 flex-col gap-1 p-3">
        <div className="truncate text-sm font-semibold" title={title}>{title}</div>
        <div className="text-xs text-[var(--color-muted)]">{subtitle}</div>
        <div className="mt-1"><StatusBadge tone={badge.tone} label={badge.label} /></div>
      </div>
    </Link>
  )
}
