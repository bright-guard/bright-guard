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
    <div className="flex min-h-full flex-col bg-[var(--bg-app)]">
      <div className="h-12 w-full bg-[var(--bg-topbar)]" />
      <div className="flex h-[3px] w-full">
        <span className="flex-1" style={{ background: "var(--csp-stripe-1)" }} />
        <span className="flex-1" style={{ background: "var(--csp-stripe-2)" }} />
        <span className="flex-1" style={{ background: "var(--csp-stripe-3)" }} />
      </div>

      <div className="flex flex-1 items-center justify-center px-4 py-16">
        <div className="w-full max-w-md rounded-xl border border-slate-200 bg-white p-8 shadow-sm">
          <div className="mb-6 flex items-center gap-3">
            <div
              className="h-9 w-9 rounded-md"
              style={{ background: "var(--accent)" }}
            />
            <div>
              <div className="text-lg font-bold tracking-tight text-slate-900">
                Bright Guard
              </div>
              <div className="text-xs text-slate-500">
                Control plane for AI infrastructure
              </div>
            </div>
          </div>

          <h1 className="mb-1 text-xl font-semibold text-slate-900">Sign in</h1>
          <p className="mb-6 text-sm text-slate-500">
            Use your Google account to continue.
          </p>

          <a
            href="/auth/google/start"
            className="block w-full rounded-md px-4 py-2 text-center text-sm font-medium text-white"
            style={{ background: "var(--accent)" }}
          >
            Continue with Google
          </a>

          {devLoginEnabled && (
            <div className="mt-8 border-t border-slate-200 pt-6">
              <div className="mb-2 text-xs uppercase tracking-wide text-amber-600">
                Dev login (local only)
              </div>
              <form onSubmit={onDevLogin} className="space-y-3">
                <input
                  type="email"
                  required
                  placeholder="you@example.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 placeholder:text-slate-400 focus:border-[var(--accent)] focus:outline-none"
                />
                <button
                  type="submit"
                  disabled={busy}
                  className="w-full rounded-md border border-slate-300 bg-slate-50 px-4 py-2 text-sm text-slate-800 hover:bg-slate-100 disabled:opacity-50"
                >
                  {busy ? "Signing in…" : "Dev sign in"}
                </button>
              </form>
              {error && (
                <div className="mt-3 text-sm text-rose-600">{error}</div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
