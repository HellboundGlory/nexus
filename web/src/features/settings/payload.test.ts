import { describe, it, expect } from "vitest"
import {
  basePath, fieldsFor, defaultValues, valuesFromRow,
  buildSavePayload, buildTestRequest, requiredMissing,
} from "./payload"
import type { SchemaEntry, SchemaField } from "./types"

const idxSchema: SchemaEntry[] = [
  { implementation: "newznab", protocol: "usenet", fields: [
    { name: "name", type: "string", required: true },
    { name: "baseUrl", type: "string", required: true },
    { name: "apiKey", type: "string", required: false },
    { name: "categories", type: "int[]", required: false },
    { name: "priority", type: "int", required: false, default: 25 },
    { name: "enabled", type: "bool", required: false, default: true },
  ]},
  { implementation: "torznab", protocol: "torrent", fields: [
    { name: "name", type: "string", required: true },
    { name: "baseUrl", type: "string", required: true },
    { name: "apiKey", type: "string", required: false },
    { name: "categories", type: "int[]", required: false },
    { name: "priority", type: "int", required: false, default: 25 },
    { name: "enabled", type: "bool", required: false, default: true },
  ]},
]
const fields = (impl: string): SchemaField[] => fieldsFor(idxSchema, impl)

describe("basePath", () => {
  it("maps kind to route base", () => {
    expect(basePath("indexer")).toBe("/indexer")
    expect(basePath("downloadclient")).toBe("/downloadclient")
  })
})

describe("defaultValues", () => {
  it("applies schema defaults (bool boolean, others stringified) and blanks the rest", () => {
    const v = defaultValues(fields("newznab"))
    expect(v.priority).toBe("25")
    expect(v.enabled).toBe(true)
    expect(v.name).toBe("")
    expect(v.apiKey).toBe("")
  })
})

describe("valuesFromRow", () => {
  it("fills from a row but never prefills the secret; joins int[]", () => {
    const v = valuesFromRow(fields("newznab"), {
      id: 1, name: "ix", implementation: "newznab", enabled: false, priority: 10,
      status: "ok", lastCheck: null, failMessage: "", baseUrl: "http://x", categories: [5000, 5040],
    } as never)
    expect(v.name).toBe("ix")
    expect(v.baseUrl).toBe("http://x")
    expect(v.categories).toBe("5000,5040")
    expect(v.enabled).toBe(false)
    expect(v.priority).toBe("10")
    expect(v.apiKey).toBe("")
  })
})

describe("buildSavePayload", () => {
  it("coerces types, adds implementation, omits empty int", () => {
    const p = buildSavePayload(fields("newznab"), {
      name: "ix", baseUrl: "http://x", apiKey: "k", categories: "5000, 5040", priority: "", enabled: true,
    }, "newznab", false)
    expect(p).toMatchObject({
      implementation: "newznab", name: "ix", baseUrl: "http://x", apiKey: "k",
      categories: [5000, 5040], enabled: true,
    })
    expect("priority" in p).toBe(false) // empty int omitted -> server default
  })
  it("omits apiKey entirely when omitSecret is true", () => {
    const p = buildSavePayload(fields("newznab"), {
      name: "ix", baseUrl: "http://x", apiKey: "", categories: "", priority: "25", enabled: true,
    }, "newznab", true)
    expect("apiKey" in p).toBe(false)
  })
  it("includes apiKey when non-empty even if omitSecret false", () => {
    const p = buildSavePayload(fields("newznab"), {
      name: "ix", baseUrl: "http://x", apiKey: "typed", categories: "", priority: "25", enabled: true,
    }, "newznab", false)
    expect(p.apiKey).toBe("typed")
  })
})

describe("buildTestRequest", () => {
  it("uses the SAVED endpoint (no body) when editing and secret untouched", () => {
    const req = buildTestRequest("indexer", {
      fields: fields("newznab"), values: { name: "ix", baseUrl: "http://x", apiKey: "" },
      impl: "newznab", id: 7, secretTouched: false, editing: true,
    })
    expect(req).toEqual({ path: "/indexer/7/test" })
  })
  it("uses the UNSAVED endpoint with full body (incl secret) in add mode", () => {
    const req = buildTestRequest("downloadclient", {
      fields: fieldsFor([{ implementation: "sabnzbd", protocol: "usenet", fields: [
        { name: "name", type: "string", required: true },
        { name: "host", type: "string", required: true },
        { name: "apiKey", type: "string" },
      ]}], "sabnzbd"),
      values: { name: "sab", host: "h", apiKey: "k" },
      impl: "sabnzbd", secretTouched: false, editing: false,
    })
    expect(req.path).toBe("/downloadclient/test")
    expect(req.body).toMatchObject({ implementation: "sabnzbd", name: "sab", host: "h", apiKey: "k" })
  })
  it("uses the UNSAVED endpoint when editing but the secret was retyped", () => {
    const req = buildTestRequest("indexer", {
      fields: fields("newznab"), values: { name: "ix", baseUrl: "http://x", apiKey: "new" },
      impl: "newznab", id: 7, secretTouched: true, editing: true,
    })
    expect(req.path).toBe("/indexer/test")
    expect(req.body?.apiKey).toBe("new")
  })
})

describe("requiredMissing", () => {
  it("reports blank required fields only", () => {
    expect(requiredMissing(fields("newznab"), { name: "", baseUrl: "http://x" })).toEqual(["name"])
    expect(requiredMissing(fields("newznab"), { name: "ix", baseUrl: "http://x" })).toEqual([])
  })
})
