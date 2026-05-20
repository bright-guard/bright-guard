import { useState } from "react";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useNavigate } from "react-router-dom";

export default function LoginPage() {
  const { devLoginEnabled, refresh } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onDevLogin(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await api<unknown>("/auth/dev/login", {
        method: "POST",
        body: JSON.stringify({ email }),
      });
      await refresh();
      navigate("/app");
    } catch (err) {
      if (err instanceof ApiError) {
        setError(typeof err.body === "string" ? err.body : err.message);
      } else {
        setError(String(err));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center px-4 py-16">
      <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900/70 p-8 shadow-2xl backdrop-blur">
        <div className="mb-6 flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-gradient-to-br from-brand-400 to-brand-700" />
          <div>
            <div className="text-lg font-semibold">Bright Guard</div>
            <div className="text-xs text-slate-400">
              Control plane for AI infrastructure
            </div>
          </div>
        </div>

        <h1 className="mb-1 text-xl font-semibold">Sign in</h1>
        <p className="mb-6 text-sm text-slate-400">
          Use your Google account to continue.
        </p>

        <a
          href="/auth/google/start"
          className="block w-full rounded-md bg-brand-500 px-4 py-2 text-center text-sm font-medium text-slate-950 hover:bg-brand-400"
        >
          Continue with Google
        </a>

        {devLoginEnabled && (
          <div className="mt-8 border-t border-slate-800 pt-6">
            <div className="mb-2 text-xs uppercase tracking-wide text-amber-400">
              Dev login (local only)
            </div>
            <form onSubmit={onDevLogin} className="space-y-3">
              <input
                type="email"
                required
                placeholder="you@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm placeholder:text-slate-500 focus:border-brand-500 focus:outline-none"
              />
              <button
                type="submit"
                disabled={busy}
                className="w-full rounded-md border border-slate-700 bg-slate-800 px-4 py-2 text-sm hover:bg-slate-700 disabled:opacity-50"
              >
                {busy ? "Signing in…" : "Dev sign in"}
              </button>
            </form>
            {error && (
              <div className="mt-3 text-sm text-rose-400">{error}</div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
