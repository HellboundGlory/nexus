import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { SchemaForm } from "./SchemaForm"
import type { SchemaEntry } from "./types"

const schema: SchemaEntry[] = [
  { implementation: "sabnzbd", protocol: "usenet", fields: [
    { name: "name", type: "string", required: true },
    { name: "host", type: "string", required: true },
    { name: "port", type: "int" },
    { name: "useSsl", type: "bool", default: false },
    { name: "apiKey", type: "string", label: "API Key" },
  ]},
  { implementation: "qbittorrent", protocol: "torrent", fields: [
    { name: "name", type: "string", required: true },
    { name: "host", type: "string", required: true },
    { name: "apiKey", type: "string", label: "Password" },
  ]},
]

function renderForm(impl = "sabnzbd", editing = false) {
  const onChange = vi.fn()
  const onImplChange = vi.fn()
  render(
    <SchemaForm
      schema={schema} impl={impl} onImplChange={onImplChange}
      values={{ name: "", host: "", port: "", useSsl: false, apiKey: "" }}
      onChange={onChange} editing={editing}
    />,
  )
  return { onChange, onImplChange }
}

describe("SchemaForm", () => {
  it("renders one input per field with the right control type", () => {
    renderForm()
    expect(screen.getByLabelText("name")).toHaveAttribute("type", "text")
    expect(screen.getByLabelText("port")).toHaveAttribute("type", "number")
    expect(screen.getByLabelText("useSsl")).toHaveAttribute("type", "checkbox")
    expect(screen.getByLabelText("API Key")).toHaveAttribute("type", "password")
  })

  it("labels the secret per implementation", () => {
    renderForm("qbittorrent")
    expect(screen.getByLabelText("Password")).toBeInTheDocument()
  })

  it("shows the keep-current placeholder in edit mode", () => {
    renderForm("sabnzbd", true)
    expect(screen.getByLabelText("API Key")).toHaveAttribute("placeholder", "leave blank to keep current")
  })

  it("emits onChange when a field is edited", async () => {
    const { onChange } = renderForm()
    await userEvent.type(screen.getByLabelText("name"), "x")
    expect(onChange).toHaveBeenCalledWith("name", "x")
  })

  it("emits onImplChange when the implementation dropdown changes", async () => {
    const { onImplChange } = renderForm()
    await userEvent.selectOptions(screen.getByLabelText("Implementation"), "qbittorrent")
    expect(onImplChange).toHaveBeenCalledWith("qbittorrent")
  })
})
