// web/src/features/activity/QueueSection.test.tsx
import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { QueueSection } from "./QueueSection"
import * as api from "./api"
import * as libApi from "@/features/library/api"
import * as qualityApi from "@/features/settings/qualityApi"
import type { QueueItem } from "./types"

vi.mock("./api")
vi.mock("@/features/library/api")
vi.mock("@/features/settings/qualityApi")

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

function row(over: Partial<QueueItem>): QueueItem {
  return {
    id: 1, downloadClientId: "", clientItemId: "x", protocol: "usenet",
    sourceTitle: "The.Matrix.1999.1080p", mediaKind: "movie", movieId: 1,
    episodeIds: [], qualityId: 3, status: "grabbed", createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(), ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(libApi.useMovies).mockReturnValue({ data: [{ id: 1, title: "The Matrix", year: 1999 }] } as never)
  vi.mocked(libApi.useSeries).mockReturnValue({ data: [] } as never)
  vi.mocked(qualityApi.useQualityDefinitions).mockReturnValue({ data: [{ id: 3, name: "WEBDL-1080p", source: 1, resolution: 3, rank: 3 }] } as never)
  vi.mocked(api.useImportItem).mockReturnValue(mut())
  vi.mocked(api.useRemoveQueueItem).mockReturnValue(mut())
  vi.mocked(api.useClearQueue).mockReturnValue(mut())
})

function renderQueue() {
  render(<ToastProvider><QueueSection /></ToastProvider>)
}

describe("QueueSection", () => {
  it("shows an empty state when the queue is empty", () => {
    vi.mocked(api.useQueue).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    renderQueue()
    expect(screen.getByText(/queue is empty/i)).toBeInTheDocument()
  })

  it("renders resolved title, sourceTitle subtext and quality", () => {
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({})], isLoading: false, isError: false } as never)
    renderQueue()
    expect(screen.getByText("The Matrix (1999)")).toBeInTheDocument()
    expect(screen.getByText("The.Matrix.1999.1080p")).toBeInTheDocument()
    expect(screen.getByText("WEBDL-1080p")).toBeInTheDocument()
  })

  it("shows the Import button on grabbed and failed rows only", () => {
    vi.mocked(api.useQueue).mockReturnValue({
      data: [
        row({ id: 1, status: "grabbed" }),
        row({ id: 2, status: "failed", error: "no space" }),
        row({ id: 3, status: "importing" }),
        row({ id: 4, status: "imported" }),
      ],
      isLoading: false, isError: false,
    } as never)
    renderQueue()
    expect(screen.getAllByRole("button", { name: /import/i })).toHaveLength(2)
  })

  it("shows the error text on a failed row", () => {
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ status: "failed", error: "disk full" })], isLoading: false, isError: false } as never)
    renderQueue()
    expect(screen.getByText("disk full")).toBeInTheDocument()
  })

  it("removes via the dialog rather than window.confirm", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useRemoveQueueItem).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ id: 1 })], isLoading: false, isError: false } as never)
    renderQueue()
    await userEvent.click(screen.getByRole("button", { name: /^remove$/i })) // row button
    await userEvent.click(screen.getByRole("button", { name: /^remove$/i })) // dialog confirm
    expect(mutate).toHaveBeenCalledWith(
      { id: 1, removeFromClient: true, blocklist: false },
      expect.anything(),
    )
  })

  it("does not remove when the dialog is cancelled", async () => {
    const mutate = vi.fn()
    vi.mocked(api.useRemoveQueueItem).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ id: 7 })], isLoading: false, isError: false } as never)
    renderQueue()
    await userEvent.click(screen.getByRole("button", { name: /^remove$/i }))
    await userEvent.click(screen.getByRole("button", { name: /^cancel$/i }))
    expect(mutate).not.toHaveBeenCalled()
  })

  it("offers Clear anyway after a 503 refusal", async () => {
    const mutate = vi.fn((_opts, cbs) => cbs.onError(new ApiError(503, "client_unavailable", "sab: connection refused")))
    vi.mocked(api.useClearQueue).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ id: 1 })], isLoading: false, isError: false } as never)
    renderQueue()
    await userEvent.click(screen.getByRole("button", { name: /clear queue/i }))
    await userEvent.click(screen.getByRole("button", { name: /^clear$/i }))
    expect(await screen.findByText(/connection refused/i)).toBeTruthy()
    expect(screen.getByRole("button", { name: /clear anyway/i })).toBeTruthy()
  })

  it("retries with force after clicking Clear anyway", async () => {
    const mutate = vi.fn((opts: { force?: boolean }, cbs) => {
      if (!opts.force) {
        cbs.onError(new ApiError(503, "client_unavailable", "sab: connection refused"))
      } else {
        cbs.onSuccess({ removed: 1 })
      }
    })
    vi.mocked(api.useClearQueue).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ id: 1 })], isLoading: false, isError: false } as never)
    renderQueue()
    await userEvent.click(screen.getByRole("button", { name: /clear queue/i }))
    await userEvent.click(screen.getByRole("button", { name: /^clear$/i }))
    await screen.findByRole("button", { name: /clear anyway/i })
    await userEvent.click(screen.getByRole("button", { name: /clear anyway/i }))
    expect(mutate).toHaveBeenLastCalledWith({ force: true }, expect.anything())
  })

  it("surfaces an import error as a toast", async () => {
    const mutate = vi.fn((_id, opts) => opts.onError(new ApiError(400, "rejected", "quality not in profile")))
    vi.mocked(api.useImportItem).mockReturnValue(mut({ mutate }))
    vi.mocked(api.useQueue).mockReturnValue({ data: [row({ id: 5, status: "failed", error: "x" })], isLoading: false, isError: false } as never)
    renderQueue()
    await userEvent.click(screen.getByRole("button", { name: /import/i }))
    expect(await screen.findByText(/quality not in profile/i)).toBeInTheDocument()
  })

  it("renders a progress bar and percent for a downloading grabbed row", () => {
    vi.mocked(api.useQueue).mockReturnValue({
      data: [row({ status: "grabbed", progress: 42.5, downloadStatus: "downloading" })],
      isLoading: false, isError: false,
    } as never)
    renderQueue()
    const bar = screen.getByRole("progressbar")
    expect(bar).toHaveAttribute("aria-valuenow", "43")
    expect(screen.getByText("43%")).toBeInTheDocument()
    expect(screen.getByText("Downloading")).toBeInTheDocument()
  })

  it("renders no progress bar for a grabbed row with no live match", () => {
    vi.mocked(api.useQueue).mockReturnValue({
      data: [row({ status: "grabbed" })], isLoading: false, isError: false,
    } as never)
    renderQueue()
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument()
    expect(screen.getByText("Grabbed")).toBeInTheDocument()
  })

  it("renders no progress bar for an importing row even if live data is present", () => {
    vi.mocked(api.useQueue).mockReturnValue({
      data: [row({ status: "importing", progress: 90, downloadStatus: "downloading" })],
      isLoading: false, isError: false,
    } as never)
    renderQueue()
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument()
    expect(screen.getByText("Importing")).toBeInTheDocument()
  })
})
