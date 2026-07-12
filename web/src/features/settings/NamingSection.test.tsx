import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { NamingSection } from "./NamingSection"
import * as api from "./configApi"

vi.mock("./configApi", async (orig) => {
  const actual = await orig<typeof import("./configApi")>()
  return { ...actual, useNamingConfig: vi.fn(), useSaveNaming: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

const cfg = {
  seriesFolder: "{Series Title}", seasonFolder: "Season {season:00}",
  episodeFile: "E", movieFolder: "{Movie Title} ({year})", movieFile: "M",
}

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

describe("NamingSection", () => {
  it("seeds inputs from the config and saves edits", async () => {
    const save = vi.fn()
    vi.mocked(api.useNamingConfig).mockReturnValue({ data: cfg, isLoading: false, isError: false } as never)
    vi.mocked(api.useSaveNaming).mockReturnValue(mut({ mutate: save }))
    render(<ToastProvider><NamingSection /></ToastProvider>)
    const series = screen.getByLabelText(/series folder/i)
    expect(series).toHaveValue("{Series Title}")
    // Brace-free text: userEvent treats { and } as special key sequences.
    await userEvent.clear(series)
    await userEvent.type(series, "Custom")
    await userEvent.click(screen.getByRole("button", { name: /^save$/i }))
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({ seriesFolder: "Custom" }),
      expect.anything(),
    )
  })

  it("renders the token legend", () => {
    vi.mocked(api.useNamingConfig).mockReturnValue({ data: cfg, isLoading: false, isError: false } as never)
    vi.mocked(api.useSaveNaming).mockReturnValue(mut())
    render(<ToastProvider><NamingSection /></ToastProvider>)
    expect(screen.getByText("{Series Title}", { selector: "code" })).toBeInTheDocument()
    expect(screen.getByText("{season:00}", { selector: "code" })).toBeInTheDocument()
  })
})
