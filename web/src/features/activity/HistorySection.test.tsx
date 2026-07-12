// web/src/features/activity/HistorySection.test.tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { HistorySection } from "./HistorySection"
import * as api from "./api"
import * as libApi from "@/features/library/api"
import * as qualityApi from "@/features/settings/qualityApi"
import type { HistoryEvent } from "./types"

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

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(libApi.useMovies).mockReturnValue({ data: [{ id: 1, title: "The Matrix", year: 1999 }] } as never)
  vi.mocked(libApi.useSeries).mockReturnValue({ data: [] } as never)
  vi.mocked(qualityApi.useQualityDefinitions).mockReturnValue({ data: [{ id: 3, name: "WEBDL-1080p", source: 1, resolution: 3, rank: 3 }] } as never)
})

describe("HistorySection", () => {
  it("shows an empty state when there is no history", () => {
    vi.mocked(api.useHistory).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    render(<HistorySection />)
    expect(screen.getByText(/no history yet/i)).toBeInTheDocument()
  })

  it("renders event label, resolved title and quality", () => {
    vi.mocked(api.useHistory).mockReturnValue({
      data: [ev({ eventType: "imported", qualityId: 3 }), ev({ id: 2, eventType: "import_failed", qualityId: null, message: "rejected" })],
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
      data: [ev({ movieId: 999, sourceTitle: "Some.Untracked.Release" })],
      isLoading: false, isError: false,
    } as never)
    render(<HistorySection />)
    expect(screen.getAllByText("Some.Untracked.Release")).toHaveLength(1)
  })
})
