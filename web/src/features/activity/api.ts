// web/src/features/activity/api.ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useEffect } from "react"
import { apiGet, apiPost, apiDelete } from "@/lib/api"
import { useActivity } from "@/lib/activity"
import { shouldRefresh } from "./resolve"
import type { QueueItem, HistoryEvent, BlocklistEntry } from "./types"

export const activityKeys = {
  queue: ["queue"] as const,
  history: ["history"] as const,
  blocklist: ["blocklist"] as const,
}

export function useQueue() {
  return useQuery({ queryKey: activityKeys.queue, queryFn: () => apiGet<QueueItem[]>("/queue") })
}

export function useHistory() {
  return useQuery({
    queryKey: activityKeys.history,
    queryFn: () => apiGet<HistoryEvent[]>("/history?limit=100"),
  })
}

export function useImportItem() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiPost<{ ok: boolean }>(`/queue/${id}/import`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: activityKeys.queue })
      qc.invalidateQueries({ queryKey: activityKeys.history })
    },
  })
}

export function useRemoveQueueItem() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/queue/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.queue }),
  })
}

export function useBlocklist() {
  return useQuery({ queryKey: activityKeys.blocklist, queryFn: () => apiGet<BlocklistEntry[]>("/blocklist") })
}

export function useRemoveBlocklist() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<void>(`/blocklist/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.blocklist }),
  })
}

export function useActivityInvalidation(): void {
  const events = useActivity()
  const qc = useQueryClient()
  const latest = events[0]
  useEffect(() => {
    if (latest && shouldRefresh(latest.type)) {
      qc.invalidateQueries({ queryKey: activityKeys.queue })
      qc.invalidateQueries({ queryKey: activityKeys.history })
      qc.invalidateQueries({ queryKey: activityKeys.blocklist })
    }
    // keyed on the latest event id so it fires once per new event
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [latest?.id])
}
