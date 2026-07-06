import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { Placeholder } from "@/pages/Placeholder"

describe("Placeholder", () => {
  it("shows the title and a coming-soon note", () => {
    render(<Placeholder title="Movies" />)
    expect(screen.getByRole("heading", { name: "Movies" })).toBeInTheDocument()
    expect(screen.getByText(/later slice/i)).toBeInTheDocument()
  })
})
