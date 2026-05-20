import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type {
  ActivityRow,
  Policy,
  PolicyAction,
  PolicySimulateResp,
} from "../api/types";
import { relativeTime } from "../lib/time";
import PageHelp from "../components/PageHelp";

const ACTION_CHIP: Record<PolicyAction, string> = {
  deny: "bg-rose-100 text-rose-700 border-rose-300",
  warn: "bg-amber-100 text-amber-700 border-amber-300",
};

// enforcementUseCase inspects the CEL source for the variable references that
// the shim turns into real denials at the gateway (vs the audit-only sweep).
// Returns the matched vision use case, or null when the policy is audit-only.
function enforcementUseCase(expression: string): string | null {
  if (/server\.exposure_state/.test(expression)) return "UC8";
  if (/caller\.flagged_new|caller\.acknowledged/.test(expression)) return "UC9";
  return null;
}

export default function PolicyDetailPage() {
  const { activeOrgId } = useAuth();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const [policy, setPolicy] = useState<Policy | null>(null);
  const [editExpr, setEditExpr] = useState("");
  const [editAction, setEditAction] = useState<PolicyAction>("deny");
  const [editEnabled, setEditEnabled] = useState(true);
  const [editName, setEditName] = useState("");
  const [recentMatches, setRecentMatches] = useState<ActivityRow[]>([]);
  const [simulating, setSimulating] = useState(false);
  const [simResult, setSimResult] = useState<PolicySimulateResp | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!activeOrgId || !id) return;
    api<Policy>(`/api/orgs/${activeOrgId}/policies/${id}`).then((p) => {
      setPolicy(p);
      setEditExpr(p.expression);
      setEditAction(p.action);
      setEditEnabled(p.enabled);
      setEditName(p.name);
    });
    // Pull a fresh activity slice and keep only rows whose decisions reference
    // this policy. Simpler than building a dedicated endpoint for MVP.
    api<{ items: ActivityRow[] }>(`/api/orgs/${activeOrgId}/activity?limit=200`).then((res) => {
      const matches = (res.items ?? []).filter((row) =>
        row.decisions?.some((d) => d.policyId === id),
      );
      setRecentMatches(matches);
    });
  }, [activeOrgId, id]);

  async function save() {
    if (!activeOrgId || !id) return;
    setSaving(true);
    setError(null);
    try {
      const updated = await api<Policy>(`/api/orgs/${activeOrgId}/policies/${id}`, {
        method: "PATCH",
        body: JSON.stringify({
          name: editName,
          expression: editExpr,
          action: editAction,
          enabled: editEnabled,
        }),
      });
      setPolicy(updated);
    } catch (err) {
      setError(extractError(err));
    } finally {
      setSaving(false);
    }
  }

  async function simulate() {
    if (!activeOrgId || !id) return;
    setSimulating(true);
    setError(null);
    try {
      const to = new Date().toISOString();
      const from = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
      const result = await api<PolicySimulateResp>(
        `/api/orgs/${activeOrgId}/policies/${id}/simulate`,
        {
          method: "POST",
          body: JSON.stringify({ expression: editExpr, from, to }),
        },
      );
      setSimResult(result);
    } catch (err) {
      setError(extractError(err));
    } finally {
      setSimulating(false);
    }
  }

  async function remove() {
    if (!activeOrgId || !id) return;
    if (!window.confirm(`Delete policy "${policy?.name}"?`)) return;
    await api(`/api/orgs/${activeOrgId}/policies/${id}`, { method: "DELETE" });
    navigate("/app/policies");
  }

  if (!policy) {
    return <div className="text-sm text-slate-500">Loading…</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-semibold">{policy.name}</h1>
            <PageHelp slug="policies/cel-primer" />
          </div>
          <p className="mt-1 text-sm text-slate-500">
            Created {relativeTime(policy.createdAt)} ·
            <span className={`ml-2 inline-flex rounded-md border px-2 py-0.5 text-xs uppercase ${ACTION_CHIP[policy.action]}`}>
              {policy.action}
            </span>
            {enforcementUseCase(policy.expression) && (
              <span
                className="ml-2 inline-flex rounded-md border border-violet-300 bg-violet-50 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-violet-700"
                title="The shim evaluates this policy locally and returns denied — not audit-only."
              >
                Enforces {enforcementUseCase(policy.expression)}
              </span>
            )}
          </p>
        </div>
        <button
          onClick={remove}
          className="rounded-md border border-rose-300 px-3 py-1.5 text-sm text-rose-600 hover:bg-rose-50"
        >
          Delete
        </button>
      </div>

      <section className="space-y-3 rounded-xl border border-slate-200 bg-white p-5">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-500">Definition</h2>
        <label className="block text-sm">
          <span className="text-slate-700">Name</span>
          <input
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
            className="mt-1 w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm"
          />
        </label>
        <label className="block text-sm">
          <span className="text-slate-700">Expression</span>
          <textarea
            value={editExpr}
            onChange={(e) => setEditExpr(e.target.value)}
            rows={5}
            spellCheck={false}
            className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 font-mono text-sm"
          />
        </label>
        <div className="flex items-center gap-4">
          <label className="text-sm">
            <span className="mr-2 text-slate-700">Action</span>
            <select
              value={editAction}
              onChange={(e) => setEditAction(e.target.value as PolicyAction)}
              className="rounded-md border border-slate-300 px-2 py-1 text-sm"
            >
              <option value="deny">Deny</option>
              <option value="warn">Warn</option>
            </select>
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={editEnabled} onChange={(e) => setEditEnabled(e.target.checked)} />
            Enabled
          </label>
          <button
            onClick={save}
            disabled={saving}
            className="ml-auto rounded-md bg-[var(--accent)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
          >
            {saving ? "Saving…" : "Save"}
          </button>
          <button
            onClick={simulate}
            disabled={simulating}
            className="rounded-md border border-slate-300 px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-50"
          >
            {simulating ? "Simulating…" : "Simulate against last 24h"}
          </button>
        </div>
        {error && (
          <pre className="whitespace-pre-wrap rounded-md border border-rose-300 bg-rose-50 p-3 font-mono text-xs text-rose-800">
            {error}
          </pre>
        )}
      </section>

      {simResult && (
        <section className="space-y-2 rounded-xl border border-slate-200 bg-white p-5">
          <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-500">
            Simulate result — {simResult.matches.length} of {simResult.scanned} invocations matched
          </h2>
          <ul className="divide-y divide-slate-100 text-sm">
            {simResult.matches.slice(0, 50).map((m) => (
              <li key={m.invocationId} className="py-2">
                <span className="font-mono text-slate-700">{m.serverName}</span> ·{" "}
                <span className="text-slate-500">{m.capabilityKind}</span>/
                <span className="font-mono">{m.capabilityName}</span> ·{" "}
                <span className="text-slate-500">{relativeTime(m.at)}</span>
              </li>
            ))}
            {simResult.matches.length === 0 && (
              <li className="py-3 text-slate-500">No matches in window.</li>
            )}
          </ul>
        </section>
      )}

      <section className="space-y-2 rounded-xl border border-slate-200 bg-white p-5">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-500">
          Recent matches ({recentMatches.length})
        </h2>
        <ul className="divide-y divide-slate-100 text-sm">
          {recentMatches.slice(0, 100).map((row) => (
            <li key={row.id} className="py-2">
              <span className="font-mono text-slate-700">{row.mcpServer.name}</span> ·{" "}
              <span className="text-slate-500">{row.capabilityKind}</span>/
              <span className="font-mono">{row.capabilityName}</span> ·{" "}
              <span className="text-slate-500">{relativeTime(row.at)}</span>
            </li>
          ))}
          {recentMatches.length === 0 && (
            <li className="py-3 text-slate-500">
              No recorded matches yet — the policy sweep runs every 30s.
            </li>
          )}
        </ul>
      </section>
    </div>
  );
}

function extractError(err: unknown): string {
  if (err instanceof ApiError) {
    const body = err.body as { error?: { message?: string } } | null;
    if (body?.error?.message) {
      return body.error.message;
    }
  }
  return String(err);
}
