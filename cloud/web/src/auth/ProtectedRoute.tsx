import { Navigate } from "react-router-dom";
import type { ReactNode } from "react";
import { useAuth } from "./AuthContext";

export function ProtectedRoute({
  children,
  requireMembership = true,
}: {
  children: ReactNode;
  requireMembership?: boolean;
}) {
  const { loading, user, memberships } = useAuth();
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
  if (requireMembership && memberships.length === 0) {
    return <Navigate to="/onboarding" replace />;
  }
  if (!requireMembership && memberships.length > 0) {
    return <Navigate to="/app" replace />;
  }
  return <>{children}</>;
}
