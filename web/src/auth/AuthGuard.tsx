import { Navigate, Outlet } from "react-router-dom";
import { TokenManager } from "./TokenManager";
import { useLocation } from "react-router-dom";

export function RequireAuth() {
  const location = useLocation();
  const tokenManager = TokenManager.getInstance();

  if (!tokenManager.isAuthenticated()) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return <Outlet />;
}

export function RequireAdmin() {
  const tokenManager = TokenManager.getInstance();
  const role = tokenManager.getUserRole();

  if (role !== "admin") {
    return <Navigate to="/" replace />;
  }

  return <Outlet />;
}
