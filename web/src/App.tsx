import { Route, Routes } from "react-router-dom";
import { AppShell } from "./components/layout/AppShell";
import { RequireAdmin, RequireAuth, RequireOrgAdmin } from "./components/RequireAuth";
import { LoginPage } from "./pages/LoginPage";
import { DashboardPage } from "./pages/DashboardPage";
import { ServerDetailPage } from "./pages/ServerDetailPage";
import { EggsPage } from "./pages/EggsPage";
import { MembersPage } from "./pages/MembersPage";
import { AuditPage } from "./pages/AuditPage";
import { OrgsPage } from "./pages/OrgsPage";
import { UsersPage } from "./pages/UsersPage";

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />

      <Route element={<RequireAuth />}>
        <Route path="/" element={<AppShell><DashboardPage /></AppShell>} />
        <Route path="/orgs/:org/servers/:name" element={<AppShell><ServerDetailPage /></AppShell>} />
        <Route path="/eggs" element={<AppShell><EggsPage /></AppShell>} />
        <Route path="/members" element={<AppShell><MembersPage /></AppShell>} />

        <Route element={<RequireOrgAdmin />}>
          <Route path="/audit" element={<AppShell><AuditPage /></AppShell>} />
        </Route>

        <Route element={<RequireAdmin />}>
          <Route path="/orgs" element={<AppShell><OrgsPage /></AppShell>} />
          <Route path="/users" element={<AppShell><UsersPage /></AppShell>} />
        </Route>
      </Route>
    </Routes>
  );
}
