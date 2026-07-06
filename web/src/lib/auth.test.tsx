import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Routes, Route, useNavigate } from "react-router-dom"
import { AuthProvider, RequireAuth, useAuth } from "@/lib/auth"
import * as api from "@/lib/api"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, getStatus: vi.fn(), login: vi.fn(), logout: vi.fn() }
})

function Protected() {
  return <div>secret</div>
}
function LoginStub() {
  const { login } = useAuth()
  const navigate = useNavigate()
  return (
    <button
      onClick={async () => {
        await login("a", "b")
        navigate("/")
      }}
    >
      do-login
    </button>
  )
}

function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<LoginStub />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <Protected />
            </RequireAuth>
          }
        />
      </Routes>
    </AuthProvider>
  )
}

beforeEach(() => vi.clearAllMocks())

describe("auth", () => {
  it("renders protected content when the probe succeeds", async () => {
    vi.mocked(api.getStatus).mockResolvedValue({ version: "1", appName: "Nexus", healthy: true, taskCount: 0 })
    render(<MemoryRouter initialEntries={["/"]}><App /></MemoryRouter>)
    expect(await screen.findByText("secret")).toBeInTheDocument()
  })

  it("redirects to /login when the probe 401s", async () => {
    vi.mocked(api.getStatus).mockRejectedValue(new api.ApiError(401, "unauthorized", "no"))
    render(<MemoryRouter initialEntries={["/"]}><App /></MemoryRouter>)
    expect(await screen.findByText("do-login")).toBeInTheDocument()
  })

  it("login() then a successful probe reveals protected content", async () => {
    vi.mocked(api.getStatus)
      .mockRejectedValueOnce(new api.ApiError(401, "unauthorized", "no"))
      .mockResolvedValue({ version: "1", appName: "Nexus", healthy: true, taskCount: 0 })
    vi.mocked(api.login).mockResolvedValue()
    render(<MemoryRouter initialEntries={["/"]}><App /></MemoryRouter>)
    await screen.findByText("do-login")
    await userEvent.click(screen.getByText("do-login"))
    expect(await screen.findByText("secret")).toBeInTheDocument()
  })
})
