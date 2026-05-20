import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth, type Org } from "../auth/AuthContext";
import PageHelp from "../components/PageHelp";

export default function OnboardingPage() {
  const { user, refresh } = useAuth();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await api<Org>("/api/orgs", {
        method: "POST",
        body: JSON.stringify({ name }),
      });
      await refresh();
      navigate("/app");
    } catch (err) {
      setError(
        err instanceof ApiError && typeof err.body === "string"
          ? err.body
          : String(err),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center px-4 py-16">
      <div className="w-full max-w-lg rounded-2xl border border-slate-200 bg-white p-8 shadow-xl">
        <div className="mb-6 text-sm text-slate-500">
          Signed in as{" "}
          <span className="text-slate-900">{user?.email}</span>
        </div>
        <div className="mb-1 flex items-center gap-2">
          <h1 className="text-2xl font-semibold">Create your organization</h1>
          <PageHelp slug="getting-started" />
        </div>
        <p className="mb-6 text-sm text-slate-500">
          You'll be the owner. You can invite teammates later.
        </p>
        <form onSubmit={onSubmit} className="space-y-4">
          <label className="block text-sm">
            <span className="mb-1 block text-slate-600">Organization name</span>
            <input
              type="text"
              required
              autoFocus
              placeholder="Acme, Inc."
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm placeholder:text-slate-400 focus:border-brand-500 focus:outline-none"
            />
          </label>
          <button
            type="submit"
            disabled={busy || name.trim() === ""}
            className="w-full rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-white hover:bg-brand-400 disabled:opacity-50"
          >
            {busy ? "Creating…" : "Create organization"}
          </button>
          {error && <div className="text-sm text-rose-600">{error}</div>}
        </form>
      </div>
    </div>
  );
}
