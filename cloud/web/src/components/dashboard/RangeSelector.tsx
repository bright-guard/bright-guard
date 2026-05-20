import type { DashboardRange } from "../../api/types";

const RANGES: DashboardRange[] = ["7d", "30d", "90d"];

const LABEL: Record<DashboardRange, string> = {
  "7d": "7 days",
  "30d": "30 days",
  "90d": "90 days",
};

type Props = {
  value: DashboardRange;
  onChange: (v: DashboardRange) => void;
};

export default function RangeSelector({ value, onChange }: Props) {
  return (
    <div className="inline-flex rounded-lg border border-slate-200 bg-white p-0.5 shadow-sm">
      {RANGES.map((r) => {
        const active = r === value;
        return (
          <button
            key={r}
            type="button"
            onClick={() => onChange(r)}
            aria-pressed={active}
            className={
              "px-3 py-1 text-xs font-medium transition-colors " +
              (active
                ? "rounded-md bg-slate-900 text-white"
                : "text-slate-600 hover:text-slate-900")
            }
          >
            {LABEL[r]}
          </button>
        );
      })}
    </div>
  );
}
