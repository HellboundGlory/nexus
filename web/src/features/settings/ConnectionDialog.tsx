import { useMemo, useState } from "react"
import { Dialog, DialogTitle } from "@/components/ui/dialog"
import { useToast } from "@/lib/toast"
import { SchemaForm } from "./SchemaForm"
import { useConnectionSchema, useSaveConnection, useTestConnection } from "./api"
import {
  buildSavePayload, buildTestRequest, defaultValues, fieldsFor, requiredMissing, valuesFromRow,
} from "./payload"
import type { ConnectionKind, ConnectionRow, FormValues, TestResult } from "./types"

export function ConnectionDialog({
  kind, existing, open, onOpenChange,
}: {
  kind: ConnectionKind
  existing?: ConnectionRow
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const { toast } = useToast()
  const schemaQ = useConnectionSchema(kind)
  const save = useSaveConnection(kind)
  const testMut = useTestConnection(kind)
  const schema = useMemo(() => schemaQ.data ?? [], [schemaQ.data])
  const editing = existing != null

  const [impl, setImpl] = useState<string>(() => existing?.implementation ?? "")
  const activeImpl = impl || schema[0]?.implementation || ""
  const fields = fieldsFor(schema, activeImpl)

  const [values, setValues] = useState<FormValues>({})
  const [initialized, setInitialized] = useState(false)
  const [secretTouched, setSecretTouched] = useState(false)
  const [result, setResult] = useState<TestResult | null>(null)

  // Seed form values once the schema has loaded (defaults for add, row for edit).
  if (!initialized && schema.length > 0 && activeImpl) {
    setValues(existing ? valuesFromRow(fields, existing) : defaultValues(fields))
    setInitialized(true)
  }

  function onImplChange(next: string) {
    setImpl(next)
    setValues(defaultValues(fieldsFor(schema, next)))
    setSecretTouched(false)
    setResult(null)
  }

  function onChange(name: string, value: string | boolean) {
    if (name === "apiKey") setSecretTouched(true)
    setValues((v) => ({ ...v, [name]: value }))
  }

  async function onTest() {
    setResult(null)
    const req = buildTestRequest(kind, { fields, values, impl: activeImpl, id: existing?.id, secretTouched, editing })
    try {
      const res = await testMut.mutateAsync(req)
      setResult(res)
    } catch (e) {
      setResult({ ok: false, error: e instanceof Error ? e.message : "test failed" })
    }
  }

  async function onSave() {
    const missing = requiredMissing(fields, values)
    if (missing.length > 0) { toast(`Required: ${missing.join(", ")}`, { variant: "error" }); return }
    const payload = buildSavePayload(fields, values, activeImpl, editing && !secretTouched)
    try {
      await save.mutateAsync({ payload, id: existing?.id })
      toast(editing ? "Saved" : "Added", { variant: "ok" })
      onOpenChange(false)
    } catch (e) {
      toast(e instanceof Error ? e.message : "Save failed", { variant: "error" })
    }
  }

  const pending = save.isPending
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle>{editing ? "Edit" : "Add"} {kind === "indexer" ? "Indexer" : "Download Client"}</DialogTitle>
      {schema.length === 0 ? (
        <p className="text-sm text-[var(--color-muted)]">Loading…</p>
      ) : (
        <>
          <SchemaForm
            schema={schema} impl={activeImpl} onImplChange={onImplChange}
            values={values} onChange={onChange} editing={editing}
          />
          {result && (
            <div
              role="status"
              className={`mt-3 rounded-md border px-3 py-2 text-sm ${
                result.ok
                  ? "border-[var(--color-ok)] text-[var(--color-ok)]"
                  : "border-[var(--color-warn)] text-[var(--color-warn)]"
              }`}
            >
              {result.ok ? "Connection OK" : `Test failed: ${result.error ?? "unknown error"}`}
              {result.ok && result.capabilities != null && (
                <pre className="mt-1 max-h-32 overflow-auto text-xs text-[var(--color-muted)]">
                  {JSON.stringify(result.capabilities, null, 2)}
                </pre>
              )}
            </div>
          )}
          <div className="mt-4 flex justify-end gap-2">
            <button
              onClick={onTest}
              disabled={testMut.isPending}
              className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm disabled:opacity-50"
            >
              {testMut.isPending ? "Testing…" : "Test"}
            </button>
            <button
              onClick={onSave}
              disabled={pending}
              className="rounded-md bg-[var(--color-brand)] px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50"
            >
              {pending ? "Saving…" : "Save"}
            </button>
          </div>
        </>
      )}
    </Dialog>
  )
}
