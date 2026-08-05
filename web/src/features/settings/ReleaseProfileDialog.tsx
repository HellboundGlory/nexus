import { useState } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"
import { useToast } from "@/lib/toast"
import { ApiError } from "@/lib/api"
import { useSaveReleaseProfile } from "./releaseProfileApi"
import { useTags } from "./tagApi"
import type { ReleaseProfile, ReleaseProfilePayload } from "./releaseProfileTypes"

type FormState = {
  name: string
  requiredMode: "any" | "all"
  requiredAny: string
  requiredAll: string
  ignored: string
  preferred: string
  tagIds: number[]
}

function splitTerms(s: string): string[] {
  return s.split(",").map((t) => t.trim()).filter((t) => t !== "")
}

function formFromProfile(p: ReleaseProfile): FormState {
  return {
    name: p.name,
    requiredMode: p.requiredMode,
    requiredAny: p.requiredAny.join(", "),
    requiredAll: p.requiredAll.join(", "),
    ignored: p.ignored.join(", "),
    preferred: p.preferred.join(", "),
    tagIds: p.tagIds,
  }
}

function emptyForm(): FormState {
  return { name: "", requiredMode: "any", requiredAny: "", requiredAll: "", ignored: "", preferred: "", tagIds: [] }
}

function payloadFromForm(f: FormState): ReleaseProfilePayload {
  return {
    name: f.name.trim(),
    requiredMode: f.requiredMode,
    requiredAny: splitTerms(f.requiredAny),
    requiredAll: splitTerms(f.requiredAll),
    ignored: splitTerms(f.ignored),
    preferred: splitTerms(f.preferred),
    tagIds: f.tagIds,
  }
}

export function ReleaseProfileDialog({
  existing, open, onOpenChange,
}: {
  existing?: ReleaseProfile
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const { toast } = useToast()
  const save = useSaveReleaseProfile()
  const tagsQ = useTags()
  const tags = tagsQ.data ?? []
  const [form, setForm] = useState<FormState>(existing ? formFromProfile(existing) : emptyForm())

  const hasTerms =
    splitTerms(form.requiredAny).length > 0 ||
    splitTerms(form.requiredAll).length > 0 ||
    splitTerms(form.ignored).length > 0 ||
    splitTerms(form.preferred).length > 0
  const valid = form.name.trim() !== "" && hasTerms

  const toggleTag = (id: number) => {
    const tagIds = form.tagIds.includes(id) ? form.tagIds.filter((t) => t !== id) : [...form.tagIds, id]
    setForm({ ...form, tagIds })
  }

  const onSave = () => {
    save.mutate(
      { payload: payloadFromForm(form), id: existing?.id },
      {
        onSuccess: () => { toast("Saved"); onOpenChange(false) },
        onError: (e) => toast(e instanceof ApiError ? e.message : "Save failed", { variant: "error" }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>{existing ? "Edit Release Profile" : "Add Release Profile"}</DialogTitle>
      <div className="flex flex-col gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span>Name</span>
          <input
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span>Required mode</span>
          <select
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.requiredMode}
            onChange={(e) => setForm({ ...form, requiredMode: e.target.value as "any" | "all" })}
          >
            <option value="any">Any</option>
            <option value="all">All</option>
          </select>
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span>Required (any) — comma separated</span>
          <input
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.requiredAny}
            onChange={(e) => setForm({ ...form, requiredAny: e.target.value })}
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span>Required (all) — comma separated</span>
          <input
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.requiredAll}
            onChange={(e) => setForm({ ...form, requiredAll: e.target.value })}
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span>Ignored — comma separated</span>
          <input
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.ignored}
            onChange={(e) => setForm({ ...form, ignored: e.target.value })}
          />
        </label>

        <label className="flex flex-col gap-1 text-sm">
          <span>Preferred — comma separated</span>
          <input
            className="rounded-md border border-[var(--color-border)] bg-transparent px-2 py-1"
            value={form.preferred}
            onChange={(e) => setForm({ ...form, preferred: e.target.value })}
          />
        </label>

        <fieldset className="flex flex-col gap-1">
          <legend className="mb-1 text-sm font-medium">Tags</legend>
          {tags.length === 0 ? (
            <p className="text-xs text-[var(--color-muted)]">No tags yet.</p>
          ) : (
            tags.map((t) => (
              <label key={t.id} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  aria-label={t.label}
                  checked={form.tagIds.includes(t.id)}
                  onChange={() => toggleTag(t.id)}
                />
                <span>{t.label}</span>
              </label>
            ))
          )}
        </fieldset>

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