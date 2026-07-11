import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { SettingsLayout } from "./SettingsLayout"

describe("SettingsLayout", () => {
  it("renders tab links for the 3a sections with correct hrefs", () => {
    render(<MemoryRouter initialEntries={["/settings/indexers"]}><SettingsLayout /></MemoryRouter>)
    const indexers = screen.getByRole("link", { name: "Indexers" })
    const clients = screen.getByRole("link", { name: "Download Clients" })
    expect(indexers).toHaveAttribute("href", "/settings/indexers")
    expect(clients).toHaveAttribute("href", "/settings/downloadclients")
  })
})
