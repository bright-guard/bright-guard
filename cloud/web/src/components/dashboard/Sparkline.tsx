type Props = {
  values: number[];
  width?: number;
  height?: number;
  color?: string;
  fill?: string;
  ariaLabel?: string;
};

export default function Sparkline({
  values,
  width = 80,
  height = 24,
  color = "var(--accent)",
  fill,
  ariaLabel,
}: Props) {
  if (values.length === 0) {
    return <svg width={width} height={height} aria-hidden />;
  }
  const max = Math.max(...values, 0);
  const min = Math.min(...values, 0);
  const range = max - min || 1;
  const pad = 1;
  const innerW = width - pad * 2;
  const innerH = height - pad * 2;
  const stepX = innerW / Math.max(values.length - 1, 1);
  const points = values.map((v, i) => {
    const x = pad + i * stepX;
    const y = pad + innerH - ((v - min) / range) * innerH;
    return [x, y] as const;
  });
  const d = points
    .map(([x, y], i) => (i === 0 ? `M ${x.toFixed(2)} ${y.toFixed(2)}` : `L ${x.toFixed(2)} ${y.toFixed(2)}`))
    .join(" ");
  const area =
    `M ${points[0][0].toFixed(2)} ${(height - pad).toFixed(2)} ` +
    points.map(([x, y]) => `L ${x.toFixed(2)} ${y.toFixed(2)}`).join(" ") +
    ` L ${points[points.length - 1][0].toFixed(2)} ${(height - pad).toFixed(2)} Z`;
  const fillColor = fill ?? color;
  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={ariaLabel}
      className="overflow-visible"
    >
      <path d={area} fill={fillColor} fillOpacity={0.1} />
      <path
        d={d}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
