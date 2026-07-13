import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { CalendarView } from "./CalendarView"
import * as api from "./api"
import type { CalendarEntry } from "./types"

vi.mock("./api")

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.useCalendarInvalidation).mockReturnValue(undefined)
})

function renderView() {
  render(
    <MemoryRouter>
      <CalendarView />
    </MemoryRouter>,
  )
}

describe("CalendarView", () => {
  it("shows an empty state when nothing is scheduled", () => {
    vi.mocked(api.useCalendar).mockReturnValue({ data: [], isLoading: false } as never)
    renderView()
    expect(screen.getByText(/nothing scheduled/i)).toBeInTheDocument()
  })

  it("shows an error message (not the empty state) when the query fails", () => {
    vi.mocked(api.useCalendar).mockReturnValue({ data: undefined, isLoading: false, isError: true, error: new Error("boom") } as never)
    renderView()
    expect(screen.getByText(/couldn.t load the calendar/i)).toBeInTheDocument()
    expect(screen.queryByText(/nothing scheduled/i)).not.toBeInTheDocument()
  })

  it("renders episode and movie rows grouped under Today", () => {
    const now = new Date()
    const iso = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`
    const data: CalendarEntry[] = [
      { type: "episode", date: iso, hasFile: false, seriesId: 1, seriesTitle: "The Show", seasonNumber: 2, episodeNumber: 4, episodeTitle: "Deep" },
      { type: "movie", date: iso, hasFile: true, movieId: 7, movieTitle: "The Film", year: 2026 },
    ]
    vi.mocked(api.useCalendar).mockReturnValue({ data, isLoading: false } as never)
    renderView()
    expect(screen.getByText("Today")).toBeInTheDocument()
    expect(screen.getByText("The Show")).toBeInTheDocument()
    expect(screen.getByText("S02E04")).toBeInTheDocument()
    expect(screen.getByText("The Film (2026)")).toBeInTheDocument()
  })
})
