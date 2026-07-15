// web/src/features/activity/BlocklistSection.test.tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { BlocklistSection } from "./BlocklistSection"
import * as api from "./api"
import * as qualityApi from "@/features/settings/qualityApi"
import type { BlocklistEntry } from "./types"

vi.mock("./api")
vi.mock("@/features/settings/qualityApi")

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

function entry(over: Partial<BlocklistEntry>): BlocklistEntry {
  return {
    id: 1, mediaKind: "movie", movieId: 1, sourceTitle: "Dune.2021-GRP",
    protocol: "usenet", qualityId: 3, reason: "missing articles",
    createdAt: new Date().toISOString(), title: "Dune", ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(qualityApi.useQualityDefinitions).mockReturnValue({ data: [{ id: 3, name: "WEBDL-1080p", source: 1, resolution: 3, rank: 3 }] } as never)
  vi.mocked(api.useRemoveBlocklist).mockReturnValue(mut())
})

function renderBlocklist() {
  render(<ToastProvider><BlocklistSection /></ToastProvider>)
}

describe("BlocklistSection", () => {
  it("shows an empty state when the blocklist is empty", () => {
    vi.mocked(api.useBlocklist).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    renderBlocklist()
    expect(screen.getByText(/no blocklisted releases/i)).toBeInTheDocument()
  })

  it("lists entries with title, reason and quality", () => {
    vi.mocked(api.useBlocklist).mockReturnValue({ data: [entry({})], isLoading: false, isError: false } as never)
    renderBlocklist()
    expect(screen.getByText("Dune.2021-GRP")).toBeInTheDocument()
    expect(screen.getByText("Dune")).toBeInTheDocument()
    expect(screen.getByText(/missing articles/i)).toBeInTheDocument()
    expect(screen.getByText("WEBDL-1080p")).toBeInTheDocument()
  })

  it("removes an entry after confirm", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useRemoveBlocklist).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useBlocklist).mockReturnValue({ data: [entry({ id: 7 })], isLoading: false, isError: false } as never)
    vi.spyOn(window, "confirm").mockReturnValue(true)
    renderBlocklist()
    await userEvent.click(screen.getByRole("button", { name: /remove/i }))
    expect(mutate).toHaveBeenCalledWith(7, expect.anything())
  })

  it("does not remove when confirm is cancelled", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useRemoveBlocklist).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useBlocklist).mockReturnValue({ data: [entry({ id: 7 })], isLoading: false, isError: false } as never)
    vi.spyOn(window, "confirm").mockReturnValue(false)
    renderBlocklist()
    await userEvent.click(screen.getByRole("button", { name: /remove/i }))
    expect(mutate).not.toHaveBeenCalled()
  })
})
