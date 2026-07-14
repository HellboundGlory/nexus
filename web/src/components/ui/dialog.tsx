import { Dialog as D } from "radix-ui"

export function Dialog({ open, onOpenChange, children, className }: { open: boolean; onOpenChange: (o: boolean) => void; children: React.ReactNode; className?: string }) {
  // width comes from `className` (defaults to w-[32rem]); callers that need a
  // wider surface (e.g. the poster-grid Add dialog) pass their own w-* class.
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/60" />
        <D.Content className={`fixed left-1/2 top-1/2 z-50 max-w-[90vw] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-[var(--color-border)] bg-[var(--color-panel)] p-5 shadow-2xl ${className ?? "w-[32rem]"}`}>
          {children}
        </D.Content>
      </D.Portal>
    </D.Root>
  )
}

export function DialogTitle({ children }: { children: React.ReactNode }) {
  return <D.Title className="mb-3 text-lg font-semibold">{children}</D.Title>
}
