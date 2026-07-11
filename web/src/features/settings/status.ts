type Tone = "ok" | "warn" | "muted"

export function connectionStatusBadge(row: { status: string }): { tone: Tone; label: string } {
  if (row.status === "ok") return { tone: "ok", label: "OK" }
  if (row.status === "failed") return { tone: "warn", label: "Failed" }
  return { tone: "muted", label: "Unknown" }
}
