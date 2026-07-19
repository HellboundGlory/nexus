import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { TasksSection } from "@/features/system/TasksSection"
import * as api from "@/features/system/systemApi"

vi.mock("@/features/system/systemApi", async (orig) => ({
  ...(await orig<typeof import("@/features/system/systemApi")>()),
  useTasks: vi.fn(),
  useRunTask: vi.fn(),
}))
vi.mock("@/lib/activity", () => ({ useActivity: () => [] }))

function stub(run = vi.fn()) {
  vi.mocked(api.useTasks).mockReturnValue({
    data: {
      scheduled: [
        { name: "ImportCompletedDownloads", intervalSeconds: 5, lastExecution: "2026-07-18T19:00:00Z", lastDurationSeconds: 0, nextExecution: "2999-01-01T00:00:00Z" },
      ],
      queue: [
        { id: "q1", name: "DownloadQueueMonitor", status: "completed", queuedAt: "2026-07-18T19:00:00Z", startedAt: "2026-07-18T19:00:00Z", endedAt: "2026-07-18T19:00:01Z", durationSeconds: 1 },
        { id: "q2", name: "ImportCompletedDownloads", status: "running", queuedAt: "2026-07-18T19:00:02Z", startedAt: "2026-07-18T19:00:02Z", endedAt: null, durationSeconds: null },
      ],
    },
    isLoading: false,
  } as unknown as ReturnType<typeof api.useTasks>)
  vi.mocked(api.useRunTask).mockReturnValue({ mutate: run, isPending: false } as unknown as ReturnType<typeof api.useRunTask>)
  return run
}

function renderTasks() {
  render(<ToastProvider><TasksSection /></ToastProvider>)
}

describe("TasksSection", () => {
  it("renders humanized scheduled + queue rows", () => {
    stub()
    renderTasks()
    expect(screen.getByText("Import Completed Downloads")).toBeInTheDocument()
    expect(screen.getByText("Download Queue Monitor")).toBeInTheDocument()
    expect(screen.getByText(/running/i)).toBeInTheDocument() // the running queue row
  })

  it("runs a scheduled task", async () => {
    const run = stub()
    renderTasks()
    await userEvent.click(screen.getByRole("button", { name: /run import completed downloads/i }))
    expect(run).toHaveBeenCalledWith("ImportCompletedDownloads")
  })
})
