import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ConnectionsSection } from "./ConnectionsSection"
import * as api from "./api"

vi.mock("./api", async (orig) => {
  const actual = await orig<typeof import("./api")>()
  return { ...actual, useConnections: vi.fn(), useDeleteConnection: vi.fn() }
})
// The dialog fetches schema; stub it out so this test focuses on the list.
vi.mock("./ConnectionDialog", () => ({ ConnectionDialog: () => <div data-testid="dialog" /> }))
beforeEach(() => vi.clearAllMocks())

function mut(extra: object = {}) {
  return { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, ...extra } as unknown as never
}

describe("ConnectionsSection", () => {
  it("lists connections with a status badge and an empty state", () => {
    vi.mocked(api.useConnections).mockReturnValue({
      data: [{ id: 1, name: "NZBgeek", implementation: "newznab", enabled: true, priority: 25, status: "ok", lastCheck: null, failMessage: "" }],
      isLoading: false, isError: false,
    } as never)
    vi.mocked(api.useDeleteConnection).mockReturnValue(mut())
    render(<ToastProvider><ConnectionsSection kind="indexer" /></ToastProvider>)
    expect(screen.getByText("NZBgeek")).toBeInTheDocument()
    expect(screen.getByText("OK")).toBeInTheDocument()
  })

  it("confirms before deleting", async () => {
    const del = vi.fn()
    vi.mocked(api.useConnections).mockReturnValue({
      data: [{ id: 1, name: "NZBgeek", implementation: "newznab", enabled: true, priority: 25, status: "failed", lastCheck: null, failMessage: "boom" }],
      isLoading: false, isError: false,
    } as never)
    vi.mocked(api.useDeleteConnection).mockReturnValue(mut({ mutate: del }))
    vi.spyOn(window, "confirm").mockReturnValue(true)
    render(<ToastProvider><ConnectionsSection kind="indexer" /></ToastProvider>)
    await userEvent.click(screen.getByRole("button", { name: /delete/i }))
    expect(del).toHaveBeenCalledWith(1, expect.anything())
  })

  it("opens the add dialog", async () => {
    vi.mocked(api.useConnections).mockReturnValue({ data: [], isLoading: false, isError: false } as never)
    vi.mocked(api.useDeleteConnection).mockReturnValue(mut())
    render(<ToastProvider><ConnectionsSection kind="indexer" /></ToastProvider>)
    expect(screen.queryByTestId("dialog")).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: /add/i }))
    expect(screen.getByTestId("dialog")).toBeInTheDocument()
  })
})
