import { useState, useCallback } from "react"

export const MIN_SCALE = 110
export const MAX_SCALE = 230
export const DEFAULT_SCALE = 120
export const SCALE_KEY = "nexus.grid.scale"

export function clampScale(n: number): number {
  if (!Number.isFinite(n)) return DEFAULT_SCALE
  return Math.min(MAX_SCALE, Math.max(MIN_SCALE, Math.round(n)))
}

export function readScale(): number {
  try {
    const raw = localStorage.getItem(SCALE_KEY)
    if (raw == null || raw.trim() === "") return DEFAULT_SCALE
    const n = Number(raw)
    if (!Number.isFinite(n)) return DEFAULT_SCALE
    return clampScale(n)
  } catch {
    return DEFAULT_SCALE
  }
}

export function useGridScale(): [number, (n: number) => void] {
  const [scale, setScale] = useState<number>(() => readScale())
  const set = useCallback((n: number) => {
    const c = clampScale(n)
    setScale(c)
    try {
      localStorage.setItem(SCALE_KEY, String(c))
    } catch {
      /* storage unavailable — keep in-memory only */
    }
  }, [])
  return [scale, set]
}
