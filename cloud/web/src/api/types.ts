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
  disabledCapabilityCount: number;
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

export type CreateConnectionInput = {
  name: string;
  endpointUrl: string;
  transport: MCPConnectionTransport;
  authMethod: MCPConnectionAuthMethod;
  authSecret: {
    headerName: string;
    headerValue: string;
    bearerToken: string;
    username: string;
    password: string;
  };
  oauthConfig?: OAuthConfigInput;
  // When true, the API runs RFC 7591 Dynamic Client Registration against
  // the endpoint instead of consuming `oauthConfig`. A 422 dcr_unsupported
  // response means the SPA should prompt the user to switch to manual config.
  oauthDcr?: boolean;
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
  enabled: boolean;
  disabledAt: string | null;
  disabledBy: string | null;
  disabledByEmail?: string;
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

export type ActivityRowDecision = {
  policyId: string;
  policyName: string;
  action: string;
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
  decisions: ActivityRowDecision[];
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

export type OrgRole = "owner" | "admin" | "member";

export type Member = {
  userId: string;
  email: string;
  displayName: string;
  avatarUrl: string;
  role: OrgRole;
  joinedAt: string;
};

export type InvitationStatus =
  | "pending"
  | "accepted"
  | "declined"
  | "revoked"
  | "expired";

export type Invitation = {
  id: string;
  orgId: string;
  orgName: string;
  orgSlug: string;
  email: string;
  invitedBy: string;
  inviterEmail: string;
  inviterName: string;
  role: OrgRole;
  status: InvitationStatus;
  acceptedAt: string | null;
  declinedAt: string | null;
  createdAt: string;
  expiresAt: string;
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

// --- Platform-admin (backoffice) shapes ---

export type PlatformOverview = {
  users: { total: number; active30d: number; newLast7d: number };
  orgs: { total: number; newLast7d: number };
  gateways: { total: number; online: number };
  mcpServers: { total: number; publicExposure: number };
  capabilities: {
    total: number;
    byKind: { tool: number; resource: number; prompt: number };
  };
  invocations: { last24h: number; last7d: number; denied24h: number };
  connections: { total: number; oauthPending: number; needsReauth: number };
  callers: { total: number; flaggedNew: number };
};

export type PlatformUser = {
  id: string;
  email: string;
  displayName: string;
  avatarUrl: string;
  createdAt: string;
  orgCount: number;
  lastSeenAt: string | null;
  suspendedAt: string | null;
  platformAdmin: boolean;
};

export type PlatformUserOrgRef = {
  id: string;
  name: string;
  slug: string;
  role: "owner" | "admin" | "member" | string;
};

export type PlatformUserDetail = PlatformUser & {
  orgs: PlatformUserOrgRef[];
  sessionCount: number;
  lastActivityAt: string | null;
};

export type PlatformUserListResp = {
  items: PlatformUser[];
  nextCursor: string | null;
};

export type PlatformOrg = {
  id: string;
  name: string;
  slug: string;
  createdBy: string;
  createdAt: string;
  memberCount: number;
  gatewayCount: number;
  mcpServerCount: number;
  connectionCount: number;
  lastActivityAt: string | null;
  suspendedAt: string | null;
};

export type PlatformOrgMember = {
  userId: string;
  email: string;
  displayName: string;
  role: string;
};

export type PlatformOrgGateway = {
  id: string;
  name: string;
  status: string;
  lastSeenAt: string | null;
};

export type PlatformOrgConnection = {
  id: string;
  name: string;
  status: string;
  oauthStatus: string;
};

export type PlatformOrgMCPServer = {
  id: string;
  name: string;
  exposureState: ExposureState;
  lastSeenAt: string;
};

export type PlatformOrgDetail = PlatformOrg & {
  members: PlatformOrgMember[];
  gateways: PlatformOrgGateway[];
  connections: PlatformOrgConnection[];
  mcpServers: PlatformOrgMCPServer[];
};

export type PlatformOrgListResp = {
  items: PlatformOrg[];
  nextCursor: string | null;
};

export type PlatformAdmin = {
  userId: string;
  email: string;
  displayName: string;
  addedBy: string | null;
  addedByEmail: string;
  addedAt: string;
};

export type PlatformAdminListResp = {
  items: PlatformAdmin[];
};

export type PlatformAuditEntry = {
  id: string;
  actorId: string;
  actorEmail: string;
  action: string;
  targetKind: string;
  targetId: string;
  details: Record<string, unknown>;
  at: string;
};

export type PlatformAuditListResp = {
  items: PlatformAuditEntry[];
  nextCursor: string | null;
};

export type PlatformActivityListResp = {
  items: ActivityRow[];
  nextCursor: string | null;
};

// --- Policies (UC4 — CEL-based audit-mode policies) ---

export type PolicyAction = "deny" | "warn";

export type Policy = {
  id: string;
  orgId: string;
  name: string;
  description: string;
  expression: string;
  action: PolicyAction;
  enabled: boolean;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
  last24hMatches: number;
};

export type PolicyCreateReq = {
  name: string;
  description: string;
  expression: string;
  action: PolicyAction;
  enabled: boolean;
};

export type PolicyPatchReq = Partial<PolicyCreateReq>;

export type PolicySimulateMatch = {
  invocationId: string;
  at: string;
  serverName: string;
  capabilityKind: string;
  capabilityName: string;
  status: string;
  caller: Record<string, unknown>;
};

export type PolicySimulateResp = {
  scanned: number;
  matches: PolicySimulateMatch[];
  from: string;
  to: string;
};

// --- Executive dashboard (Overview page) ---

export type DashboardRange = "7d" | "30d" | "90d";

export type DashboardKpiTile = {
  key:
    | "posture"
    | "footprint"
    | "invocations"
    | "denials"
    | "publicExposure"
    | "activeCallers";
  current: number;
  prior: number;
  deltaPercent: number;
  higherIsBetter: boolean;
  sparkline: number[];
  extra?: Record<string, number | string>;
};

export type DashboardKpisResp = {
  rangeDays: number;
  from: string;
  to: string;
  tiles: DashboardKpiTile[];
  updatedAt: string;
};

export type DashboardTimeseriesPoint = {
  day: string;
  allowed?: number;
  audited?: number;
  denied?: number;
  value?: number;
};

export type DashboardTimeseriesResp = {
  metric: string;
  rangeDays: number;
  from: string;
  to: string;
  series: DashboardTimeseriesPoint[];
};

export type DashboardTopCapability = {
  capabilityKind: string;
  capabilityName: string;
  serverName: string;
  count: number;
};

export type DashboardTopCaller = {
  signature: string;
  label: string;
  caller: Record<string, unknown>;
  count: number;
};

export type DashboardRecentDenied = {
  id: string;
  at: string;
  serverName: string;
  capabilityKind: string;
  capabilityName: string;
  caller: Record<string, unknown>;
};

export type DashboardHighlightsResp = {
  from: string;
  to: string;
  rangeDays: number;
  topCapabilities: DashboardTopCapability[];
  topCallers: DashboardTopCaller[];
  recentDenied: DashboardRecentDenied[];
};

export type DashboardCalloutsResp = {
  publicExposureServers: number;
  flaggedNewCallers: number;
  capabilitiesNoPolicy: number;
};
