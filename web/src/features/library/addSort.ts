import type { MetadataResult } from "./types"

export type AddSort = "relevance" | "newest" | "oldest"

export function sortResults(results: MetadataResult[], sort: AddSort): MetadataResult[] {
  const copy = [...results]
  if (sort === "relevance") return copy
  return copy.sort((a, b) => {
    const ay = a.year || 0
    const by = b.year || 0
    // Missing years (0) always sort last regardless of direction.
    if (ay === 0 && by === 0) return 0
    if (ay === 0) return 1
    if (by === 0) return -1
    return sort === "newest" ? by - ay : ay - by
  })
}
