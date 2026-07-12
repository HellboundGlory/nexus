import { useMemo, useState } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { useQualityDefinitions, useSaveProfile } from "./qualityApi"
import {
  buildProfilePayload, defaultNewProfile, formStateFromProfile, isProfileFormValid, resolveCutoff,
} from "./qualityForm"
import type { ProfileFormState, QualityProfile } from "./qualityTypes"

export function ProfileDialog({
  existing, open, onOpenChange,
}: {
  existing?: QualityProfile
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const { toast } = useToast()
  const defsQ = useQualityDefinitions()
  const save = useSaveProfile()
  const defs = useMemo(() => defsQ.data ?? [], [defsQ.data])

  const [form, setForm] = useState<ProfileFormState | null>(null)
  const [initialized, setInitialized] = useState(false)
  if (!initialized && defs.length > 0) {
    setForm(existing ? formStateFromProfile(existing, defs) : defaultNewProfile(defs))
    setInitialized(true)
  }

  if (!form) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogTitle>{existing ? "Edit Quality Profile" : "Add Quality Profile"}</DialogTitle>
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      </Dialog>
    )
  }

  const toggle = (id: number, on: boolean) => {
    const allowed = { ...form.allowed, [id]: on }
    setForm({ ...form, allowed, cutoffQualityId: resolveCutoff(allowed, form.cutoffQualityId, defs) })
  }

  const valid = isProfileFormValid(form)
  const allowedDefs = defs.filter((d) => form.allowed[d.id])

  const onSave = () => {
    save.mutate(
      { payload: buildProfilePayload(form, defs), id: existing?.id },
      {
        onSuccess: () => { toast("Saved"); onOpenChange(false) },
        onError: (e) => toast(e instanceof ApiError ? e.message : "Save failed", { variant: "error" }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>{existing ? "Edit Quality Profile" : "Add Quality Profile"}</DialogTitle>
      <div className="flex flex-col gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span>Name</span>
          <input
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </label>

        <fieldset className="flex flex-col gap-1">
          <legend className="mb-1 text-sm font-medium">Qualities</legend>
          {defs.map((d) => (
            <label key={d.id} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                aria-label={d.name}
                checked={!!form.allowed[d.id]}
                onChange={(e) => toggle(d.id, e.target.checked)}
              />
              <span>{d.name}</span>
            </label>
          ))}
        </fieldset>

        <label className="flex flex-col gap-1 text-sm">
          <span>Cutoff</span>
          <select
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.cutoffQualityId}
            onChange={(e) => setForm({ ...form, cutoffQualityId: Number(e.target.value) })}
          >
            {allowedDefs.map((d) => (
              <option key={d.id} value={d.id}>{d.name}</option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={form.upgradeAllowed}
            onChange={(e) => setForm({ ...form, upgradeAllowed: e.target.checked })}
          />
          <span>Upgrades allowed</span>
        </label>

        <div className="mt-2 flex justify-end gap-2">
          <button className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm" onClick={() => onOpenChange(false)}>
            Cancel
          </button>
          <button
            disabled={!valid || save.isPending}
            onClick={onSave}
            className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
          >
            Save
          </button>
        </div>
      </div>
    </Dialog>
  )
}
