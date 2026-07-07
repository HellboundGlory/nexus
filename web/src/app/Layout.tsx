import { Outlet, useLocation } from "react-router-dom"
import { Sidebar, NAV_ITEMS } from "@/app/Sidebar"
import { TopBar } from "@/app/TopBar"
import { ActivityProvider } from "@/lib/activity"
import { ToastProvider } from "@/lib/toast"

function titleForPath(pathname: string): string {
  const match = NAV_ITEMS.find((n) => (n.to === "/" ? pathname === "/" : pathname.startsWith(n.to)))
  return match?.label ?? "Nexus"
}

export function Layout() {
  const { pathname } = useLocation()
  return (
    <ActivityProvider>
      <ToastProvider>
        <div className="flex h-screen overflow-hidden">
          <Sidebar />
          <div className="flex min-w-0 flex-1 flex-col">
            <TopBar title={titleForPath(pathname)} />
            <main className="flex-1 overflow-auto">
              <Outlet />
            </main>
          </div>
        </div>
      </ToastProvider>
    </ActivityProvider>
  )
}
