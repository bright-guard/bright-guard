import { Navigate } from "react-router-dom";
import type { ReactNode } from "react";
import { useAuth } from "../auth/AuthContext";

// Wraps an admin-section route. Assumes the caller has already gated on
// authentication (ProtectedRoute with requireMembership=false). Redirects
// non-admins back to the tenant shell.
export function RequirePlatformAdmin({ children }: { children: ReactNode }) {
  const { loading, user, platformAdmin } = useAuth();
  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-slate-400">
        Loading…
      </div>
    );
  }
  if (!user) {
    return <Navigate to="/login" replace />;
  }
  if (!platformAdmin) {
    return <Navigate to="/app" replace />;
  }
  return <>{children}</>;
}
