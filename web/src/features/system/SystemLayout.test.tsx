import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { SystemLayout } from "./SystemLayout"

describe("SystemLayout", () => {
  it("renders the System heading and Status + Tasks tab links", () => {
    render(
      <MemoryRouter initialEntries={["/system/status"]}>
        <SystemLayout />
      </MemoryRouter>,
    )
    expect(screen.getByRole("heading", { name: "System" })).toBeInTheDocument()
    const status = screen.getByRole("link", { name: "Status" })
    const tasks = screen.getByRole("link", { name: "Tasks" })
    expect(status).toHaveAttribute("href", "/system/status")
    expect(tasks).toHaveAttribute("href", "/system/tasks")
  })
})
