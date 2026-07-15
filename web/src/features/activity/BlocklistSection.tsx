// web/src/features/activity/BlocklistSection.tsx
import { ApiError } from "@/lib/api"
import { useToast } from "@/lib/toast"
import { relativeTime } from "@/lib/time"
import { useQualityDefinitions } from "@/features/settings/qualityApi"
import { useBlocklist, useRemoveBlocklist } from "./api"
import { qualityName } from "./resolve"

export function BlocklistSection() {
  const blocklist = useBlocklist()
  const defs = useQualityDefinitions()
  const removeItem = useRemoveBlocklist()
  const { toast } = useToast()

  if (blocklist.isLoading) return <div className="p-6 text-sm text-[var(--color-muted)]">Loading blocklist…</div>
  if (blocklist.isError) return <div className="p-6 text-sm text-[var(--color-warn)]">Failed to load blocklist.</div>

  const rows = blocklist.data ?? []
  if (rows.length === 0) return <div className="p-6 text-sm text-[var(--color-muted)]">No blocklisted releases.</div>

  const onRemove = (id: number) => {
    if (!window.confirm("Remove this release from the blocklist?")) return
    removeItem.mutate(id, {
      onSuccess: () => toast("Removed from blocklist"),
      onError: (e) => toast(e instanceof ApiError ? e.message : "Remove failed", { variant: "error" }),
    })
  }

  return (
    <div className="p-6">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-[var(--color-border)] text-left text-xs uppercase tracking-wide text-[var(--color-muted)]">
            <th className="py-2 pr-4">Release</th>
            <th className="py-2 pr-4">For</th>
            <th className="py-2 pr-4">Quality</th>
            <th className="py-2 pr-4">Reason</th>
            <th className="py-2 pr-4">Date</th>
            <th className="py-2 pr-4 text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((b) => (
            <tr key={b.id} className="border-b border-[var(--color-border)] align-top last:border-b-0">
              <td className="py-2.5 pr-4 font-medium">{b.sourceTitle}</td>
              <td className="py-2.5 pr-4 text-[var(--color-muted)]">{b.title || "—"}</td>
              <td className="py-2.5 pr-4">{qualityName(b.qualityId, defs.data)}</td>
              <td className="py-2.5 pr-4 text-[var(--color-muted)]">{b.reason || "—"}</td>
              <td className="whitespace-nowrap py-2.5 pr-4 text-[var(--color-muted)]">
                {relativeTime(new Date(b.createdAt).getTime())}
              </td>
              <td className="whitespace-nowrap py-2.5 pr-4 text-right">
                <button
                  onClick={() => onRemove(b.id)}
                  className="rounded border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-warn)] hover:border-[var(--color-warn)]"
                >
                  Remove
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
