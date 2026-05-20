package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID            uuid.UUID  `json:"id"`
	Email         string     `json:"email"`
	DisplayName   string     `json:"displayName"`
	AvatarURL     string     `json:"avatarUrl"`
	GoogleSubject string     `json:"-"`
	CreatedAt     time.Time  `json:"createdAt"`
	SuspendedAt   *time.Time `json:"suspendedAt,omitempty"`
}

type Org struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedBy uuid.UUID `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

type OrgRole string

const (
	RoleOwner  OrgRole = "owner"
	RoleAdmin  OrgRole = "admin"
	RoleMember OrgRole = "member"
)

type Membership struct {
	Org  Org     `json:"org"`
	Role OrgRole `json:"role"`
}

type Session struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"-"`
	ActiveOrgID *uuid.UUID `json:"activeOrgId,omitempty"`
	Kind        string     `json:"kind"`
	Label       string     `json:"label"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
}

type DeviceAuthorization struct {
	ID          uuid.UUID  `json:"id"`
	UserCode    string     `json:"userCode"`
	ClientLabel string     `json:"clientLabel"`
	Status      string     `json:"status"`
	UserID      *uuid.UUID `json:"-"`
	SessionID   *uuid.UUID `json:"-"`
	ApprovedAt  *time.Time `json:"approvedAt,omitempty"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type Gateway struct {
	ID          uuid.UUID  `json:"id"`
	OrgID       uuid.UUID  `json:"orgId"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	LastSeenAt  *time.Time `json:"lastSeenAt,omitempty"`
	CreatedBy   uuid.UUID  `json:"createdBy"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type MCPServer struct {
	ID                   uuid.UUID       `json:"id"`
	OrgID                uuid.UUID       `json:"orgId"`
	GatewayID            *uuid.UUID      `json:"gatewayId,omitempty"`
	ConnectionID         *uuid.UUID      `json:"connectionId,omitempty"`
	Name                 string          `json:"name"`
	Address              string          `json:"address"`
	Transport            string          `json:"transport"`
	Version              string          `json:"version"`
	Metadata             json.RawMessage `json:"metadata"`
	FirstSeenAt          time.Time       `json:"firstSeenAt"`
	LastSeenAt           time.Time       `json:"lastSeenAt"`
	ExposureState        string          `json:"exposureState"`
	ExposureReason       string          `json:"exposureReason"`
	ExposureClassifiedAt *time.Time      `json:"exposureClassifiedAt,omitempty"`
}

type MCPServerWithCounts struct {
	MCPServer
	GatewayName             string `json:"gatewayName"`
	ConnectionName          string `json:"connectionName"`
	CapabilityCount         int    `json:"capabilityCount"`
	DisabledCapabilityCount int    `json:"disabledCapabilityCount"`
}

type MCPCapability struct {
	ID              uuid.UUID       `json:"id"`
	MCPServerID     uuid.UUID       `json:"mcpServerId"`
	Kind            string          `json:"kind"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Schema          json.RawMessage `json:"schema"`
	FirstSeenAt     time.Time       `json:"firstSeenAt"`
	LastSeenAt      time.Time       `json:"lastSeenAt"`
	Enabled         bool            `json:"enabled"`
	DisabledAt      *time.Time      `json:"disabledAt,omitempty"`
	DisabledBy      *uuid.UUID      `json:"disabledBy,omitempty"`
	DisabledByEmail string          `json:"disabledByEmail,omitempty"`
}

// DisabledCapabilityRef is the wire shape returned to gateways in the heartbeat
// response so the shim can honor per-capability disable without storing IDs.
type DisabledCapabilityRef struct {
	ServerName string `json:"serverName"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type MCPInvocation struct {
	ID             uuid.UUID       `json:"id"`
	OrgID          uuid.UUID       `json:"orgId"`
	MCPServerID    uuid.UUID       `json:"mcpServerId"`
	CapabilityID   *uuid.UUID      `json:"capabilityId,omitempty"`
	CapabilityKind string          `json:"capabilityKind"`
	CapabilityName string          `json:"capabilityName"`
	Caller         json.RawMessage `json:"caller"`
	Status         string          `json:"status"`
	LatencyMs      int             `json:"latencyMs"`
	At             time.Time       `json:"at"`
}

type MCPServerDetail struct {
	MCPServer
	GatewayName    string          `json:"gatewayName"`
	ConnectionName string          `json:"connectionName"`
	Capabilities   []MCPCapability `json:"capabilities"`
	Invocations    []MCPInvocation `json:"invocations"`
}

// AuthMethod identifies how an MCP connection authenticates to a remote server.
type AuthMethod string

const (
	AuthMethodAPIKeyHeader   AuthMethod = "api_key_header"
	AuthMethodBearer         AuthMethod = "bearer"
	AuthMethodBasic          AuthMethod = "basic"
	AuthMethodOAuth2Authcode AuthMethod = "oauth2_authcode"
)

// OAuth-status values for the oauth_status column on mcp_connections. Only
// meaningful when AuthMethod == AuthMethodOAuth2Authcode; otherwise blank.
const (
	OAuthStatusNone             = ""
	OAuthStatusPendingAuthorize = "pending_authorize"
	OAuthStatusAuthorized       = "authorized"
	OAuthStatusExpiredRefresh   = "expired_refresh"
	OAuthStatusNeedsReauth      = "needs_reauth"
)

// OrgCaller is a distinct caller identity observed in mcp_invocations.caller
// for an org, used by UC9 credential governance (detection-only).
type OrgCaller struct {
	ID              uuid.UUID       `json:"id"`
	OrgID           uuid.UUID       `json:"orgId"`
	Signature       string          `json:"signature"`
	Label           string          `json:"label"`
	Caller          json.RawMessage `json:"caller"`
	FirstSeenAt     time.Time       `json:"firstSeenAt"`
	LastSeenAt      time.Time       `json:"lastSeenAt"`
	InvocationCount int64           `json:"invocationCount"`
	FlaggedNew      bool            `json:"flaggedNew"`
}

type OrgCallerTopServer struct {
	MCPServerID uuid.UUID `json:"mcpServerId"`
	Name        string    `json:"name"`
	Count       int64     `json:"count"`
}

type OrgCallerDetail struct {
	OrgCaller
	TopServers       []OrgCallerTopServer `json:"topServers"`
	RecentInvocations []MCPInvocation     `json:"recentInvocations"`
}

// Invitation is an outstanding/decided org invite addressed to a single email.
// Status is one of: pending | accepted | declined | revoked | expired.
type Invitation struct {
	ID           uuid.UUID  `json:"id"`
	OrgID        uuid.UUID  `json:"orgId"`
	OrgName      string     `json:"orgName"`
	OrgSlug      string     `json:"orgSlug"`
	Email        string     `json:"email"`
	InvitedBy    uuid.UUID  `json:"invitedBy"`
	InviterEmail string     `json:"inviterEmail"`
	InviterName  string     `json:"inviterName"`
	Role         OrgRole    `json:"role"`
	Status       string     `json:"status"`
	AcceptedAt   *time.Time `json:"acceptedAt,omitempty"`
	DeclinedAt   *time.Time `json:"declinedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	ExpiresAt    time.Time  `json:"expiresAt"`
}

// Member is one row of an org's membership list with the joined user fields.
type Member struct {
	UserID      uuid.UUID `json:"userId"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	AvatarURL   string    `json:"avatarUrl"`
	Role        OrgRole   `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
}

type MCPConnection struct {
	ID               uuid.UUID  `json:"id"`
	OrgID            uuid.UUID  `json:"orgId"`
	Name             string     `json:"name"`
	EndpointURL      string     `json:"endpointUrl"`
	Transport        string     `json:"transport"`
	AuthMethod       AuthMethod `json:"authMethod"`
	Status           string     `json:"status"`
	LastError        string     `json:"lastError"`
	LastDiscoveredAt *time.Time `json:"lastDiscoveredAt,omitempty"`
	MCPServerID      *uuid.UUID `json:"mcpServerId,omitempty"`
	CreatedBy        uuid.UUID  `json:"createdBy"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	OAuthStatus      string     `json:"oauthStatus"`
}

// PolicyAction is the user-visible verdict a matching policy assigns.
// Both are audit-only in UC4; no in-data-path enforcement.
type PolicyAction string

const (
	PolicyActionDeny PolicyAction = "deny"
	PolicyActionWarn PolicyAction = "warn"
)

// Policy is a CEL-based rule evaluated against observed invocations.
type Policy struct {
	ID            uuid.UUID    `json:"id"`
	OrgID         uuid.UUID    `json:"orgId"`
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	Expression    string       `json:"expression"`
	Action        PolicyAction `json:"action"`
	Enabled       bool         `json:"enabled"`
	CreatedBy     uuid.UUID    `json:"createdBy"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
	Last24hMatches int         `json:"last24hMatches"`
}

// Decision is a per-invocation, per-policy verdict row.
type Decision struct {
	InvocationID uuid.UUID    `json:"invocationId"`
	PolicyID     uuid.UUID    `json:"policyId"`
	PolicyName   string       `json:"policyName"`
	Matched      bool         `json:"matched"`
	Action       PolicyAction `json:"action"`
	At           time.Time    `json:"at"`
}
