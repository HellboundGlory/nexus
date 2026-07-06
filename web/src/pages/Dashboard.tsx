import { useQuery } from "@tanstack/react-query"
import { getStatus } from "@/lib/api"
import { useActivity } from "@/lib/activity"
import { relativeTime } from "@/lib/time"
import { Card } from "@/components/ui/card"

function StatCard({ label, value, ok }: { label: string; value: string; ok?: boolean }) {
  return (
    <Card className="border-[var(--color-border)] bg-[var(--color-panel)] p-4">
      <div className="text-xs uppercase tracking-wide text-[var(--color-muted)]">{label}</div>
      <div className={`mt-2 text-2xl font-bold ${ok ? "text-[var(--color-ok)]" : ""}`}>{value}</div>
    </Card>
  )
}

export function Dashboard() {
  const status = useQuery({ queryKey: ["system-status"], queryFn: getStatus })
  const events = useActivity()

  return (
    <div className="p-6">
      <div className="mb-6 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard label="Version" value={status.data?.version ?? (status.isError ? "—" : "…")} />
        <StatCard
          label="Health"
          value={status.isError ? "Unknown" : status.data?.healthy ? "Healthy" : status.data ? "Unhealthy" : "…"}
          ok={status.data?.healthy}
        />
        <StatCard label="Active Tasks" value={status.data ? String(status.data.taskCount) : status.isError ? "—" : "…"} />
      </div>

      <div className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-panel)]">
        <div className="flex items-center justify-between border-b border-[var(--color-border)] px-4 py-3">
          <span className="text-sm font-semibold">Activity</span>
          <span className="text-xs font-semibold text-[var(--color-ok)]">LIVE</span>
        </div>
        {events.length === 0 ? (
          <div className="px-4 py-8 text-center text-sm text-[var(--color-muted)]">No activity yet.</div>
        ) : (
          <ul>
            {events.map((e) => (
              <li
                key={e.id}
                className="flex items-center gap-3 border-b border-[var(--color-border)] px-4 py-2.5 text-sm last:border-b-0"
              >
                <span className="rounded-full border border-[var(--color-border)] bg-[var(--color-panel-2)] px-2 py-0.5 text-xs text-[var(--color-muted)]">
                  {e.type}
                </span>
                <span className="min-w-0 flex-1 truncate text-[var(--color-muted)]">{describe(e.data)}</span>
                <span className="whitespace-nowrap text-xs text-[var(--color-muted)]">{relativeTime(e.receivedAt)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function describe(data: unknown): string {
  if (data && typeof data === "object") {
    const o = data as Record<string, unknown>
    const title = o.title ?? o.name ?? o.message
    if (typeof title === "string") return title
  }
  return ""
}
