import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { Dashboard } from "@/pages/Dashboard"
import * as api from "@/lib/api"
import * as activity from "@/lib/activity"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, getStatus: vi.fn() }
})
vi.mock("@/lib/activity", async (orig) => {
  const actual = await orig<typeof import("@/lib/activity")>()
  return { ...actual, useActivity: vi.fn() }
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
    vi.mocked(activity.useActivity).mockReturnValue([])
    renderDash()
    expect(await screen.findByText("0.1.0")).toBeInTheDocument()
    expect(screen.getByText(/healthy/i)).toBeInTheDocument()
    expect(screen.getByText("3")).toBeInTheDocument()
  })

  it("shows an empty state when there are no events", async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ version: "0.1.0", appName: "Nexus", healthy: true, taskCount: 0 })
    vi.mocked(activity.useActivity).mockReturnValue([])
    renderDash()
    expect(await screen.findByText(/no activity yet/i)).toBeInTheDocument()
  })

  it("renders activity rows from the WS store", async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ version: "0.1.0", appName: "Nexus", healthy: true, taskCount: 0 })
    vi.mocked(activity.useActivity).mockReturnValue([
      { id: "1", type: "import.completed", data: { title: "The Bear" }, receivedAt: Date.now() },
    ])
    renderDash()
    expect(await screen.findByText("import.completed")).toBeInTheDocument()
  })

  it("shows a graceful error state when /system/status fails", async () => {
    vi.mocked(api.getStatus).mockRejectedValue(new Error("boom"))
    vi.mocked(activity.useActivity).mockReturnValue([])
    renderDash()
    expect(await screen.findByText("Unknown")).toBeInTheDocument()
    expect(screen.getAllByText("—")).toHaveLength(2)
  })
})
