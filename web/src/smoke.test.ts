import { describe, it, expect } from "vitest"
import { cn } from "@/lib/utils"

describe("toolchain smoke", () => {
  it("cn merges class names", () => {
    expect(cn("a", false && "b", "c")).toBe("a c")
  })
})
