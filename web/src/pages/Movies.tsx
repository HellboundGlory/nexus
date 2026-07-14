import { useState } from "react"
import { useMovies } from "@/features/library/api"
import { MediaGrid } from "@/features/library/MediaGrid"
import { MediaCard } from "@/features/library/MediaCard"
import { movieBadge } from "@/features/library/StatusBadge"
import { AddMediaDialog } from "@/features/library/AddMediaDialog"
import { useGridScale } from "@/features/library/useGridScale"
import { ScaleSlider } from "@/features/library/ScaleSlider"

export function Movies() {
  const q = useMovies()
  const [filter, setFilter] = useState("")
  const [addOpen, setAddOpen] = useState(false)
  const [scale, setScale] = useGridScale()
  const items = (q.data ?? []).filter((m) => m.title.toLowerCase().includes(filter.toLowerCase()))

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
        empty="No movies yet — click Add to get started."
        renderCard={(m) => (
          <MediaCard
            key={m.id}
            to={`/movies/${m.id}`}
            title={m.title}
            subtitle={m.year ? String(m.year) : ""}
            posterUrl={m.posterUrl}
            badge={movieBadge(m)}
          />
        )}
      />
      {addOpen && <AddMediaDialog kind="movie" open={addOpen} onOpenChange={setAddOpen} />}
    </div>
  )
}
