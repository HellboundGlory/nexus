import { createBrowserRouter } from "react-router-dom"
import { RequireAuth } from "@/lib/auth"
import { Layout } from "@/app/Layout"
import { Login } from "@/pages/Login"
import { Placeholder } from "@/pages/Placeholder"
import { Dashboard } from "@/pages/Dashboard"

export const router = createBrowserRouter([
  { path: "/login", element: <Login /> },
  {
    path: "/",
    element: (
      <RequireAuth>
        <Layout />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <Dashboard /> },
      { path: "movies", element: <Placeholder title="Movies" /> },
      { path: "tv", element: <Placeholder title="TV Shows" /> },
      { path: "calendar", element: <Placeholder title="Calendar" /> },
      { path: "activity", element: <Placeholder title="Activity" /> },
      { path: "settings", element: <Placeholder title="Settings" /> },
      { path: "system", element: <Placeholder title="System" /> },
    ],
  },
])
