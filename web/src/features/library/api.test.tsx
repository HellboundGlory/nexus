import { describe, it, expect, vi, beforeEach } from "vitest"
import type { ReactNode } from "react"
import { renderHook, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import * as apiClient from "@/lib/api"
import { useLookup } from "@/features/library/api"

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

describe("useLookup", () => {
  it("does not fetch when the term is empty", () => {
    renderHook(() => useLookup("", "movie"), { wrapper: wrapper() })
    expect(apiClient.apiGet).not.toHaveBeenCalled()
  })

  it("fetches when the term is non-empty", async () => {
    vi.mocked(apiClient.apiGet).mockResolvedValue([])
    renderHook(() => useLookup("bear", "tv"), { wrapper: wrapper() })
    await waitFor(() => expect(apiClient.apiGet).toHaveBeenCalledWith("/media/lookup?term=bear&kind=tv"))
  })
})
