// web/src/features/search/api.ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost } from "@/lib/api"
import { activityKeys } from "@/features/activity/api"
import { interactivePath, grabBody } from "./resolve"
import type { InteractiveResult, ScoredRelease, SearchTarget } from "./types"

export const searchKeys = {
  interactive: (t: SearchTarget) => ["interactive-search", interactivePath(t)] as const,
}

// Interactive search is an explicit user action against live indexers, so it does
// not poll and does not serve stale results — refetching on mount/focus would fire
// real indexer queries the user did not ask for.
export function useInteractiveSearch(target: SearchTarget | null) {
  return useQuery({
    queryKey: target ? searchKeys.interactive(target) : ["interactive-search", "idle"],
    queryFn: () => apiGet<InteractiveResult>(interactivePath(target!)),
    enabled: target !== null,
    staleTime: Infinity,
    gcTime: 0,
    refetchOnWindowFocus: false,
    retry: false,
  })
}

// The grab reuses POST /queue — the pre-existing tracked manual-grab endpoint
// (queue row + history + QueueUpdated). downloadclient's /download would NOT
// track it and the release would never import.
export function useInteractiveGrab() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ release, target }: { release: ScoredRelease; target: SearchTarget }) =>
      apiPost<{ id: number }>("/queue", grabBody(release, target)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: activityKeys.queue })
      qc.invalidateQueries({ queryKey: activityKeys.history })
    },
  })
}
