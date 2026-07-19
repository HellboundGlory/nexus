import { describe, it, expect } from "vitest"
import { formatDuration, humanizeInterval, humanizeName } from "@/features/system/format"

describe("format", () => {
  it("formats duration HH:MM:SS", () => expect(formatDuration(65)).toBe("00:01:05"))
  it("humanizes interval", () => {
    expect(humanizeInterval(5)).toBe("5 seconds")
    expect(humanizeInterval(900)).toBe("15 minutes")
    expect(humanizeInterval(43200)).toBe("12 hours")
  })
  it("humanizes camelCase names", () => {
    expect(humanizeName("ImportCompletedDownloads")).toBe("Import Completed Downloads")
    expect(humanizeName("RSSSync")).toBe("RSS Sync")
  })
})
