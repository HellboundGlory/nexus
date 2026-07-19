import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { GeneralSection } from "./GeneralSection"
import * as api from "./configApi"

vi.mock("./configApi", async (orig) => {
  const actual = await orig<typeof import("./configApi")>()
  return { ...actual, useAutomationConfig: vi.fn(), useSaveAutomationConfig: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

const cfg = {
  missingSearchIntervalHours: 6, missingSearchBatchSize: 100,
  rssSyncEnabled: true, rssSyncIntervalMinutes: 15,
  upgradeSearchEnabled: true, upgradeSearchIntervalHours: 12,
  upgradeSearchBatchSize: 100, upgradeGrabCooldownHours: 168,
}

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

function setup(save = vi.fn()) {
  vi.mocked(api.useAutomationConfig).mockReturnValue({ data: cfg, isLoading: false, isError: false } as never)
  vi.mocked(api.useSaveAutomationConfig).mockReturnValue(mut({ mutate: save }))
  render(<ToastProvider><GeneralSection /></ToastProvider>)
}

describe("GeneralSection", () => {
  it("shows the restart caveat", () => {
    setup()
    expect(screen.getByText(/next.*restart/i)).toBeInTheDocument()
  })

  it("saves edited automation config", async () => {
    const save = vi.fn()
    setup(save)
    const batch = screen.getByLabelText(/missing search batch size/i)
    await userEvent.clear(batch)
    await userEvent.type(batch, "50")
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ missingSearchBatchSize: 50 }), expect.anything())
  })

  it("omits a NUM field cleared to a non-positive value from the save payload", async () => {
    const save = vi.fn()
    setup(save)
    const interval = screen.getByLabelText(/missing search interval \(hours\)/i)
    await userEvent.clear(interval)
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    expect(save).toHaveBeenCalledWith(
      expect.not.objectContaining({ missingSearchIntervalHours: expect.anything() }),
      expect.anything(),
    )
  })

  it("keeps a boolean field set to false in the save payload", async () => {
    const save = vi.fn()
    setup(save)
    await userEvent.click(screen.getByLabelText(/rss sync enabled/i))
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ rssSyncEnabled: false }), expect.anything())
  })

  it("re-syncs the form to the server config after a save that cleared a field", async () => {
    // Mutation invokes onSuccess so the component's refetch/re-seed runs; the
    // server returns the defaulted config (interval 6), not the typed-in 0.
    const save = vi.fn((_payload, opts) => opts.onSuccess())
    const refetch = vi.fn().mockResolvedValue({ data: cfg })
    vi.mocked(api.useAutomationConfig).mockReturnValue({ data: cfg, isLoading: false, isError: false, refetch } as never)
    vi.mocked(api.useSaveAutomationConfig).mockReturnValue(mut({ mutate: save }))
    render(<ToastProvider><GeneralSection /></ToastProvider>)

    const interval = screen.getByLabelText(/missing search interval \(hours\)/i)
    await userEvent.clear(interval)
    expect(interval).toHaveValue(0)
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    expect(refetch).toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.getByLabelText(/missing search interval \(hours\)/i)).toHaveValue(6),
    )
  })
})
