import { NavLink } from "react-router-dom"
import { LayoutDashboard, Film, Tv, Calendar, Activity, Settings, Cpu, type LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"

export const NAV_ITEMS: { to: string; label: string; icon: LucideIcon }[] = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/movies", label: "Movies", icon: Film },
  { to: "/tv", label: "TV Shows", icon: Tv },
  { to: "/calendar", label: "Calendar", icon: Calendar },
  { to: "/activity", label: "Activity", icon: Activity },
  { to: "/settings", label: "Settings", icon: Settings },
  { to: "/system", label: "System", icon: Cpu },
]

export function Sidebar() {
  return (
    <aside className="w-52 shrink-0 border-r border-[var(--color-border)] bg-[#10151c] py-4">
      <div className="flex items-center gap-2 px-5 pb-4 text-lg font-bold">
        <span className="h-5 w-5 rounded-md bg-gradient-to-br from-[var(--color-brand)] to-[#4da8ff]" />
        Nexus
      </div>
      <nav className="flex flex-col gap-0.5 px-2.5">
        {NAV_ITEMS.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === "/"}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm text-[var(--color-muted)]",
                isActive && "bg-[rgba(124,92,255,0.16)] font-semibold text-[var(--color-fg)]",
              )
            }
          >
            <Icon className="h-4 w-4" />
            {label}
          </NavLink>
        ))}
      </nav>
    </aside>
  )
}
