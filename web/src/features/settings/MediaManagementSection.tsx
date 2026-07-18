import { useState } from "react"
import { Select } from "@/components/ui/select"
import { useToast } from "@/lib/toast"
import { useRootFolders } from "./configApi"
import { useQualityProfiles } from "./qualityApi"
import { useMediaDefaults, useSaveMediaDefaults } from "./mediaDefaultsApi"
import type { MediaDefaults } from "./mediaDefaultsTypes"

function toId(v: string): number | null {
  return v === "" ? null : Number(v)
}
function toStr(v: number | null): string {
  return v == null ? "" : String(v)
}

export function MediaManagementSection() {
  const { toast } = useToast()
  const defaults = useMediaDefaults()
  const roots = useRootFolders()
  const profiles = useQualityProfiles()
  const save = useSaveMediaDefaults()

  const [form, setForm] = useState<MediaDefaults | null>(null)
  const [initialized, setInitialized] = useState(false)
  if (!initialized && defaults.data) {
    setForm(defaults.data)
    setInitialized(true)
  }

  if (defaults.isLoading || !form) return <div className="p-6"><p className="text-sm text-[var(--color-muted)]">Loading…</p></div>
  if (defaults.isError) return <div className="p-6"><p className="text-sm text-[var(--color-warn)]">Failed to load.</p></div>

  const rootOptions = roots.data ?? []
  const profileOptions = profiles.data ?? []

  function setField(kind: "movie" | "tv", field: "rootFolderId" | "qualityProfileId", v: string) {
    setForm((f) => (f ? { ...f, [kind]: { ...f[kind], [field]: toId(v) } } : f))
  }

  const onSave = () => {
    save.mutate(form!, {
      onSuccess: () => toast("Saved"),
      onError: () => toast("Save failed", { variant: "error" }),
    })
  }

  const rows: { kind: "movie" | "tv"; label: string }[] = [
    { kind: "movie", label: "Movie" },
    { kind: "tv", label: "TV" },
  ]

  return (
    <div className="p-6">
      <h2 className="mb-4 text-lg font-semibold">Media Management</h2>
      <p className="mb-4 max-w-2xl text-sm text-[var(--color-muted)]">
        Defaults applied when adding a movie or show. You can still override them per add.
      </p>
      <div className="flex max-w-2xl flex-col gap-5">
        {rows.map((row) => (
          <div key={row.kind} className="flex flex-col gap-2">
            <div className="text-sm font-medium">{row.label}</div>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-xs text-[var(--color-muted)]">Default {row.label} Root Folder</span>
              <Select
                aria-label={`Default ${row.label} Root Folder`}
                value={toStr(form[row.kind].rootFolderId)}
                onChange={(v) => setField(row.kind, "rootFolderId", v)}
              >
                <option value="">None</option>
                {rootOptions.map((rf) => <option key={rf.id} value={rf.id}>{rf.path}</option>)}
              </Select>
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-xs text-[var(--color-muted)]">Default {row.label} Quality Profile</span>
              <Select
                aria-label={`Default ${row.label} Quality Profile`}
                value={toStr(form[row.kind].qualityProfileId)}
                onChange={(v) => setField(row.kind, "qualityProfileId", v)}
              >
                <option value="">None</option>
                {profileOptions.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </Select>
            </label>
          </div>
        ))}
        <div>
          <button
            onClick={onSave}
            disabled={save.isPending}
            className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
          >
            Save
          </button>
        </div>
      </div>
    </div>
  )
}
