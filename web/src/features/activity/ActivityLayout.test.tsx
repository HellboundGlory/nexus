// web/src/features/activity/ActivityLayout.test.tsx
import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { ActivityLayout } from "./ActivityLayout"

vi.mock("./api", () => ({ useActivityInvalidation: vi.fn() }))

describe("ActivityLayout", () => {
  it("renders Queue and History tab links", () => {
    render(
      <MemoryRouter initialEntries={["/activity/queue"]}>
        <ActivityLayout />
      </MemoryRouter>,
    )
    const queue = screen.getByRole("link", { name: /queue/i })
    const history = screen.getByRole("link", { name: /history/i })
    expect(queue).toHaveAttribute("href", "/activity/queue")
    expect(history).toHaveAttribute("href", "/activity/history")
  })
})
