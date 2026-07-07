import { useParams } from "react-router-dom"
import { MovieDetail } from "@/features/library/MovieDetail"
import { SeriesDetail } from "@/features/library/SeriesDetail"

export function MediaDetail({ kind }: { kind: "movie" | "series" }) {
  const { id } = useParams()
  const numId = Number(id)
  if (!id || Number.isNaN(numId)) return <div className="p-6 text-sm text-[var(--color-muted)]">Invalid id.</div>
  return kind === "movie" ? <MovieDetail id={numId} /> : <SeriesDetail id={numId} />
}
