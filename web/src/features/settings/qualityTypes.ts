export type QualityDefinition = {
  id: number
  name: string
  source: number
  resolution: number
  rank: number
}

export type QualityProfileItem = { qualityId: number; allowed: boolean }

export type QualityProfile = {
  id: number
  name: string
  cutoffQualityId: number
  upgradeAllowed: boolean
  items: QualityProfileItem[]
  createdAt: string
}

export type ProfilePayload = {
  name: string
  cutoffQualityId: number
  upgradeAllowed: boolean
  items: QualityProfileItem[]
}

export type ProfileFormState = {
  name: string
  allowed: Record<number, boolean>
  cutoffQualityId: number
  upgradeAllowed: boolean
}
