// web/src/features/calendar/api.ts
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect } from "react"
import { apiGet } from "@/lib/api"
import { useActivity } from "@/lib/activity"
import { shouldRefresh } from "./resolve"
import type { CalendarEntry } from "./types"

export const calendarKeys = {
  all: ["calendar"] as const,
  range: (start: string, end: string) => ["calendar", start, end] as const,
}

export function useCalendar(start: string, end: string) {
  return useQuery({
    queryKey: calendarKeys.range(start, end),
    queryFn: () => apiGet<CalendarEntry[]>(`/calendar?start=${start}&end=${end}`),
  })
}

export function useCalendarInvalidation(): void {
  const events = useActivity()
  const qc = useQueryClient()
  const latest = events[0]
  useEffect(() => {
    if (latest && shouldRefresh(latest.type)) {
      qc.invalidateQueries({ queryKey: calendarKeys.all })
    }
    // keyed on the latest event id so it fires once per new event
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [latest?.id])
}
