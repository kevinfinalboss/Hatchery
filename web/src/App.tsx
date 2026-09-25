import { Route, Routes } from "react-router-dom";
import { AppShell } from "./components/layout/AppShell";
import { RequireAdmin, RequireAuth, RequireOrgAdmin } from "./components/RequireAuth";
import { LoginPage } from "./pages/LoginPage";
import { ForgotPasswordPage } from "./pages/ForgotPasswordPage";
import { ResetPasswordPage } from "./pages/ResetPasswordPage";
import { InvitePage } from "./pages/InvitePage";
import { ConfirmEmailPage } from "./pages/ConfirmEmailPage";
import { DashboardPage } from "./pages/DashboardPage";
import { NewServerPage } from "./pages/NewServerPage";
import { ServerDetailPage } from "./pages/ServerDetailPage";
import { EggsPage } from "./pages/EggsPage";
import { MembersPage } from "./pages/MembersPage";
import { AuditPage } from "./pages/AuditPage";
import { OrgSettingsPage } from "./pages/OrgSettingsPage";
import { OrgDetailPage } from "./pages/OrgDetailPage";
import { OrgsPage } from "./pages/OrgsPage";
import { UsersPage } from "./pages/UsersPage";

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/forgot-password" element={<ForgotPasswordPage />} />
      <Route path="/reset-password" element={<ResetPasswordPage />} />
      <Route path="/invite" element={<InvitePage />} />
      <Route path="/confirm-email" element={<ConfirmEmailPage />} />

      <Route element={<RequireAuth />}>
        <Route path="/" element={<AppShell><DashboardPage /></AppShell>} />
        <Route path="/orgs/:org/servers/:name" element={<AppShell><ServerDetailPage /></AppShell>} />
        <Route path="/eggs" element={<AppShell><EggsPage /></AppShell>} />
        <Route path="/members" element={<AppShell><MembersPage /></AppShell>} />

        <Route element={<RequireOrgAdmin />}>
          <Route path="/new-server" element={<AppShell><NewServerPage /></AppShell>} />
          <Route path="/audit" element={<AppShell><AuditPage /></AppShell>} />
          <Route path="/settings" element={<AppShell><OrgSettingsPage /></AppShell>} />
        </Route>

        <Route element={<RequireAdmin />}>
          <Route path="/orgs" element={<AppShell><OrgsPage /></AppShell>} />
          <Route path="/orgs/:slug" element={<AppShell><OrgDetailPage /></AppShell>} />
          <Route path="/users" element={<AppShell><UsersPage /></AppShell>} />
        </Route>
      </Route>
    </Routes>
  );
}
