import { describe, it, expect, vi } from "vitest"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { ToastProvider } from "@/lib/toast"
import { TasksSection } from "@/features/system/TasksSection"
import * as api from "@/features/system/systemApi"

vi.mock("@/features/system/systemApi", async (orig) => ({
  ...(await orig<typeof import("@/features/system/systemApi")>()),
  useTasks: vi.fn(),
  useRunTask: vi.fn(),
  useTasksInvalidation: vi.fn(),
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
    // "ImportCompletedDownloads" appears both as a Scheduled row and as the
    // running Queue row's name (glyph + name are now structurally uniform
    // across statuses), so scope each assertion to its own table.
    const scheduledTable = screen.getByTestId("scheduled-table")
    const queueTable = screen.getByTestId("queue-table")
    expect(within(scheduledTable).getByText("Import Completed Downloads")).toBeInTheDocument()
    expect(within(queueTable).getByText("Download Queue Monitor")).toBeInTheDocument()
    expect(within(queueTable).getByText(/running/i)).toBeInTheDocument() // the running queue row's "Running…" duration cell
  })

  it("runs a scheduled task", async () => {
    const run = stub()
    renderTasks()
    await userEvent.click(screen.getByRole("button", { name: /run import completed downloads/i }))
    expect(run).toHaveBeenCalledWith("ImportCompletedDownloads")
  })
})
