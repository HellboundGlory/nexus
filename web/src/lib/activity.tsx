import { createContext, useContext, useEffect, useState, type ReactNode } from "react"
import { createWsClient, type ActivityEvent } from "@/lib/ws"

const ActivityContext = createContext<ActivityEvent[]>([])

export function ActivityProvider({ children }: { children: ReactNode }) {
  const [events, setEvents] = useState<ActivityEvent[]>([])
  useEffect(() => {
    const client = createWsClient()
    const unsub = client.subscribe(setEvents)
    client.connect()
    return () => {
      unsub()
      client.close()
    }
  }, [])
  return <ActivityContext.Provider value={events}>{children}</ActivityContext.Provider>
}

export function useActivity(): ActivityEvent[] {
  return useContext(ActivityContext)
}
