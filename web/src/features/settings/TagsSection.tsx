import { useState } from "react"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { useTags, useCreateTag, useRenameTag, useDeleteTag } from "./tagApi"
import type { Tag } from "./tagTypes"

// Plain formatter, not a hook — it is called inside .map(), so a `use` prefix
// would trip the rules-of-hooks lint rule.
function countLabel(tag: Tag): string {
  return `${tag.seriesCount} series, ${tag.movieCount} ${tag.movieCount === 1 ? "movie" : "movies"}`
}

export function TagsSection() {
  const { toast } = useToast()
  const q = useTags()
  const create = useCreateTag()
  const rename = useRenameTag()
  const del = useDeleteTag()
  const [label, setLabel] = useState("")
  const [editing, setEditing] = useState<{ id: number; label: string } | null>(null)
  const rows = (q.data ?? []) as Tag[]

  const onAdd = () => {
    const l = label.trim()
    if (l === "") return
    create.mutate(l, {
      onSuccess: () => { setLabel(""); toast("Tag created") },
      onError: (e) =>
        toast(
          e instanceof ApiError && e.status === 409 ? "A tag with that label already exists" : "Create failed",
          { variant: "error" },
        ),
    })
  }

  const onRename = () => {
    if (!editing) return
    const l = editing.label.trim()
    if (l === "") return
    rename.mutate({ id: editing.id, label: l }, {
      onSuccess: () => { setEditing(null); toast("Tag renamed") },
      onError: (e) =>
        toast(
          e instanceof ApiError && e.status === 409 ? "A tag with that label already exists" : "Rename failed",
          { variant: "error" },
        ),
    })
  }

  const onDelete = (t: Tag) => {
    del.mutate(t.id, {
      onSuccess: () => toast("Deleted"),
      // The server's 409 message already names the counts, so show it verbatim.
      onError: (e) =>
        toast(e instanceof ApiError && e.status === 409 ? e.message : "Delete failed", { variant: "error" }),
    })
  }

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">Tags</h2>

      <div className="mb-4 flex items-center gap-2">
        <input
          type="text"
          aria-label="New tag label"
          value={label}
          placeholder="New tag…"
          onChange={(e) => setLabel(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); onAdd() } }}
          className="w-64 rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-3 py-1.5 text-sm"
        />
        <button
          onClick={onAdd}
          className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white"
        >
          Add
        </button>
      </div>

      {q.isLoading ? (
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      ) : q.isError ? (
        <p className="text-sm text-[var(--color-warn)]">Failed to load.</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-[var(--color-muted)]">No tags yet — add one above.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((t) => (
            <li
              key={t.id}
              className="flex items-center gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] px-4 py-3"
            >
              <div className="min-w-0 flex-1">
                {editing?.id === t.id ? (
                  <input
                    type="text"
                    aria-label={`Rename ${t.label}`}
                    value={editing.label}
                    autoFocus
                    onChange={(e) => setEditing({ id: t.id, label: e.target.value })}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") { e.preventDefault(); onRename() }
                      if (e.key === "Escape") setEditing(null)
                    }}
                    onBlur={onRename}
                    className="w-64 rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-2 py-1 text-sm"
                  />
                ) : (
                  <div className="font-medium">{t.label}</div>
                )}
                <div className="text-xs text-[var(--color-muted)]">{countLabel(t)}</div>
              </div>
              <button
                onClick={() => setEditing({ id: t.id, label: t.label })}
                className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
              >
                Rename
              </button>
              <button
                onClick={() => onDelete(t)}
                className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
              >
                Delete
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
