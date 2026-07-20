// web/src/features/activity/HistorySection.test.tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { HistorySection } from "./HistorySection"
import * as api from "./api"
import * as libApi from "@/features/library/api"
import * as qualityApi from "@/features/settings/qualityApi"
import type { HistoryEvent, Paged } from "./types"

vi.mock("./api")
vi.mock("@/features/library/api")
vi.mock("@/features/settings/qualityApi")

function ev(over: Partial<HistoryEvent>): HistoryEvent {
  return {
    id: 1, eventType: "grabbed", mediaKind: "movie", movieId: 1,
    sourceTitle: "The.Matrix.1999", qualityId: 3, message: "grabbed from nzb",
    createdAt: new Date().toISOString(), ...over,
  }
}

function paged(items: HistoryEvent[], over: Partial<Paged<HistoryEvent>> = {}): Paged<HistoryEvent> {
  return { items, page: 1, pageSize: 50, total: items.length, ...over }
}

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(libApi.useMovies).mockReturnValue({ data: [{ id: 1, title: "The Matrix", year: 1999 }] } as never)
  vi.mocked(libApi.useSeries).mockReturnValue({ data: [] } as never)
  vi.mocked(qualityApi.useQualityDefinitions).mockReturnValue({ data: [{ id: 3, name: "WEBDL-1080p", source: 1, resolution: 3, rank: 3 }] } as never)
  vi.mocked(api.useClearHistory).mockReturnValue(mut())
})

describe("HistorySection", () => {
  it("shows an empty state when there is no history", () => {
    vi.mocked(api.useHistory).mockReturnValue({ data: paged([], { total: 0 }), isLoading: false, isError: false } as never)
    render(<HistorySection />)
    expect(screen.getByText(/no history yet/i)).toBeInTheDocument()
  })

  it("renders event label, resolved title and quality", () => {
    vi.mocked(api.useHistory).mockReturnValue({
      data: paged([ev({ eventType: "imported", qualityId: 3 }), ev({ id: 2, eventType: "import_failed", qualityId: null, message: "rejected" })]),
      isLoading: false, isError: false,
    } as never)
    render(<HistorySection />)
    expect(screen.getByText("Imported")).toBeInTheDocument()
    expect(screen.getByText("Import failed")).toBeInTheDocument()
    expect(screen.getAllByText("The Matrix (1999)").length).toBeGreaterThan(0)
    expect(screen.getByText("WEBDL-1080p")).toBeInTheDocument()
  })

  it("does not duplicate the subtext when it matches the fallback title", () => {
    vi.mocked(api.useHistory).mockReturnValue({
      data: paged([ev({ movieId: 999, sourceTitle: "Some.Untracked.Release" })]),
      isLoading: false, isError: false,
    } as never)
    render(<HistorySection />)
    expect(screen.getAllByText("Some.Untracked.Release")).toHaveLength(1)
  })

  it("hides Clear when there is nothing to clear", () => {
    vi.mocked(api.useHistory).mockReturnValue({ data: paged([], { total: 0 }), isLoading: false, isError: false } as never)
    render(<HistorySection />)
    expect(screen.queryByRole("button", { name: /clear history/i })).toBeNull()
  })

  it("clears after confirming", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useClearHistory).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useHistory).mockReturnValue({
      data: paged([ev({}), ev({ id: 2 })], { total: 2 }),
      isLoading: false, isError: false,
    } as never)
    render(<HistorySection />)
    await userEvent.click(screen.getByRole("button", { name: /clear history/i }))
    await userEvent.click(screen.getByRole("button", { name: /^clear$/i }))
    expect(mutate).toHaveBeenCalled()
  })
})
