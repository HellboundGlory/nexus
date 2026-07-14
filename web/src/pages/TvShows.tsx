import { useState } from "react"
import { useSeries } from "@/features/library/api"
import { MediaGrid } from "@/features/library/MediaGrid"
import { MediaCard } from "@/features/library/MediaCard"
import { seriesBadge } from "@/features/library/StatusBadge"
import { AddMediaDialog } from "@/features/library/AddMediaDialog"
import { useGridScale } from "@/features/library/useGridScale"
import { ScaleSlider } from "@/features/library/ScaleSlider"

export function TvShows() {
  const q = useSeries()
  const [filter, setFilter] = useState("")
  const [addOpen, setAddOpen] = useState(false)
  const [scale, setScale] = useGridScale()
  const items = (q.data ?? []).filter((s) => s.title.toLowerCase().includes(filter.toLowerCase()))

  return (
    <div>
      <div className="flex items-center gap-3 p-6 pb-0">
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter…"
          className="w-64 rounded-md border border-[var(--color-border)] bg-[var(--color-panel)] px-3 py-1.5 text-sm"
        />
        <button
          onClick={() => setAddOpen(true)}
          className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white"
        >
          + Add
        </button>
        <ScaleSlider value={scale} onChange={setScale} />
      </div>
      <MediaGrid
        scale={scale}
        items={q.data ? items : undefined}
        isLoading={q.isLoading}
        isError={q.isError}
        onRetry={() => q.refetch()}
        empty="No TV shows yet — click Add to get started."
        renderCard={(s) => (
          <MediaCard
            key={s.id}
            to={`/tv/${s.id}`}
            title={s.title}
            subtitle={s.firstAired ? s.firstAired.slice(0, 4) : ""}
            posterUrl={s.posterUrl}
            badge={seriesBadge(s)}
          />
        )}
      />
      {addOpen && <AddMediaDialog kind="tv" open={addOpen} onOpenChange={setAddOpen} />}
    </div>
  )
}
