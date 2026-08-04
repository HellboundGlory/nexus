import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, within, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ToastProvider } from "@/lib/toast"
import { SeriesDetail } from "@/features/library/SeriesDetail"
import * as lib from "@/features/library/api"
import * as tagApi from "@/features/settings/tagApi"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return {
    ...actual,
    useSeriesDetail: vi.fn(), useQualityProfiles: vi.fn(), useSetMonitored: vi.fn(),
    useAssignProfile: vi.fn(), useRefresh: vi.fn(), useDelete: vi.fn(), useSearch: vi.fn(),
    useMediaTags: vi.fn(), useSetMediaTags: vi.fn(),
  }
})

vi.mock("@/features/settings/tagApi", async (orig) => {
  const actual = await orig<typeof import("@/features/settings/tagApi")>()
  return { ...actual, useTags: vi.fn(), useCreateTag: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())
function mut(extra: object = {}) {
  return { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, ...extra } as unknown as never
}

describe("SeriesDetail", () => {
  it("renders seasons + episodes and searches an episode", async () => {
    const search = vi.fn()
    vi.mocked(lib.useSeriesDetail).mockReturnValue({
      data: {
        id: 3, title: "The Bear", firstAired: "2022-06-23", overview: "", monitored: true,
        qualityProfileId: null, posterUrl: "", fanartUrl: "", episodeCount: 2, episodeFileCount: 1,
        seasons: [{ id: 30, seriesId: 3, seasonNumber: 1, monitored: true }],
        episodes: [
          { id: 101, seriesId: 3, seasonNumber: 1, episodeNumber: 1, tmdbId: 0, title: "System", overview: "", airDate: "2022-06-23", monitored: true, hasFile: true },
          { id: 102, seriesId: 3, seasonNumber: 1, episodeNumber: 2, tmdbId: 0, title: "Hands", overview: "", airDate: "2022-06-23", monitored: true, hasFile: false },
        ],
      },
      isLoading: false, isError: false, refetch: vi.fn(),
    } as unknown as ReturnType<typeof lib.useSeriesDetail>)
    vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
    vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
    vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
    vi.mocked(lib.useRefresh).mockReturnValue(mut())
    vi.mocked(lib.useDelete).mockReturnValue(mut())
    vi.mocked(lib.useSearch).mockReturnValue(mut({ mutate: search }))
    vi.mocked(lib.useMediaTags).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useMediaTags>)
    vi.mocked(lib.useSetMediaTags).mockReturnValue(mut())
    vi.mocked(tagApi.useTags).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    vi.mocked(tagApi.useCreateTag).mockReturnValue(mut())

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <ToastProvider>
            <SeriesDetail id={3} />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(screen.getByText("The Bear")).toBeInTheDocument()
    expect(screen.getByText("System")).toBeInTheDocument()
    expect(screen.getByText("Hands")).toBeInTheDocument()
    // per-episode search buttons; click the second episode's
    const searchButtons = screen.getAllByRole("button", { name: /^search episode/i })
    await userEvent.click(searchButtons[1])
    expect(search).toHaveBeenCalledWith({ kind: "episode", id: 102 })
  })

  it("blocks the series Search and warns when no quality profile is assigned", async () => {
    const search = vi.fn()
    vi.mocked(lib.useSeriesDetail).mockReturnValue({
      data: {
        id: 3, title: "The Bear", firstAired: "2022-06-23", overview: "", monitored: true,
        qualityProfileId: null, posterUrl: "", fanartUrl: "", episodeCount: 0, episodeFileCount: 0,
        seasons: [], episodes: [],
      },
      isLoading: false, isError: false, refetch: vi.fn(),
    } as unknown as ReturnType<typeof lib.useSeriesDetail>)
    vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
    vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
    vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
    vi.mocked(lib.useRefresh).mockReturnValue(mut())
    vi.mocked(lib.useDelete).mockReturnValue(mut())
    vi.mocked(lib.useSearch).mockReturnValue(mut({ mutate: search }))
    vi.mocked(lib.useMediaTags).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useMediaTags>)
    vi.mocked(lib.useSetMediaTags).mockReturnValue(mut())
    vi.mocked(tagApi.useTags).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    vi.mocked(tagApi.useCreateTag).mockReturnValue(mut())

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <ToastProvider>
            <SeriesDetail id={3} />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }))
    expect(search).not.toHaveBeenCalled()
    expect(await screen.findByText(/assign a quality profile/i)).toBeInTheDocument()
  })

  it("opens the delete dialog and deletes with the chosen disk option", async () => {
    const del = vi.fn()
    vi.mocked(lib.useSeriesDetail).mockReturnValue({
      data: {
        id: 3, title: "The Bear", firstAired: "2022-06-23", overview: "", monitored: true,
        qualityProfileId: 1, posterUrl: "", fanartUrl: "", episodeCount: 0, episodeFileCount: 0,
        seasons: [], episodes: [],
      },
      isLoading: false, isError: false, refetch: vi.fn(),
    } as unknown as ReturnType<typeof lib.useSeriesDetail>)
    vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
    vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
    vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
    vi.mocked(lib.useRefresh).mockReturnValue(mut())
    vi.mocked(lib.useDelete).mockReturnValue(mut({ mutate: del }))
    vi.mocked(lib.useSearch).mockReturnValue(mut())
    vi.mocked(lib.useMediaTags).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useMediaTags>)
    vi.mocked(lib.useSetMediaTags).mockReturnValue(mut())
    vi.mocked(tagApi.useTags).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    vi.mocked(tagApi.useCreateTag).mockReturnValue(mut())

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <ToastProvider>
            <SeriesDetail id={3} />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    await userEvent.click(screen.getByRole("button", { name: /^delete$/i })) // page button opens dialog
    const dialog = screen.getByRole("dialog")
    await userEvent.click(within(dialog).getByRole("checkbox"))
    await userEvent.click(within(dialog).getByRole("button", { name: /^delete$/i }))
    expect(del).toHaveBeenCalledWith(expect.objectContaining({ kind: "series", id: 3, deleteFiles: true }), expect.anything())
  })

  function renderSeriesWithTags(setTags: ReturnType<typeof vi.fn>, createTag: ReturnType<typeof vi.fn>) {
    vi.mocked(lib.useSeriesDetail).mockReturnValue({
      data: {
        id: 3, title: "The Bear", firstAired: "2022-06-23", overview: "", monitored: true,
        qualityProfileId: 1, posterUrl: "", fanartUrl: "", episodeCount: 0, episodeFileCount: 0,
        seasons: [], episodes: [],
      },
      isLoading: false, isError: false, refetch: vi.fn(),
    } as unknown as ReturnType<typeof lib.useSeriesDetail>)
    vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
    vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
    vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
    vi.mocked(lib.useRefresh).mockReturnValue(mut())
    vi.mocked(lib.useDelete).mockReturnValue(mut())
    vi.mocked(lib.useSearch).mockReturnValue(mut())
    vi.mocked(lib.useMediaTags).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useMediaTags>)
    vi.mocked(lib.useSetMediaTags).mockReturnValue(mut({ mutate: setTags }))
    vi.mocked(tagApi.useTags).mockReturnValue({
      data: [{ id: 7, label: "anime", seriesCount: 0, movieCount: 0 }],
      isLoading: false, isError: false,
    } as never)
    vi.mocked(tagApi.useCreateTag).mockReturnValue(mut({ mutateAsync: createTag }))

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <ToastProvider>
            <SeriesDetail id={3} />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    // Pin the caller-side kind literal: a copy-paste bug (e.g. "movie" here)
    // must not ship green even though the hooks themselves are mocked.
    expect(lib.useMediaTags).toHaveBeenCalledWith("series", 3)
    expect(lib.useSetMediaTags).toHaveBeenCalledWith("series", 3)
  }

  it("assigns an existing tag to the series", async () => {
    const setTags = vi.fn()
    renderSeriesWithTags(setTags, vi.fn())
    await userEvent.type(screen.getByLabelText("Tags"), "anime{Enter}")
    await waitFor(() => expect(setTags).toHaveBeenCalledWith([7]))
  })

  // Pins the seam between TagInput's onCreate contract and useCreateTag: a novel
  // label must create the tag and then assign the id the server returned.
  it("creates a new tag from the series detail page and assigns it", async () => {
    const setTags = vi.fn()
    const createTag = vi.fn().mockResolvedValue({ id: 42, label: "documentary", seriesCount: 0, movieCount: 0 })
    renderSeriesWithTags(setTags, createTag)
    await userEvent.type(screen.getByLabelText("Tags"), "documentary{Enter}")
    await waitFor(() => expect(createTag).toHaveBeenCalledWith("documentary"))
    await waitFor(() => expect(setTags).toHaveBeenCalledWith([42]))
  })
})
