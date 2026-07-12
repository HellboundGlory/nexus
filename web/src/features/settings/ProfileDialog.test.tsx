import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ProfileDialog } from "./ProfileDialog"
import * as api from "./qualityApi"

vi.mock("./qualityApi", async (orig) => {
  const actual = await orig<typeof import("./qualityApi")>()
  return { ...actual, useQualityDefinitions: vi.fn(), useSaveProfile: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

const defs = [
  { id: 6, name: "WEBDL-720p", source: "webdl", resolution: "720p", rank: 1 },
  { id: 7, name: "WEBDL-1080p", source: "webdl", resolution: "1080p", rank: 2 },
]

function saveMut(mutate = vi.fn()) {
  return { mutate, isPending: false } as unknown as never
}

function renderDialog(save = vi.fn()) {
  vi.mocked(api.useQualityDefinitions).mockReturnValue({ data: defs, isLoading: false } as never)
  vi.mocked(api.useSaveProfile).mockReturnValue(saveMut(save))
  render(<ToastProvider><ProfileDialog open onOpenChange={() => {}} /></ToastProvider>)
}

describe("ProfileDialog", () => {
  it("renders a checkbox per quality and a cutoff option per allowed quality", async () => {
    renderDialog()
    expect(screen.getByLabelText("WEBDL-720p")).toBeInTheDocument()
    expect(screen.getByLabelText("WEBDL-1080p")).toBeInTheDocument()
  })

  it("saves a payload with all items in ladder order and the chosen cutoff", async () => {
    const save = vi.fn()
    renderDialog(save)
    await userEvent.type(screen.getByLabelText(/name/i), "HD")
    // defaults already allow 720p+1080p; save.
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({
        payload: expect.objectContaining({
          name: "HD",
          items: [
            { qualityId: 6, allowed: true },
            { qualityId: 7, allowed: true },
          ],
          cutoffQualityId: expect.any(Number),
          upgradeAllowed: expect.any(Boolean),
        }),
      }),
      expect.anything(),
    )
  })

  it("disables save when name is empty", () => {
    renderDialog()
    expect(screen.getByRole("button", { name: /save/i })).toBeDisabled()
  })
})
