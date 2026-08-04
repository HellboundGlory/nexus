import { useState } from "react"

export type TagOption = { id: number; label: string }

export function TagInput({
  value, options, onChange, onCreate, disabled, "aria-label": ariaLabel = "Tags",
}: {
  value: number[]
  options: TagOption[]
  onChange: (ids: number[]) => void
  onCreate: (label: string) => Promise<number>
  disabled?: boolean
  "aria-label"?: string
}) {
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)

  const selected = value
    .map((id) => options.find((o) => o.id === id))
    .filter((o): o is TagOption => o != null)

  const typed = text.trim()
  const needle = typed.toLowerCase()
  const suggestions =
    needle === ""
      ? []
      : options.filter((o) => !value.includes(o.id) && o.label.toLowerCase().includes(needle))

  const select = (id: number) => {
    if (!value.includes(id)) onChange([...value, id])
    setText("")
  }

  const commit = async () => {
    if (typed === "" || busy) return
    // Enter acts on the typed text: an exact (case-insensitive) label selects
    // that tag, anything else creates. Deliberately NOT the first suggestion —
    // typing "an" with "anime" present must create "an".
    const exact = options.find((o) => o.label.toLowerCase() === needle)
    if (exact) {
      select(exact.id)
      return
    }
    setBusy(true)
    try {
      const id = await onCreate(typed)
      onChange([...value, id])
      setText("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-1.5">
        {selected.map((o) => (
          <span
            key={o.id}
            className="inline-flex items-center gap-1 rounded-full border border-[var(--color-border)] bg-[var(--color-panel-2)] px-2 py-0.5 text-xs"
          >
            {o.label}
            <button
              type="button"
              aria-label={`Remove ${o.label}`}
              disabled={disabled}
              onClick={() => onChange(value.filter((id) => id !== o.id))}
              className="text-[var(--color-muted)] hover:text-[var(--color-fg)]"
            >
              x
            </button>
          </span>
        ))}
      </div>
      <input
        type="text"
        aria-label={ariaLabel}
        value={text}
        disabled={disabled || busy}
        placeholder="Add a tag…"
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault()
            void commit()
          }
        }}
        className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-3 py-1.5 text-sm disabled:opacity-50"
      />
      {suggestions.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {suggestions.map((o) => (
            <li key={o.id}>
              <button
                type="button"
                onClick={() => select(o.id)}
                className="rounded-full border border-[var(--color-border)] px-2 py-0.5 text-xs text-[var(--color-muted)] hover:text-[var(--color-fg)]"
              >
                {o.label}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
