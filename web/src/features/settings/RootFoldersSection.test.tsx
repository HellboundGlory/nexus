import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { RootFoldersSection } from "./RootFoldersSection"
import * as api from "./configApi"

vi.mock("./configApi", async (orig) => {
  const actual = await orig<typeof import("./configApi")>()
  return { ...actual, useRootFolders: vi.fn(), useAddRootFolder: vi.fn(), useDeleteRootFolder: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

describe("RootFoldersSection", () => {
  it("lists root folders and adds a new path", async () => {
    const add = vi.fn()
    vi.mocked(api.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/tv", createdAt: "" }], isLoading: false, isError: false } as never)
    vi.mocked(api.useAddRootFolder).mockReturnValue(mut({ mutate: add }))
    vi.mocked(api.useDeleteRootFolder).mockReturnValue(mut())
    render(<ToastProvider><RootFoldersSection /></ToastProvider>)
    expect(screen.getByText("/media/tv")).toBeInTheDocument()
    await userEvent.type(screen.getByPlaceholderText(/path/i), "/media/movies")
    await userEvent.click(screen.getByRole("button", { name: /add/i }))
    expect(add).toHaveBeenCalledWith("/media/movies", expect.anything())
  })

  it("disables Add until a non-whitespace path is entered", async () => {
    vi.mocked(api.useRootFolders).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    vi.mocked(api.useAddRootFolder).mockReturnValue(mut())
    vi.mocked(api.useDeleteRootFolder).mockReturnValue(mut())
    render(<ToastProvider><RootFoldersSection /></ToastProvider>)
    const addBtn = screen.getByRole("button", { name: /add/i })
    expect(addBtn).toBeDisabled()
    await userEvent.type(screen.getByPlaceholderText(/path/i), "   ")
    expect(addBtn).toBeDisabled()
    await userEvent.type(screen.getByPlaceholderText(/path/i), "/media/x")
    expect(addBtn).toBeEnabled()
  })

  it("shows an in-use toast on a 409 delete", async () => {
    const mutate = vi.fn((_id, opts) => opts.onError(new ApiError(409, "conflict", "in use")))
    vi.mocked(api.useRootFolders).mockReturnValue({ data: [{ id: 1, path: "/media/tv", createdAt: "" }], isLoading: false, isError: false } as never)
    vi.mocked(api.useAddRootFolder).mockReturnValue(mut())
    vi.mocked(api.useDeleteRootFolder).mockReturnValue(mut({ mutate }))
    vi.spyOn(window, "confirm").mockReturnValue(true)
    render(<ToastProvider><RootFoldersSection /></ToastProvider>)
    await userEvent.click(screen.getByRole("button", { name: /delete/i }))
    expect(await screen.findByText(/in use/i)).toBeInTheDocument()
  })
})
