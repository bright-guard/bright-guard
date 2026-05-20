import type { DashboardTimeseriesPoint } from "../../api/types";

type Props = {
  data: DashboardTimeseriesPoint[];
  height?: number;
};

const COLORS = {
  allowed: "#10b981",
  audited: "#f59e0b",
  denied: "#ef4444",
};

// Stack labels in painted order (bottom -> top).
const STACK: Array<keyof typeof COLORS> = ["allowed", "audited", "denied"];

export default function InvocationTrendChart({ data, height = 240 }: Props) {
  // Render an SVG manually so we avoid a chart-library dep. The axis is
  // implicit; this is a hero chart, not a finance terminal.
  const width = 800;
  const padding = { top: 12, right: 16, bottom: 24, left: 36 };
  const innerW = width - padding.left - padding.right;
  const innerH = height - padding.top - padding.bottom;

  if (data.length === 0) {
    return (
      <div
        className="flex items-center justify-center rounded-xl border border-slate-200 bg-white text-sm text-slate-400"
        style={{ height }}
      >
        No data in this range yet.
      </div>
    );
  }

  const totals = data.map(
    (p) => (p.allowed ?? 0) + (p.audited ?? 0) + (p.denied ?? 0),
  );
  const max = Math.max(1, ...totals);
  const stepX = innerW / Math.max(data.length - 1, 1);

  // For each stack layer, build the upper polyline (cumulative). Then the
  // polygon for layer N is upper(N) + reverse(upper(N-1)).
  const cumByLayer: number[][] = [];
  let running = data.map(() => 0);
  for (const key of STACK) {
    const newRunning = running.map((v, i) => v + ((data[i] as unknown as Record<string, number | undefined>)[key] ?? 0));
    cumByLayer.push(newRunning);
    running = newRunning;
  }

  const yScale = (v: number) => padding.top + innerH - (v / max) * innerH;
  const xScale = (i: number) => padding.left + i * stepX;

  const areas = STACK.map((key, layerIdx) => {
    const upper = cumByLayer[layerIdx];
    const lower = layerIdx === 0 ? data.map(() => 0) : cumByLayer[layerIdx - 1];
    const top = upper.map((v, i) => `${xScale(i).toFixed(2)},${yScale(v).toFixed(2)}`).join(" ");
    const bottom = lower
      .map((v, i) => `${xScale(i).toFixed(2)},${yScale(v).toFixed(2)}`)
      .reverse()
      .join(" ");
    return { key, color: COLORS[key], points: `${top} ${bottom}` };
  });

  // X-axis date ticks: 6 evenly spaced. Tick label is "MMM d".
  const tickCount = Math.min(6, data.length);
  const ticks: { x: number; label: string }[] = [];
  for (let i = 0; i < tickCount; i++) {
    const idx = Math.round((i * (data.length - 1)) / Math.max(tickCount - 1, 1));
    const day = data[idx]?.day ?? "";
    ticks.push({ x: xScale(idx), label: formatDay(day) });
  }
  // Y axis: 4 ticks.
  const yTicks: number[] = [];
  for (let i = 0; i <= 4; i++) yTicks.push(Math.round((max * i) / 4));

  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-slate-700">
          Invocations by decision
        </h3>
        <div className="flex items-center gap-3 text-[11px] text-slate-500">
          {STACK.map((k) => (
            <span key={k} className="inline-flex items-center gap-1.5">
              <span
                className="inline-block h-2 w-2 rounded-full"
                style={{ background: COLORS[k] }}
              />
              <span className="capitalize">{k}</span>
            </span>
          ))}
        </div>
      </div>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        className="block h-[240px] w-full"
        role="img"
        aria-label="Stacked invocation count per day"
      >
        {yTicks.map((t, i) => {
          const y = yScale(t);
          return (
            <g key={i}>
              <line
                x1={padding.left}
                x2={width - padding.right}
                y1={y}
                y2={y}
                stroke="#e2e8f0"
                strokeDasharray="2 3"
              />
              <text
                x={padding.left - 6}
                y={y + 3}
                textAnchor="end"
                className="fill-slate-400 text-[10px]"
              >
                {t.toLocaleString()}
              </text>
            </g>
          );
        })}
        {areas.map((a) => (
          <polygon key={a.key} fill={a.color} fillOpacity={0.55} stroke="none" points={a.points} />
        ))}
        {ticks.map((t, i) => (
          <text
            key={i}
            x={t.x}
            y={height - 6}
            textAnchor="middle"
            className="fill-slate-400 text-[10px]"
          >
            {t.label}
          </text>
        ))}
      </svg>
    </div>
  );
}

function formatDay(d: string): string {
  if (!d) return "";
  const parts = d.split("-");
  if (parts.length !== 3) return d;
  const month = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"][
    parseInt(parts[1], 10) - 1
  ];
  return `${month ?? parts[1]} ${parseInt(parts[2], 10)}`;
}
