import { useState } from "react"
import { useToast } from "@/lib/toast"
import { useNamingConfig, useSaveNaming } from "./configApi"
import type { NamingConfig } from "./configTypes"

const DEFAULT_NAMING: NamingConfig = {
  seriesFolder: "{Series Title}",
  seasonFolder: "Season {season:00}",
  episodeFile: "{Series Title} - S{season:00}E{episode:00} - {Episode Title} [{Quality}]",
  movieFolder: "{Movie Title} ({year})",
  movieFile: "{Movie Title} ({year}) [{Quality}]",
}

const NAMING_TOKENS = [
  "{Series Title}", "{Episode Title}", "{Movie Title}", "{Quality}", "{Release Group}",
  "{season}", "{season:00}", "{episode}", "{episode:00}", "{year}",
]

const FIELDS: { key: keyof NamingConfig; label: string }[] = [
  { key: "seriesFolder", label: "Series Folder" },
  { key: "seasonFolder", label: "Season Folder" },
  { key: "episodeFile", label: "Episode File" },
  { key: "movieFolder", label: "Movie Folder" },
  { key: "movieFile", label: "Movie File" },
]

export function NamingSection() {
  const { toast } = useToast()
  const q = useNamingConfig()
  const save = useSaveNaming()
  const [form, setForm] = useState<NamingConfig | null>(null)
  const [initialized, setInitialized] = useState(false)
  if (!initialized && q.data) {
    setForm(q.data)
    setInitialized(true)
  }

  if (q.isLoading || !form) return <div className="p-6"><p className="text-sm text-[var(--color-muted)]">Loading…</p></div>
  if (q.isError) return <div className="p-6"><p className="text-sm text-[var(--color-warn)]">Failed to load.</p></div>

  const onSave = () => {
    save.mutate(form, {
      onSuccess: (saved) => { setForm(saved); toast("Saved") },
      onError: () => toast("Save failed", { variant: "error" }),
    })
  }

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">Naming</h2>
      <div className="flex max-w-2xl flex-col gap-3">
        {FIELDS.map((f) => (
          <label key={f.key} className="flex flex-col gap-1 text-sm">
            <span>{f.label}</span>
            <input
              value={form[f.key]}
              onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
              className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1.5 font-mono text-xs"
            />
          </label>
        ))}
        <div className="flex gap-2">
          <button
            onClick={onSave}
            disabled={save.isPending}
            className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
          >
            Save
          </button>
          <button
            onClick={() => setForm(DEFAULT_NAMING)}
            className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm"
          >
            Reset to defaults
          </button>
        </div>
      </div>

      <div className="mt-6">
        <h3 className="mb-2 text-sm font-medium">Available tokens</h3>
        <div className="flex flex-wrap gap-2">
          {NAMING_TOKENS.map((t) => (
            <code key={t} className="rounded bg-[var(--color-panel)] px-2 py-0.5 text-xs text-[var(--color-muted)]">{t}</code>
          ))}
        </div>
      </div>
    </div>
  )
}
