import Sparkline from "./Sparkline";

export type KpiTileProps = {
  label: string;
  value: string;
  sublabel?: string;
  deltaPercent: number;
  higherIsBetter: boolean;
  sparkline: number[];
  rangeDays: number;
  accent?: string;
  progressMax?: number;
  progressValue?: number;
};

function formatDelta(p: number): string {
  if (!isFinite(p)) return "—";
  const sign = p > 0 ? "+" : "";
  return `${sign}${p.toFixed(p % 1 === 0 ? 0 : 1)}%`;
}

function deltaTone(p: number, higherIsBetter: boolean): string {
  if (Math.abs(p) < 0.5) return "text-slate-500";
  const good = higherIsBetter ? p > 0 : p < 0;
  return good ? "text-emerald-600" : "text-red-600";
}

export default function KpiTile({
  label,
  value,
  sublabel,
  deltaPercent,
  higherIsBetter,
  sparkline,
  rangeDays,
  accent = "var(--accent)",
  progressMax,
  progressValue,
}: KpiTileProps) {
  return (
    <div className="group relative rounded-2xl border border-slate-200 bg-white p-5 transition-shadow hover:shadow-md">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[11px] font-medium uppercase tracking-wide text-slate-500">
            {label}
          </div>
          <div className="mt-2 text-4xl font-bold tabular-nums leading-tight text-slate-900">
            {value}
          </div>
          {sublabel && (
            <div className="mt-1 truncate text-xs text-slate-500">{sublabel}</div>
          )}
        </div>
        <div className="shrink-0">
          <Sparkline
            values={sparkline}
            color={accent}
            ariaLabel={`${label} sparkline, last ${rangeDays} days`}
          />
        </div>
      </div>
      <div className="mt-3 flex items-center justify-between gap-2">
        <span
          className={`inline-flex items-center gap-1 text-xs font-medium ${deltaTone(
            deltaPercent,
            higherIsBetter,
          )}`}
        >
          <DeltaArrow positive={deltaPercent > 0} />
          {formatDelta(deltaPercent)}
        </span>
        <span className="text-[10px] uppercase tracking-wider text-slate-400">
          vs prior {rangeDays}d
        </span>
      </div>
      {progressMax !== undefined && progressValue !== undefined && (
        <div className="mt-3 h-1.5 w-full overflow-hidden rounded-full bg-slate-100">
          <div
            className="h-full rounded-full transition-all"
            style={{
              width: `${Math.min(100, Math.max(0, (progressValue / progressMax) * 100))}%`,
              background: accent,
            }}
          />
        </div>
      )}
    </div>
  );
}

function DeltaArrow({ positive }: { positive: boolean }) {
  return (
    <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden>
      <path
        d={positive ? "M5 1 L9 8 L1 8 Z" : "M5 9 L1 2 L9 2 Z"}
        fill="currentColor"
      />
    </svg>
  );
}
