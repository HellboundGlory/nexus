import { Select } from "@/components/ui/select"
import { fieldsFor } from "./payload"
import type { FormValues, SchemaEntry, SchemaField } from "./types"

const inputClass =
  "w-full rounded-md border border-[var(--color-border)] bg-[var(--color-panel-2)] px-3 py-2 text-sm"

export function SchemaForm({
  schema, impl, onImplChange, values, onChange, editing,
}: {
  schema: SchemaEntry[]
  impl: string
  onImplChange: (impl: string) => void
  values: FormValues
  onChange: (name: string, value: string | boolean) => void
  editing: boolean
}) {
  const fields = fieldsFor(schema, impl)
  return (
    <div className="flex flex-col gap-3">
      <label className="text-xs text-[var(--color-muted)]">Implementation</label>
      <Select aria-label="Implementation" value={impl} onChange={onImplChange}>
        {schema.map((e) => (
          <option key={e.implementation} value={e.implementation}>
            {e.implementation} ({e.protocol})
          </option>
        ))}
      </Select>
      {fields.map((f) => (
        <Field key={f.name} field={f} value={values[f.name]} onChange={onChange} editing={editing} />
      ))}
    </div>
  )
}

function Field({
  field, value, onChange, editing,
}: {
  field: SchemaField
  value: string | boolean | undefined
  onChange: (name: string, value: string | boolean) => void
  editing: boolean
}) {
  const isSecret = field.name === "apiKey"
  const label = isSecret ? (field.label ?? "API Key") : field.name

  if (field.type === "bool") {
    return (
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          aria-label={field.name}
          checked={Boolean(value)}
          onChange={(e) => onChange(field.name, e.target.checked)}
        />
        {field.name}
      </label>
    )
  }

  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-[var(--color-muted)]" htmlFor={`f-${field.name}`}>
        {label}{field.required ? " *" : ""}
      </label>
      <input
        id={`f-${field.name}`}
        aria-label={label}
        type={isSecret ? "password" : field.type === "int" ? "number" : "text"}
        value={typeof value === "string" ? value : ""}
        placeholder={isSecret && editing ? "leave blank to keep current" : undefined}
        onChange={(e) => onChange(field.name, e.target.value)}
        className={inputClass}
      />
    </div>
  )
}
