import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type { ReleaseProfile, ReleaseProfilePayload } from "./releaseProfileTypes"

export const releaseProfileKeys = {
  profiles: ["settings", "releaseprofiles"] as const,
}

export function useReleaseProfiles() {
  return useQuery({ queryKey: releaseProfileKeys.profiles, queryFn: () => apiGet<ReleaseProfile[]>("/releaseprofile") })
}

export function useSaveReleaseProfile() {
  const qc = useQueryClient()
  return useMutation<ReleaseProfile | { ok: boolean }, Error, { payload: ReleaseProfilePayload; id?: number }>({
    mutationFn: ({ payload, id }) =>
      id == null
        ? apiPost<ReleaseProfile>("/releaseprofile", payload)
        : apiPut<{ ok: boolean }>(`/releaseprofile/${id}`, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: releaseProfileKeys.profiles }),
  })
}

export function useDeleteReleaseProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/releaseprofile/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: releaseProfileKeys.profiles }),
  })
}