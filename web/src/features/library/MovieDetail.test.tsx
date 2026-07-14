import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { ToastProvider } from "@/lib/toast"
import { MovieDetail } from "@/features/library/MovieDetail"
import * as lib from "@/features/library/api"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return {
    ...actual,
    useMovieDetail: vi.fn(), useQualityProfiles: vi.fn(), useSetMonitored: vi.fn(),
    useAssignProfile: vi.fn(), useRefresh: vi.fn(), useDelete: vi.fn(), useSearch: vi.fn(),
  }
})

beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, ...extra } as unknown as never
}

function renderMovie(id: number, movie: object, search = vi.fn()) {
  vi.mocked(lib.useMovieDetail).mockReturnValue({ data: movie, isLoading: false, isError: false, refetch: vi.fn() } as unknown as ReturnType<typeof lib.useMovieDetail>)
  vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
  vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
  vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
  vi.mocked(lib.useRefresh).mockReturnValue(mut())
  vi.mocked(lib.useDelete).mockReturnValue(mut())
  vi.mocked(lib.useSearch).mockReturnValue(mut({ mutate: search }))
  render(
    <MemoryRouter>
      <ToastProvider>
        <MovieDetail id={id} />
      </ToastProvider>
    </MemoryRouter>,
  )
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
})
