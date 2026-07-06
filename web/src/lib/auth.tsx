import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react"
import { Navigate } from "react-router-dom"
import { ApiError, getStatus, login as apiLogin, logout as apiLogout, setUnauthorizedHandler } from "@/lib/api"

export type AuthStatus = "loading" | "authed" | "unauthed"

type AuthContextValue = {
  status: AuthStatus
  login: (u: string, p: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading")
  const mounted = useRef(true)

  const refresh = useCallback(async () => {
    try {
      await getStatus()
      if (mounted.current) setStatus("authed")
    } catch {
      // Any failed probe (401 or transient error) sends the user to /login for
      // this slice; a network hiccup on boot simply re-prompts. Accepted.
      if (mounted.current) setStatus("unauthed")
    }
  }, [])

  const login = useCallback(
    async (u: string, p: string) => {
      await apiLogin(u, p) // throws ApiError(401) on bad credentials; caller handles
      await refresh()
    },
    [refresh],
  )

  const logout = useCallback(async () => {
    try {
      await apiLogout()
    } catch {
      // ignore — we clear local state regardless
    }
    if (mounted.current) setStatus("unauthed")
  }, [])

  useEffect(() => {
    mounted.current = true
    setUnauthorizedHandler(() => setStatus("unauthed"))
    void refresh()
    return () => {
      mounted.current = false
      setUnauthorizedHandler(null)
    }
  }, [refresh])

  return <AuthContext.Provider value={{ status, login, logout, refresh }}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error("useAuth must be used within AuthProvider")
  return ctx
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth()
  if (status === "loading") return null
  if (status === "unauthed") return <Navigate to="/login" replace />
  return <>{children}</>
}

export { ApiError }
