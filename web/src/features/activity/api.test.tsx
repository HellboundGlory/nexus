import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import type { ReactNode } from "react"
import { renderHook, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import * as apiClient from "@/lib/api"
import { useQueue, useHistory, useRemoveQueueItem, useClearQueue } from "@/features/activity/api"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, apiGet: vi.fn(), apiDelete: vi.fn() }
})

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  }
}

beforeEach(() => vi.clearAllMocks())
afterEach(() => vi.useRealTimers())

describe("useQueue", () => {
  // /queue enriches each grabbed row with live progress from the download
  // client on every request, so the poll interval IS the progress refresh rate.
  // Without it the bar only moves when a WS download.status event lands.
  it("polls every 5s so download progress advances visibly", async () => {
    vi.mocked(apiClient.apiGet).mockResolvedValue([])
    vi.useFakeTimers({ shouldAdvanceTime: true })

    renderHook(() => useQueue(), { wrapper: wrapper() })
    await waitFor(() => expect(apiClient.apiGet).toHaveBeenCalledTimes(1))

    await vi.advanceTimersByTimeAsync(5_000)
    await waitFor(() => expect(apiClient.apiGet).toHaveBeenCalledTimes(2))

    await vi.advanceTimersByTimeAsync(5_000)
    await waitFor(() => expect(apiClient.apiGet).toHaveBeenCalledTimes(3))

    expect(apiClient.apiGet).toHaveBeenCalledWith("/queue")
  })
})

describe("useHistory", () => {
  it("requests the asked-for page and unwraps the envelope", async () => {
    vi.mocked(apiClient.apiGet).mockResolvedValue({ items: [{ id: 1 }], page: 2, pageSize: 25, total: 60 })
    const { result } = renderHook(() => useHistory(2, 25), { wrapper: wrapper() })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(apiClient.apiGet).toHaveBeenCalledWith("/history?page=2&pageSize=25")
    expect(result.current.data?.total).toBe(60)
  })

  it("keys the cache by page so changing page refetches", async () => {
    vi.mocked(apiClient.apiGet).mockResolvedValue({ items: [], page: 1, pageSize: 50, total: 0 })
    const w = wrapper()
    const { result, rerender } = renderHook(({ p }: { p: number }) => useHistory(p, 50), {
      wrapper: w,
      initialProps: { p: 1 },
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    rerender({ p: 2 })
    await waitFor(() => expect(apiClient.apiGet).toHaveBeenCalledWith("/history?page=2&pageSize=50"))
  })
})

describe("useRemoveQueueItem", () => {
  it("defaults nothing — it sends exactly the flags it is given", async () => {
    vi.mocked(apiClient.apiDelete).mockResolvedValue({ ok: true })
    const { result } = renderHook(() => useRemoveQueueItem(), { wrapper: wrapper() })
    result.current.mutate({ id: 7, removeFromClient: true, blocklist: false })
    await waitFor(() =>
      expect(apiClient.apiDelete).toHaveBeenCalledWith("/queue/7?removeFromClient=true&blocklist=false"),
    )
  })

  it("passes both flags through when set", async () => {
    vi.mocked(apiClient.apiDelete).mockResolvedValue({ ok: true })
    const { result } = renderHook(() => useRemoveQueueItem(), { wrapper: wrapper() })
    result.current.mutate({ id: 7, removeFromClient: false, blocklist: true })
    await waitFor(() =>
      expect(apiClient.apiDelete).toHaveBeenCalledWith("/queue/7?removeFromClient=false&blocklist=true"),
    )
  })
})

describe("useClearQueue", () => {
  it("omits force by default", async () => {
    vi.mocked(apiClient.apiDelete).mockResolvedValue({ removed: 3 })
    const { result } = renderHook(() => useClearQueue(), { wrapper: wrapper() })
    result.current.mutate({})
    await waitFor(() => expect(apiClient.apiDelete).toHaveBeenCalledWith("/queue"))
  })

  it("sends force=true when forcing", async () => {
    vi.mocked(apiClient.apiDelete).mockResolvedValue({ removed: 3 })
    const { result } = renderHook(() => useClearQueue(), { wrapper: wrapper() })
    result.current.mutate({ force: true })
    await waitFor(() => expect(apiClient.apiDelete).toHaveBeenCalledWith("/queue?force=true"))
  })
})
