export type Gateway = {
  id: string;
  orgId: string;
  name: string;
  description: string;
  status: "pending" | "online" | "offline" | "revoked";
  lastSeenAt: string | null;
  createdBy: string;
  createdAt: string;
};

export type GatewayCreateResp = {
  gateway: Gateway;
  enrollmentToken: string;
  installCommand: string;
  expiresAt: string;
};

export type ExposureState =
  | "unknown"
  | "internal"
  | "cloud_internal"
  | "public"
  | "unreachable";

export type MCPServer = {
  id: string;
  orgId: string;
  gatewayId: string | null;
  connectionId: string | null;
  name: string;
  address: string;
  transport: string;
  version: string;
  metadata: Record<string, unknown>;
  firstSeenAt: string;
  lastSeenAt: string;
  exposureState: ExposureState;
  exposureReason: string;
  exposureClassifiedAt: string | null;
};

export type ExposureCount = {
  state: ExposureState;
  count: number;
};

export type ExposureSummary = {
  counts: ExposureCount[];
};

export type MCPServerWithCounts = MCPServer & {
  gatewayName: string;
  connectionName: string;
  capabilityCount: number;
};

export type MCPConnectionStatus =
  | "pending"
  | "healthy"
  | "error"
  | "unauthorized";

export type MCPConnectionAuthMethod =
  | "api_key_header"
  | "bearer"
  | "basic"
  | "oauth2_authcode";

export type MCPConnectionTransport = "streamable-http" | "sse" | "http";

export type OAuthStatus =
  | ""
  | "pending_authorize"
  | "authorized"
  | "expired_refresh"
  | "needs_reauth";

export type MCPConnection = {
  id: string;
  orgId: string;
  name: string;
  endpointUrl: string;
  transport: MCPConnectionTransport;
  authMethod: MCPConnectionAuthMethod;
  status: MCPConnectionStatus;
  lastError: string;
  lastDiscoveredAt: string | null;
  mcpServerId: string | null;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
  oauthStatus: OAuthStatus;
};

export type OAuthConfigInput = {
  authorizeUrl: string;
  tokenUrl: string;
  clientId: string;
  clientSecret: string;
  scopes: string;
  extraParams?: Record<string, string>;
};

export type AuthorizeResp = {
  authorizeUrl: string;
};

export type MCPCapability = {
  id: string;
  mcpServerId: string;
  kind: "tool" | "resource" | "prompt" | string;
  name: string;
  description: string;
  schema: Record<string, unknown>;
  firstSeenAt: string;
  lastSeenAt: string;
};

export type MCPInvocation = {
  id: string;
  orgId: string;
  mcpServerId: string;
  capabilityId: string | null;
  capabilityKind: string;
  capabilityName: string;
  caller: Record<string, unknown>;
  status: string;
  latencyMs: number;
  at: string;
};

export type MCPServerDetail = MCPServerWithCounts & {
  capabilities: MCPCapability[];
  invocations: MCPInvocation[];
};

export type GatewayDetail = {
  gateway: Gateway;
  mcpServers: MCPServerWithCounts[];
};

export type DeviceLookup = {
  clientLabel: string;
  status: "pending" | "approved" | "denied" | "expired";
  expiresAt: string;
};

export type ActivityRow = {
  id: string;
  at: string;
  mcpServer: { id: string; name: string };
  capabilityKind: string;
  capabilityName: string;
  status: string;
  latencyMs: number;
  caller: Record<string, unknown>;
};

export type ActivityListResp = {
  items: ActivityRow[];
  nextCursor: string | null;
  totals: { ok: number; error: number; denied: number };
};

export type ActivitySummary = {
  totalInvocations: number;
  byStatus: { ok: number; error: number; denied: number };
  byCapabilityKind: { tool: number; resource: number; prompt: number };
  topCapabilities: {
    capabilityKind: string;
    capabilityName: string;
    count: number;
  }[];
  topCallers: { caller: Record<string, unknown>; count: number }[];
};

export type OrgCaller = {
  id: string;
  orgId: string;
  signature: string;
  label: string;
  caller: Record<string, unknown>;
  firstSeenAt: string;
  lastSeenAt: string;
  invocationCount: number;
  flaggedNew: boolean;
};

export type OrgCallerListResp = {
  items: OrgCaller[];
  nextCursor: string | null;
  totals: { total: number; flaggedNew: number };
};

export type OrgCallerTopServer = {
  mcpServerId: string;
  name: string;
  count: number;
};

export type OrgCallerDetail = OrgCaller & {
  topServers: OrgCallerTopServer[];
  recentInvocations: MCPInvocation[];
};

export type Session = {
  id: string;
  kind: "cookie" | "cli";
  label: string;
  activeOrgId?: string | null;
  createdAt: string;
  lastSeenAt: string;
  expiresAt: string;
};
