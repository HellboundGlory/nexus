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
import { QualityProfilesSection } from "@/features/settings/QualityProfilesSection"
import { RootFoldersSection } from "@/features/settings/RootFoldersSection"
import { NamingSection } from "@/features/settings/NamingSection"
import { MediaManagementSection } from "@/features/settings/MediaManagementSection"
import { GeneralSection } from "@/features/settings/GeneralSection"
import { ActivityLayout } from "@/features/activity/ActivityLayout"
import { QueueSection } from "@/features/activity/QueueSection"
import { HistorySection } from "@/features/activity/HistorySection"
import { BlocklistSection } from "@/features/activity/BlocklistSection"
import { CalendarView } from "@/features/calendar/CalendarView"

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
      { path: "calendar", element: <CalendarView /> },
      {
        path: "activity",
        element: <ActivityLayout />,
        children: [
          { index: true, element: <Navigate to="/activity/queue" replace /> },
          { path: "queue", element: <QueueSection /> },
          { path: "history", element: <HistorySection /> },
          { path: "blocklist", element: <BlocklistSection /> },
        ],
      },
      {
        path: "settings",
        element: <SettingsLayout />,
        children: [
          { index: true, element: <Navigate to="/settings/indexers" replace /> },
          { path: "indexers", element: <ConnectionsSection kind="indexer" /> },
          { path: "downloadclients", element: <ConnectionsSection kind="downloadclient" /> },
          { path: "qualityprofiles", element: <QualityProfilesSection /> },
          { path: "rootfolders", element: <RootFoldersSection /> },
          { path: "naming", element: <NamingSection /> },
          { path: "mediamanagement", element: <MediaManagementSection /> },
          { path: "general", element: <GeneralSection /> },
        ],
      },
      { path: "system", element: <Placeholder title="System" /> },
    ],
  },
])
