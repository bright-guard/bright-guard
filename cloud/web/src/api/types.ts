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

export type Session = {
  id: string;
  kind: "cookie" | "cli";
  label: string;
  activeOrgId?: string | null;
  createdAt: string;
  lastSeenAt: string;
  expiresAt: string;
};
