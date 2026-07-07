import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider, useToast } from "@/lib/toast"

function Trigger() {
  const { toast } = useToast()
  return <button onClick={() => toast("Saved!", { variant: "ok" })}>go</button>
}

describe("toast", () => {
  it("shows a message after toast() is called", async () => {
    render(
      <ToastProvider>
        <Trigger />
      </ToastProvider>,
    )
    expect(screen.queryByText("Saved!")).not.toBeInTheDocument()
    await userEvent.click(screen.getByText("go"))
    expect(await screen.findByText("Saved!")).toBeInTheDocument()
  })
})
