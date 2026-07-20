import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { RemoveQueueItemDialog } from "./RemoveQueueItemDialog"

function open(onConfirm = vi.fn()) {
  render(
    <RemoveQueueItemDialog open onOpenChange={vi.fn()} title="Dune.2021-GRP" onConfirm={onConfirm} />,
  )
  return onConfirm
}

describe("RemoveQueueItemDialog", () => {
  it("defaults to removing from the client but not blocklisting", async () => {
    const onConfirm = open()
    await userEvent.click(screen.getByRole("button", { name: /^remove$/i }))
    expect(onConfirm).toHaveBeenCalledWith({ removeFromClient: true, blocklist: false })
  })

  it("passes both flags when the user changes them", async () => {
    const onConfirm = open()
    await userEvent.click(screen.getByLabelText(/remove from download client/i))
    await userEvent.click(screen.getByLabelText(/blocklist/i))
    await userEvent.click(screen.getByRole("button", { name: /^remove$/i }))
    expect(onConfirm).toHaveBeenCalledWith({ removeFromClient: false, blocklist: true })
  })

  it("warns that not blocklisting invites a re-grab", () => {
    open()
    expect(screen.getByText(/re-grab/i)).toBeTruthy()
  })

  it("resets checkboxes after closing and reopening for a different row", async () => {
    const onConfirm = vi.fn()
    const { rerender } = render(
      <RemoveQueueItemDialog
        open
        onOpenChange={vi.fn()}
        title="Dune.2021-GRP"
        onConfirm={onConfirm}
      />,
    )

    await userEvent.click(screen.getByLabelText(/remove from download client/i))
    await userEvent.click(screen.getByLabelText(/blocklist/i))

    rerender(
      <RemoveQueueItemDialog
        open={false}
        onOpenChange={vi.fn()}
        title="Dune.2021-GRP"
        onConfirm={onConfirm}
      />,
    )
    rerender(
      <RemoveQueueItemDialog
        open
        onOpenChange={vi.fn()}
        title="Another.Show.2022-GRP"
        onConfirm={onConfirm}
      />,
    )

    await userEvent.click(screen.getByRole("button", { name: /^remove$/i }))
    expect(onConfirm).toHaveBeenCalledWith({ removeFromClient: true, blocklist: false })
  })
})
