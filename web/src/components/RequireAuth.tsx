import { Navigate, Outlet } from "react-router-dom";
import { useAuth } from "../lib/auth";
import { atLeast, useOrg } from "../lib/org";

export function RequireAuth() {
  const { user, loading } = useAuth();

  if (loading) return null;
  if (!user) return <Navigate to="/login" replace />;
  return <Outlet />;
}

export function RequireAdmin() {
  const { user } = useAuth();
  if (!user?.isAdmin) return <Navigate to="/" replace />;
  return <Outlet />;
}

export function RequireOrgAdmin() {
  const { current, loading } = useOrg();
  if (loading) return null;
  if (!atLeast(current?.role, "admin")) return <Navigate to="/" replace />;
  return <Outlet />;
}
