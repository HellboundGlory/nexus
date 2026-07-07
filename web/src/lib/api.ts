const BASE = "/api/v1"

export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
  }
}

export type SystemStatus = {
  version: string
  appName: string
  healthy: boolean
  taskCount: number
}

let unauthorizedHandler: (() => void) | null = null
export function setUnauthorizedHandler(fn: (() => void) | null): void {
  unauthorizedHandler = fn
}

async function request<T>(method: string, path: string, body?: unknown, skipAuthHandler = false): Promise<T> {
  const init: RequestInit = {
    method,
    credentials: "include",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  }
  const res = await fetch(`${BASE}${path}`, init)
  if (res.status === 401 && !skipAuthHandler && unauthorizedHandler) unauthorizedHandler()
  if (!res.ok) throw await toApiError(res)
  if (res.status === 204) return undefined as T
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

async function toApiError(res: Response): Promise<ApiError> {
  try {
    const data = await res.clone().json()
    if (data && data.error && typeof data.error.code === "string") {
      return new ApiError(res.status, data.error.code, data.error.message ?? res.statusText)
    }
  } catch {
    // fall through to a generic error
  }
  return new ApiError(res.status, "unknown", res.statusText || "request failed")
}

export function apiGet<T>(path: string): Promise<T> {
  return request<T>("GET", path)
}
export function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return request<T>("POST", path, body)
}
export function apiPut<T>(path: string, body?: unknown): Promise<T> {
  return request<T>("PUT", path, body)
}
export function apiDelete<T>(path: string): Promise<T> {
  return request<T>("DELETE", path)
}

export function getStatus(): Promise<SystemStatus> {
  return apiGet<SystemStatus>("/system/status")
}
export function login(username: string, password: string): Promise<void> {
  return request<void>("POST", "/auth/login", { username, password }, true)
}
export function logout(): Promise<void> {
  return apiPost<void>("/auth/logout")
}
