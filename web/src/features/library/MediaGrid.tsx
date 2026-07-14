import * as React from "react"
import { DEFAULT_SCALE } from "./useGridScale"

export function MediaGrid<T>({
  items, isLoading, isError, onRetry, empty, renderCard, scale = DEFAULT_SCALE,
}: {
  items: T[] | undefined
  isLoading: boolean
  isError: boolean
  onRetry: () => void
  empty: string
  renderCard: (item: T) => React.ReactNode
  scale?: number
}) {
  const gridStyle: React.CSSProperties = {
    gridTemplateColumns: `repeat(auto-fill, minmax(${scale}px, 1fr))`,
  }
  if (isLoading) {
    return (
      <div data-testid="grid-loading" className="grid gap-4 p-6" style={gridStyle}>
        {Array.from({ length: 12 }).map((_, i) => (
          <div key={i} className="aspect-[2/3] animate-pulse rounded-lg bg-[var(--color-panel-2)]" />
        ))}
      </div>
    )
  }
  if (isError) {
    return (
      <div className="m-6 rounded-lg border border-[var(--color-warn)] bg-[var(--color-panel)] p-6 text-center">
        <p className="text-sm text-[var(--color-muted)]">Failed to load. Please try again.</p>
        <button
          onClick={onRetry}
          className="mt-3 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm hover:border-[var(--color-brand)]"
        >
          Retry
        </button>
      </div>
    )
  }
  if (!items || items.length === 0) {
    return <div className="p-10 text-center text-sm text-[var(--color-muted)]">{empty}</div>
  }
  return (
    <div className="grid gap-4 p-6" style={gridStyle}>
      {items.map((it) => renderCard(it))}
    </div>
  )
}
