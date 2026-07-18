import { useEffect, useState } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"

export function DeleteConfirmDialog({
  open,
  onOpenChange,
  title,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  onConfirm: (deleteFiles: boolean) => void
}) {
  const [deleteFiles, setDeleteFiles] = useState(false)
  useEffect(() => {
    if (open) setDeleteFiles(false)
  }, [open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>Delete {title}?</DialogTitle>
      <label className="mt-2 flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={deleteFiles}
          onChange={(e) => setDeleteFiles(e.target.checked)}
        />
        Delete files from disk
      </label>
      <p className="mt-1 text-xs text-[var(--color-muted)]">
        Also remove the folder and its files from disk.
      </p>
      <div className="mt-4 flex justify-end gap-2">
        <button
          onClick={() => onOpenChange(false)}
          className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
        >
          Cancel
        </button>
        <button
          onClick={() => onConfirm(deleteFiles)}
          className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
        >
          Delete
        </button>
      </div>
    </Dialog>
  )
}
