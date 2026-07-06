import { describe, it, expect, beforeEach, vi } from "vitest"
import { apiGet, apiPost, ApiError, getStatus, setUnauthorizedHandler } from "@/lib/api"

function mockFetch(status: number, body: unknown, contentType = "application/json") {
  return vi.fn(async () =>
    new Response(body == null ? null : JSON.stringify(body), {
      status,
      headers: { "Content-Type": contentType },
    }),
  )
}

beforeEach(() => {
  setUnauthorizedHandler(null)
  vi.restoreAllMocks()
})

describe("api client", () => {
  it("apiGet prefixes /api/v1 and sends credentials", async () => {
    const f = mockFetch(200, { ok: true })
    vi.stubGlobal("fetch", f)
    await apiGet("/system/status")
    const [url, init] = f.mock.calls[0]
    expect(url).toBe("/api/v1/system/status")
    expect((init as RequestInit).credentials).toBe("include")
  })

  it("getStatus returns typed status", async () => {
    vi.stubGlobal("fetch", mockFetch(200, { version: "0.1.0", appName: "Nexus", healthy: true, taskCount: 2 }))
    const s = await getStatus()
    expect(s.version).toBe("0.1.0")
    expect(s.taskCount).toBe(2)
  })

  it("normalizes the error envelope into ApiError", async () => {
    vi.stubGlobal("fetch", mockFetch(400, { error: { code: "bad_request", message: "invalid JSON" } }))
    await expect(apiPost("/auth/login", {})).rejects.toMatchObject({
      status: 400,
      code: "bad_request",
      message: "invalid JSON",
    })
  })

  it("wraps a non-JSON error body", async () => {
    vi.stubGlobal("fetch", mockFetch(500, "boom", "text/plain"))
    const err = await apiGet("/x").catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(500)
    expect(err.code).toBe("unknown")
  })

  it("invokes the unauthorized handler on 401", async () => {
    vi.stubGlobal("fetch", mockFetch(401, { error: { code: "unauthorized", message: "nope" } }))
    const onUnauth = vi.fn()
    setUnauthorizedHandler(onUnauth)
    await expect(getStatus()).rejects.toBeInstanceOf(ApiError)
    expect(onUnauth).toHaveBeenCalledOnce()
  })
})
