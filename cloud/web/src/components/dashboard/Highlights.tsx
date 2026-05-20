import type { DashboardHighlightsResp } from "../../api/types";
import { relativeTime } from "../../lib/time";

type Props = {
  data: DashboardHighlightsResp | null;
};

export default function Highlights({ data }: Props) {
  if (!data) {
    return (
      <div className="rounded-2xl border border-slate-200 bg-white p-5">
        <div className="text-xs text-slate-400">Loading…</div>
      </div>
    );
  }
  const maxCap = Math.max(1, ...data.topCapabilities.map((c) => c.count));
  const maxCaller = Math.max(1, ...data.topCallers.map((c) => c.count));
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
      <Card title="Top capabilities">
        {data.topCapabilities.length === 0 ? (
          <Empty label="No invocations in this range" />
        ) : (
          <ul className="space-y-2">
            {data.topCapabilities.map((c) => (
              <li
                key={`${c.serverName}-${c.capabilityKind}-${c.capabilityName}`}
                className="flex items-center gap-3 text-sm"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium text-slate-800">
                    {c.capabilityName}
                  </div>
                  <div className="truncate text-[11px] text-slate-400">
                    {c.serverName} · {c.capabilityKind}
                  </div>
                </div>
                <div className="w-16 text-right tabular-nums text-xs text-slate-600">
                  {c.count.toLocaleString()}
                </div>
                <div className="h-1.5 w-20 overflow-hidden rounded-full bg-slate-100">
                  <div
                    className="h-full rounded-full bg-[var(--accent)]"
                    style={{ width: `${(c.count / maxCap) * 100}%` }}
                  />
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <Card title="Top callers">
        {data.topCallers.length === 0 ? (
          <Empty label="No callers in this range" />
        ) : (
          <ul className="space-y-2">
            {data.topCallers.map((c) => (
              <li key={c.signature} className="flex items-center gap-3 text-sm">
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium text-slate-800">
                    {c.label || "unknown"}
                  </div>
                </div>
                <div className="w-16 text-right tabular-nums text-xs text-slate-600">
                  {c.count.toLocaleString()}
                </div>
                <div className="h-1.5 w-20 overflow-hidden rounded-full bg-slate-100">
                  <div
                    className="h-full rounded-full bg-[var(--accent)]"
                    style={{ width: `${(c.count / maxCaller) * 100}%` }}
                  />
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <Card title="Recent denied">
        {data.recentDenied.length === 0 ? (
          <Empty label="No denies in this range" />
        ) : (
          <ul className="space-y-2">
            {data.recentDenied.map((d) => (
              <li key={d.id} className="text-xs">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate font-medium text-slate-800">
                    {d.capabilityName}
                  </span>
                  <span className="shrink-0 text-slate-400">
                    {relativeTime(d.at)}
                  </span>
                </div>
                <div className="truncate text-[11px] text-slate-500">
                  {d.serverName} · {labelForCaller(d.caller)}
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

function Card({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5">
      <h3 className="mb-3 text-sm font-semibold text-slate-700">{title}</h3>
      {children}
    </div>
  );
}

function Empty({ label }: { label: string }) {
  return <div className="text-xs text-slate-400">{label}</div>;
}

function labelForCaller(c: Record<string, unknown>): string {
  if (!c) return "unknown";
  for (const k of ["userEmail", "agent", "client", "name"]) {
    const v = c[k];
    if (typeof v === "string" && v.length > 0) return v;
  }
  return "unknown";
}
