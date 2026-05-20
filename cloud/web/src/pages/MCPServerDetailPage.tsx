import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { ExposureState, MCPCapability, MCPServerDetail } from "../api/types";
import { relativeTime } from "../lib/time";
import { EXPOSURE_BADGE_CLASS, EXPOSURE_LABEL } from "../lib/exposure";

const KIND_ORDER = ["tool", "resource", "prompt"];

export default function MCPServerDetailPage() {
  const { id } = useParams();
  const { activeOrgId } = useAuth();
  const [detail, setDetail] = useState<MCPServerDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reclassifying, setReclassifying] = useState(false);
  const [togglingId, setTogglingId] = useState<string | null>(null);

  useEffect(() => {
    if (!activeOrgId || !id) return;
    api<MCPServerDetail>(`/api/orgs/${activeOrgId}/mcp-servers/${id}`)
      .then(setDetail)
      .catch((err) => setError(err instanceof ApiError ? `${err.status}` : String(err)));
  }, [activeOrgId, id]);

  const reclassify = async () => {
    if (!activeOrgId || !id) return;
    setReclassifying(true);
    try {
      const updated = await api<MCPServerDetail>(
        `/api/orgs/${activeOrgId}/mcp-servers/${id}/reclassify-exposure`,
        { method: "POST" },
      );
      setDetail(updated);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.status}` : String(err));
    } finally {
      setReclassifying(false);
    }
  };

  const toggleCapability = async (cap: MCPCapability) => {
    if (!activeOrgId || !id) return;
    setTogglingId(cap.id);
    try {
      const updated = await api<MCPServerDetail>(
        `/api/orgs/${activeOrgId}/mcp-servers/${id}/capabilities/${cap.id}`,
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ enabled: !cap.enabled }),
        },
      );
      setDetail(updated);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.status}` : String(err));
    } finally {
      setTogglingId(null);
    }
  };

  if (error) return <div className="text-rose-600">Error: {error}</div>;
  if (!detail) return <div className="text-slate-500">Loading…</div>;

  const byKind: Record<string, MCPCapability[]> = {};
  for (const c of detail.capabilities) {
    (byKind[c.kind] ??= []).push(c);
  }
  const kinds = Object.keys(byKind).sort(
    (a, b) =>
      (KIND_ORDER.indexOf(a) === -1 ? 999 : KIND_ORDER.indexOf(a)) -
      (KIND_ORDER.indexOf(b) === -1 ? 999 : KIND_ORDER.indexOf(b)),
  );

  return (
    <div className="space-y-6">
      <div>
        <div className="text-xs text-slate-500">
          <Link to="/app/mcp-servers" className="hover:underline">
            MCP Servers
          </Link>{" "}
          / {detail.name}
        </div>
        <h1 className="mt-1 text-2xl font-semibold">{detail.name}</h1>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-4">
        <Field label="Address" value={detail.address || "—"} />
        <Field label="Transport" value={detail.transport || "—"} />
        <Field label="Version" value={detail.version || "—"} />
        <Field label="Last seen" value={relativeTime(detail.lastSeenAt)} />
      </div>

      <div className="text-sm text-slate-500">
        {detail.gatewayId ? (
          <>
            Reported by gateway{" "}
            <Link to={`/app/gateways/${detail.gatewayId}`} className="text-brand-600 hover:underline">
              {detail.gatewayName}
            </Link>
          </>
        ) : detail.connectionId ? (
          <>
            Discovered via connection{" "}
            <Link to="/app/mcp-connections" className="text-brand-600 hover:underline">
              {detail.connectionName}
            </Link>
          </>
        ) : null}
      </div>

      <div className="rounded-xl border border-slate-200 bg-white p-5">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-500">
              Exposure
            </h2>
            <div className="mt-3 flex flex-wrap items-center gap-3">
              <ExposureBadge state={detail.exposureState} />
              <span className="text-sm text-slate-600">
                {detail.exposureReason || "no reason recorded"}
              </span>
            </div>
            <div className="mt-2 text-xs text-slate-500">
              Last classified{" "}
              {detail.exposureClassifiedAt
                ? relativeTime(detail.exposureClassifiedAt)
                : "never"}
            </div>
          </div>
          <button
            type="button"
            onClick={reclassify}
            disabled={reclassifying}
            className="inline-flex items-center rounded-md border border-slate-300 bg-slate-50 px-3 py-1.5 text-xs text-slate-900 hover:border-slate-500 disabled:opacity-50"
          >
            {reclassifying ? "Reclassifying…" : "Re-classify"}
          </button>
        </div>
      </div>

      <div className="space-y-5">
        {kinds.map((kind) => (
          <div key={kind}>
            <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-slate-500">
              {kind}s ({byKind[kind].length})
            </h2>
            <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
              <table className="min-w-full text-sm">
                <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
                  <tr>
                    <th className="px-4 py-3">Name</th>
                    <th className="px-4 py-3">Description</th>
                    <th className="px-4 py-3 text-right">Enabled</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-200">
                  {byKind[kind].map((c) => (
                    <tr key={c.id} className={c.enabled ? "" : "opacity-60"}>
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <span className="font-mono text-slate-900">{c.name}</span>
                          {!c.enabled && (
                            <span className="rounded-full bg-[#b71c1c]/10 px-2 py-0.5 text-[10px] uppercase tracking-wide text-rose-700">
                              DISABLED
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-slate-500">{c.description || "—"}</td>
                      <td className="px-4 py-3 text-right">
                        <CapabilityToggle
                          cap={c}
                          busy={togglingId === c.id}
                          onToggle={() => toggleCapability(c)}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        ))}
        {kinds.length === 0 && (
          <div className="rounded-xl border border-slate-200 bg-white p-6 text-sm text-slate-500">
            No capabilities reported yet.
          </div>
        )}
      </div>

      <div>
        <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-slate-500">
          Recent activity
        </h2>
        {detail.invocations.length === 0 ? (
          <div className="rounded-xl border border-slate-200 bg-white p-6 text-sm text-slate-500">
            No invocations recorded.
          </div>
        ) : (
          <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
            <table className="min-w-full text-sm">
              <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
                <tr>
                  <th className="px-4 py-3">When</th>
                  <th className="px-4 py-3">Capability</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Latency</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-200">
                {detail.invocations.map((inv) => (
                  <tr key={inv.id}>
                    <td className="px-4 py-3 text-slate-500">{relativeTime(inv.at)}</td>
                    <td className="px-4 py-3 font-mono text-slate-900">
                      <span className="text-slate-500">{inv.capabilityKind}/</span>
                      {inv.capabilityName}
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={`rounded-full px-2 py-0.5 text-xs ${
                          inv.status === "ok"
                            ? "bg-[#006128]/10 text-[#006128]"
                            : "bg-[#b71c1c]/10 text-rose-700"
                        }`}
                      >
                        {inv.status}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-slate-500">{inv.latencyMs}ms</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-sm text-slate-900">{value}</div>
    </div>
  );
}

function ExposureBadge({ state }: { state: ExposureState }) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs ${EXPOSURE_BADGE_CLASS[state]}`}
    >
      {EXPOSURE_LABEL[state]}
    </span>
  );
}

function CapabilityToggle({
  cap,
  busy,
  onToggle,
}: {
  cap: MCPCapability;
  busy: boolean;
  onToggle: () => void;
}) {
  const tip = !cap.enabled && cap.disabledAt
    ? `Disabled by ${cap.disabledByEmail || "(unknown)"} on ${new Date(cap.disabledAt).toLocaleString()}`
    : cap.enabled
      ? "Click to disable"
      : "Click to enable";
  return (
    <button
      type="button"
      onClick={onToggle}
      disabled={busy}
      title={tip}
      aria-pressed={cap.enabled}
      className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
        cap.enabled ? "bg-emerald-600" : "bg-slate-300"
      } disabled:opacity-50`}
    >
      <span
        className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
          cap.enabled ? "translate-x-4" : "translate-x-1"
        }`}
      />
    </button>
  );
}
