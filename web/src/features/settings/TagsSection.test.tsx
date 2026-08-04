import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { TagsSection } from "./TagsSection"
import * as api from "./tagApi"

vi.mock("./tagApi", async (orig) => {
  const actual = await orig<typeof import("./tagApi")>()
  return { ...actual, useTags: vi.fn(), useCreateTag: vi.fn(), useRenameTag: vi.fn(), useDeleteTag: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

const tags = [
  { id: 1, label: "anime", seriesCount: 3, movieCount: 0 },
  { id: 2, label: "classics", seriesCount: 0, movieCount: 2 },
]

function setup(rows: typeof tags, over: { del?: object; create?: object } = {}) {
  vi.mocked(api.useTags).mockReturnValue({ data: rows, isLoading: false, isError: false } as never)
  vi.mocked(api.useCreateTag).mockReturnValue(mut(over.create))
  vi.mocked(api.useRenameTag).mockReturnValue(mut())
  vi.mocked(api.useDeleteTag).mockReturnValue(mut(over.del))
  render(<ToastProvider><TagsSection /></ToastProvider>)
}

describe("TagsSection", () => {
  it("shows each tag with its in-use counts", () => {
    setup(tags)
    expect(screen.getByText("anime")).toBeInTheDocument()
    expect(screen.getByText(/3 series, 0 movies/)).toBeInTheDocument()
    expect(screen.getByText(/0 series, 2 movies/)).toBeInTheDocument()
  })

  it("creates a tag from the inline input", async () => {
    const create = vi.fn()
    setup(tags, { create: { mutate: create } })
    await userEvent.type(screen.getByLabelText("New tag label"), "documentary")
    await userEvent.click(screen.getByRole("button", { name: "Add" }))
    expect(create).toHaveBeenCalledWith("documentary", expect.anything())
  })

  it("shows the server's in-use message verbatim on a 409 delete", async () => {
    const del = vi.fn((_id, opts) =>
      opts.onError(new ApiError(409, "tag_in_use", "tag is in use by 3 series and 0 movies")),
    )
    setup(tags, { del: { mutate: del } })
    await userEvent.click(screen.getAllByRole("button", { name: "Delete" })[0])
    // Asserting the exact server text, not a client-side constant: this is what
    // makes the refusal actionable.
    expect(await screen.findByText("tag is in use by 3 series and 0 movies")).toBeInTheDocument()
  })

  it("shows an empty state when there are no tags", () => {
    setup([])
    expect(screen.getByText(/No tags yet/)).toBeInTheDocument()
  })
})
