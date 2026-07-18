import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPut } from "@/lib/api"
import type { MediaDefaults } from "./mediaDefaultsTypes"

export const mediaDefaultsKey = ["settings", "mediaDefaults"] as const

export function useMediaDefaults() {
  return useQuery({ queryKey: mediaDefaultsKey, queryFn: () => apiGet<MediaDefaults>("/config/media-defaults") })
}

export function useSaveMediaDefaults() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (d: MediaDefaults) => apiPut<MediaDefaults>("/config/media-defaults", d),
    onSuccess: () => qc.invalidateQueries({ queryKey: mediaDefaultsKey }),
  })
}
