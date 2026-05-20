import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { DeviceLookup } from "../api/types";

type Phase = "input" | "loading" | "confirm" | "approved" | "denied" | "error";

export default function DevicePage() {
  const { user } = useAuth();
  const [params, setParams] = useSearchParams();
  const initialCode = params.get("code") ?? "";

  const [code, setCode] = useState(initialCode);
  const [phase, setPhase] = useState<Phase>(initialCode ? "loading" : "input");
  const [info, setInfo] = useState<DeviceLookup | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const normalize = (raw: string) =>
    raw.toUpperCase().replaceAll(" ", "").replace(/^([A-Z0-9]{4})([A-Z0-9]{4})$/, "$1-$2");

  const lookup = useCallback(async (raw: string) => {
    const c = normalize(raw);
    if (!c.match(/^[A-Z0-9]{4}-[A-Z0-9]{4}$/)) {
      setPhase("input");
      setError("Code should be 8 letters/digits like ABCD-WXYZ.");
      return;
    }
    setPhase("loading");
    setError(null);
    try {
      const got = await api<DeviceLookup>(`/api/device/lookup?code=${encodeURIComponent(c)}`);
      setInfo(got);
      if (got.status === "pending") {
        setPhase("confirm");
      } else if (got.status === "approved") {
        setPhase("approved");
      } else if (got.status === "denied") {
        setPhase("denied");
      } else {
        setError("This code has expired.");
        setPhase("error");
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        setError("That code doesn't match an active request.");
      } else {
        setError(err instanceof Error ? err.message : String(err));
      }
      setPhase("error");
    }
  }, []);

  useEffect(() => {
    if (initialCode) lookup(initialCode);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialCode]);

  async function approve() {
    setBusy(true);
    setError(null);
    try {
      await api<{ ok: boolean }>("/api/device/approve", {
        method: "POST",
        body: JSON.stringify({ userCode: normalize(code) }),
      });
      setPhase("approved");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function deny() {
    setBusy(true);
    setError(null);
    try {
      await api<{ ok: boolean }>("/api/device/deny", {
        method: "POST",
        body: JSON.stringify({ userCode: normalize(code) }),
      });
      setPhase("denied");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setParams({ code: normalize(code) });
    lookup(code);
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100">
      <header className="border-b border-slate-800 bg-slate-950/70">
        <div className="mx-auto flex max-w-3xl items-center justify-between px-4 py-3">
          <div className="flex items-center gap-3">
            <div className="h-7 w-7 rounded-md bg-gradient-to-br from-brand-400 to-brand-700" />
            <div className="font-semibold tracking-tight">Bright Guard</div>
          </div>
          <div className="text-sm text-slate-400">Signed in as {user?.email}</div>
        </div>
      </header>
      <main className="mx-auto grid max-w-md place-items-center px-4 py-16">
        <div className="w-full rounded-2xl border border-slate-800 bg-slate-900/50 p-6 shadow-xl">
          <h1 className="text-xl font-semibold">Authorize a device</h1>
          <p className="mt-1 text-sm text-slate-400">
            Connect a CLI or other client to your Bright Guard account.
          </p>

          {phase === "input" && (
            <form onSubmit={onSubmit} className="mt-5 space-y-4">
              <label className="block text-sm">
                <span className="mb-1 block text-slate-300">Device code</span>
                <input
                  autoFocus
                  required
                  placeholder="ABCD-WXYZ"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-center font-mono text-lg tracking-widest placeholder:text-slate-600 focus:border-brand-500 focus:outline-none"
                />
              </label>
              {error && <div className="text-sm text-rose-400">{error}</div>}
              <button
                type="submit"
                className="w-full rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-brand-400"
              >
                Continue
              </button>
            </form>
          )}

          {phase === "loading" && (
            <div className="mt-6 text-sm text-slate-400">Looking up code…</div>
          )}

          {phase === "confirm" && info && (
            <div className="mt-5 space-y-5">
              <div className="rounded-md border border-slate-700 bg-slate-950/50 px-4 py-3 text-sm">
                <div className="text-slate-300">
                  <span className="font-mono">{info.clientLabel || "bg-cli"}</span> is requesting access to your Bright Guard account.
                </div>
                <div className="mt-1 text-xs text-slate-500">
                  Expires {new Date(info.expiresAt).toLocaleString()}
                </div>
              </div>
              {error && <div className="text-sm text-rose-400">{error}</div>}
              <div className="flex gap-3">
                <button
                  onClick={deny}
                  disabled={busy}
                  className="flex-1 rounded-md border border-slate-700 px-4 py-2 text-sm hover:bg-slate-800 disabled:opacity-50"
                >
                  Deny
                </button>
                <button
                  onClick={approve}
                  disabled={busy}
                  className="flex-1 rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-brand-400 disabled:opacity-50"
                >
                  {busy ? "Working…" : "Authorize"}
                </button>
              </div>
            </div>
          )}

          {phase === "approved" && (
            <div className="mt-5 space-y-3 text-sm">
              <div className="rounded-md border border-emerald-700/60 bg-emerald-950/40 px-3 py-2 text-emerald-200">
                Authorized. You can return to your terminal.
              </div>
            </div>
          )}

          {phase === "denied" && (
            <div className="mt-5 space-y-3 text-sm">
              <div className="rounded-md border border-rose-700/60 bg-rose-950/40 px-3 py-2 text-rose-200">
                Request denied.
              </div>
            </div>
          )}

          {phase === "error" && (
            <div className="mt-5 space-y-3 text-sm">
              <div className="text-rose-400">{error ?? "Something went wrong."}</div>
              <button
                onClick={() => {
                  setError(null);
                  setPhase("input");
                  setParams({});
                }}
                className="rounded-md border border-slate-700 px-4 py-2 hover:bg-slate-800"
              >
                Enter a different code
              </button>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
