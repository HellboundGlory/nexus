import { describe, it, expect, vi, beforeEach } from "vitest"
import type { ReactNode } from "react"
import { renderHook, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import * as apiClient from "@/lib/api"
import { useLookup, useMediaTags, useSetMediaTags } from "@/features/library/api"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, apiGet: vi.fn(), apiPut: vi.fn() }
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

describe("useMediaTags", () => {
  it("fetches a series' tags by kind path and extracts .tagIds", async () => {
    vi.mocked(apiClient.apiGet).mockResolvedValue({ tagIds: [7] })
    const { result } = renderHook(() => useMediaTags("series", 3), { wrapper: wrapper() })
    await waitFor(() => expect(apiClient.apiGet).toHaveBeenCalledWith("/series/3/tags"))
    await waitFor(() => expect(result.current.data).toEqual([7]))
  })

  it("fetches a movie's tags by kind path and extracts .tagIds", async () => {
    vi.mocked(apiClient.apiGet).mockResolvedValue({ tagIds: [8] })
    const { result } = renderHook(() => useMediaTags("movie", 5), { wrapper: wrapper() })
    await waitFor(() => expect(apiClient.apiGet).toHaveBeenCalledWith("/movies/5/tags"))
    await waitFor(() => expect(result.current.data).toEqual([8]))
  })
})

describe("useSetMediaTags", () => {
  it("PUTs the { tagIds } wrapper to the movie kind path from a { id, tagIds } variable", async () => {
    vi.mocked(apiClient.apiPut).mockResolvedValue({ ok: true })
    const { result } = renderHook(() => useSetMediaTags("movie"), { wrapper: wrapper() })
    result.current.mutate({ id: 5, tagIds: [8] })
    await waitFor(() => expect(apiClient.apiPut).toHaveBeenCalledWith("/movies/5/tags", { tagIds: [8] }))
  })

  it("PUTs the { tagIds } wrapper to the series kind path from a { id, tagIds } variable", async () => {
    vi.mocked(apiClient.apiPut).mockResolvedValue({ ok: true })
    const { result } = renderHook(() => useSetMediaTags("series"), { wrapper: wrapper() })
    result.current.mutate({ id: 3, tagIds: [7] })
    await waitFor(() => expect(apiClient.apiPut).toHaveBeenCalledWith("/series/3/tags", { tagIds: [7] }))
  })
})
