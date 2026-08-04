import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type { Tag } from "./tagTypes"

export const tagKeys = {
  all: ["settings", "tags"] as const,
}

export function useTags() {
  return useQuery({ queryKey: tagKeys.all, queryFn: () => apiGet<Tag[]>("/tag") })
}

export function useCreateTag() {
  const qc = useQueryClient()
  return useMutation<Tag, Error, string>({
    mutationFn: (label) => apiPost<Tag>("/tag", { label }),
    onSuccess: () => qc.invalidateQueries({ queryKey: tagKeys.all }),
  })
}

export function useRenameTag() {
  const qc = useQueryClient()
  return useMutation<{ ok: boolean }, Error, { id: number; label: string }>({
    mutationFn: ({ id, label }) => apiPut<{ ok: boolean }>(`/tag/${id}`, { label }),
    onSuccess: () => qc.invalidateQueries({ queryKey: tagKeys.all }),
  })
}

export function useDeleteTag() {
  const qc = useQueryClient()
  return useMutation<{ ok: boolean }, Error, number>({
    mutationFn: (id) => apiDelete<{ ok: boolean }>(`/tag/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: tagKeys.all }),
  })
}
