import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { Pagination } from "./pagination"

function setup(over: Partial<React.ComponentProps<typeof Pagination>> = {}) {
  const onPageChange = vi.fn()
  const onPageSizeChange = vi.fn()
  render(
    <Pagination
      page={1}
      pageSize={50}
      total={120}
      onPageChange={onPageChange}
      onPageSizeChange={onPageSizeChange}
      {...over}
    />,
  )
  return { onPageChange, onPageSizeChange }
}

describe("Pagination", () => {
  it("summarises the visible slice", () => {
    setup({ page: 2, pageSize: 50, total: 120 })
    expect(screen.getByText(/51.*100.*120/)).toBeTruthy()
  })

  it("clamps the summary to the total on the last page", () => {
    setup({ page: 3, pageSize: 50, total: 120 })
    expect(screen.getByText(/101.*120.*120/)).toBeTruthy()
  })

  it("disables Previous on the first page", () => {
    setup({ page: 1 })
    expect(screen.getByRole("button", { name: /previous/i })).toHaveProperty("disabled", true)
  })

  it("disables Next on the last page", () => {
    setup({ page: 3, pageSize: 50, total: 120 })
    expect(screen.getByRole("button", { name: /next/i })).toHaveProperty("disabled", true)
  })

  it("advances the page", async () => {
    const { onPageChange } = setup({ page: 1 })
    await userEvent.click(screen.getByRole("button", { name: /next/i }))
    expect(onPageChange).toHaveBeenCalledWith(2)
  })

  it("reports a page-size change", async () => {
    const { onPageSizeChange } = setup()
    await userEvent.selectOptions(screen.getByLabelText(/per page/i), "25")
    expect(onPageSizeChange).toHaveBeenCalledWith(25)
  })

  it("renders nothing when there is nothing to page", () => {
    const { container } = render(
      <Pagination page={1} pageSize={50} total={0} onPageChange={vi.fn()} onPageSizeChange={vi.fn()} />,
    )
    expect(container.textContent).toBe("")
  })
})
