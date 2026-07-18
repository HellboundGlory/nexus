import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { apiGet, apiPost, apiPut, apiDelete } from "@/lib/api"
import type {
  Movie, Series, SeriesDetail, RootFolder, QualityProfile, MetadataResult,
  AddMovieBody, AddSeriesBody, MediaKind,
} from "./types"

export const libraryKeys = {
  movies: ["library", "movies"] as const,
  movie: (id: number) => ["library", "movie", id] as const,
  series: ["library", "series"] as const,
  seriesDetail: (id: number) => ["library", "series", id] as const,
  rootFolders: ["library", "rootfolders"] as const,
  qualityProfiles: ["library", "qualityprofiles"] as const,
  lookup: (term: string, kind: MediaKind) => ["library", "lookup", kind, term] as const,
}

// ---- reads ----
export function useMovies() {
  return useQuery({ queryKey: libraryKeys.movies, queryFn: () => apiGet<Movie[]>("/movies") })
}
export function useSeries() {
  return useQuery({ queryKey: libraryKeys.series, queryFn: () => apiGet<Series[]>("/series") })
}
export function useMovieDetail(id: number) {
  return useQuery({ queryKey: libraryKeys.movie(id), queryFn: () => apiGet<Movie>(`/movies/${id}`) })
}
export function useSeriesDetail(id: number) {
  return useQuery({ queryKey: libraryKeys.seriesDetail(id), queryFn: () => apiGet<SeriesDetail>(`/series/${id}`) })
}
export function useRootFolders() {
  return useQuery({ queryKey: libraryKeys.rootFolders, queryFn: () => apiGet<RootFolder[]>("/rootfolder") })
}
export function useQualityProfiles() {
  return useQuery({ queryKey: libraryKeys.qualityProfiles, queryFn: () => apiGet<QualityProfile[]>("/qualityprofile") })
}
export function useLookup(term: string, kind: MediaKind) {
  const q = term.trim()
  return useQuery({
    queryKey: libraryKeys.lookup(q, kind),
    queryFn: () => apiGet<MetadataResult[]>(`/media/lookup?term=${encodeURIComponent(q)}&kind=${kind}`),
    enabled: q.length > 0,
  })
}

// ---- mutations ----
export function useAddMovie() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: AddMovieBody) => apiPost<Movie>("/movies", b),
    onSuccess: () => qc.invalidateQueries({ queryKey: libraryKeys.movies }),
  })
}
export function useAddSeries() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: AddSeriesBody) => apiPost<Series>("/series", b),
    onSuccess: () => qc.invalidateQueries({ queryKey: libraryKeys.series }),
  })
}

type MonitorTarget =
  | { kind: "series"; id: number }
  | { kind: "movie"; id: number }
  | { kind: "season"; id: number }
  | { kind: "episode"; id: number }

export function useSetMonitored(invalidate: readonly unknown[]) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ target, monitored }: { target: MonitorTarget; monitored: boolean }) => {
      const path =
        target.kind === "series" ? `/series/${target.id}/monitor`
        : target.kind === "movie" ? `/movies/${target.id}/monitor`
        : target.kind === "season" ? `/season/${target.id}/monitor`
        : `/episode/${target.id}/monitor`
      return apiPut<{ ok: boolean }>(path, { monitored })
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: invalidate }),
  })
}

export function useAssignProfile(invalidate: readonly unknown[]) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ kind, id, qualityProfileId }: { kind: "movie" | "series"; id: number; qualityProfileId: number }) =>
      apiPut<{ ok: boolean }>(`/${kind === "movie" ? "movies" : "series"}/${id}/qualityprofile`, { qualityProfileId }),
    onSuccess: () => qc.invalidateQueries({ queryKey: invalidate }),
  })
}

export function useRefresh(invalidate: readonly unknown[]) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ kind, id }: { kind: "movie" | "series"; id: number }) =>
      apiPost<{ ok: boolean }>(`/${kind === "movie" ? "movies" : "series"}/${id}/refresh`),
    onSuccess: () => qc.invalidateQueries({ queryKey: invalidate }),
  })
}

export function useDelete() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ kind, id, deleteFiles }: { kind: "movie" | "series"; id: number; deleteFiles?: boolean }) =>
      apiDelete<{ ok: boolean }>(
        `/${kind === "movie" ? "movies" : "series"}/${id}${deleteFiles ? "?deleteFiles=true" : ""}`,
      ),
    onSuccess: (_d, v) =>
      qc.invalidateQueries({ queryKey: v.kind === "movie" ? libraryKeys.movies : libraryKeys.series }),
  })
}

export function useDeleteMovieFile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiDelete<{ ok: boolean }>(`/movies/${id}/file`),
    onSuccess: (_d, id) => qc.invalidateQueries({ queryKey: libraryKeys.movie(id) }),
  })
}

// Fire-and-forget search. 202 Accepted; results arrive later via the Activity feed.
export function useSearch() {
  return useMutation({
    mutationFn: (target:
      | { kind: "movie"; id: number }
      | { kind: "series"; id: number }
      | { kind: "season"; seriesId: number; seasonNumber: number }
      | { kind: "episode"; id: number }) => {
      const path =
        target.kind === "movie" ? `/automation/search/movie/${target.id}`
        : target.kind === "series" ? `/automation/search/series/${target.id}`
        : target.kind === "season" ? `/automation/search/series/${target.seriesId}/season/${target.seasonNumber}`
        : `/automation/search/episode/${target.id}`
      return apiPost<unknown>(path)
    },
  })
}
