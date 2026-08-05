import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ReleaseProfileDialog } from "./ReleaseProfileDialog"
import * as api from "./releaseProfileApi"
import * as tagApi from "./tagApi"

vi.mock("./releaseProfileApi", async (orig) => {
  const actual = await orig<typeof import("./releaseProfileApi")>()
  return { ...actual, useSaveReleaseProfile: vi.fn() }
})
vi.mock("./tagApi", async (orig) => {
  const actual = await orig<typeof import("./tagApi")>()
  return { ...actual, useTags: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

const tags = [
  { id: 1, label: "anime", seriesCount: 0, movieCount: 0 },
  { id: 2, label: "4k", seriesCount: 0, movieCount: 0 },
]

function saveMut(mutate = vi.fn()) {
  return { mutate, isPending: false } as unknown as never
}

function renderDialog(save = vi.fn()) {
  vi.mocked(api.useSaveReleaseProfile).mockReturnValue(saveMut(save))
  vi.mocked(tagApi.useTags).mockReturnValue({ data: tags, isLoading: false } as never)
  render(<ToastProvider><ReleaseProfileDialog open onOpenChange={() => {}} /></ToastProvider>)
}

describe("ReleaseProfileDialog", () => {
  it("edits term lists and toggles required mode", async () => {
    const save = vi.fn()
    renderDialog(save)
    await userEvent.type(screen.getByLabelText(/name/i), "No Dub")
    await userEvent.type(screen.getByLabelText(/required \(any\)/i), "1080p, bluray")
    await userEvent.type(screen.getByLabelText(/ignored/i), "dub")
    await userEvent.selectOptions(screen.getByLabelText(/required mode/i), "all")
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({
        payload: expect.objectContaining({
          name: "No Dub",
          requiredMode: "all",
          requiredAny: ["1080p", "bluray"],
          ignored: ["dub"],
        }),
      }),
      expect.anything(),
    )
  })

  it("multi-selects tags", async () => {
    const save = vi.fn()
    renderDialog(save)
    await userEvent.type(screen.getByLabelText(/name/i), "P")
    await userEvent.type(screen.getByLabelText(/required \(any\)/i), "1080p")
    await userEvent.click(screen.getByLabelText("anime"))
    await userEvent.click(screen.getByLabelText("4k"))
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({
        payload: expect.objectContaining({ tagIds: [1, 2] }),
      }),
      expect.anything(),
    )
  })

  it("disables save when name is empty", () => {
    renderDialog()
    expect(screen.getByRole("button", { name: /save/i })).toBeDisabled()
  })
})