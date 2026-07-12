import { NavLink, Outlet } from "react-router-dom"
import { cn } from "@/lib/utils"

const TABS: { to: string; label: string }[] = [
  { to: "/settings/indexers", label: "Indexers" },
  { to: "/settings/downloadclients", label: "Download Clients" },
  { to: "/settings/qualityprofiles", label: "Quality Profiles" },
  { to: "/settings/rootfolders", label: "Root Folders" },
  { to: "/settings/naming", label: "Naming" },
  { to: "/settings/general", label: "General" },
]

export function SettingsLayout() {
  return (
    <div>
      <div className="border-b border-[var(--color-border)] px-6 pt-6">
        <h1 className="mb-3 text-2xl font-bold">Settings</h1>
        <nav className="flex gap-1">
          {TABS.map((t) => (
            <NavLink
              key={t.to}
              to={t.to}
              className={({ isActive }) =>
                cn(
                  "rounded-t-md px-4 py-2 text-sm text-[var(--color-muted)]",
                  isActive && "bg-[rgba(124,92,255,0.16)] font-semibold text-[var(--color-fg)]",
                )
              }
            >
              {t.label}
            </NavLink>
          ))}
        </nav>
      </div>
      <Outlet />
    </div>
  )
}
