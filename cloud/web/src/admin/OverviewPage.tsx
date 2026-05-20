import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type {
  PlatformOverview,
  PlatformAuditEntry,
  PlatformAuditListResp,
} from "../api/types";
import { relativeTime } from "../lib/time";

export default function AdminOverviewPage() {
  const [o, setO] = useState<PlatformOverview | null>(null);
  const [audit, setAudit] = useState<PlatformAuditEntry[]>([]);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    api<PlatformOverview>("/api/platform/overview")
      .then(setO)
      .catch((e) => setErr(String(e?.message ?? e)));
    api<PlatformAuditListResp>("/api/platform/audit?limit=10")
      .then((r) => setAudit(r.items ?? []))
      .catch(() => setAudit([]));
  }, []);

  if (err) {
    return <div className="text-red-400">Failed to load overview: {err}</div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Platform Overview</h1>
        <p className="mt-1 text-sm text-slate-400">
          Cross-tenant aggregate metrics. All counts are global.
        </p>
      </div>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat
          label="Users"
          value={o?.users.total}
          sub={
            o
              ? `${o.users.active30d} active 30d · ${o.users.newLast7d} new 7d`
              : undefined
          }
        />
        <Stat
          label="Orgs"
          value={o?.orgs.total}
          sub={o ? `${o.orgs.newLast7d} new 7d` : undefined}
        />
        <Stat
          label="Gateways"
          value={o?.gateways.total}
          sub={o ? `${o.gateways.online} online` : undefined}
        />
        <Stat
          label="MCP servers"
          value={o?.mcpServers.total}
          sub={
            o
              ? `${o.mcpServers.publicExposure} publicly exposed`
              : undefined
          }
        />
        <Stat
          label="Capabilities"
          value={o?.capabilities.total}
          sub={
            o
              ? `${o.capabilities.byKind.tool} tools · ${o.capabilities.byKind.resource} resources · ${o.capabilities.byKind.prompt} prompts`
              : undefined
          }
        />
        <Stat
          label="Invocations 24h"
          value={o?.invocations.last24h}
          sub={
            o
              ? `${o.invocations.denied24h} denied · ${o.invocations.last7d} last 7d`
              : undefined
          }
        />
        <Stat
          label="Connections"
          value={o?.connections.total}
          sub={
            o
              ? `${o.connections.oauthPending} pending · ${o.connections.needsReauth} reauth`
              : undefined
          }
        />
        <Stat
          label="Callers"
          value={o?.callers.total}
          sub={o ? `${o.callers.flaggedNew} flagged new` : undefined}
        />
      </div>

      <div className="rounded-xl border border-slate-800 bg-slate-900/40">
        <div className="flex items-center justify-between border-b border-slate-800 px-4 py-3">
          <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-400">
            Recent audit activity
          </h2>
          <Link to="/admin/audit" className="text-xs text-red-300 hover:underline">
            See all
          </Link>
        </div>
        {audit.length === 0 ? (
          <div className="px-4 py-6 text-sm text-slate-500">
            No platform actions yet.
          </div>
        ) : (
          <ul className="divide-y divide-slate-800">
            {audit.map((e) => (
              <li key={e.id} className="grid grid-cols-[1fr_auto] gap-2 px-4 py-2 text-sm">
                <div className="truncate">
                  <span className="text-slate-200">{e.actorEmail || "system"}</span>{" "}
                  <span className="rounded bg-red-950 px-1.5 py-0.5 font-mono text-xs text-red-300">
                    {e.action}
                  </span>{" "}
                  <span className="text-slate-500">→ {e.targetKind}</span>
                </div>
                <div className="text-xs text-slate-500" title={e.at}>
                  {relativeTime(e.at)}
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function Stat({
  label,
  value,
  sub,
}: {
  label: string;
  value: number | undefined;
  sub?: string;
}) {
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900/40 p-5">
      <div className="text-xs uppercase tracking-wide text-slate-400">{label}</div>
      <div className="mt-1 text-3xl font-semibold tabular-nums">
        {value ?? "—"}
      </div>
      {sub && <div className="mt-1 text-xs text-slate-500">{sub}</div>}
    </div>
  );
}
