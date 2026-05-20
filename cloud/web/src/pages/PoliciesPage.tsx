import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type {
  Policy,
  PolicyAction,
  PolicySimulationRange,
  PolicySimulationResp,
  PolicyTemplate,
} from "../api/types";
import { relativeTime } from "../lib/time";
import PageHelp from "../components/PageHelp";
import HelpTooltip from "../components/HelpTooltip";
import SimulationResultPanel from "../components/simulation/SimulationResultPanel";
import SimulationRangeSelector from "../components/simulation/SimulationRangeSelector";

const RANGE_LABEL: Record<PolicySimulationRange, string> = {
  "7d": "7 days",
  "30d": "30 days",
  "90d": "90 days",
};

const ACTION_CHIP: Record<PolicyAction, string> = {
  deny: "bg-rose-950/60 text-rose-300 border-rose-700/60",
  warn: "bg-amber-950/60 text-amber-300 border-amber-700/60",
};

export default function PoliciesPage() {
  const { activeOrgId } = useAuth();
  const [rows, setRows] = useState<Policy[]>([]);
  const [loading, setLoading] = useState(true);
  const [showNew, setShowNew] = useState(false);
  const [showTemplates, setShowTemplates] = useState(false);
  const [seedExpression, setSeedExpression] = useState<string | undefined>(undefined);
  const [seedName, setSeedName] = useState<string | undefined>(undefined);
  const [seedAction, setSeedAction] = useState<PolicyAction | undefined>(undefined);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function reload() {
    if (!activeOrgId) return;
    setLoading(true);
    try {
      const list = await api<Policy[]>(`/api/orgs/${activeOrgId}/policies`);
      setRows(list ?? []);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeOrgId]);

  async function toggleEnabled(p: Policy) {
    if (!activeOrgId) return;
    setBusy(p.id);
    try {
      await api<Policy>(`/api/orgs/${activeOrgId}/policies/${p.id}`, {
        method: "PATCH",
        body: JSON.stringify({ enabled: !p.enabled }),
      });
      await reload();
    } catch (err) {
      setError(extractError(err));
    } finally {
      setBusy(null);
    }
  }

  async function remove(p: Policy) {
    if (!activeOrgId) return;
    if (!window.confirm(`Delete policy "${p.name}"? This also removes its recorded decisions.`)) {
      return;
    }
    setBusy(p.id);
    try {
      await api(`/api/orgs/${activeOrgId}/policies/${p.id}`, { method: "DELETE" });
      await reload();
    } catch (err) {
      setError(extractError(err));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-semibold">Policies</h1>
            <PageHelp slug="policies/cel-primer" />
          </div>
          <p className="mt-1 text-sm text-slate-400">
            <HelpTooltip term="cel">CEL</HelpTooltip> expressions evaluated against observed MCP invocations. Audit-mode —
            matching invocations are flagged in Activity but never blocked.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowTemplates(true)}
            className="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
          >
            Start from a template
          </button>
          <button
            onClick={() => {
              setSeedExpression(undefined);
              setSeedName(undefined);
              setSeedAction(undefined);
              setShowNew(true);
            }}
            className="rounded-md bg-[var(--accent)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90"
          >
            New policy
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-md border border-rose-900/60 bg-rose-950/40 px-4 py-3 text-sm text-rose-300">
          {error}
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
            <tr>
              <th className="px-4 py-3">Name</th>
              <th className="px-4 py-3">Action</th>
              <th className="px-4 py-3">Enabled</th>
              <th className="px-4 py-3 text-right">Last 24h matches</th>
              <th className="px-4 py-3">Updated</th>
              <th className="px-4 py-3" />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {loading && rows.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-8 text-center text-slate-500">Loading…</td></tr>
            )}
            {!loading && rows.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-10 text-center text-slate-500">
                No policies yet. Create one to start flagging invocations.
              </td></tr>
            )}
            {rows.map((p) => (
              <tr key={p.id} className="hover:bg-slate-50">
                <td className="px-4 py-3">
                  <Link to={`/app/policies/${p.id}`} className="font-medium text-[var(--accent)] hover:underline">
                    {p.name}
                  </Link>
                  {p.description && (
                    <div className="mt-0.5 text-xs text-slate-500">{p.description}</div>
                  )}
                  <div className="mt-1 font-mono text-[11px] text-slate-500">{p.expression}</div>
                </td>
                <td className="px-4 py-3">
                  <span className={`inline-flex rounded-md border px-2 py-0.5 text-xs uppercase ${ACTION_CHIP[p.action]}`}>
                    {p.action}
                  </span>
                </td>
                <td className="px-4 py-3">
                  <button
                    onClick={() => toggleEnabled(p)}
                    disabled={busy === p.id}
                    className={`rounded-md px-2 py-0.5 text-xs ${p.enabled ? "bg-emerald-100 text-emerald-700" : "bg-slate-100 text-slate-500"}`}
                  >
                    {p.enabled ? "Enabled" : "Disabled"}
                  </button>
                </td>
                <td className="px-4 py-3 text-right text-slate-700">{p.last24hMatches.toLocaleString()}</td>
                <td className="px-4 py-3 text-slate-500" title={p.updatedAt}>{relativeTime(p.updatedAt)}</td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => remove(p)}
                    disabled={busy === p.id}
                    className="text-xs text-rose-600 hover:underline"
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {showNew && (
        <NewPolicyModal
          orgId={activeOrgId!}
          initialExpression={seedExpression}
          initialName={seedName}
          initialAction={seedAction}
          onClose={() => setShowNew(false)}
          onCreated={() => {
            setShowNew(false);
            reload();
          }}
        />
      )}

      {showTemplates && (
        <TemplatesModal
          onClose={() => setShowTemplates(false)}
          onPick={(tpl) => {
            setSeedExpression(tpl.expression);
            setSeedName(tpl.name);
            setSeedAction(tpl.action);
            setShowTemplates(false);
            setShowNew(true);
          }}
        />
      )}
    </div>
  );
}

// TemplatesModal lists starter CEL policies from /api/policy/templates and
// hands the chosen one back to the parent so the NewPolicyModal opens with
// the template pre-filled. Two ships in Wave N+8 (UC8 + UC9); list is static
// today, so a single fetch on mount is sufficient.
function TemplatesModal({
  onClose,
  onPick,
}: {
  onClose: () => void;
  onPick: (tpl: PolicyTemplate) => void;
}) {
  const [items, setItems] = useState<PolicyTemplate[] | null>(null);
  useEffect(() => {
    api<{ items: PolicyTemplate[] }>(`/api/policy/templates`)
      .then((r) => setItems(r.items ?? []))
      .catch(() => setItems([]));
  }, []);

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-slate-900/40 p-4">
      <div className="w-full max-w-2xl rounded-xl border border-slate-200 bg-white p-6 shadow-xl">
        <div className="flex items-start justify-between">
          <div>
            <h2 className="text-lg font-semibold">Start from a template</h2>
            <p className="mt-1 text-sm text-slate-500">
              One-click starter policies. Both enforce in real-time at the gateway
              (not audit-only): UC8 blocks public-exposure servers, UC9 blocks
              unapproved callers.
            </p>
          </div>
          <button onClick={onClose} className="text-sm text-slate-500 hover:text-slate-700">
            Close
          </button>
        </div>

        <div className="mt-4 space-y-3">
          {items === null && <div className="text-sm text-slate-500">Loading…</div>}
          {items && items.length === 0 && (
            <div className="text-sm text-slate-500">No templates available.</div>
          )}
          {items?.map((tpl) => (
            <div
              key={tpl.id}
              className="rounded-md border border-slate-200 p-4 hover:border-[var(--accent)]"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <h3 className="text-sm font-semibold text-slate-900">{tpl.name}</h3>
                    {tpl.useCase && (
                      <span className="rounded-md border border-violet-300 bg-violet-50 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-violet-700">
                        Enforces {tpl.useCase}
                      </span>
                    )}
                  </div>
                  <p className="mt-1 text-sm text-slate-600">{tpl.description}</p>
                  <pre className="mt-2 overflow-x-auto rounded-md bg-slate-50 p-2 font-mono text-[11px] text-slate-700">
                    {tpl.expression}
                  </pre>
                </div>
                <button
                  onClick={() => onPick(tpl)}
                  className="shrink-0 rounded-md bg-[var(--accent)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90"
                >
                  Use template
                </button>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function NewPolicyModal({
  orgId,
  initialExpression,
  initialName,
  initialAction,
  onClose,
  onCreated,
}: {
  orgId: string;
  initialExpression?: string;
  initialName?: string;
  initialAction?: PolicyAction;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState(initialName ?? "");
  const [description, setDescription] = useState("");
  const [expression, setExpression] = useState(
    initialExpression ?? `capability.name == "create_issue"`,
  );
  const [action, setAction] = useState<PolicyAction>(initialAction ?? "deny");
  const [enabled, setEnabled] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Pre-mortem simulator state. Tab "author" is the original form; tab
  // "simulate" runs the engine against recent traffic and shows the impact.
  const [tab, setTab] = useState<"author" | "simulate">("author");
  const [simRange, setSimRange] = useState<PolicySimulationRange>("30d");
  const [simResult, setSimResult] = useState<PolicySimulationResp | null>(null);
  const [simulating, setSimulating] = useState(false);

  async function submit() {
    setSubmitting(true);
    setError(null);
    try {
      await api<Policy>(`/api/orgs/${orgId}/policies`, {
        method: "POST",
        body: JSON.stringify({ name, description, expression, action, enabled }),
      });
      onCreated();
    } catch (err) {
      setError(extractError(err));
    } finally {
      setSubmitting(false);
    }
  }

  async function runSimulation() {
    setSimulating(true);
    setError(null);
    try {
      const result = await api<PolicySimulationResp>(
        `/api/orgs/${orgId}/policies/simulate`,
        {
          method: "POST",
          body: JSON.stringify({ expression, action, range: simRange }),
        },
      );
      setSimResult(result);
    } catch (err) {
      setError(extractError(err));
    } finally {
      setSimulating(false);
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-slate-900/40 p-4">
      <div className="max-h-[90vh] w-full max-w-3xl overflow-y-auto rounded-xl border border-slate-200 bg-white p-6 shadow-xl">
        <h2 className="text-lg font-semibold">New policy</h2>
        <p className="mt-1 text-sm text-slate-500">
          CEL expression evaluated against observed invocations. Returns bool.
        </p>
        <CELVariablesHint />

        {/* Tab strip — author vs simulate. Stays mounted to preserve form
            state when the operator flips between Author and Simulate. */}
        <div className="mt-4 inline-flex rounded-lg border border-slate-200 bg-slate-50 p-0.5">
          {(["author", "simulate"] as const).map((t) => {
            const active = t === tab;
            return (
              <button
                key={t}
                type="button"
                onClick={() => setTab(t)}
                aria-pressed={active}
                className={
                  "px-3 py-1 text-xs font-medium transition-colors " +
                  (active
                    ? "rounded-md bg-white text-slate-900 shadow-sm"
                    : "text-slate-600 hover:text-slate-900")
                }
              >
                {t === "author" ? "Author" : "Simulate"}
              </button>
            );
          })}
        </div>

        {tab === "author" && (
          <div className="mt-4 space-y-4">
            <label className="block text-sm">
              <span className="text-slate-700">Name</span>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="block create_issue on github-mcp"
                className="mt-1 w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm focus:border-[var(--accent)] focus:outline-none"
              />
            </label>

            <label className="block text-sm">
              <span className="text-slate-700">Description</span>
              <input
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                className="mt-1 w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm focus:border-[var(--accent)] focus:outline-none"
              />
            </label>

            <div className="grid grid-cols-2 gap-4">
              <label className="block text-sm">
                <span className="text-slate-700">Action</span>
                <select
                  value={action}
                  onChange={(e) => setAction(e.target.value as PolicyAction)}
                  className="mt-1 w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm focus:border-[var(--accent)] focus:outline-none"
                >
                  <option value="deny">Deny (audit)</option>
                  <option value="warn">Warn (audit)</option>
                </select>
              </label>
              <label className="mt-6 flex items-center gap-2 text-sm text-slate-700">
                <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
                Enabled
              </label>
            </div>

            <label className="block text-sm">
              <span className="text-slate-700">Expression</span>
              <textarea
                value={expression}
                onChange={(e) => setExpression(e.target.value)}
                rows={5}
                spellCheck={false}
                className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 font-mono text-sm focus:border-[var(--accent)] focus:outline-none"
              />
            </label>
          </div>
        )}

        {tab === "simulate" && (
          <div className="mt-4 space-y-4">
            <div className="flex items-center justify-between gap-3">
              <p className="text-sm text-slate-600">
                Dry-run this expression against the org's recent observed traffic. No policy is
                created or persisted.
              </p>
              <SimulationRangeSelector
                value={simRange}
                onChange={(r) => {
                  setSimRange(r);
                  setSimResult(null);
                }}
                disabled={simulating}
              />
            </div>

            <label className="block text-sm">
              <span className="text-slate-700">Expression</span>
              <textarea
                value={expression}
                onChange={(e) => {
                  setExpression(e.target.value);
                  setSimResult(null);
                }}
                rows={4}
                spellCheck={false}
                className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 font-mono text-sm focus:border-[var(--accent)] focus:outline-none"
              />
            </label>

            <div className="flex justify-start">
              <button
                onClick={runSimulation}
                disabled={simulating || !expression.trim()}
                className="rounded-md bg-[var(--accent)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
              >
                {simulating ? "Running…" : "Run simulation"}
              </button>
            </div>

            {simResult && (
              <SimulationResultPanel
                result={simResult}
                mode="pre"
                rangeLabel={RANGE_LABEL[simRange]}
              />
            )}
          </div>
        )}

        {error && (
          <pre className="mt-4 whitespace-pre-wrap rounded-md border border-rose-300 bg-rose-50 p-3 font-mono text-xs text-rose-800">
            {error}
          </pre>
        )}

        <div className="mt-6 flex justify-end gap-2">
          <button
            onClick={onClose}
            disabled={submitting}
            className="rounded-md border border-slate-300 px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-50"
          >
            Cancel
          </button>
          <button
            onClick={submit}
            disabled={submitting || !name.trim() || !expression.trim()}
            className="rounded-md bg-[var(--accent)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
          >
            {submitting ? "Saving…" : tab === "simulate" && simResult ? "Looks good — save policy" : "Create policy"}
          </button>
        </div>
      </div>
    </div>
  );
}

// CELVariablesHint renders the per-scope variable reference shown next to the
// CEL editor on the new-policy and policy-detail pages. Exported as a named
// export so PolicyDetailPage can pull the same shape without duplicating the
// markup. Kept in this file because it's authored once, surfaced twice; a
// dedicated component file would be over-organised for ~30 lines of JSX.
export function CELVariablesHint() {
  const [open, setOpen] = useState(false);
  return (
    <div className="mt-2 rounded-md border border-slate-200 bg-slate-50 text-xs">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between px-3 py-1.5 text-left text-slate-700"
      >
        <span className="font-semibold">Available variables</span>
        <span className="text-slate-400">{open ? "Hide" : "Show"}</span>
      </button>
      {open && (
        <ul className="space-y-1 border-t border-slate-200 px-3 py-2 font-mono text-[11px] text-slate-700">
          <li>server.&#123;id, name, address, exposure_state, transport&#125;</li>
          <li>caller.&#123;signature, label, flagged_new, acknowledged&#125;</li>
          <li>capability.&#123;kind, name, description&#125;</li>
          <li>
            workload.&#123;host, cluster, namespace, agent_class&#125;{" "}
            <span className="text-emerald-600">NEW</span>
          </li>
          <li>
            network.&#123;subnet, vpc, zone, caller_ip&#125;{" "}
            <span className="text-emerald-600">NEW</span>
          </li>
          <li>request.now (timestamp)</li>
        </ul>
      )}
    </div>
  );
}

function extractError(err: unknown): string {
  if (err instanceof ApiError) {
    const body = err.body as { error?: { message?: string; code?: string } } | null;
    if (body?.error?.message) {
      return body.error.message;
    }
  }
  return String(err);
}
