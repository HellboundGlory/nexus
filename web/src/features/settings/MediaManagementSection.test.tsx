import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { MediaManagementSection } from "./MediaManagementSection"
import * as mdApi from "./mediaDefaultsApi"
import * as cfg from "./configApi"
import * as qual from "./qualityApi"

vi.mock("./mediaDefaultsApi")
vi.mock("./configApi")
vi.mock("./qualityApi")

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(cfg.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/movies", createdAt: "" }, { id: 2, path: "/media/tv", createdAt: "" }] } as never)
  vi.mocked(qual.useQualityProfiles).mockReturnValue({ data: [{ id: 5, name: "HD-1080p", cutoffQualityId: 7, upgradeAllowed: true, items: [], createdAt: "" }] } as never)
  vi.mocked(mdApi.useSaveMediaDefaults).mockReturnValue(mut())
})

function renderSection() {
  render(<ToastProvider><MediaManagementSection /></ToastProvider>)
}

describe("MediaManagementSection", () => {
  it("seeds the four dropdowns from the saved defaults", () => {
    vi.mocked(mdApi.useMediaDefaults).mockReturnValue({
      data: { movie: { rootFolderId: 1, qualityProfileId: 5 }, tv: { rootFolderId: 2, qualityProfileId: null } },
      isLoading: false, isError: false,
    } as never)
    renderSection()
    expect((screen.getByLabelText("Default Movie Root Folder") as HTMLSelectElement).value).toBe("1")
    expect((screen.getByLabelText("Default TV Root Folder") as HTMLSelectElement).value).toBe("2")
    expect((screen.getByLabelText("Default TV Quality Profile") as HTMLSelectElement).value).toBe("")
  })

  it("saves the PUT body, sending null for a 'None' selection", async () => {
    const mutate = vi.fn()
    vi.mocked(mdApi.useSaveMediaDefaults).mockReturnValue(mut({ mutate }))
    vi.mocked(mdApi.useMediaDefaults).mockReturnValue({
      data: { movie: { rootFolderId: 1, qualityProfileId: 5 }, tv: { rootFolderId: 2, qualityProfileId: 5 } },
      isLoading: false, isError: false,
    } as never)
    renderSection()

    await userEvent.selectOptions(screen.getByLabelText("Default TV Quality Profile"), "") // choose None
    await userEvent.click(screen.getByRole("button", { name: /save/i }))

    expect(mutate).toHaveBeenCalledWith(
      { movie: { rootFolderId: 1, qualityProfileId: 5 }, tv: { rootFolderId: 2, qualityProfileId: null } },
      expect.anything(),
    )
  })

  it("shows an error state (not a stuck spinner) when the defaults fail to load", () => {
    vi.mocked(mdApi.useMediaDefaults).mockReturnValue({
      data: undefined, isLoading: false, isError: true,
    } as never)
    renderSection()
    expect(screen.getByText(/failed to load/i)).toBeInTheDocument()
    expect(screen.queryByText(/loading…/i)).not.toBeInTheDocument()
  })
})
