import { describe, it, expect } from "vitest"
import { connectionStatusBadge } from "./status"

describe("connectionStatusBadge", () => {
  it("maps status strings to tone + label", () => {
    expect(connectionStatusBadge({ status: "ok" })).toEqual({ tone: "ok", label: "OK" })
    expect(connectionStatusBadge({ status: "failed" })).toEqual({ tone: "warn", label: "Failed" })
    expect(connectionStatusBadge({ status: "" })).toEqual({ tone: "muted", label: "Unknown" })
  })
})
