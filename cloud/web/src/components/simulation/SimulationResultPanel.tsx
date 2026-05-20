import type {
  PolicySimulationBucket,
  PolicySimulationResult,
} from "../../api/types";
import { relativeTime } from "../../lib/time";

type Props = {
  result: PolicySimulationResult;
  // mode "pre" frames the big number as "would block N of M"; mode "post"
  // adds the "approximated from the audit log" footnote so admins know we're
  // not replaying real gateway state.
  mode?: "pre" | "post";
  rangeLabel: string;
};

// SimulationResultPanel renders one simulation outcome. Used by both
// pre-mortem (PoliciesPage new-policy modal) and post-mortem (PolicyDetailPage
// "Simulate" panel). All numbers come straight off the wire — no client-side
// computation.
export default function SimulationResultPanel({ result, mode = "pre", rangeLabel }: Props) {
  const verb = mode === "post" ? "would have blocked" : "would block";
  const matchCount = result.wouldBlockCount + result.wouldWarnCount;
  return (
    <div className="space-y-4 rounded-xl border border-slate-200 bg-white p-5">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span className="text-3xl font-semibold text-slate-900">{matchCount.toLocaleString()}</span>
        <span className="text-sm text-slate-600">
          {verb} <strong className="font-semibold">{matchCount.toLocaleString()}</strong>{" "}
          of {result.totalInvocations.toLocaleString()} invocations in the last {rangeLabel}
        </span>
        {result.wouldWarnCount > 0 && (
          <span className="rounded-md border border-amber-300 bg-amber-50 px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide text-amber-700">
            warn
          </span>
        )}
      </div>

      {result.truncated && (
        <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800">
          Result truncated at 50,000 invocations. The estimate above is from the most recent slice;
          actual count over the full range may be higher.
        </div>
      )}

      {mode === "post" && (
        <p className="text-xs text-slate-500">
          Computed from the audit log; treats each historical invocation as if the policy had been
          live at the time. Real-time enforcement decisions can differ when the shim's local
          context (workload, network) was unavailable at observation time.
        </p>
      )}

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <BreakdownCard title="By server" rows={result.breakdownByServer} />
        <BreakdownCard title="By capability" rows={result.breakdownByCapability} />
        <BreakdownCard title="By caller" rows={result.breakdownByCaller} />
      </div>

      <div className="rounded-md border border-slate-200">
        <div className="border-b border-slate-200 bg-slate-50 px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-slate-600">
          Sample matches ({result.samples.length})
        </div>
        {result.samples.length === 0 ? (
          <div className="px-3 py-4 text-sm text-slate-500">
            No matches in the selected range.
          </div>
        ) : (
          <table className="w-full text-sm">
            <thead className="bg-white text-left text-[11px] uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-3 py-1.5 font-medium">Server</th>
                <th className="px-3 py-1.5 font-medium">Capability</th>
                <th className="px-3 py-1.5 font-medium">Caller</th>
                <th className="px-3 py-1.5 font-medium">When</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {result.samples.map((s) => (
                <tr key={s.invocationId}>
                  <td className="px-3 py-1.5 font-mono text-slate-700">{s.server || "—"}</td>
                  <td className="px-3 py-1.5 font-mono text-slate-700">{s.capability || "—"}</td>
                  <td className="px-3 py-1.5 text-slate-600">{s.caller || "(unknown)"}</td>
                  <td className="px-3 py-1.5 text-slate-500" title={s.timestamp}>
                    {relativeTime(s.timestamp)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="text-[11px] text-slate-400">
        Evaluated in {result.durationMs}ms.
      </div>
    </div>
  );
}

// BreakdownCard renders a single top-N bar list. Bars are scaled against the
// max count in this list (not the global match count), so a small list still
// reads clearly.
function BreakdownCard({ title, rows }: { title: string; rows: PolicySimulationBucket[] }) {
  const max = rows.reduce((m, r) => (r.count > m ? r.count : m), 0);
  return (
    <div className="rounded-md border border-slate-200 p-3">
      <h4 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-slate-500">
        {title}
      </h4>
      {rows.length === 0 ? (
        <div className="text-xs text-slate-400">No matches.</div>
      ) : (
        <ul className="space-y-1.5">
          {rows.slice(0, 5).map((r) => {
            const pct = max > 0 ? Math.round((r.count / max) * 100) : 0;
            return (
              <li key={r.name} className="text-xs">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="truncate font-mono text-slate-700" title={r.name}>
                    {r.name || "—"}
                  </span>
                  <span className="shrink-0 text-slate-500">{r.count.toLocaleString()}</span>
                </div>
                <div className="mt-1 h-1.5 w-full overflow-hidden rounded-sm bg-slate-100">
                  <div
                    className="h-full bg-[var(--accent)] opacity-70"
                    style={{ width: `${pct}%` }}
                  />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
