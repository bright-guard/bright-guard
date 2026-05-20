import { Link } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";

export default function NoOrgEmptyState() {
  const { platformAdmin } = useAuth();
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-10 text-center">
      <div
        className="mx-auto mb-4 h-12 w-12 rounded-full"
        style={{ background: "var(--accent-soft)", border: "1px solid var(--accent)" }}
      />
      <h2 className="text-lg font-semibold">No organization selected</h2>
      <p className="mx-auto mt-2 max-w-md text-sm text-slate-500">
        You're not a member of any org yet. Tenant pages need an active org
        before they have anything to show.
      </p>
      <div className="mt-5 flex flex-wrap items-center justify-center gap-2">
        <Link
          to="/onboarding"
          className="rounded-md px-4 py-2 text-sm font-medium text-white"
          style={{ background: "var(--accent)" }}
        >
          Create or join an org
        </Link>
        {platformAdmin && (
          <Link
            to="/admin"
            className="rounded-md border border-slate-300 px-4 py-2 text-sm text-slate-700 hover:bg-slate-100"
          >
            Visit admin console
          </Link>
        )}
      </div>
    </div>
  );
}
