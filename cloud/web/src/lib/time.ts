export function relativeTime(iso: string | null | undefined): string {
  if (!iso) return "never";
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "—";
  const diff = Date.now() - t;
  const s = Math.round(diff / 1000);
  if (s < 5) return "just now";
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.round(h / 24);
  return `${d}d ago`;
}

const onlineWindowMs = 5 * 60 * 1000;

export function isOnline(lastSeen: string | null | undefined, status?: string) {
  if (status === "revoked") return false;
  if (!lastSeen) return false;
  return Date.now() - new Date(lastSeen).getTime() < onlineWindowMs;
}
