import type { PolicySimulationResult } from "../../api/types";

type Props = {
  current: PolicySimulationResult;
  proposed: PolicySimulationResult;
};

// ComparisonDiff renders a side-by-side scoreboard of two simulation results
// + the deltas between them. The two simulations run over the same input set
// (the handler loads the org's window once and evaluates both expressions
// against it) so subtracting the counts is apples-to-apples.
export default function ComparisonDiff({ current, proposed }: Props) {
  const currMatches = current.wouldBlockCount + current.wouldWarnCount;
  const propMatches = proposed.wouldBlockCount + proposed.wouldWarnCount;
  const delta = propMatches - currMatches;
  const deltaSign = delta > 0 ? "+" : "";
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-5">
      <h4 className="mb-3 text-sm font-semibold text-slate-700">Comparison</h4>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Stat label="Current expression" value={currMatches} />
        <Stat label="Proposed expression" value={propMatches} />
        <Stat
          label="Net change"
          value={`${deltaSign}${delta.toLocaleString()}`}
          tone={delta > 0 ? "block-more" : delta < 0 ? "block-less" : "neutral"}
        />
      </div>
      <p className="mt-3 text-xs text-slate-500">
        Both expressions evaluated against the same {current.totalInvocations.toLocaleString()}{" "}
        invocations.
      </p>
    </div>
  );
}

type StatTone = "block-more" | "block-less" | "neutral";
function Stat({ label, value, tone }: { label: string; value: string | number; tone?: StatTone }) {
  const toneClass =
    tone === "block-more"
      ? "text-rose-700"
      : tone === "block-less"
      ? "text-emerald-700"
      : "text-slate-900";
  return (
    <div className="rounded-md border border-slate-200 p-3">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-slate-500">
        {label}
      </div>
      <div className={`mt-1 text-2xl font-semibold ${toneClass}`}>
        {typeof value === "number" ? value.toLocaleString() : value}
      </div>
    </div>
  );
}
