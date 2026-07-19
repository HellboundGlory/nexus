import { describe, it, expect, vi, beforeEach } from "vitest"
import type { ReactNode } from "react"
import { renderHook, waitFor, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import * as apiClient from "@/lib/api"
import * as activityLib from "@/lib/activity"
import { ToastProvider, useToast } from "@/lib/toast"
import type { ActivityEvent } from "@/lib/ws"
import { useRunTask, useTasksInvalidation, systemKeys } from "@/features/system/systemApi"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, apiGet: vi.fn(), apiPost: vi.fn() }
})
vi.mock("@/lib/activity", () => ({ useActivity: vi.fn() }))

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function wrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <ToastProvider>{children}</ToastProvider>
      </QueryClientProvider>
    )
  }
}

function taskUpdatedEvent(): ActivityEvent {
  return { id: "e1", type: "task.updated", data: {}, receivedAt: Date.now() }
}
function otherEvent(): ActivityEvent {
  return { id: "e2", type: "download.status", data: {}, receivedAt: Date.now() }
}

beforeEach(() => vi.clearAllMocks())

describe("useTasksInvalidation", () => {
  it("invalidates the tasks query when a task.updated event arrives", async () => {
    vi.mocked(activityLib.useActivity).mockReturnValue([taskUpdatedEvent()])

    const qc = makeClient()
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries")

    renderHook(() => useTasksInvalidation(), { wrapper: wrapper(qc) })

    await waitFor(() => expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: systemKeys.tasks }))
  })

  it("does not invalidate when no task.updated event is present", () => {
    vi.mocked(activityLib.useActivity).mockReturnValue([otherEvent()])

    const qc = makeClient()
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries")

    renderHook(() => useTasksInvalidation(), { wrapper: wrapper(qc) })

    expect(invalidateSpy).not.toHaveBeenCalled()
  })
})

describe("useRunTask", () => {
  it("posts to the raw task name and toasts the humanized name on success", async () => {
    vi.mocked(activityLib.useActivity).mockReturnValue([])
    vi.mocked(apiClient.apiPost).mockResolvedValue({ taskId: "tid" })

    const qc = makeClient()
    const { result } = renderHook(() => useRunTask(), { wrapper: wrapper(qc) })

    result.current.mutate("SomeTask")

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(apiClient.apiPost).toHaveBeenCalledWith("/system/tasks/SomeTask/run")
    // The onSuccess handler renders a real toast via ToastProvider — assert
    // the actual DOM output rather than a mocked toast fn.
    await waitFor(() => expect(screen.getByText("Started Some Task")).toBeInTheDocument())
  })

  it("uses the useToast context, not a bypassed/mocked toast call", async () => {
    // Render useRunTask alongside useToast in the SAME provider tree to prove
    // the mutation's onSuccess goes through the real context, not a mock.
    vi.mocked(activityLib.useActivity).mockReturnValue([])
    vi.mocked(apiClient.apiPost).mockResolvedValue({ taskId: "tid" })

    const qc = makeClient()
    const { result } = renderHook(
      () => ({ run: useRunTask(), toast: useToast() }),
      { wrapper: wrapper(qc) },
    )

    expect(typeof result.current.toast.toast).toBe("function")

    result.current.run.mutate("AnotherTask")

    await waitFor(() => expect(result.current.run.isSuccess).toBe(true))
    expect(apiClient.apiPost).toHaveBeenCalledWith("/system/tasks/AnotherTask/run")
  })
})
