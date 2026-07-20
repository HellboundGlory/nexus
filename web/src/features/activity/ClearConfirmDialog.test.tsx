import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ClearConfirmDialog } from "./ClearConfirmDialog"

describe("ClearConfirmDialog", () => {
  it("shows the body and confirms", async () => {
    const onConfirm = vi.fn()
    render(
      <ClearConfirmDialog
        open
        onOpenChange={vi.fn()}
        title="Clear history?"
        body="This removes all 12 history events."
        onConfirm={onConfirm}
      />,
    )
    expect(screen.getByText(/all 12 history events/i)).toBeTruthy()
    await userEvent.click(screen.getByRole("button", { name: /^clear$/i }))
    expect(onConfirm).toHaveBeenCalled()
  })

  it("surfaces a warning and a custom confirm label", () => {
    render(
      <ClearConfirmDialog
        open
        onOpenChange={vi.fn()}
        title="Clear queue?"
        body="This removes all 3 items."
        warning="sab: connection refused"
        confirmLabel="Clear anyway"
        onConfirm={vi.fn()}
      />,
    )
    expect(screen.getByText(/connection refused/i)).toBeTruthy()
    expect(screen.getByRole("button", { name: /clear anyway/i })).toBeTruthy()
  })
})
