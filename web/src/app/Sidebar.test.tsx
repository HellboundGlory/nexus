import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { Sidebar, NAV_ITEMS } from "@/app/Sidebar"

describe("Sidebar", () => {
  it("renders all nav items", () => {
    render(<MemoryRouter initialEntries={["/"]}><Sidebar /></MemoryRouter>)
    for (const item of NAV_ITEMS) {
      expect(screen.getByRole("link", { name: new RegExp(item.label, "i") })).toBeInTheDocument()
    }
  })

  it("marks the active route with aria-current", () => {
    render(<MemoryRouter initialEntries={["/movies"]}><Sidebar /></MemoryRouter>)
    const active = screen.getByRole("link", { name: /movies/i })
    expect(active).toHaveAttribute("aria-current", "page")
  })
})
