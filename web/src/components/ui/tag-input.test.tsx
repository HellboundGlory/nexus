import { describe, it, expect, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { TagInput, type TagOption } from "./tag-input"

const options: TagOption[] = [
  { id: 1, label: "anime" },
  { id: 2, label: "uk-tv" },
]

describe("TagInput", () => {
  it("renders selected tags as chips and removes them", async () => {
    const onChange = vi.fn()
    render(<TagInput value={[1]} options={options} onChange={onChange} onCreate={vi.fn()} />)
    expect(screen.getByText("anime")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Remove anime" }))
    expect(onChange).toHaveBeenCalledWith([])
  })

  it("filters suggestions and selects one on click", async () => {
    const onChange = vi.fn()
    render(<TagInput value={[]} options={options} onChange={onChange} onCreate={vi.fn()} />)
    await userEvent.type(screen.getByRole("textbox"), "uk")
    expect(screen.queryByRole("button", { name: "anime" })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "uk-tv" }))
    expect(onChange).toHaveBeenCalledWith([2])
  })

  it("hides already-selected tags from the suggestions", async () => {
    render(<TagInput value={[1]} options={options} onChange={vi.fn()} onCreate={vi.fn()} />)
    await userEvent.type(screen.getByRole("textbox"), "anim")
    expect(screen.queryByRole("button", { name: "anime" })).not.toBeInTheDocument()
  })

  it("creates a new tag on Enter when the text matches nothing", async () => {
    const onCreate = vi.fn().mockResolvedValue(9)
    const onChange = vi.fn()
    render(<TagInput value={[]} options={options} onChange={onChange} onCreate={onCreate} />)
    await userEvent.type(screen.getByRole("textbox"), "documentary{Enter}")
    await waitFor(() => expect(onCreate).toHaveBeenCalledWith("documentary"))
    await waitFor(() => expect(onChange).toHaveBeenCalledWith([9]))
  })

  it("selects the existing tag instead of creating when the label differs only by case", async () => {
    const onCreate = vi.fn()
    const onChange = vi.fn()
    render(<TagInput value={[]} options={options} onChange={onChange} onCreate={onCreate} />)
    await userEvent.type(screen.getByRole("textbox"), "ANIME{Enter}")
    await waitFor(() => expect(onChange).toHaveBeenCalledWith([1]))
    expect(onCreate).not.toHaveBeenCalled()
  })

  it("does not pick the first suggestion on Enter", async () => {
    const onCreate = vi.fn().mockResolvedValue(9)
    const onChange = vi.fn()
    render(<TagInput value={[]} options={options} onChange={onChange} onCreate={onCreate} />)
    // "an" is a prefix of "anime" but not equal to it: this must CREATE "an".
    await userEvent.type(screen.getByRole("textbox"), "an{Enter}")
    await waitFor(() => expect(onCreate).toHaveBeenCalledWith("an"))
    expect(onChange).not.toHaveBeenCalledWith([1])
  })

  it("ignores Enter on blank input", async () => {
    const onCreate = vi.fn()
    render(<TagInput value={[]} options={options} onChange={vi.fn()} onCreate={onCreate} />)
    await userEvent.type(screen.getByRole("textbox"), "   {Enter}")
    expect(onCreate).not.toHaveBeenCalled()
  })
})
