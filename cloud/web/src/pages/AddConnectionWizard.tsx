import { useState } from "react";
import { api, ApiError } from "../api/client";
import type {
  AuthorizeResp,
  MCPConnection,
  MCPConnectionAuthMethod,
  MCPConnectionTransport,
  OAuthConfigInput,
} from "../api/types";
import PageHelp from "../components/PageHelp";

type OAuthMode = "auto" | "manual";

type Step = 1 | 2 | 3;

// Pre-fills for two common providers. Users still supply their own client_id
// + secret; the URLs and scopes save tedious copy/paste.
type OAuthPreset = {
  label: string;
  authorizeUrl: string;
  tokenUrl: string;
  scopes: string;
  extraParams: string;
};

// isDcrUnsupported pulls the structured error code out of an ApiError body
// shaped like {"error":{"code":"dcr_unsupported","message":"..."}}.
function isDcrUnsupported(err: ApiError): boolean {
  const body = err.body;
  if (body && typeof body === "object") {
    const e = (body as { error?: { code?: string } }).error;
    return e?.code === "dcr_unsupported";
  }
  return false;
}

const OAUTH_PRESETS: Record<string, OAuthPreset> = {
  atlassian: {
    label: "Atlassian Cloud (Jira / Confluence)",
    authorizeUrl: "https://auth.atlassian.com/authorize",
    tokenUrl: "https://auth.atlassian.com/oauth/token",
    scopes: "read:jira-work read:jira-user offline_access",
    extraParams: '{"audience":"api.atlassian.com","prompt":"consent"}',
  },
  notion: {
    label: "Notion",
    authorizeUrl: "https://api.notion.com/v1/oauth/authorize",
    tokenUrl: "https://api.notion.com/v1/oauth/token",
    scopes: "",
    extraParams: '{"owner":"user"}',
  },
};

// Quick-start tiles rendered at the top of step 1. Each pre-fills the OAuth
// auth method + URLs so an admin connecting to a known provider doesn't have
// to navigate two steps deep into the wizard to discover the OAuth path.
type QuickStartTile = {
  key: "atlassian" | "notion" | "custom";
  label: string;
  hint: string;
  defaultName: string;
  endpointPlaceholder: string;
};

const QUICK_START_TILES: QuickStartTile[] = [
  {
    key: "atlassian",
    label: "Atlassian / Jira",
    hint: "OAuth2 · pre-fills Atlassian endpoints",
    defaultName: "atlassian-mcp",
    endpointPlaceholder: "https://your-instance.atlassian.net/mcp/v1",
  },
  {
    key: "notion",
    label: "Notion",
    hint: "OAuth2 · pre-fills Notion endpoints",
    defaultName: "notion-mcp",
    endpointPlaceholder: "https://api.notion.com/mcp",
  },
  {
    key: "custom",
    label: "Custom",
    hint: "Bearer / API key / OAuth2 — your choice",
    defaultName: "",
    endpointPlaceholder: "https://mcp.example.com/v1",
  },
];

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

  // OAuth2 form state.
  const [oauthMode, setOauthMode] = useState<OAuthMode>("auto");
  const [oauthAuthorize, setOauthAuthorize] = useState("");
  const [oauthToken, setOauthToken] = useState("");
  const [oauthClientId, setOauthClientId] = useState("");
  const [oauthClientSecret, setOauthClientSecret] = useState("");
  const [oauthScopes, setOauthScopes] = useState("");
  const [oauthExtraJSON, setOauthExtraJSON] = useState("");

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<MCPConnection | null>(null);

  const [quickStart, setQuickStart] = useState<QuickStartTile["key"] | null>(null);
  const endpointPlaceholder =
    QUICK_START_TILES.find((t) => t.key === quickStart)?.endpointPlaceholder ??
    "https://mcp.example.com/v1";

  function applyPreset(key: string) {
    const p = OAUTH_PRESETS[key];
    if (!p) return;
    setOauthAuthorize(p.authorizeUrl);
    setOauthToken(p.tokenUrl);
    setOauthScopes(p.scopes);
    setOauthExtraJSON(p.extraParams);
  }

  function selectQuickStart(tile: QuickStartTile) {
    setQuickStart(tile.key);
    if (!name.trim() && tile.defaultName) setName(tile.defaultName);
    if (tile.key === "atlassian" || tile.key === "notion") {
      setAuthMethod("oauth2_authcode");
      // Default to DCR (auto-discover) — MCP-spec-compliant servers expose
      // registration_endpoint and we mint our own client per RFC 7591, so the
      // user does NOT need a pre-registered client_id/secret. The preset URLs
      // are still applied so they're pre-filled if DCR fails and the wizard
      // bounces the user into manual mode.
      setOauthMode("auto");
      applyPreset(tile.key);
    }
  }

  function step1Ready(): boolean {
    if (!name.trim() || !endpointUrl.trim()) return false;
    try {
      const u = new URL(endpointUrl);
      return u.protocol === "http:" || u.protocol === "https:";
    } catch {
      return false;
    }
  }

  function parsedExtraParams(): Record<string, string> | string {
    const s = oauthExtraJSON.trim();
    if (s === "") return {};
    try {
      const v = JSON.parse(s);
      if (v && typeof v === "object" && !Array.isArray(v)) {
        const out: Record<string, string> = {};
        for (const [k, val] of Object.entries(v)) {
          out[k] = String(val);
        }
        return out;
      }
    } catch (e) {
      return `Extra params must be a JSON object: ${(e as Error).message}`;
    }
    return "Extra params must be a JSON object";
  }

  function step2Ready(): boolean {
    switch (authMethod) {
      case "api_key_header":
        return headerName.trim() !== "" && headerValue !== "";
      case "bearer":
        return bearerToken !== "";
      case "basic":
        return username !== "";
      case "oauth2_authcode": {
        if (oauthMode === "auto") {
          // In auto-discover mode there's nothing to fill in on step 2 — the
          // server tells us where to register on submit.
          return true;
        }
        if (!oauthClientId.trim() || !oauthAuthorize.trim() || !oauthToken.trim()) return false;
        try {
          const a = new URL(oauthAuthorize);
          const t = new URL(oauthToken);
          if (!/^https?:$/.test(a.protocol) || !/^https?:$/.test(t.protocol)) return false;
        } catch {
          return false;
        }
        return typeof parsedExtraParams() !== "string";
      }
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
      if (authMethod === "oauth2_authcode") {
        if (oauthMode === "auto") {
          body.oauthDcr = true;
        } else {
          const extra = parsedExtraParams();
          if (typeof extra === "string") {
            throw new Error(extra);
          }
          const oauthConfig: OAuthConfigInput = {
            authorizeUrl: oauthAuthorize.trim(),
            tokenUrl: oauthToken.trim(),
            clientId: oauthClientId.trim(),
            clientSecret: oauthClientSecret,
            scopes: oauthScopes.trim(),
            extraParams: extra,
          };
          body.oauthConfig = oauthConfig;
        }
      }
      let r: MCPConnection;
      try {
        r = await api<MCPConnection>(
          `/api/orgs/${orgId}/mcp-connections`,
          { method: "POST", body: JSON.stringify(body) },
        );
      } catch (err) {
        // dcr_unsupported = the server doesn't advertise the OAuth metadata we
        // need, or registration itself rejected us. Surface a clear prompt and
        // switch the user into manual mode rather than leaving them stuck.
        if (
          err instanceof ApiError &&
          err.status === 422 &&
          authMethod === "oauth2_authcode" &&
          oauthMode === "auto" &&
          isDcrUnsupported(err)
        ) {
          setOauthMode("manual");
          setStep(2);
          setError(
            "This server doesn't support automatic registration. Enter the OAuth client details manually.",
          );
          return;
        }
        throw err;
      }
      setResult(r);
      // For OAuth2 we immediately kick off the authorize handshake and
      // navigate the browser away to the provider's consent screen.
      if (authMethod === "oauth2_authcode") {
        const returnTo = encodeURIComponent("/app/mcp-connections");
        const auth = await api<AuthorizeResp>(
          `/api/orgs/${orgId}/mcp-connections/${r.id}/authorize?returnTo=${returnTo}`,
        );
        window.location.href = auth.authorizeUrl;
        return;
      }
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
          <div className="flex items-center gap-2">
            <h2 className="text-lg font-semibold">Add MCP connection</h2>
            <PageHelp slug="connections/adding-an-mcp-connection" />
          </div>
          <div className="text-xs text-slate-500">Step {step} of 3</div>
        </div>

        {step === 1 && (
          <div className="space-y-4">
            <div>
              <div className="mb-1 text-xs uppercase tracking-wider text-slate-400">
                Quick start
              </div>
              <div className="grid grid-cols-3 gap-2">
                {QUICK_START_TILES.map((t) => {
                  const active = quickStart === t.key;
                  return (
                    <button
                      key={t.key}
                      type="button"
                      onClick={() => selectQuickStart(t)}
                      className={
                        "rounded-md border px-3 py-2 text-left text-xs transition " +
                        (active
                          ? "border-brand-500 bg-brand-500/10 text-slate-100"
                          : "border-slate-700 bg-slate-950 text-slate-300 hover:border-slate-500")
                      }
                    >
                      <span className="block text-sm font-semibold">{t.label}</span>
                      <span className="block text-[11px] text-slate-400">{t.hint}</span>
                    </button>
                  );
                })}
              </div>
              {(quickStart === "atlassian" || quickStart === "notion") && (
                <div className="mt-2 rounded-md border border-brand-500/40 bg-brand-500/5 px-3 py-2 text-xs text-slate-300">
                  OAuth2 + Dynamic Client Registration. No client ID or secret
                  required — Bright Guard registers a client with{" "}
                  {quickStart === "atlassian" ? "Atlassian" : "Notion"} on save,
                  then redirects you to approve access. If the server doesn't
                  support DCR you'll be bounced to manual mode with the URLs
                  pre-filled.
                </div>
              )}
            </div>
            <label className="block text-sm">
              <span className="mb-1 block text-slate-300">Name</span>
              <input
                required
                autoFocus
                placeholder="github-mcp"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm placeholder:text-slate-500 focus:border-brand-500 focus:outline-none"
              />
            </label>
            <label className="block text-sm">
              <span className="mb-1 block text-slate-300">Endpoint URL</span>
              <input
                required
                placeholder={endpointPlaceholder}
                value={endpointUrl}
                onChange={(e) => setEndpointUrl(e.target.value)}
                className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono placeholder:text-slate-500 focus:border-brand-500 focus:outline-none"
              />
            </label>
            <label className="block text-sm">
              <span className="mb-1 block text-slate-300">Transport</span>
              <select
                value={transport}
                onChange={(e) => setTransport(e.target.value as MCPConnectionTransport)}
                className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
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
                className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
              >
                <option value="bearer">Bearer token</option>
                <option value="api_key_header">API key in custom header</option>
                <option value="basic">HTTP Basic (user + pass)</option>
                <option value="oauth2_authcode">OAuth2 (authorization code)</option>
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
                  className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
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
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                  />
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Header value</span>
                  <input
                    type="password"
                    value={headerValue}
                    onChange={(e) => setHeaderValue(e.target.value)}
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
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
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
                  />
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Password</span>
                  <input
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none"
                  />
                </label>
              </>
            )}

            {authMethod === "oauth2_authcode" && (
              <>
                <div className="space-y-2 rounded-md border border-slate-700 bg-slate-950 p-3 text-sm">
                  <label className="flex cursor-pointer items-start gap-2">
                    <input
                      type="radio"
                      name="oauthMode"
                      checked={oauthMode === "auto"}
                      onChange={() => setOauthMode("auto")}
                      className="mt-1"
                    />
                    <span>
                      <span className="block text-slate-200">
                        Auto-discover (recommended)
                      </span>
                      <span className="block text-xs text-slate-400">
                        Probes the endpoint's well-known OAuth metadata and
                        registers a client per RFC 7591.
                      </span>
                    </span>
                  </label>
                  <label className="flex cursor-pointer items-start gap-2">
                    <input
                      type="radio"
                      name="oauthMode"
                      checked={oauthMode === "manual"}
                      onChange={() => setOauthMode("manual")}
                      className="mt-1"
                    />
                    <span>
                      <span className="block text-slate-200">
                        Configure manually
                      </span>
                      <span className="block text-xs text-slate-400">
                        Paste client_id / client_secret + authorize/token URLs.
                      </span>
                    </span>
                  </label>
                </div>
                {oauthMode === "auto" && (
                  <div className="rounded-md bg-slate-950 px-3 py-2 text-xs text-slate-400">
                    Nothing else to fill in — Bright Guard will register the
                    OAuth client when you save.
                  </div>
                )}
                {oauthMode === "manual" && (
                <>
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-slate-400">Preset:</span>
                  <button
                    type="button"
                    onClick={() => applyPreset("atlassian")}
                    className="rounded-md border border-slate-700 px-2 py-1 hover:bg-slate-800"
                  >
                    {OAUTH_PRESETS.atlassian.label}
                  </button>
                  <button
                    type="button"
                    onClick={() => applyPreset("notion")}
                    className="rounded-md border border-slate-700 px-2 py-1 hover:bg-slate-800"
                  >
                    {OAUTH_PRESETS.notion.label}
                  </button>
                </div>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Authorize URL</span>
                  <input
                    value={oauthAuthorize}
                    onChange={(e) => setOauthAuthorize(e.target.value)}
                    placeholder="https://auth.example.com/authorize"
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                  />
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Token URL</span>
                  <input
                    value={oauthToken}
                    onChange={(e) => setOauthToken(e.target.value)}
                    placeholder="https://auth.example.com/oauth/token"
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                  />
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Client ID</span>
                  <input
                    value={oauthClientId}
                    onChange={(e) => setOauthClientId(e.target.value)}
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                  />
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Client secret</span>
                  <input
                    type="password"
                    value={oauthClientSecret}
                    onChange={(e) => setOauthClientSecret(e.target.value)}
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                  />
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">Scopes (space-separated)</span>
                  <input
                    value={oauthScopes}
                    onChange={(e) => setOauthScopes(e.target.value)}
                    placeholder="read:jira-work write:jira-work"
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                  />
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block text-slate-300">
                    Extra params (JSON object, optional)
                  </span>
                  <textarea
                    value={oauthExtraJSON}
                    onChange={(e) => setOauthExtraJSON(e.target.value)}
                    rows={3}
                    placeholder='{"audience":"api.atlassian.com"}'
                    className="w-full rounded-md border border-slate-700 bg-slate-950 text-slate-100 px-3 py-2 text-sm font-mono focus:border-brand-500 focus:outline-none"
                  />
                </label>
                </>
                )}
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
                  {authMethod === "oauth2_authcode" && (
                    <div className="mt-3 rounded-md bg-amber-950/40 px-3 py-2 text-xs text-amber-200">
                      You'll be redirected to the provider's consent screen
                      after saving. Tokens are persisted server-side.
                    </div>
                  )}
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
                      {busy
                        ? authMethod === "oauth2_authcode"
                          ? "Redirecting…"
                          : "Testing…"
                        : authMethod === "oauth2_authcode"
                        ? "Save & authorize"
                        : "Test & save"}
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
