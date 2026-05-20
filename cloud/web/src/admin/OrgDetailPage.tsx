import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { PlatformOrgDetail } from "../api/types";
import { relativeTime } from "../lib/time";
import { describeError } from "./UsersPage";

export default function AdminOrgDetailPage() {
  const { id = "" } = useParams();
  const [o, setO] = useState<PlatformOrgDetail | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setErr(null);
    try {
      const r = await api<PlatformOrgDetail>(`/api/platform/orgs/${id}`);
      setO(r);
    } catch (e) {
      setErr(describeError(e));
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  const action = async (path: string, init?: RequestInit) => {
    setBusy(true);
    setErr(null);
    try {
      await api<void>(path, init);
      await load();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  };

  const onSuspend = () => {
    if (!o) return;
    if (!window.confirm(`Suspend org "${o.name}"? Writes will be blocked.`)) return;
    void action(`/api/platform/orgs/${id}/suspend`, { method: "POST" });
  };

  const onDelete = () => {
    if (!o) return;
    const phrase = `delete org ${o.slug}`;
    const got = window.prompt(`Type "${phrase}" to confirm hard-delete:`);
    if (got == null) return;
    void action(`/api/platform/orgs/${id}`, {
      method: "DELETE",
      body: JSON.stringify({ confirm: got }),
    });
  };

  if (err) return <div className="text-red-400">{err}</div>;
  if (!o) return <div className="text-slate-400">Loading…</div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <Link to="/admin/orgs" className="text-xs text-red-300 hover:underline">
            ← All orgs
          </Link>
          <h1 className="mt-1 text-2xl font-semibold">{o.name}</h1>
          <div className="text-sm text-slate-400">{o.slug}</div>
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            onClick={onSuspend}
            disabled={busy || !!o.suspendedAt}
            className="rounded-md border border-amber-700 bg-amber-950/40 px-3 py-1.5 text-sm text-amber-200 hover:bg-amber-950 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {o.suspendedAt ? "Suspended" : "Suspend"}
          </button>
          <button
            onClick={onDelete}
            disabled={busy}
            className="rounded-md border border-red-700 bg-red-900/60 px-3 py-1.5 text-sm text-white hover:bg-red-800"
          >
            Delete…
          </button>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat label="Members" value={o.memberCount} />
        <Stat label="Gateways" value={o.gatewayCount} />
        <Stat label="MCP servers" value={o.mcpServerCount} />
        <Stat label="Connections" value={o.connectionCount} />
      </div>

      <Section title="Members">
        {o.members.length === 0 ? (
          <Empty>No members.</Empty>
        ) : (
          <table className="w-full text-sm">
            <thead className="text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-4 py-2 text-left">Email</th>
                <th className="px-4 py-2 text-left">Display name</th>
                <th className="px-4 py-2 text-left">Role</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {o.members.map((m) => (
                <tr key={m.userId}>
                  <td className="px-4 py-2">
                    <Link
                      className="text-red-300 hover:underline"
                      to={`/admin/users/${m.userId}`}
                    >
                      {m.email}
                    </Link>
                  </td>
                  <td className="px-4 py-2 text-slate-300">{m.displayName || "—"}</td>
                  <td className="px-4 py-2 text-slate-400">{m.role}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Section>

      <Section title="Gateways">
        {o.gateways.length === 0 ? (
          <Empty>No gateways.</Empty>
        ) : (
          <table className="w-full text-sm">
            <thead className="text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-4 py-2 text-left">Name</th>
                <th className="px-4 py-2 text-left">Status</th>
                <th className="px-4 py-2 text-left">Last seen</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {o.gateways.map((g) => (
                <tr key={g.id}>
                  <td className="px-4 py-2 text-slate-200">{g.name}</td>
                  <td className="px-4 py-2 text-slate-400">{g.status}</td>
                  <td className="px-4 py-2 text-slate-400">
                    {g.lastSeenAt ? relativeTime(g.lastSeenAt) : "never"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Section>

      <Section title="MCP servers">
        {o.mcpServers.length === 0 ? (
          <Empty>No MCP servers.</Empty>
        ) : (
          <table className="w-full text-sm">
            <thead className="text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-4 py-2 text-left">Name</th>
                <th className="px-4 py-2 text-left">Exposure</th>
                <th className="px-4 py-2 text-left">Last seen</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {o.mcpServers.map((s) => (
                <tr key={s.id}>
                  <td className="px-4 py-2 text-slate-200">{s.name}</td>
                  <td className="px-4 py-2 text-slate-400">{s.exposureState}</td>
                  <td className="px-4 py-2 text-slate-400">{relativeTime(s.lastSeenAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Section>

      <Section title="Connections">
        {o.connections.length === 0 ? (
          <Empty>No connections.</Empty>
        ) : (
          <table className="w-full text-sm">
            <thead className="text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-4 py-2 text-left">Name</th>
                <th className="px-4 py-2 text-left">Status</th>
                <th className="px-4 py-2 text-left">OAuth</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {o.connections.map((c) => (
                <tr key={c.id}>
                  <td className="px-4 py-2 text-slate-200">{c.name}</td>
                  <td className="px-4 py-2 text-slate-400">{c.status}</td>
                  <td className="px-4 py-2 text-slate-400">{c.oauthStatus || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Section>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900/40 p-4">
      <div className="text-xs uppercase tracking-wide text-slate-400">{label}</div>
      <div className="mt-1 text-xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="overflow-x-auto rounded-xl border border-slate-800 bg-slate-900/40">
      <div className="border-b border-slate-800 px-4 py-3 text-sm font-semibold uppercase tracking-wide text-slate-400">
        {title}
      </div>
      {children}
    </div>
  );
}

function Empty({ children }: { children: React.ReactNode }) {
  return <div className="px-4 py-6 text-sm text-slate-500">{children}</div>;
}
