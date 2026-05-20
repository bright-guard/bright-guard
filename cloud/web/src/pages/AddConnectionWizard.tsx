import { useState } from "react";
import { api, ApiError } from "../api/client";
import type {
  MCPConnection,
  MCPConnectionAuthMethod,
  MCPConnectionTransport,
} from "../api/types";

type Step = 1 | 2 | 3;

export default function AddConnectionWizard({
  orgId,
  onClose,
  onDone,
}: {
  orgId: string;
  onClose: () => void;
  onDone: () => void;
}) {
  const [step, setStep] = useState<Step>(1);

  const [name, setName] = useState("");
  const [endpointUrl, setEndpointUrl] = useState("");
  const [transport, setTransport] = useState<MCPConnectionTransport>("streamable-http");

  const [authMethod, setAuthMethod] = useState<MCPConnectionAuthMethod>("bearer");
  const [headerName, setHeaderName] = useState("X-Api-Key");
  const [headerValue, setHeaderValue] = useState("");
  const [bearerToken, setBearerToken] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<MCPConnection | null>(null);

  function step1Ready(): boolean {
    if (!name.trim() || !endpointUrl.trim()) return false;
    try {
      const u = new URL(endpointUrl);
      return u.protocol === "http:" || u.protocol === "https:";
    } catch {
      return false;
    }
  }

  function step2Ready(): boolean {
    switch (authMethod) {
      case "api_key_header":
        return headerName.trim() !== "" && headerValue !== "";
      case "bearer":
        return bearerToken !== "";
      case "basic":
        return username !== "";
      case "oauth2_authcode":
        return false;
    }
  }

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      const body: Record<string, unknown> = {
        name: name.trim(),
        endpointUrl: endpointUrl.trim(),
        transport,
        authMethod,
        authSecret: {
          headerName: authMethod === "api_key_header" ? headerName.trim() : "",
          headerValue: authMethod === "api_key_header" ? headerValue : "",
          bearerToken: authMethod === "bearer" ? bearerToken : "",
          username: authMethod === "basic" ? username : "",
          password: authMethod === "basic" ? password : "",
        },
      };
      const r = await api<MCPConnection>(
        `/api/orgs/${orgId}/mcp-connections`,
        { method: "POST", body: JSON.stringify(body) },
      );
      setResult(r);
    } catch (err) {
      if (err instanceof ApiError) {
        setError(typeof err.body === "string" ? err.body : err.message);
      } else {
        setError(String(err));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-30 grid place-items-center bg-black/60 px-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-xl rounded-2xl border border-slate-700 bg-slate-900 p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Add MCP connection</h2>
          <div className="text-xs text-slate-500">Step {step} of 3</div>
        </div>

        {step === 1 && (
          <div className="space-y-4">
            <label className="block text-sm">
              <span className="mb-1 block text-slate-300">Name</span>
              <input
                required
                autoFocus
                placeholder="github-mcp"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm placeholder:text-slate-500 focus:border-brand-500 focus:outline-none"
              />
            </label>
            <label className="block text-sm">
              <span className="mb-1 block text-slate-300">Endpoint URL</span>
              <input
                required
                placeholder="https://mcp.example.com/v1"
                value={endpointUrl}
                onChange={(e) => setEndpointUrl(e.target.value)}
                className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm font-mono placeholder:text-slate-500 focus:border-brand-500 focus:outline-none"
              />
            </label>
            <label className="block text-sm">
              <span className="mb-1 block text-slate-300">Transport</span>
              <select
                value={transport}
                onChange={(e) => setTransport(e.target.value as MCPConnectionTransport)}
                className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
              >
                <option value="streamable-http">streamable-http (recommended)</option>
                <option value="sse">sse</option>
                <option value="http">http (JSON-RPC over POST)</option>
              </select>
            </label>
            <div className="flex justify-end gap-2 pt-2">
              <button onClick={onClose} className="rounded-md border border-slate-700 px-4 py-2 text-sm hover:bg-slate-800">
                Cancel
              </button>
              <button
                disabled={!step1Ready()}
                onClick={() => setStep(2)}
                className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-brand-400 disabled:opacity-50"
              >
                Next
              </button>
            </div>
          </div>
        )}

        {step === 2 && (
          <div className="space-y-4">
            <label className="block text-sm">
              <span className="mb-1 block text-slate-300">Authentication</span>
              <select
                value={authMethod}
                onChange={(e) => setAuthMethod(e.target.value as MCPConnectionAuthMethod)}
                className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
              >
                <option value="bearer">Bearer token</option>
                <option value="api_key_header">API key in custom header</option>
                <option value="basic">HTTP Basic (user + pass)</option>
                <option value="oauth2_authcode" disabled>
                  OAuth2 — coming soon (issue #8)
                </option>
              </select>
            </label>

            {authMethod === "bearer" && (
              <label className="block text-sm">
                <span className="mb-1 block text-slate-300">Bearer token</span>
                <input
                  type="password"
                  autoFocus
                  value={bearerToken}
                  onChange={(e) => setBearerToken(e.target.value)}
                  className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                />
              </label>
            )}

            {authMethod === "api_key_header" && (
              <>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Header name</span>
                  <input
                    value={headerName}
                    onChange={(e) => setHeaderName(e.target.value)}
                    className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                  />
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Header value</span>
                  <input
                    type="password"
                    value={headerValue}
                    onChange={(e) => setHeaderValue(e.target.value)}
                    className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                  />
                </label>
              </>
            )}

            {authMethod === "basic" && (
              <>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Username</span>
                  <input
                    autoFocus
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
                  />
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Password</span>
                  <input
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
                  />
                </label>
              </>
            )}

            <div className="flex justify-between gap-2 pt-2">
              <button onClick={() => setStep(1)} className="rounded-md border border-slate-700 px-4 py-2 text-sm hover:bg-slate-800">
                Back
              </button>
              <div className="flex gap-2">
                <button onClick={onClose} className="rounded-md border border-slate-700 px-4 py-2 text-sm hover:bg-slate-800">
                  Cancel
                </button>
                <button
                  disabled={!step2Ready()}
                  onClick={() => setStep(3)}
                  className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-brand-400 disabled:opacity-50"
                >
                  Review
                </button>
              </div>
            </div>
          </div>
        )}

        {step === 3 && (
          <div className="space-y-4">
            {!result ? (
              <>
                <div className="rounded-xl border border-slate-700 bg-slate-950 p-4 text-sm">
                  <div className="text-slate-300">{name}</div>
                  <div className="mt-1 font-mono text-xs text-slate-400">{endpointUrl}</div>
                  <div className="mt-2 grid grid-cols-2 gap-2 text-xs text-slate-500">
                    <div>Transport: <span className="text-slate-300">{transport}</span></div>
                    <div>Auth: <span className="text-slate-300">{authMethod}</span></div>
                  </div>
                </div>
                {error && <div className="text-sm text-rose-400">{error}</div>}
                <div className="flex justify-between gap-2 pt-2">
                  <button onClick={() => setStep(2)} className="rounded-md border border-slate-700 px-4 py-2 text-sm hover:bg-slate-800">
                    Back
                  </button>
                  <div className="flex gap-2">
                    <button onClick={onClose} className="rounded-md border border-slate-700 px-4 py-2 text-sm hover:bg-slate-800">
                      Cancel
                    </button>
                    <button
                      disabled={busy}
                      onClick={submit}
                      className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-brand-400 disabled:opacity-50"
                    >
                      {busy ? "Testing…" : "Test & save"}
                    </button>
                  </div>
                </div>
              </>
            ) : (
              <>
                <div
                  className={`rounded-xl border p-4 text-sm ${
                    result.status === "healthy"
                      ? "border-emerald-700/60 bg-emerald-950/40 text-emerald-200"
                      : result.status === "unauthorized"
                      ? "border-amber-700/60 bg-amber-950/40 text-amber-200"
                      : "border-rose-700/60 bg-rose-950/40 text-rose-200"
                  }`}
                >
                  <div className="font-medium">
                    {result.status === "healthy" ? "Connected — discovered server" : "Saved, but discovery failed"}
                  </div>
                  {result.lastError && (
                    <div className="mt-1 text-xs">{result.lastError}</div>
                  )}
                </div>
                <div className="flex justify-end gap-2 pt-2">
                  <button
                    onClick={onDone}
                    className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-brand-400"
                  >
                    Done
                  </button>
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
