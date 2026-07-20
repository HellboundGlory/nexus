import { Dialog, DialogTitle } from "@/components/ui/dialog"

export function ClearConfirmDialog({
  open, onOpenChange, title, body, warning, confirmLabel, onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  body: string
  /** When set, the dialog stays open in a warning state explaining why the
   *  first attempt was refused; confirming then retries with force. */
  warning?: string | null
  confirmLabel?: string
  onConfirm: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>{title}</DialogTitle>
      <p className="text-sm text-[var(--color-muted)]">{body}</p>
      {warning ? (
        <p className="mt-2 text-sm text-[var(--color-warn)]">{warning}</p>
      ) : null}
      <div className="mt-4 flex justify-end gap-2">
        <button
          onClick={() => onOpenChange(false)}
          className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
        >
          Cancel
        </button>
        <button
          onClick={onConfirm}
          className="rounded-md border border-[var(--color-warn)] px-3 py-1.5 text-sm text-[var(--color-warn)]"
        >
          {confirmLabel ?? "Clear"}
        </button>
      </div>
    </Dialog>
  )
}
