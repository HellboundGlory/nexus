import { describe, it, expect } from "vitest"
import { seasonSections } from "@/features/library/seasonSections"
import type { Season, Episode } from "@/features/library/types"

const season = (id: number, n: number): Season => ({ id, seriesId: 1, seasonNumber: n, monitored: true })
const ep = (id: number, n: number, e: number, hasFile: boolean): Episode => ({
  id, seriesId: 1, seasonNumber: n, episodeNumber: e, tmdbId: 0, title: `E${e}`,
  overview: "", airDate: "", monitored: true, hasFile,
})

describe("seasonSections", () => {
  it("titles Season 0 as Specials and orders it last", () => {
    const out = seasonSections(
      [season(10, 0), season(11, 2), season(12, 1)],
      [],
    )
    expect(out.map((s) => s.title)).toEqual(["Season 1", "Season 2", "Specials"])
  })
  it("marks specials closed by default and regular seasons open", () => {
    const out = seasonSections([season(10, 0), season(11, 1)], [])
    const specials = out.find((s) => s.seasonNumber === 0)!
    const s1 = out.find((s) => s.seasonNumber === 1)!
    expect(specials.defaultOpen).toBe(false)
    expect(s1.defaultOpen).toBe(true)
  })
  it("groups + counts episodes with files per season, sorted by episode number", () => {
    const out = seasonSections(
      [season(11, 1)],
      [ep(102, 1, 2, false), ep(101, 1, 1, true)],
    )
    const s1 = out[0]
    expect(s1.eps.map((e) => e.episodeNumber)).toEqual([1, 2])
    expect(s1.withFile).toBe(1)
  })
})
