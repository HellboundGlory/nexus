import { useState } from "react"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { ProfileDialog } from "./ProfileDialog"
import { useQualityProfiles, useDeleteProfile } from "./qualityApi"
import type { QualityProfile } from "./qualityTypes"

export function QualityProfilesSection() {
  const { toast } = useToast()
  const q = useQualityProfiles()
  const del = useDeleteProfile()
  const [addOpen, setAddOpen] = useState(false)
  const [editing, setEditing] = useState<QualityProfile | null>(null)
  const rows = q.data ?? []

  const onDelete = (p: QualityProfile) => {
    if (!confirm(`Delete ${p.name}?`)) return
    del.mutate(p.id, {
      onSuccess: () => toast("Deleted"),
      onError: (e) =>
        toast(
          e instanceof ApiError && e.status === 409 ? "Profile is in use" : "Delete failed",
          { variant: "error" },
        ),
    })
  }

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold">Quality Profiles</h2>
        <button
          onClick={() => setAddOpen(true)}
          className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white"
        >
          + Add
        </button>
      </div>

      {q.isLoading ? (
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      ) : q.isError ? (
        <p className="text-sm text-[var(--color-warn)]">Failed to load.</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-[var(--color-muted)]">No quality profiles — click Add to create one.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((p) => {
            const allowedCount = p.items.filter((it) => it.allowed).length
            return (
              <li
                key={p.id}
                className="flex items-center gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="font-medium">{p.name}</div>
                  <div className="text-xs text-[var(--color-muted)]">
                    {allowedCount} qualities · cutoff #{p.cutoffQualityId} · upgrades {p.upgradeAllowed ? "on" : "off"}
                  </div>
                </div>
                <button onClick={() => setEditing(p)} className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm">
                  Edit
                </button>
                <button
                  onClick={() => onDelete(p)}
                  className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
                >
                  Delete
                </button>
              </li>
            )
          })}
        </ul>
      )}

      {addOpen && <ProfileDialog open={addOpen} onOpenChange={setAddOpen} />}
      {editing && (
        <ProfileDialog existing={editing} open={editing != null} onOpenChange={(o) => { if (!o) setEditing(null) }} />
      )}
    </div>
  )
}
