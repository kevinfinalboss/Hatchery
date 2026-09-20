import { Route, Routes } from "react-router-dom";
import { AppShell } from "./components/layout/AppShell";
import { RequireAdmin, RequireAuth } from "./components/RequireAuth";
import { LoginPage } from "./pages/LoginPage";
import { DashboardPage } from "./pages/DashboardPage";
import { ServerDetailPage } from "./pages/ServerDetailPage";
import { UsersPage } from "./pages/UsersPage";

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />

      <Route element={<RequireAuth />}>
        <Route
          path="/"
          element={
            <AppShell>
              <DashboardPage />
            </AppShell>
          }
        />
        <Route
          path="/servers/:namespace/:name"
          element={
            <AppShell>
              <ServerDetailPage />
            </AppShell>
          }
        />
        <Route element={<RequireAdmin />}>
          <Route
            path="/users"
            element={
              <AppShell>
                <UsersPage />
              </AppShell>
            }
          />
        </Route>
      </Route>
    </Routes>
  );
}
