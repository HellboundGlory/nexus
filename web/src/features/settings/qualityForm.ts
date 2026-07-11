import type {
  ProfileFormState, ProfilePayload, QualityDefinition, QualityProfile, QualityProfileItem,
} from "./qualityTypes"

export function buildProfileItems(
  allowed: Record<number, boolean>,
  defs: QualityDefinition[],
): QualityProfileItem[] {
  return defs.map((d) => ({ qualityId: d.id, allowed: !!allowed[d.id] }))
}

export function buildProfilePayload(form: ProfileFormState, defs: QualityDefinition[]): ProfilePayload {
  return {
    name: form.name.trim(),
    cutoffQualityId: form.cutoffQualityId,
    upgradeAllowed: form.upgradeAllowed,
    items: buildProfileItems(form.allowed, defs),
  }
}

// Keep the current cutoff if it is still allowed; otherwise the highest-rank
// allowed quality; otherwise 0.
export function resolveCutoff(
  allowed: Record<number, boolean>,
  current: number,
  defs: QualityDefinition[],
): number {
  if (allowed[current]) return current
  const allowedDefs = defs.filter((d) => allowed[d.id])
  if (allowedDefs.length === 0) return 0
  return allowedDefs.reduce((hi, d) => (d.rank > hi.rank ? d : hi), allowedDefs[0]).id
}

export function isProfileFormValid(form: ProfileFormState): boolean {
  if (form.name.trim() === "") return false
  const anyAllowed = Object.values(form.allowed).some(Boolean)
  if (!anyAllowed) return false
  return !!form.allowed[form.cutoffQualityId]
}

export function formStateFromProfile(p: QualityProfile, defs: QualityDefinition[]): ProfileFormState {
  const allowed: Record<number, boolean> = {}
  for (const d of defs) allowed[d.id] = false
  for (const it of p.items) allowed[it.qualityId] = it.allowed
  return {
    name: p.name,
    allowed,
    cutoffQualityId: p.cutoffQualityId,
    upgradeAllowed: p.upgradeAllowed,
  }
}

// A sensible baseline: allow every 1080p-and-below WEBDL/Bluray/HDTV/SDTV
// quality, cutoff at the highest allowed, upgrades on. Falls back gracefully
// for an unexpected ladder.
export function defaultNewProfile(defs: QualityDefinition[]): ProfileFormState {
  const allowed: Record<number, boolean> = {}
  for (const d of defs) {
    allowed[d.id] = d.resolution === "480p" || d.resolution === "720p" || d.resolution === "1080p"
  }
  if (!Object.values(allowed).some(Boolean) && defs.length > 0) {
    // Unexpected ladder — allow the single highest so the form is valid.
    const top = defs.reduce((hi, d) => (d.rank > hi.rank ? d : hi), defs[0])
    allowed[top.id] = true
  }
  const cutoff = resolveCutoff(allowed, -1, defs)
  // Empty name so the Add dialog opens with the Save button disabled until
  // the user types a profile name; the quality selection/cutoff are valid.
  return { name: "", allowed, cutoffQualityId: cutoff, upgradeAllowed: true }
}
