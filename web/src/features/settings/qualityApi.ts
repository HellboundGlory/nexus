import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type { ProfilePayload, QualityDefinition, QualityProfile } from "./qualityTypes"

export const qualityKeys = {
  profiles: ["settings", "quality", "profiles"] as const,
  definitions: ["settings", "quality", "definitions"] as const,
}

export function useQualityProfiles() {
  return useQuery({ queryKey: qualityKeys.profiles, queryFn: () => apiGet<QualityProfile[]>("/qualityprofile") })
}

export function useQualityDefinitions() {
  return useQuery({ queryKey: qualityKeys.definitions, queryFn: () => apiGet<QualityDefinition[]>("/quality/definitions") })
}

export function useSaveProfile() {
  const qc = useQueryClient()
  return useMutation<QualityProfile | { ok: boolean }, Error, { payload: ProfilePayload; id?: number }>({
    mutationFn: ({ payload, id }) =>
      id == null
        ? apiPost<QualityProfile>("/qualityprofile", payload)
        : apiPut<{ ok: boolean }>(`/qualityprofile/${id}`, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: qualityKeys.profiles }),
  })
}

export function useDeleteProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/qualityprofile/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: qualityKeys.profiles }),
  })
}
