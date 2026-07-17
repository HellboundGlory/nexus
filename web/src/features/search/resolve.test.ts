import { describe, it, expect } from "vitest"
import {
  interactivePath, formatSize, formatAge, rowTone,
  rejectionSummary, needsConfirm, grabBody, missingSeasonEpisodeIds,
} from "./resolve"
import type { ScoredRelease } from "./types"
import type { Episode } from "@/features/library/types"

function release(over: Partial<ScoredRelease> = {}): ScoredRelease {
  return {
    title: "Some.Movie.2019.480p.HDTV.x264-GRP",
    downloadUrl: "http://x/1",
    size: 1_500_000_000,
    indexerId: "1",
    protocol: "usenet",
    publishDate: "2026-07-15T00:00:00Z",
    quality: { id: 1, name: "SDTV", source: "hdtv", resolution: "480p", rank: 1 },
    score: 10,
    accepted: true,
    rejections: [],
    ...over,
  }
}

describe("interactivePath", () => {
  it("builds the movie path", () => {
    expect(interactivePath({ kind: "movie", id: 7 })).toBe("/automation/search/movie/7/interactive")
  })
  it("builds the season path", () => {
    expect(interactivePath({ kind: "season", seriesId: 3, seasonNumber: 2 }))
      .toBe("/automation/search/series/3/season/2/interactive")
  })
  it("builds the episode path", () => {
    expect(interactivePath({ kind: "episode", id: 42 })).toBe("/automation/search/episode/42/interactive")
  })
})

describe("needsConfirm", () => {
  // The single UI rule: any rejections → confirm. Empty rejections means
  // automation would have grabbed it, so it grabs on click.
  it("is false for a clean row", () => {
    expect(needsConfirm(release())).toBe(false)
  })
  it("is true for any rejected row", () => {
    expect(needsConfirm(release({ rejections: ["quality not in profile"] }))).toBe(true)
  })
})

describe("rowTone", () => {
  it("is neutral for a clean row", () => {
    expect(rowTone(release())).toBe("neutral")
  })
  it("is muted for a rejected row", () => {
    expect(rowTone(release({ rejections: ["blocklisted: Not on your server(s)"] }))).toBe("muted")
  })
})

describe("rejectionSummary", () => {
  it("is empty for a clean row", () => {
    expect(rejectionSummary(release())).toBe("")
  })
  it("joins reasons verbatim", () => {
    expect(rejectionSummary(release({ rejections: ["quality not in profile", "does not cover S01E05"] })))
      .toBe("quality not in profile. does not cover S01E05")
  })
})

describe("formatSize", () => {
  it("formats GB", () => expect(formatSize(1_500_000_000)).toBe("1.5 GB"))
  it("formats MB", () => expect(formatSize(350_000_000)).toBe("350 MB"))
  it("handles zero", () => expect(formatSize(0)).toBe("—"))
})

describe("formatAge", () => {
  const now = new Date("2026-07-17T00:00:00Z")
  it("formats days", () => expect(formatAge("2026-07-15T00:00:00Z", now)).toBe("2d"))
  it("formats hours", () => expect(formatAge("2026-07-16T20:00:00Z", now)).toBe("4h"))
  it("handles an empty date", () => expect(formatAge("", now)).toBe("—"))
})

function episode(over: Partial<Episode> = {}): Episode {
  return {
    id: 1,
    seriesId: 3,
    seasonNumber: 1,
    episodeNumber: 1,
    tmdbId: 100,
    title: "Episode",
    overview: "",
    airDate: "2026-01-01T00:00:00Z",
    monitored: true,
    hasFile: false,
    ...over,
  }
}

describe("missingSeasonEpisodeIds", () => {
  it("excludes episodes from a different season", () => {
    const episodes = [
      episode({ id: 1, seasonNumber: 1 }),
      episode({ id: 2, seasonNumber: 2 }),
    ]
    expect(missingSeasonEpisodeIds(episodes, 1)).toEqual([1])
  })

  it("excludes unmonitored episodes", () => {
    const episodes = [
      episode({ id: 1, seasonNumber: 1, monitored: true }),
      episode({ id: 2, seasonNumber: 1, monitored: false }),
    ]
    expect(missingSeasonEpisodeIds(episodes, 1)).toEqual([1])
  })

  it("excludes episodes that already have a file", () => {
    const episodes = [
      episode({ id: 1, seasonNumber: 1, hasFile: false }),
      episode({ id: 2, seasonNumber: 1, hasFile: true }),
    ]
    expect(missingSeasonEpisodeIds(episodes, 1)).toEqual([1])
  })

  it("returns ids, not episode objects, in list order", () => {
    const episodes = [
      episode({ id: 5, seasonNumber: 1 }),
      episode({ id: 3, seasonNumber: 1 }),
      episode({ id: 9, seasonNumber: 1 }),
    ]
    expect(missingSeasonEpisodeIds(episodes, 1)).toEqual([5, 3, 9])
  })

  it("returns an empty array for empty input", () => {
    expect(missingSeasonEpisodeIds([], 1)).toEqual([])
  })
})

describe("grabBody", () => {
  // force is sent for ANY rejected row. Server-side it is only load-bearing for
  // quality-rejected rows — on a blocklisted or non-covering row whose quality is
  // fine it is a harmless no-op, because Enqueue would have accepted it anyway.
  // Sending it uniformly keeps one client rule and does not overstate what the
  // server enforces.
  it("sends force=false for a clean movie row", () => {
    expect(grabBody(release(), { kind: "movie", id: 7 })).toEqual({
      downloadUrl: "http://x/1",
      title: "Some.Movie.2019.480p.HDTV.x264-GRP",
      protocol: "usenet",
      indexerId: "1",
      mediaKind: "movie",
      movieId: 7,
      force: false,
    })
  })
  it("sends force=true for a rejected row", () => {
    const b = grabBody(release({ rejections: ["quality not in profile"] }), { kind: "movie", id: 7 })
    expect(b.force).toBe(true)
  })
  it("sends seriesId + episodeIds for an episode target", () => {
    expect(grabBody(release(), { kind: "episode", id: 42, seriesId: 3 })).toMatchObject({
      mediaKind: "tv",
      seriesId: 3,
      episodeIds: [42],
    })
  })
  it("sends seriesId + all missing episodeIds for a season target", () => {
    expect(grabBody(release(), { kind: "season", seriesId: 3, seasonNumber: 2, episodeIds: [10, 11] }))
      .toMatchObject({ mediaKind: "tv", seriesId: 3, episodeIds: [10, 11] })
  })
})
