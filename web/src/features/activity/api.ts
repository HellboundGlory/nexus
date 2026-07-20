// web/src/features/activity/api.ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useEffect } from "react"
import { apiGet, apiPost, apiDelete } from "@/lib/api"
import { useActivity } from "@/lib/activity"
import { shouldRefresh } from "./resolve"
import type { QueueItem, HistoryEvent, BlocklistEntry, Paged, ClearResult } from "./types"

export const activityKeys = {
  queue: ["queue"] as const,
  history: ["history"] as const,
  blocklist: ["blocklist"] as const,
  historyPage: (page: number, pageSize: number) => ["history", page, pageSize] as const,
  blocklistPage: (page: number, pageSize: number) => ["blocklist", page, pageSize] as const,
}

// /queue enriches each grabbed row with live progress from the download client
// on every request, so this interval is what makes the progress bar advance.
// It only polls while an Activity tab is mounted.
const QUEUE_POLL_MS = 5_000

export function useQueue() {
  return useQuery({
    queryKey: activityKeys.queue,
    queryFn: () => apiGet<QueueItem[]>("/queue"),
    refetchInterval: QUEUE_POLL_MS,
  })
}

export function useHistory(page: number, pageSize: number) {
  return useQuery({
    queryKey: activityKeys.historyPage(page, pageSize),
    queryFn: () => apiGet<Paged<HistoryEvent>>(`/history?page=${page}&pageSize=${pageSize}`),
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
    mutationFn: ({ id, removeFromClient, blocklist }: { id: number; removeFromClient: boolean; blocklist: boolean }) =>
      apiDelete<{ ok: boolean }>(
        `/queue/${id}?removeFromClient=${removeFromClient}&blocklist=${blocklist}`,
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: activityKeys.queue })
      qc.invalidateQueries({ queryKey: activityKeys.blocklist })
    },
  })
}

export function useClearQueue() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ force }: { force?: boolean }) =>
      apiDelete<ClearResult>(force ? "/queue?force=true" : "/queue"),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.queue }),
  })
}

export function useClearHistory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => apiDelete<{ removed: number }>("/history"),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.history }),
  })
}

export function useClearBlocklist() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => apiDelete<{ removed: number }>("/blocklist"),
    onSuccess: () => qc.invalidateQueries({ queryKey: activityKeys.blocklist }),
  })
}

export function useBlocklist(page: number, pageSize: number) {
  return useQuery({
    queryKey: activityKeys.blocklistPage(page, pageSize),
    queryFn: () => apiGet<Paged<BlocklistEntry>>(`/blocklist?page=${page}&pageSize=${pageSize}`),
  })
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
