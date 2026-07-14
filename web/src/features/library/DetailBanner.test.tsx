import { describe, it, expect } from "vitest"
import { render, screen } from "@testing-library/react"
import { DetailBanner } from "@/features/library/DetailBanner"

describe("DetailBanner", () => {
  it("renders a backdrop image when fanartUrl is set", () => {
    render(
      <DetailBanner fanartUrl="http://img/bd.jpg" posterUrl="" title="Breaking Bad">
        <p>meta</p>
      </DetailBanner>,
    )
    const bd = screen.getByTestId("banner-backdrop") as HTMLImageElement
    expect(bd.tagName).toBe("IMG")
    expect(bd.src).toContain("bd.jpg")
    expect(screen.getByText("meta")).toBeInTheDocument()
  })
  it("renders no backdrop image when fanartUrl is empty", () => {
    render(
      <DetailBanner fanartUrl="" posterUrl="" title="Breaking Bad">
        <p>meta</p>
      </DetailBanner>,
    )
    expect(screen.queryByTestId("banner-backdrop")).toBeNull()
    expect(screen.getByText("meta")).toBeInTheDocument()
  })
  it("renders the back slot when provided", () => {
    render(
      <DetailBanner fanartUrl="http://img/bd.jpg" posterUrl="" title="Breaking Bad" back={<button>← TV Shows</button>}>
        <p>meta</p>
      </DetailBanner>,
    )
    expect(screen.getByRole("button", { name: /← tv shows/i })).toBeInTheDocument()
  })
})
