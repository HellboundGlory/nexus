import { describe, it, expect } from "vitest"
import { movieBadge, seriesBadge } from "@/features/library/StatusBadge"

describe("badge logic", () => {
  it("movie downloaded → ok", () => {
    expect(movieBadge({ monitored: true, hasFile: true })).toEqual({ label: "Downloaded", tone: "ok" })
  })
  it("movie monitored, no file → warn Missing", () => {
    expect(movieBadge({ monitored: true, hasFile: false })).toEqual({ label: "Missing", tone: "warn" })
  })
  it("movie unmonitored, no file → muted", () => {
    expect(movieBadge({ monitored: false, hasFile: false })).toEqual({ label: "Unmonitored", tone: "muted" })
  })
  it("series complete → ok", () => {
    expect(seriesBadge({ monitored: true, episodeCount: 10, episodeFileCount: 10 })).toEqual({ label: "10 / 10", tone: "ok" })
  })
  it("series partial → warn", () => {
    expect(seriesBadge({ monitored: true, episodeCount: 10, episodeFileCount: 7 })).toEqual({ label: "7 / 10", tone: "warn" })
  })
  it("series unmonitored → muted", () => {
    expect(seriesBadge({ monitored: false, episodeCount: 0, episodeFileCount: 0 })).toEqual({ label: "0 / 0", tone: "muted" })
  })
})
