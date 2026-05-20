import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { ExposureState, MCPServerWithCounts } from "../api/types";
import { relativeTime } from "../lib/time";
import {
  EXPOSURE_BADGE_CLASS,
  EXPOSURE_DOT_CLASS,
  EXPOSURE_LABEL,
  EXPOSURE_STATES,
} from "../lib/exposure";
import PageHelp from "../components/PageHelp";
import HelpTooltip from "../components/HelpTooltip";

const EXPOSURE_TERM: Record<ExposureState, string> = {
  public: "public_exposure",
  cloud_internal: "cloud_internal_exposure",
  internal: "internal_exposure",
  unreachable: "unreachable_exposure",
  unknown: "unknown_exposure",
};

type Filter = ExposureState | "all";

export default function MCPServersPage() {
  const { activeOrgId } = useAuth();
  const [servers, setServers] = useState<MCPServerWithCounts[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState<Filter>("all");

  useEffect(() => {
    if (!activeOrgId) return;
    api<MCPServerWithCounts[]>(`/api/orgs/${activeOrgId}/mcp-servers`)
      .then((s) => setServers(s ?? []))
      .finally(() => setLoading(false));
  }, [activeOrgId]);

  const counts = useMemo(() => {
    const out: Record<Filter, number> = {
      all: servers.length,
      public: 0,
      cloud_internal: 0,
      internal: 0,
      unreachable: 0,
      unknown: 0,
    };
    for (const s of servers) {
      out[s.exposureState] = (out[s.exposureState] ?? 0) + 1;
    }
    return out;
  }, [servers]);

  const visible = useMemo(
    () => (filter === "all" ? servers : servers.filter((s) => s.exposureState === filter)),
    [servers, filter],
  );

  return (
    <div className="space-y-6">
      <div>
        <div className="flex items-center gap-2">
          <h1 className="text-2xl font-semibold">MCP Servers</h1>
          <PageHelp slug="activity-timeline" />
        </div>
        <p className="mt-1 text-sm text-slate-500">
          Every MCP server reported by one of your gateways.
        </p>
      </div>

      <div className="flex flex-wrap gap-2">
        <FilterChip
          label={`All (${counts.all})`}
          active={filter === "all"}
          onClick={() => setFilter("all")}
        />
        {EXPOSURE_STATES.map((st) => (
          <FilterChip
            key={st}
            label={`${EXPOSURE_LABEL[st]} (${counts[st]})`}
            active={filter === st}
            badgeClass={EXPOSURE_DOT_CLASS[st]}
            onClick={() => setFilter(st)}
          />
        ))}
      </div>

      <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
            <tr>
              <th className="px-4 py-3">Name</th>
              <th className="px-4 py-3">Address</th>
              <th className="px-4 py-3">Exposure</th>
              <th className="px-4 py-3">Transport</th>
              <th className="px-4 py-3">Capabilities</th>
              <th className="px-4 py-3">Source</th>
              <th className="px-4 py-3">Last seen</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-200">
            {loading && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {!loading && visible.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-slate-500">
                  {servers.length === 0
                    ? "No MCP servers reported yet."
                    : "No servers match this filter."}
                </td>
              </tr>
            )}
            {visible.map((s) => (
              <tr key={s.id} className="hover:bg-slate-50">
                <td className="px-4 py-3">
                  <Link to={`/app/mcp-servers/${s.id}`} className="text-brand-600 hover:underline">
                    {s.name}
                  </Link>
                </td>
                <td className="px-4 py-3 text-slate-500">{s.address || "—"}</td>
                <td className="px-4 py-3">
                  <ExposureBadge state={s.exposureState} />
                </td>
                <td className="px-4 py-3 text-slate-500">{s.transport || "—"}</td>
                <td className="px-4 py-3 text-slate-500">{s.capabilityCount}</td>
                <td className="px-4 py-3 text-slate-500">
                  {s.gatewayId ? (
                    <Link to={`/app/gateways/${s.gatewayId}`} className="hover:underline">
                      <span className="text-xs text-slate-500">gateway / </span>
                      {s.gatewayName}
                    </Link>
                  ) : s.connectionId ? (
                    <Link to={`/app/mcp-connections`} className="hover:underline">
                      <span className="text-xs text-slate-500">connection / </span>
                      {s.connectionName}
                    </Link>
                  ) : (
                    "—"
                  )}
                </td>
                <td className="px-4 py-3 text-slate-500">{relativeTime(s.lastSeenAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function FilterChip({
  label,
  active,
  badgeClass,
  onClick,
}: {
  label: string;
  active: boolean;
  badgeClass?: string;
  onClick: () => void;
}) {
  const base = active
    ? "border-brand-500 bg-brand-500/10 text-brand-700"
    : "border-slate-300 bg-white text-slate-600 hover:border-slate-500";
  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs ${base}`}
    >
      {badgeClass && <span className={`inline-block h-2 w-2 rounded-full ${badgeClass}`} />}
      {label}
    </button>
  );
}

function ExposureBadge({ state }: { state: ExposureState }) {
  return (
    <HelpTooltip term={EXPOSURE_TERM[state]}>
      <span
        className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs ${EXPOSURE_BADGE_CLASS[state]}`}
      >
        {EXPOSURE_LABEL[state]}
      </span>
    </HelpTooltip>
  );
}
