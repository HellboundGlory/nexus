import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { createWsClient, type SocketLike } from "@/lib/ws"

class FakeSocket implements SocketLike {
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: ((e?: unknown) => void) | null = null
  closed = false
  close() {
    this.closed = true
    this.onclose?.()
  }
  emit(type: string, data: unknown) {
    this.onmessage?.({ data: JSON.stringify({ type, data }) })
  }
}

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

function harness(cap = 50) {
  const sockets: FakeSocket[] = []
  let t = 1000
  const client = createWsClient({
    url: "ws://x/ws",
    cap,
    now: () => ++t,
    factory: () => {
      const s = new FakeSocket()
      sockets.push(s)
      return s
    },
    backoffBaseMs: 100,
    backoffMaxMs: 800,
  })
  return { client, sockets }
}

describe("ws client", () => {
  it("buffers parsed events and notifies subscribers", () => {
    const { client, sockets } = harness()
    const seen: number[] = []
    client.subscribe((evs) => seen.push(evs.length))
    client.connect()
    sockets[0].onopen?.()
    sockets[0].emit("import.completed", { title: "X" })
    const evs = client.getEvents()
    expect(evs).toHaveLength(1)
    expect(evs[0].type).toBe("import.completed")
    expect(seen.at(-1)).toBe(1)
  })

  it("keeps newest events first and caps the buffer", () => {
    const { client, sockets } = harness(3)
    client.connect()
    sockets[0].onopen?.()
    for (let i = 0; i < 5; i++) sockets[0].emit("indexer.status", { i })
    const evs = client.getEvents()
    expect(evs).toHaveLength(3)
    expect((evs[0].data as { i: number }).i).toBe(4) // newest first
    expect((evs[2].data as { i: number }).i).toBe(2)
  })

  it("ignores malformed messages without throwing", () => {
    const { client, sockets } = harness()
    client.connect()
    sockets[0].onmessage?.({ data: "not json" })
    expect(client.getEvents()).toHaveLength(0)
  })

  it("reconnects with backoff after a close", () => {
    const { client, sockets } = harness()
    client.connect()
    sockets[0].onopen?.()
    sockets[0].onclose?.() // unexpected drop
    expect(sockets).toHaveLength(1)
    vi.advanceTimersByTime(100)
    expect(sockets).toHaveLength(2) // reconnected
  })

  it("does not reconnect after an explicit close()", () => {
    const { client, sockets } = harness()
    client.connect()
    sockets[0].onopen?.()
    client.close()
    vi.advanceTimersByTime(5000)
    expect(sockets).toHaveLength(1)
  })
})
