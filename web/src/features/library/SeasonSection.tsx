import * as React from "react"
import { useState } from "react"
import { StatusBadge } from "./StatusBadge"

export function SeasonSection({
  title, withFile, total, monitored, defaultOpen, onToggleMonitor, onSearch, children,
}: {
  title: string
  withFile: number
  total: number
  monitored: boolean
  defaultOpen: boolean
  onToggleMonitor: () => void
  onSearch: () => void
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="overflow-hidden rounded-lg border border-[var(--color-border)]">
      <div className="flex items-center justify-between bg-[var(--color-panel-2)] px-4 py-2">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          className="flex items-center gap-3"
        >
          <span aria-hidden className="text-xs text-[var(--color-muted)]">{open ? "▾" : "▸"}</span>
          <span className="font-semibold">{title}</span>
          <StatusBadge tone={withFile >= total && total > 0 ? "ok" : "warn"} label={`${withFile} / ${total}`} />
        </button>
        <div className="flex items-center gap-2">
          <button onClick={onSearch} className="text-xs text-[var(--color-brand)]">Search season</button>
          <label className="flex items-center gap-1 text-xs text-[var(--color-muted)]">
            <input type="checkbox" checked={monitored} onChange={onToggleMonitor} /> monitor
          </label>
        </div>
      </div>
      {open ? <ul>{children}</ul> : null}
    </div>
  )
}
