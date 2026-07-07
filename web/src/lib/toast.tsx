import * as React from "react"

type Toast = { id: number; msg: string; variant: "ok" | "error" }
type ToastCtx = { toast: (msg: string, opts?: { variant?: "ok" | "error" }) => void }

const Ctx = React.createContext<ToastCtx | null>(null)

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = React.useState<Toast[]>([])
  const nextId = React.useRef(1)

  const toast = React.useCallback((msg: string, opts?: { variant?: "ok" | "error" }) => {
    const id = nextId.current++
    setToasts((t) => [...t, { id, msg, variant: opts?.variant ?? "ok" }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4000)
  }, [])

  return (
    <Ctx.Provider value={{ toast }}>
      {children}
      <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            role="status"
            className={`rounded-md border px-4 py-2 text-sm shadow-lg ${
              t.variant === "error"
                ? "border-[var(--color-warn)] bg-[var(--color-panel-2)] text-[var(--color-warn)]"
                : "border-[var(--color-ok)] bg-[var(--color-panel-2)] text-[var(--color-fg)]"
            }`}
          >
            {t.msg}
          </div>
        ))}
      </div>
    </Ctx.Provider>
  )
}

export function useToast(): ToastCtx {
  const ctx = React.useContext(Ctx)
  if (!ctx) throw new Error("useToast must be used within ToastProvider")
  return ctx
}
