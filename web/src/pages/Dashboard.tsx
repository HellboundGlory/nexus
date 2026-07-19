import { useQuery } from "@tanstack/react-query"
import { getStatus } from "@/lib/api"
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
    </div>
  )
}
