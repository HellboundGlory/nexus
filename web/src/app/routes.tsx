import { createBrowserRouter } from "react-router-dom"
import { RequireAuth } from "@/lib/auth"
import { Layout } from "@/app/Layout"
import { Login } from "@/pages/Login"
import { Placeholder } from "@/pages/Placeholder"
import { Dashboard } from "@/pages/Dashboard"
import { Movies } from "@/pages/Movies"
import { TvShows } from "@/pages/TvShows"
import { MediaDetail } from "@/pages/MediaDetail"
import { Navigate } from "react-router-dom"
import { SettingsLayout } from "@/features/settings/SettingsLayout"
import { ConnectionsSection } from "@/features/settings/ConnectionsSection"

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
      { path: "movies", element: <Movies /> },
      { path: "tv", element: <TvShows /> },
      { path: "movies/:id", element: <MediaDetail kind="movie" /> },
      { path: "tv/:id", element: <MediaDetail kind="series" /> },
      { path: "calendar", element: <Placeholder title="Calendar" /> },
      { path: "activity", element: <Placeholder title="Activity" /> },
      {
        path: "settings",
        element: <SettingsLayout />,
        children: [
          { index: true, element: <Navigate to="/settings/indexers" replace /> },
          { path: "indexers", element: <ConnectionsSection kind="indexer" /> },
          { path: "downloadclients", element: <ConnectionsSection kind="downloadclient" /> },
        ],
      },
      { path: "system", element: <Placeholder title="System" /> },
    ],
  },
])
