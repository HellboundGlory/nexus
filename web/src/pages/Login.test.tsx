import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Routes, Route } from "react-router-dom"
import { AuthProvider } from "@/lib/auth"
import { Login } from "@/pages/Login"
import * as api from "@/lib/api"

vi.mock("@/lib/api", async (orig) => {
  const actual = await orig<typeof import("@/lib/api")>()
  return { ...actual, getStatus: vi.fn(), login: vi.fn(), logout: vi.fn() }
})

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={["/login"]}>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/" element={<div>home</div>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getStatus).mockRejectedValue(new api.ApiError(401, "unauthorized", "no"))
})

describe("Login", () => {
  it("logs in and navigates home on success", async () => {
    vi.mocked(api.login).mockResolvedValue()
    // Boot probe (while on /login) 401s → unauthed; the post-login probe succeeds.
    vi.mocked(api.getStatus)
      .mockReset()
      .mockRejectedValueOnce(new api.ApiError(401, "unauthorized", "no"))
      .mockResolvedValue({ version: "1", appName: "Nexus", healthy: true, taskCount: 0 })
    renderLogin()
    await userEvent.type(screen.getByLabelText(/username/i), "admin")
    await userEvent.type(screen.getByLabelText(/password/i), "secret")
    await userEvent.click(screen.getByRole("button", { name: /sign in/i }))
    expect(await screen.findByText("home")).toBeInTheDocument()
  })

  it("shows an error on invalid credentials", async () => {
    vi.mocked(api.login).mockRejectedValue(new api.ApiError(401, "unauthorized", "invalid credentials"))
    renderLogin()
    await userEvent.type(screen.getByLabelText(/username/i), "admin")
    await userEvent.type(screen.getByLabelText(/password/i), "wrong")
    await userEvent.click(screen.getByRole("button", { name: /sign in/i }))
    expect(await screen.findByText(/invalid username or password/i)).toBeInTheDocument()
  })
})
