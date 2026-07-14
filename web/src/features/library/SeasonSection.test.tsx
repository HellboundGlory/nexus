import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { SeasonSection } from "@/features/library/SeasonSection"

function renderOne(defaultOpen: boolean, onSearch = vi.fn(), onToggleMonitor = vi.fn()) {
  render(
    <SeasonSection
      title="Specials" withFile={0} total={3} monitored
      defaultOpen={defaultOpen} onSearch={onSearch} onToggleMonitor={onToggleMonitor}
    >
      <li>episode-body</li>
    </SeasonSection>,
  )
}

describe("SeasonSection", () => {
  it("hides the body when defaultOpen is false and shows it after clicking the header", async () => {
    renderOne(false)
    expect(screen.queryByText("episode-body")).toBeNull()
    await userEvent.click(screen.getByText("Specials"))
    expect(screen.getByText("episode-body")).toBeInTheDocument()
  })
  it("shows the body when defaultOpen is true", () => {
    renderOne(true)
    expect(screen.getByText("episode-body")).toBeInTheDocument()
  })
  it("does not collapse when the search control is used", async () => {
    const onSearch = vi.fn()
    renderOne(true, onSearch)
    await userEvent.click(screen.getByRole("button", { name: /search season/i }))
    expect(onSearch).toHaveBeenCalled()
    expect(screen.getByText("episode-body")).toBeInTheDocument()
  })
})
