import { Link } from "react-router-dom";
import { useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type {
  DashboardCalloutsResp,
  DashboardHighlightsResp,
  DashboardKpisResp,
  DashboardKpiTile,
  DashboardRange,
  DashboardTimeseriesResp,
  Gateway,
} from "../api/types";
import PageHelp from "../components/PageHelp";
import KpiTile from "../components/dashboard/KpiTile";
import RangeSelector from "../components/dashboard/RangeSelector";
import InvocationTrendChart from "../components/dashboard/InvocationTrendChart";
import RiskCallouts from "../components/dashboard/RiskCallouts";
import Highlights from "../components/dashboard/Highlights";

const TILE_LABEL: Record<DashboardKpiTile["key"], string> = {
  posture: "Governance posture",
  footprint: "MCP footprint",
  invocations: "Invocations",
  denials: "Enforcement activity",
  publicExposure: "Exposure risk",
  activeCallers: "Active callers",
};

export default function OverviewPage() {
  const { activeOrgId } = useAuth();
  const [range, setRange] = useState<DashboardRange>("30d");
  const [kpis, setKpis] = useState<DashboardKpisResp | null>(null);
  const [series, setSeries] = useState<DashboardTimeseriesResp | null>(null);
  const [callouts, setCallouts] = useState<DashboardCalloutsResp | null>(null);
  const [highlights, setHighlights] = useState<DashboardHighlightsResp | null>(null);
  const [gateways, setGateways] = useState<Gateway[] | null>(null);

  useEffect(() => {
    if (!activeOrgId) return;
    api<Gateway[]>(`/api/orgs/${activeOrgId}/gateways`).then(setGateways).catch(() => setGateways([]));
  }, [activeOrgId]);

  useEffect(() => {
    if (!activeOrgId) return;
    let cancelled = false;
    setKpis(null);
    setSeries(null);
    setHighlights(null);
    api<DashboardKpisResp>(`/api/orgs/${activeOrgId}/dashboard/kpis?range=${range}`)
      .then((r) => !cancelled && setKpis(r))
      .catch(() => !cancelled && setKpis(null));
    api<DashboardTimeseriesResp>(
      `/api/orgs/${activeOrgId}/dashboard/timeseries?metric=invocations&range=${range}`,
    )
      .then((r) => !cancelled && setSeries(r))
      .catch(() => !cancelled && setSeries(null));
    api<DashboardHighlightsResp>(`/api/orgs/${activeOrgId}/dashboard/highlights?range=${range}`)
      .then((r) => !cancelled && setHighlights(r))
      .catch(() => !cancelled && setHighlights(null));
    return () => {
      cancelled = true;
    };
  }, [activeOrgId, range]);

  useEffect(() => {
    if (!activeOrgId) return;
    api<DashboardCalloutsResp>(`/api/orgs/${activeOrgId}/dashboard/callouts`)
      .then(setCallouts)
      .catch(() => setCallouts(null));
  }, [activeOrgId]);

  const hasGateways = (gateways?.length ?? 0) > 0;
  const updatedAt = kpis?.updatedAt;
  const updatedLabel = useMemo(() => {
    if (!updatedAt) return "—";
    const d = new Date(updatedAt);
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }, [updatedAt]);

  const tilesByKey = useMemo(() => {
    const m: Partial<Record<DashboardKpiTile["key"], DashboardKpiTile>> = {};
    kpis?.tiles.forEach((t) => (m[t.key] = t));
    return m;
  }, [kpis]);

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
            <PageHelp slug="getting-started" />
          </div>
          <p className="mt-1 text-sm text-slate-500">
            Executive view of MCP posture, enforcement, and traffic.
          </p>
          <p className="mt-1 text-[11px] uppercase tracking-wider text-slate-400">
            Last updated: {updatedLabel}
          </p>
        </div>
        <RangeSelector value={range} onChange={setRange} />
      </header>

      {!hasGateways && gateways !== null ? (
        <EmptyState />
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {(["posture", "footprint", "invocations", "denials", "publicExposure", "activeCallers"] as DashboardKpiTile["key"][]).map(
              (k) => {
                const t = tilesByKey[k];
                return (
                  <KpiTile
                    key={k}
                    label={TILE_LABEL[k]}
                    value={t ? formatValue(k, t.current, t.extra) : "—"}
                    sublabel={t ? sublabelFor(k, t, range) : undefined}
                    deltaPercent={t?.deltaPercent ?? 0}
                    higherIsBetter={t?.higherIsBetter ?? true}
                    sparkline={t?.sparkline ?? []}
                    rangeDays={kpis?.rangeDays ?? rangeToDays(range)}
                    accent={accentFor(k)}
                    progressMax={k === "posture" ? 100 : undefined}
                    progressValue={k === "posture" ? t?.current : undefined}
                  />
                );
              },
            )}
          </section>

          <section>
            <InvocationTrendChart data={series?.series ?? []} />
          </section>

          <section className="grid grid-cols-1 gap-4 lg:grid-cols-3">
            <div className="lg:col-span-1">
              <RiskCallouts data={callouts} />
            </div>
            <div className="lg:col-span-2">
              <Highlights data={highlights} />
            </div>
          </section>
        </>
      )}
    </div>
  );
}

function formatValue(
  key: DashboardKpiTile["key"],
  v: number,
  _extra?: Record<string, number | string>,
): string {
  if (key === "posture") return Math.round(v).toString();
  if (key === "footprint") return Math.round(v).toLocaleString();
  return Math.round(v).toLocaleString();
}

function sublabelFor(
  key: DashboardKpiTile["key"],
  t: DashboardKpiTile,
  range: DashboardRange,
): string | undefined {
  const e = t.extra ?? {};
  switch (key) {
    case "posture":
      return "out of 100";
    case "footprint": {
      const caps = (e.totalCapabilities as number) ?? 0;
      const newSrv = (e.newServers as number) ?? 0;
      return `${caps.toLocaleString()} capabilities · ${newSrv} new`;
    }
    case "invocations": {
      const a = (e.allowed as number) ?? 0;
      const au = (e.audited as number) ?? 0;
      const d = (e.denied as number) ?? 0;
      return `${a.toLocaleString()} allowed · ${au.toLocaleString()} audited · ${d.toLocaleString()} denied`;
    }
    case "denials":
      return `last ${range}`;
    case "publicExposure":
      return "internet-reachable servers";
    case "activeCallers": {
      const n = (e.newCallers as number) ?? 0;
      return `${n} new`;
    }
  }
}

function accentFor(key: DashboardKpiTile["key"]): string {
  switch (key) {
    case "posture":
      return "#0091e1";
    case "footprint":
      return "#6366f1";
    case "invocations":
      return "#10b981";
    case "denials":
      return "#f59e0b";
    case "publicExposure":
      return "#ef4444";
    case "activeCallers":
      return "#8b5cf6";
  }
}

function rangeToDays(r: DashboardRange): number {
  return r === "7d" ? 7 : r === "90d" ? 90 : 30;
}

function EmptyState() {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-10 text-center">
      <div className="mx-auto mb-4 h-12 w-12 rounded-full bg-brand-50 ring-1 ring-brand-300" />
      <h2 className="text-lg font-semibold">No gateways yet</h2>
      <p className="mx-auto mt-2 max-w-md text-sm text-slate-500">
        Install the Bright Guard shim on a host and it will start reporting the MCP
        servers it sees.
      </p>
      <Link
        to="/app/gateways"
        className="mt-5 inline-block rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-white hover:bg-brand-400"
      >
        Add a gateway
      </Link>
    </div>
  );
}
