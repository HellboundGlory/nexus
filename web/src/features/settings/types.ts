export type FieldType = "string" | "int" | "int[]" | "bool"

export type SchemaField = {
  name: string
  type: FieldType
  required?: boolean
  default?: unknown
  label?: string
}

export type SchemaEntry = {
  implementation: string
  protocol: string
  fields: SchemaField[]
}

export type ConnectionRow = {
  id: number
  name: string
  implementation: string
  protocol?: string
  enabled: boolean
  priority: number
  status: string
  lastCheck: string | null
  failMessage: string
  [key: string]: unknown // other config fields (baseUrl, host, port, categories, ...)
}

export type TestResult = { ok: boolean; error?: string; capabilities?: unknown }

export type FormValues = Record<string, string | boolean>

export type ConnectionKind = "indexer" | "downloadclient"
