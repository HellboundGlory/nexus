import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type { NamingConfig, RootFolder } from "./configTypes"

export const configKeys = {
  rootFolders: ["settings", "rootfolders"] as const,
  naming: ["settings", "naming"] as const,
  automation: ["settings", "automation"] as const,
  systemStatus: ["settings", "systemStatus"] as const,
}

export function useRootFolders() {
  return useQuery({ queryKey: configKeys.rootFolders, queryFn: () => apiGet<RootFolder[]>("/rootfolder") })
}

export function useAddRootFolder() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (path: string) => apiPost<RootFolder>("/rootfolder", { path }),
    onSuccess: () => qc.invalidateQueries({ queryKey: configKeys.rootFolders }),
  })
}

export function useDeleteRootFolder() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/rootfolder/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: configKeys.rootFolders }),
  })
}

export function useNamingConfig() {
  return useQuery({ queryKey: configKeys.naming, queryFn: () => apiGet<NamingConfig>("/config/naming") })
}

export function useSaveNaming() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (cfg: NamingConfig) => apiPut<NamingConfig>("/config/naming", cfg),
    onSuccess: () => qc.invalidateQueries({ queryKey: configKeys.naming }),
  })
}
