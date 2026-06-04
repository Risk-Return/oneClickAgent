import { createBrowserRouter } from "react-router-dom";
import { Layout } from "@/components/Layout";
import { RequireAuth, RequireAdmin } from "@/auth/AuthGuard";
import { LoginPage } from "@/pages/LoginPage";
import { RegisterPage } from "@/pages/RegisterPage";
import { NotFoundPage } from "@/pages/NotFoundPage";
import { DashboardPage } from "@/pages/DashboardPage";
import { JobsPage } from "@/pages/JobsPage";
import { JobHistoryPage } from "@/pages/JobHistoryPage";
import { AgentsPage } from "@/pages/AgentsPage";
import { SkillsPage } from "@/pages/SkillsPage";
import { SettingsPage } from "@/pages/SettingsPage";
import { SavedLoginsPage } from "@/pages/SavedLoginsPage";
import { DeviceFleetPage } from "@/pages/admin/DeviceFleetPage";
import { SkillVaultPage } from "@/pages/admin/SkillVaultPage";
import { FleetRolloutPage } from "@/pages/admin/FleetRolloutPage";
import { OrganizationsPage } from "@/pages/admin/OrganizationsPage";
import { VisibilityPage } from "@/pages/admin/VisibilityPage";
import { UserTiersPage } from "@/pages/admin/UserTiersPage";
import { AgentPoolPage } from "@/pages/admin/AgentPoolPage";

const BASENAME = import.meta.env.VITE_BASE || '/';

export const router = createBrowserRouter([
  {
    path: "/login",
    element: <LoginPage />,
  },
  {
    path: "/register",
    element: <RegisterPage />,
  },
  {
    element: <RequireAuth />,
    children: [
      {
        element: <Layout />,
        children: [
          { index: true, element: <DashboardPage /> },
          { path: "jobs", element: <JobsPage /> },
          { path: "jobs/:jobId", element: <JobsPage /> },
          { path: "history", element: <JobHistoryPage /> },
          { path: "agents", element: <AgentsPage /> },
          { path: "skills", element: <SkillsPage /> },
          { path: "logins", element: <SavedLoginsPage /> },
          { path: "settings", element: <SettingsPage /> },
        ],
      },
    ],
  },
  {
    element: <RequireAdmin />,
    children: [
      {
        element: <Layout />,
        children: [
          { path: "admin/devices", element: <DeviceFleetPage /> },
          { path: "admin/skill-vault", element: <SkillVaultPage /> },
          { path: "admin/fleet-rollout", element: <FleetRolloutPage /> },
          { path: "admin/organizations", element: <OrganizationsPage /> },
          { path: "admin/visibility", element: <VisibilityPage /> },
          { path: "admin/users", element: <UserTiersPage /> },
          { path: "admin/pool", element: <AgentPoolPage /> },
        ],
      },
    ],
  },
  { path: "*", element: <NotFoundPage /> },
], { basename: BASENAME });
