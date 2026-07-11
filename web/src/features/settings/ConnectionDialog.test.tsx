import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { ConnectionDialog } from "./ConnectionDialog"
import * as api from "./api"
import type { SchemaEntry } from "./types"

vi.mock("./api", async (orig) => {
  const actual = await orig<typeof import("./api")>()
  return { ...actual, useConnectionSchema: vi.fn(), useSaveConnection: vi.fn(), useTestConnection: vi.fn() }
})
beforeEach(() => vi.clearAllMocks())

const schema: SchemaEntry[] = [
  { implementation: "newznab", protocol: "usenet", fields: [
    { name: "name", type: "string", required: true },
    { name: "baseUrl", type: "string", required: true },
    { name: "apiKey", type: "string", label: "API Key" },
    { name: "enabled", type: "bool", default: true },
  ]},
]

function mut(extra: object = {}) {
  return { mutate: vi.fn(), mutateAsync: vi.fn().mockResolvedValue({ ok: true }), isPending: false, ...extra } as unknown as never
}

function setup(existing?: object) {
  vi.mocked(api.useConnectionSchema).mockReturnValue({ data: schema, isLoading: false } as never)
  const save = vi.fn().mockResolvedValue({ id: 1 })
  const test = vi.fn().mockResolvedValue({ ok: false, error: "refused" })
  vi.mocked(api.useSaveConnection).mockReturnValue(mut({ mutateAsync: save }))
  vi.mocked(api.useTestConnection).mockReturnValue(mut({ mutateAsync: test }))
  render(
    <ToastProvider>
      <ConnectionDialog kind="indexer" existing={existing as never} open onOpenChange={vi.fn()} />
    </ToastProvider>,
  )
  return { save, test }
}

describe("ConnectionDialog", () => {
  it("submits a create payload built from the form (add mode)", async () => {
    const { save } = setup()
    await userEvent.type(screen.getByLabelText("name"), "My Indexer")
    await userEvent.type(screen.getByLabelText("baseUrl"), "http://x")
    await userEvent.type(screen.getByLabelText("API Key"), "k")
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0][0]).toMatchObject({
      payload: { implementation: "newznab", name: "My Indexer", baseUrl: "http://x", apiKey: "k" },
    })
    expect(save.mock.calls[0][0].id).toBeUndefined()
  })

  it("omits apiKey on save when editing and the secret is untouched", async () => {
    const existing = { id: 9, name: "ix", implementation: "newznab", enabled: true, priority: 25, status: "ok", lastCheck: null, failMessage: "", baseUrl: "http://x" }
    const { save } = setup(existing)
    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    await waitFor(() => expect(save).toHaveBeenCalled())
    const arg = save.mock.calls[0][0]
    expect(arg.id).toBe(9)
    expect("apiKey" in arg.payload).toBe(false)
  })

  it("tests the SAVED endpoint when editing with untouched secret and shows the error", async () => {
    const existing = { id: 9, name: "ix", implementation: "newznab", enabled: true, priority: 25, status: "ok", lastCheck: null, failMessage: "", baseUrl: "http://x" }
    const { test } = setup(existing)
    await userEvent.click(screen.getByRole("button", { name: /test/i }))
    await waitFor(() => expect(test).toHaveBeenCalledWith({ path: "/indexer/9/test" }))
    expect(await screen.findByText(/refused/)).toBeInTheDocument()
  })

  it("tests the UNSAVED endpoint (with body) in add mode", async () => {
    const { test } = setup()
    await userEvent.type(screen.getByLabelText("name"), "ix")
    await userEvent.type(screen.getByLabelText("baseUrl"), "http://x")
    await userEvent.click(screen.getByRole("button", { name: /test/i }))
    await waitFor(() => expect(test).toHaveBeenCalled())
    const req = test.mock.calls[0][0]
    expect(req.path).toBe("/indexer/test")
    expect(req.body).toMatchObject({ implementation: "newznab", name: "ix", baseUrl: "http://x" })
  })

  it("includes the retyped secret on save and tests the UNSAVED endpoint when editing", async () => {
    const existing = { id: 9, name: "ix", implementation: "newznab", enabled: true, priority: 25, status: "ok", lastCheck: null, failMessage: "", baseUrl: "http://x" }
    const { save, test } = setup(existing)
    await userEvent.type(screen.getByLabelText("API Key"), "newkey")

    await userEvent.click(screen.getByRole("button", { name: /save/i }))
    await waitFor(() => expect(save).toHaveBeenCalled())
    const saveArg = save.mock.calls[0][0]
    expect(saveArg.id).toBe(9)
    expect(saveArg.payload.apiKey).toBe("newkey")

    await userEvent.click(screen.getByRole("button", { name: /test/i }))
    await waitFor(() => expect(test).toHaveBeenCalled())
    const req = test.mock.calls[0][0]
    expect(req.path).toBe("/indexer/test")
    expect(req.body.apiKey).toBe("newkey")
  })
})
