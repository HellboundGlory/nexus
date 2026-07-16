import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import type { ReactNode } from "react"
import { renderHook, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import * as apiClient from "@/lib/api"
import { useQueue } from "@/features/activity/api"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, apiGet: vi.fn() }
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
