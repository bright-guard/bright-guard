import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth, type Org } from "../auth/AuthContext";

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
      <div className="w-full max-w-lg rounded-2xl border border-slate-800 bg-slate-900/70 p-8 shadow-2xl">
        <div className="mb-6 text-sm text-slate-400">
          Signed in as{" "}
          <span className="text-slate-200">{user?.email}</span>
        </div>
        <h1 className="mb-1 text-2xl font-semibold">Create your organization</h1>
        <p className="mb-6 text-sm text-slate-400">
          You'll be the owner. You can invite teammates later.
        </p>
        <form onSubmit={onSubmit} className="space-y-4">
          <label className="block text-sm">
            <span className="mb-1 block text-slate-300">Organization name</span>
            <input
              type="text"
              required
              autoFocus
              placeholder="Acme, Inc."
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm placeholder:text-slate-500 focus:border-brand-500 focus:outline-none"
            />
          </label>
          <button
            type="submit"
            disabled={busy || name.trim() === ""}
            className="w-full rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-brand-400 disabled:opacity-50"
          >
            {busy ? "Creating…" : "Create organization"}
          </button>
          {error && <div className="text-sm text-rose-400">{error}</div>}
        </form>
      </div>
    </div>
  );
}
