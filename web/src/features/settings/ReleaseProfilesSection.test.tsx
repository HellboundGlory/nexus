import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { ReleaseProfilesSection } from "./ReleaseProfilesSection"
import * as api from "./releaseProfileApi"

vi.mock("./releaseProfileApi", async (orig) => {
  const actual = await orig<typeof import("./releaseProfileApi")>()
  return { ...actual, useReleaseProfiles: vi.fn(), useDeleteReleaseProfile: vi.fn() }
})
vi.mock("./ReleaseProfileDialog", () => ({ ReleaseProfileDialog: () => <div data-testid="dialog" /> }))
beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

const profile = {
  id: 1, name: "No Dub", requiredMode: "any",
  requiredAny: ["1080p"], requiredAll: [], ignored: ["dub"], preferred: [],
  tagIds: [1], createdAt: "",
}

describe("ReleaseProfilesSection", () => {
  it("lists profiles", () => {
    vi.mocked(api.useReleaseProfiles).mockReturnValue({ data: [profile], isLoading: false, isError: false } as never)
    vi.mocked(api.useDeleteReleaseProfile).mockReturnValue(mut())
    render(<ToastProvider><ReleaseProfilesSection /></ToastProvider>)
    expect(screen.getByText("No Dub")).toBeInTheDocument()
  })

  it("surfaces a 400 delete as an error toast", async () => {
    const mutate = vi.fn((_id, opts) => opts.onError(new ApiError(400, "bad_request", "release profile in use")))
    vi.mocked(api.useReleaseProfiles).mockReturnValue({ data: [profile], isLoading: false, isError: false } as never)
    vi.mocked(api.useDeleteReleaseProfile).mockReturnValue(mut({ mutate }))
    vi.spyOn(window, "confirm").mockReturnValue(true)
    render(<ToastProvider><ReleaseProfilesSection /></ToastProvider>)
    await userEvent.click(screen.getByRole("button", { name: /delete/i }))
    expect(await screen.findByText(/in use/i)).toBeInTheDocument()
  })
})