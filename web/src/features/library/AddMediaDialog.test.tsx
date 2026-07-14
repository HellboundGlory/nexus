import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { AddMediaDialog } from "@/features/library/AddMediaDialog"
import * as lib from "@/features/library/api"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return {
    ...actual,
    useLookup: vi.fn(),
    useRootFolders: vi.fn(),
    useAddMovie: vi.fn(),
    useAddSeries: vi.fn(),
  }
})

beforeEach(() => vi.clearAllMocks())

function stub() {
  vi.mocked(lib.useLookup).mockReturnValue({ data: [{ tmdbId: 1, title: "Dune", year: 2021, overview: "", posterUrl: "", kind: "movie" }], isLoading: false } as unknown as ReturnType<typeof lib.useLookup>)
  vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync: vi.fn(), isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
  vi.mocked(lib.useAddSeries).mockReturnValue({ mutateAsync: vi.fn(), isPending: false } as unknown as ReturnType<typeof lib.useAddSeries>)
}

describe("AddMediaDialog", () => {
  it("blocks submit and guides to Settings when there are no root folders", async () => {
    stub()
    vi.mocked(lib.useRootFolders).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useRootFolders>)
    render(
      <ToastProvider>
        <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
      </ToastProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
    await userEvent.click(await screen.findByText("Dune"))
    expect(screen.getByText(/no root folder configured/i)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /add movie/i })).toBeDisabled()
  })

  it("surfaces a lookup error instead of a silent empty list", async () => {
    vi.mocked(lib.useLookup).mockReturnValue({
      data: undefined, isLoading: false, isError: true,
      error: new ApiError(400, "not_configured", "metadata provider not configured"),
    } as unknown as ReturnType<typeof lib.useLookup>)
    vi.mocked(lib.useRootFolders).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useRootFolders>)
    vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync: vi.fn(), isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
    vi.mocked(lib.useAddSeries).mockReturnValue({ mutateAsync: vi.fn(), isPending: false } as unknown as ReturnType<typeof lib.useAddSeries>)
    render(
      <ToastProvider>
        <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
      </ToastProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
    expect(await screen.findByText(/metadata provider not configured/i)).toBeInTheDocument()
  })

  it("renders results as poster tiles and can reorder by sort", async () => {
    stub()
    vi.mocked(lib.useLookup).mockReturnValue({
      data: [
        { tmdbId: 1, title: "Older Film", year: 2001, overview: "", posterUrl: "", kind: "movie" },
        { tmdbId: 2, title: "Newer Film", year: 2020, overview: "", posterUrl: "", kind: "movie" },
      ],
      isLoading: false,
    } as unknown as ReturnType<typeof lib.useLookup>)
    vi.mocked(lib.useRootFolders).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useRootFolders>)
    render(
      <ToastProvider>
        <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
      </ToastProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/search/i), "film")
    // the result tiles show the title and the year
    expect(await screen.findByText("Older Film")).toBeInTheDocument()
    expect(screen.getByText("Newer Film")).toBeInTheDocument()
    expect(screen.getByText("2001")).toBeInTheDocument()
    expect(screen.getByText("2020")).toBeInTheDocument()
    // the sort control is present
    const sortSelect = screen.getByLabelText("Sort")
    expect(sortSelect).toBeInTheDocument()

    function tileOrder() {
      return screen
        .getAllByRole("button")
        .map((b) => b.textContent ?? "")
        .filter((t) => t.includes("Older Film") || t.includes("Newer Film"))
    }

    // default (relevance) order matches the order returned by the lookup
    expect(tileOrder()).toEqual([
      expect.stringContaining("Older Film"),
      expect.stringContaining("Newer Film"),
    ])

    // oldest-first: 2001 before 2020
    await userEvent.selectOptions(sortSelect, "oldest")
    expect(tileOrder()).toEqual([
      expect.stringContaining("Older Film"),
      expect.stringContaining("Newer Film"),
    ])

    // newest-first: order flips
    await userEvent.selectOptions(sortSelect, "newest")
    expect(tileOrder()).toEqual([
      expect.stringContaining("Newer Film"),
      expect.stringContaining("Older Film"),
    ])
  })
})
