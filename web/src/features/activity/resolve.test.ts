// web/src/features/activity/resolve.test.ts
import { describe, it, expect } from "vitest"
import type { Movie, Series } from "@/features/library/types"
import type { QualityDefinition } from "@/features/settings/qualityTypes"
import {
  movieTitleMap, seriesTitleMap, resolveTitle, qualityName,
  eventLabel, statusLabel, statusTone, shouldRefresh,
  liveStatusLabel, queueRowDisplay,
} from "./resolve"

const movies = [
  { id: 1, title: "The Matrix", year: 1999 },
  { id: 2, title: "No Year", year: 0 },
] as Movie[]
const series = [{ id: 10, title: "The Show" }] as Series[]
const defs: QualityDefinition[] = [
  { id: 0, name: "Unknown", source: 0, resolution: 0, rank: 0 },
  { id: 3, name: "WEBDL-1080p", source: 1, resolution: 3, rank: 3 },
]

describe("title maps", () => {
  it("formats movie title with year, bare title when year is 0", () => {
    const m = movieTitleMap(movies)
    expect(m.get(1)).toBe("The Matrix (1999)")
    expect(m.get(2)).toBe("No Year")
  })
  it("maps series id to plain title", () => {
    expect(seriesTitleMap(series).get(10)).toBe("The Show")
  })
  it("returns an empty map for undefined input", () => {
    expect(movieTitleMap(undefined).size).toBe(0)
    expect(seriesTitleMap(undefined).size).toBe(0)
  })
})

describe("resolveTitle", () => {
  const mm = movieTitleMap(movies)
  const sm = seriesTitleMap(series)
  it("resolves a movie row to the clean title", () => {
    expect(resolveTitle({ mediaKind: "movie", movieId: 1, sourceTitle: "raw" }, mm, sm)).toBe("The Matrix (1999)")
  })
  it("resolves a series row to the clean title", () => {
    expect(resolveTitle({ mediaKind: "tv", seriesId: 10, sourceTitle: "raw" }, mm, sm)).toBe("The Show")
  })
  it("falls back to sourceTitle when the id is missing (deleted media)", () => {
    expect(resolveTitle({ mediaKind: "movie", movieId: 999, sourceTitle: "Some.Release" }, mm, sm)).toBe("Some.Release")
  })
  it("falls back to sourceTitle when maps are empty (still loading)", () => {
    expect(resolveTitle({ mediaKind: "movie", movieId: 1, sourceTitle: "Some.Release" }, new Map(), new Map())).toBe("Some.Release")
  })
  it("falls back to sourceTitle when there is no media id", () => {
    expect(resolveTitle({ mediaKind: "movie", sourceTitle: "Untracked.Release" }, mm, sm)).toBe("Untracked.Release")
  })
  it("returns em dash when sourceTitle is also empty", () => {
    expect(resolveTitle({ mediaKind: "movie", sourceTitle: "" }, mm, sm)).toBe("—")
  })
})

describe("qualityName", () => {
  it("resolves a numeric id to its name", () => {
    expect(qualityName(3, defs)).toBe("WEBDL-1080p")
  })
  it("returns em dash for null, 0, or unknown id", () => {
    expect(qualityName(null, defs)).toBe("—")
    expect(qualityName(0, defs)).toBe("—")
    expect(qualityName(99, defs)).toBe("—")
    expect(qualityName(3, undefined)).toBe("—")
  })
})

describe("labels and tones", () => {
  it("maps event types to labels", () => {
    expect(eventLabel("grabbed")).toBe("Grabbed")
    expect(eventLabel("imported")).toBe("Imported")
    expect(eventLabel("upgraded")).toBe("Upgraded")
    expect(eventLabel("import_failed")).toBe("Import failed")
    expect(eventLabel("weird")).toBe("weird")
  })
  it("maps queue statuses to labels", () => {
    expect(statusLabel("grabbed")).toBe("Grabbed")
    expect(statusLabel("importing")).toBe("Importing")
    expect(statusLabel("imported")).toBe("Imported")
    expect(statusLabel("failed")).toBe("Failed")
  })
  it("maps statuses to tones", () => {
    expect(statusTone("imported")).toBe("ok")
    expect(statusTone("importing")).toBe("info")
    expect(statusTone("failed")).toBe("error")
    expect(statusTone("grabbed")).toBe("neutral")
  })
})

describe("shouldRefresh", () => {
  it("is true for queue/import/download events", () => {
    expect(shouldRefresh("queue.updated")).toBe(true)
    expect(shouldRefresh("import.completed")).toBe(true)
    expect(shouldRefresh("download.status")).toBe(true)
  })
  it("is false for unrelated events", () => {
    expect(shouldRefresh("indexer.status")).toBe(false)
    expect(shouldRefresh("")).toBe(false)
  })
  it("refreshes on download.failed", () => {
    expect(shouldRefresh("download.failed")).toBe(true)
    expect(shouldRefresh("nope")).toBe(false)
  })
})

describe("liveStatusLabel", () => {
  it("maps known live statuses", () => {
    expect(liveStatusLabel("downloading")).toBe("Downloading")
    expect(liveStatusLabel("queued")).toBe("Queued")
    expect(liveStatusLabel("paused")).toBe("Paused")
    expect(liveStatusLabel("warning")).toBe("Warning")
    expect(liveStatusLabel("completed")).toBe("Completed")
  })
  it("passes through an unknown status", () => {
    expect(liveStatusLabel("weird")).toBe("weird")
  })
})

describe("queueRowDisplay", () => {
  it("shows live progress for a grabbed row with a live match at 0% (presence, not value)", () => {
    const d = queueRowDisplay({ status: "grabbed", progress: 0, downloadStatus: "downloading" })
    expect(d).toEqual({ kind: "live", percent: 0, label: "Downloading", tone: "info" })
  })
  it("shows live progress mid-download", () => {
    const d = queueRowDisplay({ status: "grabbed", progress: 42.5, downloadStatus: "downloading" })
    expect(d).toEqual({ kind: "live", percent: 42.5, label: "Downloading", tone: "info" })
  })
  it("shows Completed at 100% for a grabbed row still in the client", () => {
    const d = queueRowDisplay({ status: "grabbed", progress: 100, downloadStatus: "completed" })
    expect(d).toEqual({ kind: "live", percent: 100, label: "Completed", tone: "info" })
  })
  it("falls back to the grab status when a grabbed row has no live match", () => {
    expect(queueRowDisplay({ status: "grabbed" })).toEqual({ kind: "status", label: "Grabbed", tone: "neutral" })
  })
  it("ignores live data on non-grabbed rows (importing keeps its label)", () => {
    expect(queueRowDisplay({ status: "importing", progress: 90, downloadStatus: "downloading" }))
      .toEqual({ kind: "status", label: "Importing", tone: "info" })
  })
  it("keeps the grab label for imported and failed rows", () => {
    expect(queueRowDisplay({ status: "imported" })).toEqual({ kind: "status", label: "Imported", tone: "ok" })
    expect(queueRowDisplay({ status: "failed" })).toEqual({ kind: "status", label: "Failed", tone: "error" })
  })
})
