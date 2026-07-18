import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { AddMediaDialog } from "@/features/library/AddMediaDialog"
import * as lib from "@/features/library/api"
import * as md from "@/features/settings/mediaDefaultsApi"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return {
    ...actual,
    useLookup: vi.fn(),
    useRootFolders: vi.fn(),
    useQualityProfiles: vi.fn(),
    useAddMovie: vi.fn(),
    useAddSeries: vi.fn(),
  }
})
vi.mock("@/features/settings/mediaDefaultsApi")

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
  vi.mocked(md.useMediaDefaults).mockReturnValue({ data: { movie: { rootFolderId: null, qualityProfileId: null }, tv: { rootFolderId: null, qualityProfileId: null } } } as unknown as ReturnType<typeof md.useMediaDefaults>)
})

function stub() {
  vi.mocked(lib.useLookup).mockReturnValue({ data: [{ tmdbId: 1, title: "Dune", year: 2021, overview: "", posterUrl: "", kind: "movie" }], isLoading: false } as unknown as ReturnType<typeof lib.useLookup>)
  vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync: vi.fn(), isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
  vi.mocked(lib.useAddSeries).mockReturnValue({ mutateAsync: vi.fn(), isPending: false } as unknown as ReturnType<typeof lib.useAddSeries>)
  vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [{ id: 5, name: "HD-1080p", cutoffQualityId: 7, upgradeAllowed: true, items: [], createdAt: "" }] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
  vi.mocked(md.useMediaDefaults).mockReturnValue({ data: { movie: { rootFolderId: 1, qualityProfileId: 5 }, tv: { rootFolderId: null, qualityProfileId: null } } } as unknown as ReturnType<typeof md.useMediaDefaults>)
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

  it("pre-selects the default root folder and profile, and sends them on add", async () => {
    stub()
    const mutateAsync = vi.fn().mockResolvedValue({})
    vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync, isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
    vi.mocked(lib.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }] } as unknown as ReturnType<typeof lib.useRootFolders>)
    render(
      <ToastProvider>
        <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
      </ToastProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
    await userEvent.click(await screen.findByText("Dune"))

    // defaults are pre-selected; with both set, Add is enabled
    expect((screen.getByLabelText("Root folder") as HTMLSelectElement).value).toBe("1")
    expect((screen.getByLabelText("Quality profile") as HTMLSelectElement).value).toBe("5")
    expect(screen.getByRole("button", { name: /add movie/i })).toBeEnabled()

    await userEvent.click(screen.getByRole("button", { name: /add movie/i }))
    expect(mutateAsync).toHaveBeenCalledWith({ tmdbId: 1, rootFolderId: 1, monitored: true, qualityProfileId: 5 })
  })

  it("requires a profile: Add is disabled until one is chosen when there is no default", async () => {
    stub()
    const mutateAsync = vi.fn().mockResolvedValue({})
    vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync, isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
    vi.mocked(lib.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }] } as unknown as ReturnType<typeof lib.useRootFolders>)
    // no default profile for movies
    vi.mocked(md.useMediaDefaults).mockReturnValue({ data: { movie: { rootFolderId: 1, qualityProfileId: null }, tv: { rootFolderId: null, qualityProfileId: null } } } as unknown as ReturnType<typeof md.useMediaDefaults>)
    render(
      <ToastProvider>
        <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
      </ToastProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
    await userEvent.click(await screen.findByText("Dune"))

    // no profile pre-selected → Add disabled
    expect(screen.getByRole("button", { name: /add movie/i })).toBeDisabled()

    await userEvent.selectOptions(screen.getByLabelText("Quality profile"), "5")
    expect(screen.getByRole("button", { name: /add movie/i })).toBeEnabled()
    await userEvent.click(screen.getByRole("button", { name: /add movie/i }))
    expect(mutateAsync).toHaveBeenCalledWith({ tmdbId: 1, rootFolderId: 1, monitored: true, qualityProfileId: 5 })
  })

  it("requires a root folder: Add is disabled until one is chosen when there is no default", async () => {
    stub()
    const mutateAsync = vi.fn().mockResolvedValue({})
    vi.mocked(lib.useAddMovie).mockReturnValue({ mutateAsync, isPending: false } as unknown as ReturnType<typeof lib.useAddMovie>)
    vi.mocked(lib.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }] } as unknown as ReturnType<typeof lib.useRootFolders>)
    // profile default present (5), but no root-folder default
    vi.mocked(md.useMediaDefaults).mockReturnValue({ data: { movie: { rootFolderId: null, qualityProfileId: 5 }, tv: { rootFolderId: null, qualityProfileId: null } } } as unknown as ReturnType<typeof md.useMediaDefaults>)
    render(
      <ToastProvider>
        <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
      </ToastProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
    await userEvent.click(await screen.findByText("Dune"))

    // profile satisfied by the default, but no root folder → Add disabled
    expect(screen.getByRole("button", { name: /add movie/i })).toBeDisabled()

    await userEvent.selectOptions(screen.getByLabelText("Root folder"), "1")
    expect(screen.getByRole("button", { name: /add movie/i })).toBeEnabled()
    await userEvent.click(screen.getByRole("button", { name: /add movie/i }))
    expect(mutateAsync).toHaveBeenCalledWith({ tmdbId: 1, rootFolderId: 1, monitored: true, qualityProfileId: 5 })
  })

  it("shows a hint and disables Add when no quality profiles are configured", async () => {
    stub()
    vi.mocked(lib.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }] } as unknown as ReturnType<typeof lib.useRootFolders>)
    vi.mocked(lib.useQualityProfiles).mockReturnValue({ data: [] } as unknown as ReturnType<typeof lib.useQualityProfiles>)
    vi.mocked(md.useMediaDefaults).mockReturnValue({ data: { movie: { rootFolderId: 1, qualityProfileId: null }, tv: { rootFolderId: null, qualityProfileId: null } } } as unknown as ReturnType<typeof md.useMediaDefaults>)
    render(
      <ToastProvider>
        <AddMediaDialog kind="movie" open onOpenChange={() => {}} />
      </ToastProvider>,
    )
    await userEvent.type(screen.getByPlaceholderText(/search/i), "dune")
    await userEvent.click(await screen.findByText("Dune"))

    expect(screen.getByText(/no quality profile configured/i)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /add movie/i })).toBeDisabled()
  })
})
