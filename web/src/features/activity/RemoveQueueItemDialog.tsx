import { useEffect, useState } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"

export function RemoveQueueItemDialog({
  open, onOpenChange, title, onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  onConfirm: (opts: { removeFromClient: boolean; blocklist: boolean }) => void
}) {
  const [removeFromClient, setRemoveFromClient] = useState(true)
  const [blocklist, setBlocklist] = useState(false)

  // Reset on every open so a previous removal's choices never carry over.
  useEffect(() => {
    if (open) {
      setRemoveFromClient(true)
      setBlocklist(false)
    }
  }, [open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>Remove from queue?</DialogTitle>
      <p className="text-sm text-[var(--color-muted)]">{title}</p>

      <label className="mt-3 flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={removeFromClient}
          onChange={(e) => setRemoveFromClient(e.target.checked)}
        />
        Remove from download client
      </label>
      <p className="mt-1 text-xs text-[var(--color-muted)]">
        Also cancel the download and delete its files.
      </p>

      <label className="mt-3 flex items-center gap-2 text-sm">
        <input type="checkbox" checked={blocklist} onChange={(e) => setBlocklist(e.target.checked)} />
        Blocklist this release
      </label>
      <p className="mt-1 text-xs text-[var(--color-muted)]">
        Stop this release being grabbed again. Without this, automation may re-grab the same file.
      </p>

      <div className="mt-4 flex justify-end gap-2">
        <button
          onClick={() => onOpenChange(false)}
          className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
        >
          Cancel
        </button>
        <button
          onClick={() => onConfirm({ removeFromClient, blocklist })}
          className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
        >
          Remove
        </button>
      </div>
    </Dialog>
  )
}
