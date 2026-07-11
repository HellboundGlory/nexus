import type { ConnectionKind, ConnectionRow, FormValues, SchemaEntry, SchemaField } from "./types"

export function basePath(kind: ConnectionKind): string {
  return kind === "indexer" ? "/indexer" : "/downloadclient"
}

export function fieldsFor(schema: SchemaEntry[], impl: string): SchemaField[] {
  return schema.find((e) => e.implementation === impl)?.fields ?? []
}

export function defaultValues(fields: SchemaField[]): FormValues {
  const v: FormValues = {}
  for (const f of fields) {
    if (f.type === "bool") v[f.name] = Boolean(f.default ?? false)
    else v[f.name] = f.default != null ? String(f.default) : ""
  }
  return v
}

export function valuesFromRow(fields: SchemaField[], row: ConnectionRow): FormValues {
  const v: FormValues = {}
  for (const f of fields) {
    if (f.name === "apiKey") { v[f.name] = ""; continue } // never prefill secrets
    const raw = row[f.name]
    if (f.type === "bool") v[f.name] = Boolean(raw)
    else if (f.type === "int[]") v[f.name] = Array.isArray(raw) ? raw.join(",") : ""
    else v[f.name] = raw != null ? String(raw) : ""
  }
  return v
}

export function parseFieldValue(field: SchemaField, raw: string | boolean): unknown {
  if (field.type === "bool") return Boolean(raw)
  const s = typeof raw === "string" ? raw : String(raw)
  if (field.type === "int") return s.trim() === "" ? undefined : Number(s)
  if (field.type === "int[]") {
    return s.split(",").map((p) => p.trim()).filter((p) => p !== "").map(Number)
  }
  return s // string
}

export function buildSavePayload(
  fields: SchemaField[], values: FormValues, impl: string, omitSecret: boolean,
): Record<string, unknown> {
  const out: Record<string, unknown> = { implementation: impl }
  for (const f of fields) {
    if (f.name === "apiKey" && omitSecret) continue
    const parsed = parseFieldValue(f, values[f.name])
    if (parsed === undefined) continue // empty int -> let server apply default
    out[f.name] = parsed
  }
  return out
}

export function buildTestRequest(
  kind: ConnectionKind,
  args: { fields: SchemaField[]; values: FormValues; impl: string; id?: number; secretTouched: boolean; editing: boolean },
): { path: string; body?: Record<string, unknown> } {
  const bp = basePath(kind)
  // Editing with an untouched secret -> test the SAVED entity so the stored key
  // is used server-side. Otherwise test the UNSAVED values (they carry the key).
  if (args.editing && args.id != null && !args.secretTouched) {
    return { path: `${bp}/${args.id}/test` }
  }
  return { path: `${bp}/test`, body: buildSavePayload(args.fields, args.values, args.impl, false) }
}

export function requiredMissing(fields: SchemaField[], values: FormValues): string[] {
  return fields
    .filter((f) => f.required && f.type !== "bool" && String(values[f.name] ?? "").trim() === "")
    .map((f) => f.name)
}
