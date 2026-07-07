import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MediaGrid } from "@/features/library/MediaGrid"

describe("MediaGrid", () => {
  it("shows loading state", () => {
    render(<MediaGrid items={undefined} isLoading isError={false} onRetry={() => {}} empty="none" renderCard={() => null} />)
    expect(screen.getByTestId("grid-loading")).toBeInTheDocument()
  })
  it("shows empty state", () => {
    render(<MediaGrid items={[]} isLoading={false} isError={false} onRetry={() => {}} empty="No movies yet" renderCard={() => null} />)
    expect(screen.getByText("No movies yet")).toBeInTheDocument()
  })
  it("shows an error state with a working retry button", async () => {
    const onRetry = vi.fn()
    render(<MediaGrid items={undefined} isLoading={false} isError onRetry={onRetry} empty="none" renderCard={() => null} />)
    await userEvent.click(screen.getByRole("button", { name: /retry/i }))
    expect(onRetry).toHaveBeenCalled()
  })
  it("renders cards for items", () => {
    render(
      <MediaGrid
        items={[{ id: 1 }, { id: 2 }]}
        isLoading={false}
        isError={false}
        onRetry={() => {}}
        empty="none"
        renderCard={(it: { id: number }) => <div key={it.id}>card-{it.id}</div>}
      />,
    )
    expect(screen.getByText("card-1")).toBeInTheDocument()
    expect(screen.getByText("card-2")).toBeInTheDocument()
  })
})
