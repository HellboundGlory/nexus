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

describe("MovieDetail", () => {
  it("renders the movie and triggers a search toast", async () => {
    const search = vi.fn()
    vi.mocked(lib.useMovieDetail).mockReturnValue({ data: { id: 5, title: "Dune", year: 2021, overview: "x", monitored: true, hasFile: false, qualityProfileId: null, posterUrl: "", fanartUrl: "" }, isLoading: false, isError: false, refetch: vi.fn() } as unknown as ReturnType<typeof lib.useMovieDetail>)
    vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
    vi.mocked(lib.useSetMonitored).mockReturnValue(mut())
    vi.mocked(lib.useAssignProfile).mockReturnValue(mut())
    vi.mocked(lib.useRefresh).mockReturnValue(mut())
    vi.mocked(lib.useDelete).mockReturnValue(mut())
    vi.mocked(lib.useSearch).mockReturnValue(mut({ mutate: search }))

    render(
      <MemoryRouter>
        <ToastProvider>
          <MovieDetail id={5} />
        </ToastProvider>
      </MemoryRouter>,
    )
    expect(screen.getByText("Dune")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: /search/i }))
    expect(search).toHaveBeenCalledWith({ kind: "movie", id: 5 })
  })
})
