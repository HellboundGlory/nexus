import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import { basePath } from "./payload"
import type { ConnectionKind, ConnectionRow, SchemaEntry, TestResult } from "./types"

export const settingsKeys = {
  list: (kind: ConnectionKind) => ["settings", kind, "list"] as const,
  schema: (kind: ConnectionKind) => ["settings", kind, "schema"] as const,
}

export function useConnections(kind: ConnectionKind) {
  return useQuery({
    queryKey: settingsKeys.list(kind),
    queryFn: () => apiGet<ConnectionRow[]>(basePath(kind)),
  })
}

export function useConnectionSchema(kind: ConnectionKind) {
  return useQuery({
    queryKey: settingsKeys.schema(kind),
    queryFn: () => apiGet<SchemaEntry[]>(`${basePath(kind)}/schema`),
  })
}

export function useSaveConnection(kind: ConnectionKind) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ payload, id }: { payload: Record<string, unknown>; id?: number }) =>
      id == null
        ? apiPost<ConnectionRow>(basePath(kind), payload)
        : apiPut<ConnectionRow>(`${basePath(kind)}/${id}`, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: settingsKeys.list(kind) }),
  })
}

export function useDeleteConnection(kind: ConnectionKind) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`${basePath(kind)}/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: settingsKeys.list(kind) }),
  })
}

export function useTestConnection(kind: ConnectionKind) {
  void kind
  return useMutation({
    mutationFn: (req: { path: string; body?: Record<string, unknown> }) =>
      apiPost<TestResult>(req.path, req.body),
  })
}
