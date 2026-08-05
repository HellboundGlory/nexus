export type ReleaseProfile = {
  id: number
  name: string
  requiredMode: "any" | "all"
  requiredAny: string[]
  requiredAll: string[]
  ignored: string[]
  preferred: string[]
  tagIds: number[]
  createdAt: string
}

export type ReleaseProfilePayload = {
  name: string
  requiredMode: "any" | "all"
  requiredAny: string[]
  requiredAll: string[]
  ignored: string[]
  preferred: string[]
  tagIds: number[]
}