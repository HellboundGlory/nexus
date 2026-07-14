import { describe, it, expect } from "vitest"
import { sortResults } from "@/features/library/addSort"
import type { MetadataResult } from "@/features/library/types"

const r = (tmdbId: number, year: number): MetadataResult => ({
  tmdbId, title: `t${tmdbId}`, year, overview: "", posterUrl: "", kind: "movie",
})

describe("sortResults", () => {
  const input = [r(1, 2001), r(2, 0), r(3, 2020)]
  it("keeps relevance order and does not mutate the input", () => {
    const out = sortResults(input, "relevance")
    expect(out.map((x) => x.tmdbId)).toEqual([1, 2, 3])
    expect(input.map((x) => x.tmdbId)).toEqual([1, 2, 3])
  })
  it("sorts newest first with missing years last", () => {
    expect(sortResults(input, "newest").map((x) => x.tmdbId)).toEqual([3, 1, 2])
  })
  it("sorts oldest first with missing years last", () => {
    expect(sortResults(input, "oldest").map((x) => x.tmdbId)).toEqual([1, 3, 2])
  })
})
