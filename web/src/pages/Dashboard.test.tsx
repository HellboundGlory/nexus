import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { Dashboard } from "@/pages/Dashboard"
import * as api from "@/lib/api"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, getStatus: vi.fn() }
})

function renderDash() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <Dashboard />
    </QueryClientProvider>,
  )
}

beforeEach(() => vi.clearAllMocks())

describe("Dashboard", () => {
  it("renders status cards from /system/status", async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ version: "0.1.0", appName: "Nexus", healthy: true, taskCount: 3 })
    renderDash()
    expect(await screen.findByText("0.1.0")).toBeInTheDocument()
    expect(screen.getByText(/healthy/i)).toBeInTheDocument()
    expect(screen.getByText("Active Tasks")).toBeInTheDocument()
    expect(screen.getByText("3")).toBeInTheDocument()
  })

  it("shows a graceful error state when /system/status fails", async () => {
    vi.mocked(api.getStatus).mockRejectedValue(new Error("boom"))
    renderDash()
    expect(await screen.findByText("Unknown")).toBeInTheDocument()
    expect(screen.getAllByText("—")).toHaveLength(2)
  })
})
