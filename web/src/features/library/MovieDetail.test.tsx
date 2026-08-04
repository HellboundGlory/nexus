import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ToastProvider } from "@/lib/toast"
import { MovieDetail } from "@/features/library/MovieDetail"
import * as lib from "@/features/library/api"
import * as tagApi from "@/features/settings/tagApi"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return {
    ...actual,
    useMovieDetail: vi.fn(), useQualityProfiles: vi.fn(), useSetMonitored: vi.fn(),
    useAssignProfile: vi.fn(), useRefresh: vi.fn(), useDelete: vi.fn(), useSearch: vi.fn(),
    useDeleteMovieFile: vi.fn(), useMediaTags: vi.fn(),
  }
})

vi.mock("@/features/settings/tagApi", async (orig) => {
  const actual = await orig<typeof import("@/features/settings/tagApi")>()
  return { ...actual, useTags: vi.fn() }
})

beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, ...extra } as unknown as never
}

type RenderOpts = {
  mediaTags?: number[]
  tags?: { id: number; label: string; seriesCount: number; movieCount: number }[]
}

function renderMovie(id: number, movie: object, search = vi.fn(), delFile = vi.fn(), delItem = vi.fn(), opts: RenderOpts = {}) {
  vi.mocked(lib.useMovieDetail).mockReturnValue({ data: movie, isLoading: false, isError: false, refetch: vi.fn() } as unknown as ReturnType<typeof lib.useMovieDetail>)
  vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
  vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
  vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
  vi.mocked(lib.useRefresh).mockReturnValue(mut())
  vi.mocked(lib.useDelete).mockReturnValue(mut({ mutate: delItem }))
  vi.mocked(lib.useSearch).mockReturnValue(mut({ mutate: search }))
  vi.mocked(lib.useDeleteMovieFile).mockReturnValue(mut({ mutate: delFile }))
  vi.mocked(lib.useMediaTags).mockReturnValue({ data: opts.mediaTags ?? [] } as unknown as ReturnType<typeof lib.useMediaTags>)
  vi.mocked(tagApi.useTags).mockReturnValue({ data: opts.tags ?? [], isLoading: false, isError: false } as never)
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ToastProvider>
          <MovieDetail id={id} />
        </ToastProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  // Pin the caller-side kind literal: a copy-paste bug (e.g. "series" here)
  // must not ship green even though the hook itself is mocked.
  expect(lib.useMediaTags).toHaveBeenCalledWith("movie", id)
  return search
}

describe("MovieDetail", () => {
  it("triggers a search when a quality profile is assigned", async () => {
    const search = renderMovie(5, { id: 5, title: "Dune", year: 2021, overview: "x", monitored: true, hasFile: false, qualityProfileId: 1, posterUrl: "", fanartUrl: "" })
    expect(screen.getByText("Dune")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }))
    expect(search).toHaveBeenCalledWith({ kind: "movie", id: 5 })
  })

  it("does not search and warns when no quality profile is assigned", async () => {
    const search = renderMovie(5, { id: 5, title: "Dune", year: 2021, overview: "x", monitored: true, hasFile: false, qualityProfileId: null, posterUrl: "", fanartUrl: "" })
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }))
    expect(search).not.toHaveBeenCalled()
    expect(await screen.findByText(/assign a quality profile/i)).toBeInTheDocument()
  })

  it("floats the back link above the banner", () => {
    renderMovie(5, { id: 5, title: "Dune", year: 2021, overview: "x", monitored: true, hasFile: false, qualityProfileId: 1, posterUrl: "", fanartUrl: "http://img/bd.jpg" })
    expect(screen.getByRole("button", { name: /← movies/i })).toBeInTheDocument()
  })

  const FILE = {
    relativePath: "Film (2020)/Film.2020.1080p.mkv",
    size: 8455160320, qualityId: 9, quality: "Bluray-1080p",
    addedAt: "2026-07-10T14:22:03Z",
  }

  it("renders the file box when a file is present", () => {
    renderMovie(5, { id: 5, title: "Film", year: 2020, overview: "x", monitored: true, hasFile: true, qualityProfileId: 1, posterUrl: "", fanartUrl: "", file: FILE })
    expect(screen.getByText("Film.2020.1080p.mkv")).toBeInTheDocument()
    expect(screen.getByText(/Bluray-1080p/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /delete file/i })).toBeInTheDocument()
  })

  it("hides the file box when no file", () => {
    renderMovie(5, { id: 5, title: "Film", year: 2020, overview: "x", monitored: true, hasFile: false, qualityProfileId: 1, posterUrl: "", fanartUrl: "" })
    expect(screen.queryByRole("button", { name: /delete file/i })).not.toBeInTheDocument()
  })

  it("deletes the file after confirm", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true)
    const del = vi.fn()
    renderMovie(5, { id: 5, title: "Film", year: 2020, overview: "x", monitored: true, hasFile: true, qualityProfileId: 1, posterUrl: "", fanartUrl: "", file: FILE }, vi.fn(), del)
    await userEvent.click(screen.getByRole("button", { name: /delete file/i }))
    expect(del).toHaveBeenCalledWith(5, expect.anything())
  })

  it("opens the delete dialog and deletes with the chosen disk option", async () => {
    const del = vi.fn()
    renderMovie(5, { id: 5, title: "Film", year: 2020, overview: "x", monitored: true, hasFile: false, qualityProfileId: 1, posterUrl: "", fanartUrl: "" }, vi.fn(), vi.fn(), del)
    await userEvent.click(screen.getByRole("button", { name: /^delete$/i })) // page button opens dialog
    const dialog = screen.getByRole("dialog")
    await userEvent.click(within(dialog).getByRole("checkbox"))
    await userEvent.click(within(dialog).getByRole("button", { name: /^delete$/i }))
    expect(del).toHaveBeenCalledWith(expect.objectContaining({ kind: "movie", id: 5, deleteFiles: true }), expect.anything())
  })

  it("renders a read-only Tags chip that reveals assigned tags on hover", async () => {
    renderMovie(5, { id: 5, title: "Dune", year: 2021, overview: "x", monitored: true, hasFile: false, qualityProfileId: 1, posterUrl: "", fanartUrl: "" }, vi.fn(), vi.fn(), vi.fn(), {
      mediaTags: [8],
      tags: [{ id: 8, label: "classics", seriesCount: 0, movieCount: 0 }],
    })
    const chip = screen.getByTestId("tags-chip")
    expect(chip).toBeInTheDocument()
    // hidden until hover
    expect(screen.queryByText("classics")).not.toBeInTheDocument()
    await userEvent.hover(chip)
    expect(screen.getByText("classics")).toBeInTheDocument()
    await userEvent.unhover(chip)
    expect(screen.queryByText("classics")).not.toBeInTheDocument()
  })

  it("shows 'No tags.' in the panel when nothing is assigned", async () => {
    renderMovie(5, { id: 5, title: "Dune", year: 2021, overview: "x", monitored: true, hasFile: false, qualityProfileId: 1, posterUrl: "", fanartUrl: "" }, vi.fn(), vi.fn(), vi.fn(), {
      tags: [{ id: 8, label: "classics", seriesCount: 0, movieCount: 0 }],
    })
    await userEvent.hover(screen.getByTestId("tags-chip"))
    expect(screen.getByText("No tags.")).toBeInTheDocument()
  })
})
