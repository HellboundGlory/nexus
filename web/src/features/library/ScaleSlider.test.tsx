import { describe, it, expect, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { ScaleSlider } from "@/features/library/ScaleSlider"

describe("ScaleSlider", () => {
  it("renders a range input reflecting the value", () => {
    render(<ScaleSlider value={150} onChange={() => {}} />)
    const range = screen.getByLabelText("Card size") as HTMLInputElement
    expect(range).toHaveAttribute("type", "range")
    expect(range.value).toBe("150")
  })
  it("calls onChange with a number when dragged", () => {
    const onChange = vi.fn()
    render(<ScaleSlider value={150} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText("Card size"), { target: { value: "200" } })
    expect(onChange).toHaveBeenCalledWith(200)
  })
})
