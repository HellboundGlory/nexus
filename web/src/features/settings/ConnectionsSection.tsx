import { useState } from "react"
import { useToast } from "@/lib/toast"
import { StatusBadge } from "@/features/library/StatusBadge"
import { ConnectionDialog } from "./ConnectionDialog"
import { useConnections, useDeleteConnection } from "./api"
import { connectionStatusBadge } from "./status"
import type { ConnectionKind, ConnectionRow } from "./types"

const LABELS: Record<ConnectionKind, { singular: string; plural: string }> = {
  indexer: { singular: "Indexer", plural: "Indexers" },
  downloadclient: { singular: "Download Client", plural: "Download Clients" },
}

export function ConnectionsSection({ kind }: { kind: ConnectionKind }) {
  const { toast } = useToast()
  const q = useConnections(kind)
  const del = useDeleteConnection(kind)
  const [addOpen, setAddOpen] = useState(false)
  const [editing, setEditing] = useState<ConnectionRow | null>(null)
  const rows = q.data ?? []
  const labels = LABELS[kind]

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold">{labels.plural}</h2>
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
        <p className="text-sm text-[var(--color-muted)]">No {labels.plural.toLowerCase()} configured — click Add to create one.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((row) => {
            const badge = connectionStatusBadge(row)
            return (
              <li
                key={row.id}
                className="flex items-center gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{row.name}</span>
                    <span className="text-xs text-[var(--color-muted)]">{row.implementation}</span>
                    {!row.enabled && <span className="text-xs text-[var(--color-muted)]">(disabled)</span>}
                  </div>
                  <div className="text-xs text-[var(--color-muted)]">priority {row.priority}</div>
                </div>
                <span title={row.failMessage || undefined}>
                  <StatusBadge tone={badge.tone} label={badge.label} />
                </span>
                <button
                  onClick={() => setEditing(row)}
                  className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
                >
                  Edit
                </button>
                <button
                  onClick={() => {
                    if (confirm(`Delete ${row.name}?`)) {
                      del.mutate(row.id, { onSuccess: () => toast("Deleted") })
                    }
                  }}
                  className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
                >
                  Delete
                </button>
              </li>
            )
          })}
        </ul>
      )}

      {addOpen && <ConnectionDialog kind={kind} open={addOpen} onOpenChange={setAddOpen} />}
      {editing && (
        <ConnectionDialog
          kind={kind}
          existing={editing}
          open={editing != null}
          onOpenChange={(o) => { if (!o) setEditing(null) }}
        />
      )}
    </div>
  )
}
