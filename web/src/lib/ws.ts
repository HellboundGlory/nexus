export type ActivityEvent = { id: string; type: string; data: unknown; receivedAt: number }

export interface SocketLike {
  close(): void
  onopen: (() => void) | null
  onmessage: ((ev: { data: string }) => void) | null
  onclose: (() => void) | null
  onerror: ((e?: unknown) => void) | null
}

export interface WsClientOptions {
  url?: string
  cap?: number
  factory?: (url: string) => SocketLike
  now?: () => number
  schedule?: (fn: () => void, ms: number) => void
  backoffBaseMs?: number
  backoffMaxMs?: number
}

export interface WsClient {
  connect(): void
  close(): void
  getEvents(): ActivityEvent[]
  subscribe(fn: (events: ActivityEvent[]) => void): () => void
}

function defaultUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${window.location.host}/api/v1/ws`
}

export function createWsClient(opts: WsClientOptions = {}): WsClient {
  const url = opts.url ?? defaultUrl()
  const cap = opts.cap ?? 50
  const now = opts.now ?? (() => Date.now())
  const schedule = opts.schedule ?? ((fn, ms) => setTimeout(fn, ms))
  const factory = opts.factory ?? ((u) => new WebSocket(u) as unknown as SocketLike)
  const base = opts.backoffBaseMs ?? 500
  const max = opts.backoffMaxMs ?? 15000

  let sock: SocketLike | null = null
  let stopped = false
  let attempt = 0
  let seq = 0
  let events: ActivityEvent[] = []
  const listeners = new Set<(e: ActivityEvent[]) => void>()

  const notify = () => listeners.forEach((fn) => fn(events))

  const open = () => {
    if (stopped) return
    const s = factory(url)
    sock = s
    s.onopen = () => {
      attempt = 0
    }
    s.onmessage = (ev) => {
      let parsed: { type?: unknown; data?: unknown }
      try {
        parsed = JSON.parse(ev.data)
      } catch {
        return
      }
      if (typeof parsed.type !== "string") return
      const item: ActivityEvent = {
        id: `${now()}-${seq++}`,
        type: parsed.type,
        data: parsed.data,
        receivedAt: now(),
      }
      events = [item, ...events].slice(0, cap)
      notify()
    }
    s.onclose = () => {
      sock = null
      if (stopped) return
      const delay = Math.min(max, base * 2 ** attempt)
      attempt++
      schedule(open, delay)
    }
    s.onerror = () => s.close()
  }

  return {
    connect() {
      stopped = false
      if (!sock) open()
    },
    close() {
      stopped = true
      sock?.close()
      sock = null
    },
    getEvents: () => events,
    subscribe(fn) {
      listeners.add(fn)
      fn(events)
      return () => listeners.delete(fn)
    },
  }
}
