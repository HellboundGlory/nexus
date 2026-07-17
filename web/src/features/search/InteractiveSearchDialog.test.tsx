// web/src/features/search/InteractiveSearchDialog.test.tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { InteractiveSearchDialog } from "./InteractiveSearchDialog"
import * as api from "./api"
import type { ScoredRelease } from "./types"

vi.mock("./api")

// mutation-hook mock helper, exactly like BlocklistSection's `mut`
function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

// query-hook mock helper: useInteractiveSearch returns { data, isLoading, isError, refetch }
function query(over: object = {}) {
  return { data: undefined, isLoading: false, isError: false, refetch: vi.fn(), ...over } as unknown as never
}

const clean: ScoredRelease = {
  title: "Some.Movie.2019.480p.HDTV.x264-GOOD",
  downloadUrl: "http://x/good",
  size: 1_500_000_000,
  indexerId: "nzbgeek",
  protocol: "usenet",
  publishDate: "2026-07-15T00:00:00Z",
  quality: { id: 1, name: "SDTV", source: "hdtv", resolution: "480p", rank: 1 },
  score: 10,
  accepted: true,
  rejections: [],
}
const rejected: ScoredRelease = {
  ...clean,
  title: "Some.Movie.2019.1080p.WEB-DL.x264-GRP",
  downloadUrl: "http://x/rejected",
  quality: { id: 5, name: "WEBDL-1080p", source: "webdl", resolution: "1080p", rank: 5 },
  accepted: false,
  rejections: ["quality not in profile"],
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.useInteractiveGrab).mockReturnValue(mut())
})

function renderDialog() {
  render(
    <ToastProvider>
      <InteractiveSearchDialog target={{ kind: "movie", id: 7 }} title="Some Movie" onOpenChange={() => {}} />
    </ToastProvider>,
  )
}

describe("InteractiveSearchDialog", () => {
  it("renders rejected rows with their reason instead of hiding them", () => {
    vi.mocked(api.useInteractiveSearch).mockReturnValue(
      query({ data: { releases: [clean, rejected], indexerErrors: [] } }),
    )
    renderDialog()

    expect(screen.getByText(clean.title)).toBeInTheDocument()
    expect(screen.getByText(rejected.title)).toBeInTheDocument()
    expect(screen.getByText(/quality not in profile/)).toBeInTheDocument()
  })

  it("grabs a clean row on click without a confirm", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useInteractiveGrab).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useInteractiveSearch).mockReturnValue(
      query({ data: { releases: [clean], indexerErrors: [] } }),
    )
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true)
    renderDialog()

    await userEvent.click(screen.getByRole("button", { name: /grab .*GOOD/i }))

    expect(confirmSpy).not.toHaveBeenCalled()
    expect(mutate).toHaveBeenCalledWith(
      { release: clean, target: expect.anything() },
      expect.anything(),
    )
  })

  it("confirms before grabbing a rejected row, and does not grab when declined", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useInteractiveGrab).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useInteractiveSearch).mockReturnValue(
      query({ data: { releases: [rejected], indexerErrors: [] } }),
    )
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false)
    renderDialog()

    await userEvent.click(screen.getByRole("button", { name: /grab .*GRP/i }))

    expect(confirmSpy).toHaveBeenCalledWith(expect.stringContaining("quality not in profile"))
    expect(mutate).not.toHaveBeenCalled()
  })

  it("grabs a rejected row when confirmed", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useInteractiveGrab).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useInteractiveSearch).mockReturnValue(
      query({ data: { releases: [rejected], indexerErrors: [] } }),
    )
    vi.spyOn(window, "confirm").mockReturnValue(true)
    renderDialog()

    await userEvent.click(screen.getByRole("button", { name: /grab .*GRP/i }))

    expect(mutate).toHaveBeenCalledWith(
      { release: rejected, target: expect.anything() },
      expect.anything(),
    )
  })

  it("renders the partial-indexer banner naming the failures", () => {
    vi.mocked(api.useInteractiveSearch).mockReturnValue(
      query({
        data: {
          releases: [clean],
          indexerErrors: [{ indexerId: "nzbplanet", message: "timeout" }],
        },
      }),
    )
    renderDialog()

    expect(screen.getByRole("alert")).toHaveTextContent(/nzbplanet/)
  })

  it("shows an error state when the search itself fails", () => {
    vi.mocked(api.useInteractiveSearch).mockReturnValue(query({ isError: true }))
    renderDialog()

    expect(screen.getByText(/couldn't search/i)).toBeInTheDocument()
  })

  it("omits the seeders cell for usenet rows", () => {
    vi.mocked(api.useInteractiveSearch).mockReturnValue(
      query({ data: { releases: [clean], indexerErrors: [] } }),
    )
    renderDialog()

    expect(screen.getByTestId("seeders-cell")).toHaveTextContent("—")
  })
})
