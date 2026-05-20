import { useEffect, useState } from "react";
import { api, ApiError } from "../api/client";
import type { Session } from "../api/types";
import { relativeTime } from "../lib/time";

export default function SessionsPage() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const list = await api<Session[]>("/api/sessions");
      setSessions(list ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function revoke(id: string) {
    setBusyId(id);
    setError(null);
    try {
      await api<void>(`/api/sessions/${id}`, { method: "DELETE" });
      await load();
    } catch (err) {
      if (err instanceof ApiError && err.status === 400) {
        setError(typeof err.body === "string" ? err.body : "Cannot revoke this session.");
      } else {
        setError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setBusyId(null);
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Sessions</h1>
        <p className="mt-1 text-sm text-slate-400">
          Browser sessions and authorized CLIs that can act as you.
        </p>
      </div>

      {error && (
        <div className="rounded-md border border-rose-700/60 bg-rose-950/40 px-3 py-2 text-sm text-rose-200">
          {error}
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-900/40">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-900/60 text-left text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-3">Kind</th>
              <th className="px-4 py-3">Label</th>
              <th className="px-4 py-3">Created</th>
              <th className="px-4 py-3">Last seen</th>
              <th className="px-4 py-3">Expires</th>
              <th className="px-4 py-3"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {loading && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {!loading && sessions.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-500">
                  No sessions.
                </td>
              </tr>
            )}
            {sessions.map((s) => (
              <tr key={s.id} className="hover:bg-slate-900/40">
                <td className="px-4 py-3">
                  <span
                    className={`rounded-md px-2 py-0.5 text-xs ${
                      s.kind === "cli"
                        ? "bg-brand-900/40 text-brand-200"
                        : "bg-slate-800 text-slate-300"
                    }`}
                  >
                    {s.kind}
                  </span>
                </td>
                <td className="px-4 py-3 text-slate-200">{s.label || "—"}</td>
                <td className="px-4 py-3 text-slate-400">{relativeTime(s.createdAt)}</td>
                <td className="px-4 py-3 text-slate-400">{relativeTime(s.lastSeenAt)}</td>
                <td className="px-4 py-3 text-slate-400">
                  {new Date(s.expiresAt).toLocaleDateString()}
                </td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => revoke(s.id)}
                    disabled={busyId === s.id}
                    className="rounded-md border border-slate-700 px-3 py-1 text-xs text-slate-200 hover:bg-slate-800 disabled:opacity-50"
                  >
                    Revoke
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
