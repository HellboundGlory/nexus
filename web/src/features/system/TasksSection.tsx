import { useTasks, useRunTask, useTasksInvalidation, type QueueTask } from "./systemApi"
import { formatDuration, humanizeInterval, humanizeName, relativePast, relativeFuture } from "./format"

export function TasksSection() {
  const q = useTasks()
  const run = useRunTask()
  useTasksInvalidation()

  if (q.isLoading || !q.data) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading…</div>
  const { scheduled, queue } = q.data

  return (
    <div className="space-y-6 p-6">
      <section>
        <h2 className="mb-2 text-sm font-semibold">Scheduled</h2>
        <div className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-panel)]">
          <div className="overflow-x-auto">
            <table className="w-full text-sm" data-testid="scheduled-table">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide text-[var(--color-muted)]">
                  <th className="px-4 py-2.5 font-semibold">Name</th>
                  <th className="px-4 py-2.5 font-semibold">Interval</th>
                  <th className="px-4 py-2.5 font-semibold">Last Execution</th>
                  <th className="px-4 py-2.5 font-semibold">Last Duration</th>
                  <th className="px-4 py-2.5 font-semibold">Next Execution</th>
                  <th className="px-4 py-2.5 text-right font-semibold">Run</th>
                </tr>
              </thead>
              <tbody>
                {scheduled.map((t) => {
                  const next = relativeFuture(t.nextExecution)
                  return (
                    <tr key={t.name} className="border-t border-[var(--color-border)]">
                      <td className="px-4 py-2.5">{humanizeName(t.name)}</td>
                      <td className="px-4 py-2.5 text-[var(--color-muted)]">{humanizeInterval(t.intervalSeconds)}</td>
                      <td className="px-4 py-2.5 text-[var(--color-muted)]">{t.lastExecution ? relativePast(t.lastExecution) : "—"}</td>
                      <td className="px-4 py-2.5 tabular-nums text-[var(--color-muted)]">{t.lastDurationSeconds != null ? formatDuration(t.lastDurationSeconds) : "—"}</td>
                      <td className={`px-4 py-2.5 tabular-nums ${next === "now" ? "text-[var(--color-brand)]" : "text-[var(--color-muted)]"}`}>{next}</td>
                      <td className="px-4 py-2.5 text-right">
                        <button
                          aria-label={`Run ${humanizeName(t.name)} now`}
                          title="Run now"
                          onClick={() => run.mutate(t.name)}
                          className="rounded-md border border-transparent px-2 py-1 text-[var(--color-muted)] hover:border-[var(--color-border)] hover:text-[var(--color-brand)]"
                        >
                          ↻
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <section>
        <div className="mb-2 flex items-center gap-2">
          <h2 className="text-sm font-semibold">Queue</h2>
          <span className="text-xs font-semibold text-[var(--color-ok)]">LIVE</span>
        </div>
        <div className="overflow-hidden rounded-xl border border-[var(--color-border)] bg-[var(--color-panel)]">
          <div className="overflow-x-auto">
            <table className="w-full text-sm" data-testid="queue-table">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide text-[var(--color-muted)]">
                  <th className="px-4 py-2.5 font-semibold">Name</th>
                  <th className="px-4 py-2.5 font-semibold">Queued</th>
                  <th className="px-4 py-2.5 font-semibold">Started</th>
                  <th className="px-4 py-2.5 font-semibold">Ended</th>
                  <th className="px-4 py-2.5 text-right font-semibold">Duration</th>
                </tr>
              </thead>
              <tbody>
                {queue.length === 0 ? (
                  <tr><td colSpan={5} className="px-4 py-8 text-center text-[var(--color-muted)]">No recent tasks.</td></tr>
                ) : queue.map((t) => <QueueRow key={t.id} t={t} />)}
              </tbody>
            </table>
          </div>
        </div>
      </section>
    </div>
  )
}

function QueueRow({ t }: { t: QueueTask }) {
  const running = t.status === "running"
  const failed = t.status === "failed"
  const glyph = running ? "◐" : failed ? "✕" : "✓"
  const glyphColor = running ? "text-[var(--color-brand)]" : failed ? "text-[var(--color-warn)]" : "text-[var(--color-ok)]"
  return (
    <tr className="border-t border-[var(--color-border)]">
      <td className="px-4 py-2.5">
        <span className="flex items-center gap-2">
          <span className={glyphColor} aria-label={t.status}>{glyph}</span>
          {humanizeName(t.name)}
        </span>
      </td>
      <td className="px-4 py-2.5 text-[var(--color-muted)]">{relativePast(t.queuedAt)}</td>
      <td className="px-4 py-2.5 text-[var(--color-muted)]">{t.startedAt ? relativePast(t.startedAt) : "—"}</td>
      <td className="px-4 py-2.5 text-[var(--color-muted)]">{t.endedAt ? relativePast(t.endedAt) : "—"}</td>
      <td className="px-4 py-2.5 text-right tabular-nums">
        {running ? <span className="text-[var(--color-brand)]">Running…</span>
          : <span className={failed ? "text-[var(--color-warn)]" : "text-[var(--color-muted)]"}>{t.durationSeconds != null ? formatDuration(t.durationSeconds) : "—"}</span>}
      </td>
    </tr>
  )
}
