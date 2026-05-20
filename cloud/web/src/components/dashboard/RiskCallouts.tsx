import { Link } from "react-router-dom";
import type { DashboardCalloutsResp } from "../../api/types";

type Props = {
  data: DashboardCalloutsResp | null;
};

type Row = {
  label: string;
  count: number;
  to: string;
  severity: "high" | "medium" | "low";
};

const DOT: Record<Row["severity"], string> = {
  high: "bg-red-500",
  medium: "bg-amber-500",
  low: "bg-emerald-500",
};

export default function RiskCallouts({ data }: Props) {
  const rows: Row[] = data
    ? [
        {
          label: "public-exposure MCP servers",
          count: data.publicExposureServers,
          to: "/app/mcp-servers?exposure=public",
          severity: data.publicExposureServers > 0 ? "high" : "low",
        },
        {
          label: "callers flagged new and unacknowledged",
          count: data.flaggedNewCallers,
          to: "/app/callers?filter=new",
          severity:
            data.flaggedNewCallers === 0
              ? "low"
              : data.flaggedNewCallers >= 5
              ? "high"
              : "medium",
        },
        {
          label: "capabilities with no policy coverage",
          count: data.capabilitiesNoPolicy,
          to: "/app/policies",
          severity: data.capabilitiesNoPolicy === 0 ? "low" : "medium",
        },
      ]
    : [];
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5">
      <h3 className="mb-4 text-sm font-semibold text-slate-700">Risk callouts</h3>
      <ul className="space-y-3">
        {rows.length === 0 && (
          <li className="text-xs text-slate-400">Loading…</li>
        )}
        {rows.map((r) => (
          <li key={r.label}>
            <Link
              to={r.to}
              className="-mx-2 flex items-center justify-between rounded-lg px-2 py-2 transition-colors hover:bg-slate-50"
            >
              <div className="flex items-center gap-3">
                <span
                  className={`inline-block h-2.5 w-2.5 rounded-full ${DOT[r.severity]}`}
                  aria-hidden
                />
                <div>
                  <div className="text-2xl font-semibold tabular-nums text-slate-900">
                    {r.count.toLocaleString()}
                  </div>
                  <div className="text-xs text-slate-500">{r.label}</div>
                </div>
              </div>
              <svg
                width="14"
                height="14"
                viewBox="0 0 14 14"
                className="text-slate-300 group-hover:text-slate-500"
                aria-hidden
              >
                <path
                  d="M5 2 L10 7 L5 12"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
