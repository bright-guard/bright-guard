import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { Policy, PolicyAction } from "../api/types";
import { relativeTime } from "../lib/time";
import PageHelp from "../components/PageHelp";
import HelpTooltip from "../components/HelpTooltip";

const ACTION_CHIP: Record<PolicyAction, string> = {
  deny: "bg-rose-950/60 text-rose-300 border-rose-700/60",
  warn: "bg-amber-950/60 text-amber-300 border-amber-700/60",
};

export default function PoliciesPage() {
  const { activeOrgId } = useAuth();
  const [rows, setRows] = useState<Policy[]>([]);
  const [loading, setLoading] = useState(true);
  const [showNew, setShowNew] = useState(false);
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
        <button
          onClick={() => setShowNew(true)}
          className="rounded-md bg-[var(--accent)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90"
        >
          New policy
        </button>
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
          onClose={() => setShowNew(false)}
          onCreated={() => {
            setShowNew(false);
            reload();
          }}
        />
      )}
    </div>
  );
}

function NewPolicyModal({
  orgId,
  onClose,
  onCreated,
}: {
  orgId: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [expression, setExpression] = useState(`capability.name == "create_issue"`);
  const [action, setAction] = useState<PolicyAction>("deny");
  const [enabled, setEnabled] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

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

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-slate-900/40 p-4">
      <div className="w-full max-w-2xl rounded-xl border border-slate-200 bg-white p-6 shadow-xl">
        <h2 className="text-lg font-semibold">New policy</h2>
        <p className="mt-1 text-sm text-slate-500">
          CEL expression evaluated against observed invocations. Returns bool. Available
          variables: <code className="font-mono">caller</code>, <code className="font-mono">server</code>,
          <code className="font-mono"> capability</code>, <code className="font-mono">at</code>, <code className="font-mono">status</code>.
        </p>

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

          {error && (
            <pre className="whitespace-pre-wrap rounded-md border border-rose-300 bg-rose-50 p-3 font-mono text-xs text-rose-800">
              {error}
            </pre>
          )}
        </div>

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
            {submitting ? "Saving…" : "Create policy"}
          </button>
        </div>
      </div>
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
