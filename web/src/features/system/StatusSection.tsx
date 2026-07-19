import { useSystemStatus } from "@/features/settings/configApi"

export function StatusSection() {
  const statusQ = useSystemStatus()
  const s = statusQ.data
  return (
    <div className="p-6">
      <section className="rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] p-4">
        <h3 className="mb-2 text-sm font-medium">System Info</h3>
        {statusQ.isLoading || !s ? (
          <p className="text-sm text-[var(--color-muted)]">Loading…</p>
        ) : (
          <dl className="grid max-w-md grid-cols-2 gap-x-4 gap-y-1 text-sm">
            <dt className="text-[var(--color-muted)]">Version</dt><dd>{s.version}</dd>
            <dt className="text-[var(--color-muted)]">App</dt><dd>{s.appName}</dd>
            <dt className="text-[var(--color-muted)]">Healthy</dt><dd>{s.healthy ? "Yes" : "No"}</dd>
            <dt className="text-[var(--color-muted)]">Active tasks</dt><dd>{s.taskCount}</dd>
          </dl>
        )}
      </section>
    </div>
  )
}
