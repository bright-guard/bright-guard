import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { Gateway, GatewayCreateResp } from "../api/types";
import { isOnline, relativeTime } from "../lib/time";
import PageHelp from "../components/PageHelp";
import NoOrgEmptyState from "../components/NoOrgEmptyState";

export default function GatewaysPage() {
  const { activeOrgId } = useAuth();
  const [gateways, setGateways] = useState<Gateway[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);

  async function reload() {
    if (!activeOrgId) return;
    setLoading(true);
    try {
      const list = await api<Gateway[]>(`/api/orgs/${activeOrgId}/gateways`);
      setGateways(list ?? []);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeOrgId]);

  if (!activeOrgId) {
    return <NoOrgEmptyState />;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-semibold">Gateways</h1>
            <PageHelp slug="gateways/install" />
          </div>
          <p className="mt-1 text-sm text-slate-500">
            Hosts running the Bright Guard shim that report to this org.
          </p>
        </div>
        <button
          onClick={() => setShowCreate(true)}
          className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-white hover:bg-brand-400"
        >
          Add gateway
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
            <tr>
              <th className="w-12 px-4 py-3"></th>
              <th className="px-4 py-3">Name</th>
              <th className="px-4 py-3">Last seen</th>
              <th className="px-4 py-3">Description</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-200">
            {loading && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {!loading && gateways.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-slate-500">
                  No gateways yet.
                </td>
              </tr>
            )}
            {gateways.map((g) => {
              const online = isOnline(g.lastSeenAt, g.status);
              return (
                <tr key={g.id} className="hover:bg-slate-50">
                  <td className="px-4 py-3">
                    <span
                      className={`inline-block h-2.5 w-2.5 rounded-full ${
                        online ? "bg-emerald-400" : "bg-slate-400"
                      }`}
                      title={online ? "online" : g.status}
                    />
                  </td>
                  <td className="px-4 py-3">
                    <Link to={`/app/gateways/${g.id}`} className="text-brand-600 hover:underline">
                      {g.name}
                    </Link>
                  </td>
                  <td className="px-4 py-3 text-slate-500">{relativeTime(g.lastSeenAt)}</td>
                  <td className="px-4 py-3 text-slate-500">{g.description || "—"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {showCreate && (
        <CreateGatewayModal
          orgId={activeOrgId}
          onClose={() => {
            setShowCreate(false);
            reload();
          }}
        />
      )}
    </div>
  );
}

function CreateGatewayModal({
  orgId,
  onClose,
}: {
  orgId: string;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<GatewayCreateResp | null>(null);
  const [copied, setCopied] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const r = await api<GatewayCreateResp>(`/api/orgs/${orgId}/gateways`, {
        method: "POST",
        body: JSON.stringify({ name, description }),
      });
      setResult(r);
    } catch (err) {
      setError(err instanceof ApiError && typeof err.body === "string" ? err.body : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function copy() {
    if (!result) return;
    try {
      await navigator.clipboard.writeText(result.installCommand);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* ignore */
    }
  }

  return (
    <div
      className="fixed inset-0 z-30 grid place-items-center bg-black/60 px-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-xl rounded-2xl border border-slate-300 bg-white p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {!result ? (
          <>
            <h2 className="text-lg font-semibold">Add gateway</h2>
            <p className="mt-1 text-sm text-slate-500">
              We'll mint a one-time enrollment token and give you a docker run line.
            </p>
            <form onSubmit={submit} className="mt-5 space-y-4">
              <label className="block text-sm">
                <span className="mb-1 block text-slate-600">Name</span>
                <input
                  required
                  autoFocus
                  placeholder="prod-us-east"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm placeholder:text-slate-400 focus:border-brand-500 focus:outline-none"
                />
              </label>
              <label className="block text-sm">
                <span className="mb-1 block text-slate-600">Description (optional)</span>
                <input
                  placeholder="Production cluster, us-east-1"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm placeholder:text-slate-400 focus:border-brand-500 focus:outline-none"
                />
              </label>
              {error && <div className="text-sm text-rose-600">{error}</div>}
              <div className="flex justify-end gap-2 pt-2">
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded-md border border-slate-300 px-4 py-2 text-sm hover:bg-slate-100"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={busy || !name.trim()}
                  className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-white hover:bg-brand-400 disabled:opacity-50"
                >
                  {busy ? "Creating…" : "Create gateway"}
                </button>
              </div>
            </form>
          </>
        ) : (
          <>
            <h2 className="text-lg font-semibold">Gateway created</h2>
            <p className="mt-1 text-sm text-slate-500">
              Run this on the host you want to enroll. The enrollment token will not be shown again.
            </p>
            <div className="mt-4 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800">
              This token will not be shown again. Copy it now.
            </div>
            <pre className="mt-4 overflow-x-auto whitespace-pre-wrap break-all rounded-md border border-slate-300 bg-white p-3 text-xs text-slate-900">
              {result.installCommand}
            </pre>
            <div className="mt-4 flex justify-end gap-2">
              <button
                onClick={copy}
                className="rounded-md border border-slate-300 px-4 py-2 text-sm hover:bg-slate-100"
              >
                {copied ? "Copied" : "Copy"}
              </button>
              <button
                onClick={onClose}
                className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-white hover:bg-brand-400"
              >
                Done
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
