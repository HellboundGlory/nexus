import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { DeleteConfirmDialog } from "@/features/library/DeleteConfirmDialog"

describe("DeleteConfirmDialog", () => {
  it("defaults the checkbox to off and confirms with false", async () => {
    const onConfirm = vi.fn()
    render(<DeleteConfirmDialog open title="Film" onOpenChange={vi.fn()} onConfirm={onConfirm} />)
    expect((screen.getByRole("checkbox") as HTMLInputElement).checked).toBe(false)
    await userEvent.click(screen.getByRole("button", { name: /^delete$/i }))
    expect(onConfirm).toHaveBeenCalledWith(false)
  })

  it("confirms with true when the box is checked", async () => {
    const onConfirm = vi.fn()
    render(<DeleteConfirmDialog open title="Film" onOpenChange={vi.fn()} onConfirm={onConfirm} />)
    await userEvent.click(screen.getByRole("checkbox"))
    await userEvent.click(screen.getByRole("button", { name: /^delete$/i }))
    expect(onConfirm).toHaveBeenCalledWith(true)
  })
})
