import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { QualityProfilesSection } from "./QualityProfilesSection"
import * as api from "./qualityApi"

vi.mock("./qualityApi", async (orig) => {
  const actual = await orig<typeof import("./qualityApi")>()
  return { ...actual, useQualityProfiles: vi.fn(), useDeleteProfile: vi.fn() }
})
vi.mock("./ProfileDialog", () => ({ ProfileDialog: () => <div data-testid="dialog" /> }))
beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

const profile = {
  id: 1, name: "HD-1080p", cutoffQualityId: 7, upgradeAllowed: true, createdAt: "",
  items: [{ qualityId: 7, allowed: true }],
}

describe("QualityProfilesSection", () => {
  it("lists profiles", () => {
    vi.mocked(api.useQualityProfiles).mockReturnValue({ data: [profile], isLoading: false, isError: false } as never)
    vi.mocked(api.useDeleteProfile).mockReturnValue(mut())
    render(<ToastProvider><QualityProfilesSection /></ToastProvider>)
    expect(screen.getByText("HD-1080p")).toBeInTheDocument()
  })

  it("shows an in-use toast on a 409 delete", async () => {
    const mutate = vi.fn((_id, opts) => opts.onError(new ApiError(409, "conflict", "in use")))
    vi.mocked(api.useQualityProfiles).mockReturnValue({ data: [profile], isLoading: false, isError: false } as never)
    vi.mocked(api.useDeleteProfile).mockReturnValue(mut({ mutate }))
    vi.spyOn(window, "confirm").mockReturnValue(true)
    render(<ToastProvider><QualityProfilesSection /></ToastProvider>)
    await userEvent.click(screen.getByRole("button", { name: /delete/i }))
    expect(await screen.findByText(/in use/i)).toBeInTheDocument()
  })
})
