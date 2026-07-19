import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import { StatusSection } from "@/features/system/StatusSection"
import * as cfg from "@/features/settings/configApi"

vi.mock("@/features/settings/configApi", async (orig) => ({
  ...(await orig<typeof import("@/features/settings/configApi")>()),
  useSystemStatus: vi.fn(),
}))

describe("StatusSection", () => {
  it("renders system info", () => {
    vi.mocked(cfg.useSystemStatus).mockReturnValue({
      data: { version: "1.2.3", appName: "Nexus", healthy: true, taskCount: 4 },
      isLoading: false,
    } as unknown as ReturnType<typeof cfg.useSystemStatus>)
    render(<StatusSection />)
    expect(screen.getByText("1.2.3")).toBeInTheDocument()
    expect(screen.getByText("4")).toBeInTheDocument()
  })
})
