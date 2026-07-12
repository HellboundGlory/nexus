import { useState } from "react"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { useRootFolders, useAddRootFolder, useDeleteRootFolder } from "./configApi"
import type { RootFolder } from "./configTypes"

export function RootFoldersSection() {
  const { toast } = useToast()
  const q = useRootFolders()
  const add = useAddRootFolder()
  const del = useDeleteRootFolder()
  const [path, setPath] = useState("")
  const rows = q.data ?? []

  const onAdd = () => {
    const p = path.trim()
    if (p === "") return
    add.mutate(p, {
      onSuccess: () => { toast("Added"); setPath("") },
      onError: (e) => toast(e instanceof ApiError ? e.message : "Add failed", { variant: "error" }),
    })
  }

  const onDelete = (rf: RootFolder) => {
    if (!confirm(`Delete ${rf.path}?`)) return
    del.mutate(rf.id, {
      onSuccess: () => toast("Deleted"),
      onError: (e) =>
        toast(
          e instanceof ApiError && e.status === 409
            ? "Root folder is in use by a movie or series"
            : "Delete failed",
          { variant: "error" },
        ),
    })
  }

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">Root Folders</h2>

      <div className="mb-4 flex gap-2">
        <input
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="/path/to/library"
          className="flex-1 rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1.5 text-sm"
        />
        <button
          onClick={onAdd}
          disabled={path.trim() === "" || add.isPending}
          className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
        >
          Add
        </button>
      </div>

      {q.isLoading ? (
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      ) : q.isError ? (
        <p className="text-sm text-[var(--color-warn)]">Failed to load.</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-[var(--color-muted)]">No root folders — add one above.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((rf) => (
            <li
              key={rf.id}
              className="flex items-center gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-panel)] px-4 py-3"
            >
              <span className="min-w-0 flex-1 truncate text-sm">{rf.path}</span>
              <button
                onClick={() => onDelete(rf)}
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
