import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter, createMemoryRouter, RouterProvider } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ToastProvider } from "@/lib/toast"
import { SettingsLayout } from "./SettingsLayout"
import { router } from "@/app/routes"
import * as tagApi from "./tagApi"

// The Tags route renders TagsSection, whose hooks we stub so the integration
// test never touches the network. Layout would render a live WebSocket-backed
// ActivityProvider and Sidebar, so swap it for a bare Outlet. The test drives
// the REAL route objects from routes.tsx (router.routes) so removing the `tags`
// child route from routes.tsx fails it.
vi.mock("./tagApi", async (orig) => {
  const actual = await orig<typeof import("./tagApi")>()
  return { ...actual, useTags: vi.fn(), useCreateTag: vi.fn(), useRenameTag: vi.fn(), useDeleteTag: vi.fn() }
})
vi.mock("@/app/Layout", async () => {
  const { Outlet } = await import("react-router-dom")
  return { Layout: () => <Outlet /> }
})
vi.mock("@/lib/auth", async () => {
  const React = await import("react")
  const PassThrough = (props: { children: React.ReactNode }) =>
    React.createElement(React.Fragment, null, props.children)
  return { RequireAuth: PassThrough }
})

function mut(extra: object = {}) {
  return { mutate: vi.fn(), isPending: false, ...extra } as unknown as never
}

beforeEach(() => vi.clearAllMocks())

describe("SettingsLayout", () => {
  it("renders tab links for the 3a sections with correct hrefs", () => {
    render(<MemoryRouter initialEntries={["/settings/indexers"]}><SettingsLayout /></MemoryRouter>)
    const indexers = screen.getByRole("link", { name: "Indexers" })
    const clients = screen.getByRole("link", { name: "Download Clients" })
    expect(indexers).toHaveAttribute("href", "/settings/indexers")
    expect(clients).toHaveAttribute("href", "/settings/downloadclients")
  })

  it("renders tab links for the 3b sections with correct hrefs", () => {
    render(<MemoryRouter initialEntries={["/settings/indexers"]}><SettingsLayout /></MemoryRouter>)
    expect(screen.getByRole("link", { name: "Quality Profiles" })).toHaveAttribute("href", "/settings/qualityprofiles")
    expect(screen.getByRole("link", { name: "Tags" })).toHaveAttribute("href", "/settings/tags")
    expect(screen.getByRole("link", { name: "Root Folders" })).toHaveAttribute("href", "/settings/rootfolders")
    expect(screen.getByRole("link", { name: "Naming" })).toHaveAttribute("href", "/settings/naming")
    expect(screen.getByRole("link", { name: "General" })).toHaveAttribute("href", "/settings/general")
  })

  it("renders the Tags section at the /settings/tags route", async () => {
    vi.mocked(tagApi.useTags).mockReturnValue({ data: [{ id: 1, label: "anime", seriesCount: 3, movieCount: 0 }], isLoading: false, isError: false } as never)
    vi.mocked(tagApi.useCreateTag).mockReturnValue(mut())
    vi.mocked(tagApi.useRenameTag).mockReturnValue(mut())
    vi.mocked(tagApi.useDeleteTag).mockReturnValue(mut())
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const memRouter = createMemoryRouter(router.routes, { initialEntries: ["/settings/tags"] })
    render(<QueryClientProvider client={qc}><ToastProvider><RouterProvider router={memRouter} /></ToastProvider></QueryClientProvider>)
    expect(await screen.findByText("anime")).toBeInTheDocument()
  })
})
