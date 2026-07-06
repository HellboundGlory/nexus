import { useNavigate } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { useAuth } from "@/lib/auth"

export function TopBar({ title }: { title: string }) {
  const { logout } = useAuth()
  const navigate = useNavigate()
  const onLogout = async () => {
    await logout()
    navigate("/login", { replace: true })
  }
  return (
    <header className="flex items-center justify-between border-b border-[var(--color-border)] bg-[var(--color-panel)] px-6 py-3.5">
      <h1 className="text-base font-semibold">{title}</h1>
      <Button variant="ghost" size="sm" onClick={onLogout}>
        Log out
      </Button>
    </header>
  )
}
