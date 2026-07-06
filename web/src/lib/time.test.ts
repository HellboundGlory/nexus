import { describe, it, expect } from "vitest"
import { relativeTime } from "@/lib/time"

describe("relativeTime", () => {
  const now = 1_000_000
  it("shows just now under 5s", () => {
    expect(relativeTime(now - 2_000, now)).toBe("just now")
  })
  it("shows seconds", () => {
    expect(relativeTime(now - 12_000, now)).toBe("12s ago")
  })
  it("shows minutes", () => {
    expect(relativeTime(now - 3 * 60_000, now)).toBe("3m ago")
  })
  it("shows hours", () => {
    expect(relativeTime(now - 2 * 3_600_000, now)).toBe("2h ago")
  })
})
