import { useState } from "react"
import { useToast } from "@/lib/toast"
import { useSystemStatus, useAutomationConfig, useSaveAutomationConfig } from "./configApi"
import type { AutomationConfig } from "./configTypes"

const NUM_FIELDS: { key: keyof AutomationConfig; label: string }[] = [
  { key: "missingSearchIntervalHours", label: "Missing search interval (hours)" },
  { key: "missingSearchBatchSize", label: "Missing search batch size" },
  { key: "rssSyncIntervalMinutes", label: "RSS sync interval (minutes)" },
  { key: "upgradeSearchIntervalHours", label: "Upgrade search interval (hours)" },
  { key: "upgradeSearchBatchSize", label: "Upgrade search batch size" },
  { key: "upgradeGrabCooldownHours", label: "Upgrade grab cooldown (hours)" },
]
const BOOL_FIELDS: { key: keyof AutomationConfig; label: string }[] = [
  { key: "rssSyncEnabled", label: "RSS sync enabled" },
  { key: "upgradeSearchEnabled", label: "Upgrade search enabled" },
]

export function GeneralSection() {
  const { toast } = useToast()
  const statusQ = useSystemStatus()
  const cfgQ = useAutomationConfig()
  const save = useSaveAutomationConfig()
  const [form, setForm] = useState<AutomationConfig | null>(null)
  const [initialized, setInitialized] = useState(false)
  if (!initialized && cfgQ.data) {
    setForm(cfgQ.data)
    setInitialized(true)
  }

  const s = statusQ.data

  const onSave = () => {
    if (!form) return
    // Clamp non-positive numbers to keep parity with the server's defaulting.
    const clamped = { ...form }
    for (const f of NUM_FIELDS) {
      if ((clamped[f.key] as number) <= 0) delete (clamped as Record<string, unknown>)[f.key]
    }
    save.mutate(clamped as AutomationConfig, {
      // The PUT echoes the raw payload, but the server substitutes defaults for
      // any omitted non-positive field on read — refetch so the form shows the
      // values the server actually persisted rather than the stale typed ones.
      onSuccess: async () => {
        toast("Saved")
        const fresh = await cfgQ.refetch()
        if (fresh.data) setForm(fresh.data)
      },
      onError: () => toast("Save failed", { variant: "error" }),
    })
  }

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">General</h2>

      <section className="mb-6 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] p-4">
        <h3 className="mb-2 text-sm font-medium">System Info</h3>
        {statusQ.isLoading || !s ? (
          <p className="text-sm text-[var(--color-muted)]">Loading…</p>
        ) : (
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
            <dt className="text-[var(--color-muted)]">Version</dt><dd>{s.version}</dd>
            <dt className="text-[var(--color-muted)]">App</dt><dd>{s.appName}</dd>
            <dt className="text-[var(--color-muted)]">Healthy</dt><dd>{s.healthy ? "Yes" : "No"}</dd>
            <dt className="text-[var(--color-muted)]">Active tasks</dt><dd>{s.taskCount}</dd>
          </dl>
        )}
      </section>

      <section className="rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] p-4">
        <h3 className="mb-1 text-sm font-medium">Task Scheduling</h3>
        <p className="mb-3 text-xs text-[var(--color-warn)]">
          Interval and enabled changes take effect on the next Nexus restart.
        </p>
        {cfgQ.isLoading || !form ? (
          <p className="text-sm text-[var(--color-muted)]">Loading…</p>
        ) : cfgQ.isError ? (
          <p className="text-sm text-[var(--color-warn)]">Failed to load.</p>
        ) : (
          <div className="flex max-w-md flex-col gap-3">
            {BOOL_FIELDS.map((f) => (
              <label key={f.key} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={form[f.key] as boolean}
                  onChange={(e) => setForm({ ...form, [f.key]: e.target.checked })}
                />
                <span>{f.label}</span>
              </label>
            ))}
            {NUM_FIELDS.map((f) => (
              <label key={f.key} className="flex flex-col gap-1 text-sm">
                <span>{f.label}</span>
                <input
                  type="number"
                  aria-label={f.label}
                  value={form[f.key] as number}
                  onChange={(e) => setForm({ ...form, [f.key]: Number(e.target.value) })}
                  className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1.5"
                />
              </label>
            ))}
            <div>
              <button
                onClick={onSave}
                disabled={save.isPending}
                className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
              >
                Save
              </button>
            </div>
          </div>
        )}
      </section>
    </div>
  )
}
