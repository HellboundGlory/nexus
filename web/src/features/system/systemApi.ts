import { useEffect } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost } from "@/lib/api"
import { useActivity } from "@/lib/activity"
import { useToast } from "@/lib/toast"
import { humanizeName } from "./format"

export type ScheduledTask = {
  name: string
  intervalSeconds: number
  lastExecution: string | null
  lastDurationSeconds: number | null
  nextExecution: string
}
export type QueueTask = {
  id: string
  name: string
  status: string
  queuedAt: string
  startedAt: string | null
  endedAt: string | null
  durationSeconds: number | null
}
export type TasksData = { scheduled: ScheduledTask[]; queue: QueueTask[] }

export const systemKeys = { tasks: ["system", "tasks"] as const }

export function useTasks() {
  const events = useActivity()
  const qc = useQueryClient()
  const query = useQuery({ queryKey: systemKeys.tasks, queryFn: () => apiGet<TasksData>("/system/tasks") })

  // Live: refetch when a task.updated event arrives.
  useEffect(() => {
    if (events.some((e) => e.type === "task.updated")) {
      qc.invalidateQueries({ queryKey: systemKeys.tasks })
    }
  }, [events, qc])

  return query
}

export function useRunTask() {
  const qc = useQueryClient()
  const { toast } = useToast()
  return useMutation({
    mutationFn: (name: string) => apiPost<{ taskId: string }>(`/system/tasks/${encodeURIComponent(name)}/run`),
    onSuccess: (_data, name) => {
      qc.invalidateQueries({ queryKey: systemKeys.tasks })
      toast(`Started ${humanizeName(name)}`)
    },
  })
}
