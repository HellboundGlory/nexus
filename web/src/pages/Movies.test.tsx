import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { Movies } from "@/pages/Movies"
import * as lib from "@/features/library/api"

vi.mock("@/features/library/api", async (orig) => {
  const actual = await orig<typeof import("@/features/library/api")>()
  return { ...actual, useMovies: vi.fn() }
})

function renderMovies() {
  return render(
    <MemoryRouter>
      <Movies />
    </MemoryRouter>,
  )
}

beforeEach(() => vi.clearAllMocks())

describe("Movies page", () => {
  it("renders a card per movie", () => {
    vi.mocked(lib.useMovies).mockReturnValue({
      data: [
        { id: 1, title: "Dune", year: 2021, posterUrl: "", monitored: true, hasFile: true },
        { id: 2, title: "Arrival", year: 2016, posterUrl: "", monitored: true, hasFile: false },
      ],
      isLoading: false, isError: false, refetch: vi.fn(),
    } as unknown as ReturnType<typeof lib.useMovies>)
    renderMovies()
    expect(screen.getByText("Dune")).toBeInTheDocument()
    expect(screen.getByText("Arrival")).toBeInTheDocument()
    expect(screen.getByText("Downloaded")).toBeInTheDocument()
    expect(screen.getByText("Missing")).toBeInTheDocument()
  })
})
