import { describe, it, expect, beforeEach } from "vitest"
import {
  clampScale, readScale, MIN_SCALE, MAX_SCALE, DEFAULT_SCALE, SCALE_KEY,
} from "@/features/library/useGridScale"

describe("clampScale", () => {
  it("passes through an in-range value", () => {
    expect(clampScale(150)).toBe(150)
  })
  it("clamps below MIN and above MAX", () => {
    expect(clampScale(10)).toBe(MIN_SCALE)
    expect(clampScale(9999)).toBe(MAX_SCALE)
  })
  it("falls back to DEFAULT on non-finite input", () => {
    expect(clampScale(NaN)).toBe(DEFAULT_SCALE)
  })
})

describe("readScale", () => {
  beforeEach(() => localStorage.clear())
  it("returns DEFAULT when nothing is stored", () => {
    expect(readScale()).toBe(DEFAULT_SCALE)
  })
  it("returns DEFAULT when the stored value is garbage", () => {
    localStorage.setItem(SCALE_KEY, "not-a-number")
    expect(readScale()).toBe(DEFAULT_SCALE)
  })
  it("returns the clamped stored value", () => {
    localStorage.setItem(SCALE_KEY, "9999")
    expect(readScale()).toBe(MAX_SCALE)
  })
})
