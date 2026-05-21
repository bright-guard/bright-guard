package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

type Discovery struct {
	Pool *pgxpool.Pool
}

func jsonOrEmpty(b json.RawMessage) []byte {
	if len(b) == 0 {
		return []byte(`{}`)
	}
	return b
}

func (d *Discovery) UpsertMCPServer(ctx context.Context, orgID, gatewayID uuid.UUID, name, address, transport, version string, metadata json.RawMessage) (*models.MCPServer, error) {
	// gateway-sourced upsert: matches the partial unique index on (gateway_id, name).
	const q = `
		insert into mcp_servers (org_id, gateway_id, name, address, transport, version, metadata)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (gateway_id, name) where gateway_id is not null do update set
			address      = excluded.address,
			transport    = excluded.transport,
			version      = excluded.version,
			metadata     = excluded.metadata,
			last_seen_at = now()
		returning id, org_id, gateway_id, connection_id, name, address, transport, version, metadata, first_seen_at, last_seen_at`
	s := &models.MCPServer{}
	err := d.Pool.QueryRow(ctx, q, orgID, gatewayID, name, address, transport, version, jsonOrEmpty(metadata)).Scan(
		&s.ID, &s.OrgID, &s.GatewayID, &s.ConnectionID, &s.Name, &s.Address, &s.Transport, &s.Version, &s.Metadata, &s.FirstSeenAt, &s.LastSeenAt,
	)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// UpsertMCPServerForConnection upserts a server tied to a direct connection
// (no gateway). Matches the partial unique index on (connection_id, name).
func (d *Discovery) UpsertMCPServerForConnection(ctx context.Context, orgID, connectionID uuid.UUID, name, address, transport, version string, metadata json.RawMessage) (*models.MCPServer, error) {
	const q = `
		insert into mcp_servers (org_id, connection_id, name, address, transport, version, metadata)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (connection_id, name) where connection_id is not null do update set
			address      = excluded.address,
			transport    = excluded.transport,
			version      = excluded.version,
			metadata     = excluded.metadata,
			last_seen_at = now()
		returning id, org_id, gateway_id, connection_id, name, address, transport, version, metadata, first_seen_at, last_seen_at`
	s := &models.MCPServer{}
	err := d.Pool.QueryRow(ctx, q, orgID, connectionID, name, address, transport, version, jsonOrEmpty(metadata)).Scan(
		&s.ID, &s.OrgID, &s.GatewayID, &s.ConnectionID, &s.Name, &s.Address, &s.Transport, &s.Version, &s.Metadata, &s.FirstSeenAt, &s.LastSeenAt,
	)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *Discovery) UpsertCapability(ctx context.Context, mcpServerID uuid.UUID, kind, name, description string, schema json.RawMessage) (*models.MCPCapability, error) {
	const q = `
		insert into mcp_capabilities (mcp_server_id, kind, name, description, schema)
		values ($1, $2, $3, $4, $5)
		on conflict (mcp_server_id, kind, name) do update set
			description  = excluded.description,
			schema       = excluded.schema,
			last_seen_at = now()
		returning id, mcp_server_id, kind, name, description, schema, first_seen_at, last_seen_at, enabled, disabled_at, disabled_by`
	c := &models.MCPCapability{}
	err := d.Pool.QueryRow(ctx, q, mcpServerID, kind, name, description, jsonOrEmpty(schema)).Scan(
		&c.ID, &c.MCPServerID, &c.Kind, &c.Name, &c.Description, &c.Schema, &c.FirstSeenAt, &c.LastSeenAt,
		&c.Enabled, &c.DisabledAt, &c.DisabledBy,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// SetCapabilityEnabled flips the per-capability toggle. The caller must already
// have proven the cap belongs to a server in their org (handler enforces the join).
// When enabling, disabled_at / disabled_by are cleared; when disabling, both are stamped.
func (d *Discovery) SetCapabilityEnabled(ctx context.Context, capID uuid.UUID, enabled bool, byUser uuid.UUID) error {
	const q = `
		update mcp_capabilities
		set enabled     = $2,
		    disabled_at = case when $2 then null::timestamptz else now() end,
		    disabled_by = case when $2 then null::uuid else $3::uuid end
		where id = $1`
	tag, err := d.Pool.Exec(ctx, q, capID, enabled, byUser)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CapabilityBelongsToOrgServer returns true when the cap exists, lives under
// the named server, and that server is owned by orgID. A single join enforces
// tenancy for the PATCH toggle.
func (d *Discovery) CapabilityBelongsToOrgServer(ctx context.Context, orgID, serverID, capID uuid.UUID) (bool, error) {
	const q = `
		select 1
		from mcp_capabilities c
		join mcp_servers s on s.id = c.mcp_server_id
		where c.id = $1 and s.id = $2 and s.org_id = $3`
	var one int
	err := d.Pool.QueryRow(ctx, q, capID, serverID, orgID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListDisabledCapabilitiesForGateway returns the (server, kind, name) tuples that
// are currently disabled across all MCP servers reported by this gateway. The shim
// uses this on every heartbeat to refresh its local denylist.
func (d *Discovery) ListDisabledCapabilitiesForGateway(ctx context.Context, gatewayID uuid.UUID) ([]models.DisabledCapabilityRef, error) {
	const q = `
		select s.name, c.kind, c.name
		from mcp_capabilities c
		join mcp_servers s on s.id = c.mcp_server_id
		where s.gateway_id = $1 and c.enabled = false
		order by s.name, c.kind, c.name`
	rows, err := d.Pool.Query(ctx, q, gatewayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.DisabledCapabilityRef{}
	for rows.Next() {
		var ref models.DisabledCapabilityRef
		if err := rows.Scan(&ref.ServerName, &ref.Kind, &ref.Name); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// InvocationDecision is the per-policy verdict a gateway/shim ships alongside
// a new invocation in the observations payload. The control plane persists
// these into mcp_invocation_decisions in the same transaction as the
// invocation row itself — that way the sweep can detect "already evaluated"
// via the presence of decision rows and skip them.
type InvocationDecision struct {
	PolicyID uuid.UUID
	Action   models.PolicyAction
	Matched  bool
}

// InsertInvocationOpts is a functional-options bag so we can extend the
// invocation insert path additively. Existing callers can keep the old
// no-options call site unchanged.
type InsertInvocationOpts struct {
	decisions []InvocationDecision
	workload  *InvocationWorkload
	network   *InvocationNetwork
}

// InsertInvocationOption applies to InsertInvocationOpts.
type InsertInvocationOption func(*InsertInvocationOpts)

// WithDecisions attaches a slice of decisions (matched + action per policy)
// to the invocation insert. Empty / nil slices are a no-op.
func WithDecisions(decs []InvocationDecision) InsertInvocationOption {
	return func(o *InsertInvocationOpts) { o.decisions = decs }
}

// InvocationWorkload is the per-invocation subject context the gateway / shim
// reports (UC6). All fields optional; empty strings persist as nulls so the
// CEL env reads them as "".
type InvocationWorkload struct {
	Host       string
	Cluster    string
	Namespace  string
	AgentClass string
}

// InvocationNetwork is the per-invocation network position (UC7).
type InvocationNetwork struct {
	Subnet   string
	VPC      string
	Zone     string
	CallerIP string
}

// WithWorkload attaches the per-invocation subject context. Nil is a no-op.
func WithWorkload(w *InvocationWorkload) InsertInvocationOption {
	return func(o *InsertInvocationOpts) { o.workload = w }
}

// WithNetwork attaches the per-invocation network context. Nil is a no-op.
func WithNetwork(n *InvocationNetwork) InsertInvocationOption {
	return func(o *InsertInvocationOpts) { o.network = n }
}

func (d *Discovery) InsertInvocation(
	ctx context.Context,
	orgID, mcpServerID uuid.UUID,
	capabilityKind, capabilityName string,
	caller json.RawMessage,
	status string,
	latencyMs int,
	at time.Time,
	opts ...InsertInvocationOption,
) error {
	o := InsertInvocationOpts{}
	for _, fn := range opts {
		fn(&o)
	}

	// Try to attach a capability id; if none matches, leave NULL.
	var capID *uuid.UUID
	const capQ = `select id from mcp_capabilities where mcp_server_id = $1 and kind = $2 and name = $3`
	var cid uuid.UUID
	err := d.Pool.QueryRow(ctx, capQ, mcpServerID, capabilityKind, capabilityName).Scan(&cid)
	if err == nil {
		capID = &cid
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	// Wave N+9: optional workload + network context columns. Empty / unset
	// values insert as NULL so partial-context invocations stay queryable.
	wHost, wCluster, wNamespace, wAgentClass := nullableWorkload(o.workload)
	nSubnet, nVPC, nZone, nCallerIP := nullableNetwork(o.network)

	// Fast path: no decisions → single insert, no transaction overhead.
	if len(o.decisions) == 0 {
		const q = `
			insert into mcp_invocations (
				org_id, mcp_server_id, capability_id, capability_kind, capability_name,
				caller, status, latency_ms, at,
				workload_host, workload_cluster, workload_namespace, agent_class,
				network_subnet, network_vpc, network_zone, caller_ip
			)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`
		_, err = d.Pool.Exec(ctx, q, orgID, mcpServerID, capID, capabilityKind, capabilityName,
			jsonOrEmpty(caller), status, latencyMs, at,
			wHost, wCluster, wNamespace, wAgentClass,
			nSubnet, nVPC, nZone, nCallerIP,
		)
		return err
	}

	// With decisions: write invocation + decisions in one transaction so they
	// either both land or neither — the sweep uses the decisions row's
	// presence as a "skip" marker, and a partial write would mis-evaluate.
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var invID uuid.UUID
	const insertInv = `
		insert into mcp_invocations (
			org_id, mcp_server_id, capability_id, capability_kind, capability_name,
			caller, status, latency_ms, at,
			workload_host, workload_cluster, workload_namespace, agent_class,
			network_subnet, network_vpc, network_zone, caller_ip
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		returning id`
	if err := tx.QueryRow(ctx, insertInv,
		orgID, mcpServerID, capID, capabilityKind, capabilityName,
		jsonOrEmpty(caller), status, latencyMs, at,
		wHost, wCluster, wNamespace, wAgentClass,
		nSubnet, nVPC, nZone, nCallerIP,
	).Scan(&invID); err != nil {
		return err
	}

	const insertDec = `
		insert into mcp_invocation_decisions (invocation_id, policy_id, matched, action)
		values ($1, $2, $3, $4)
		on conflict (invocation_id, policy_id) do update
		  set matched = excluded.matched, action = excluded.action, at = now()`
	for _, dec := range o.decisions {
		if _, err := tx.Exec(ctx, insertDec, invID, dec.PolicyID, dec.Matched, string(dec.Action)); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (d *Discovery) ListServers(ctx context.Context, orgID uuid.UUID) ([]models.MCPServerWithCounts, error) {
	const q = `
		select s.id, s.org_id, s.gateway_id, s.connection_id, s.name, s.address, s.transport, s.version, s.metadata,
		       s.first_seen_at, s.last_seen_at,
		       s.exposure_state, s.exposure_reason, s.exposure_classified_at,
		       coalesce(g.name, '') as gateway_name,
		       coalesce(mc.name, '') as connection_name,
		       coalesce(c.cnt, 0) as capability_count,
		       coalesce(c.disabled_cnt, 0) as disabled_capability_count
		from mcp_servers s
		left join gateways g on g.id = s.gateway_id
		left join mcp_connections mc on mc.id = s.connection_id
		left join (
			select mcp_server_id,
			       count(*) as cnt,
			       count(*) filter (where enabled = false) as disabled_cnt
			from mcp_capabilities
			group by mcp_server_id
		) c on c.mcp_server_id = s.id
		where s.org_id = $1
		order by s.last_seen_at desc`
	rows, err := d.Pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.MCPServerWithCounts{}
	for rows.Next() {
		var s models.MCPServerWithCounts
		if err := rows.Scan(
			&s.ID, &s.OrgID, &s.GatewayID, &s.ConnectionID, &s.Name, &s.Address, &s.Transport, &s.Version, &s.Metadata,
			&s.FirstSeenAt, &s.LastSeenAt,
			&s.ExposureState, &s.ExposureReason, &s.ExposureClassifiedAt,
			&s.GatewayName, &s.ConnectionName, &s.CapabilityCount, &s.DisabledCapabilityCount,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *Discovery) ListServersForGateway(ctx context.Context, orgID, gatewayID uuid.UUID) ([]models.MCPServerWithCounts, error) {
	const q = `
		select s.id, s.org_id, s.gateway_id, s.connection_id, s.name, s.address, s.transport, s.version, s.metadata,
		       s.first_seen_at, s.last_seen_at,
		       s.exposure_state, s.exposure_reason, s.exposure_classified_at,
		       g.name as gateway_name,
		       '' as connection_name,
		       coalesce(c.cnt, 0) as capability_count,
		       coalesce(c.disabled_cnt, 0) as disabled_capability_count
		from mcp_servers s
		join gateways g on g.id = s.gateway_id
		left join (
			select mcp_server_id,
			       count(*) as cnt,
			       count(*) filter (where enabled = false) as disabled_cnt
			from mcp_capabilities
			group by mcp_server_id
		) c on c.mcp_server_id = s.id
		where s.org_id = $1 and s.gateway_id = $2
		order by s.last_seen_at desc`
	rows, err := d.Pool.Query(ctx, q, orgID, gatewayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.MCPServerWithCounts{}
	for rows.Next() {
		var s models.MCPServerWithCounts
		if err := rows.Scan(
			&s.ID, &s.OrgID, &s.GatewayID, &s.ConnectionID, &s.Name, &s.Address, &s.Transport, &s.Version, &s.Metadata,
			&s.FirstSeenAt, &s.LastSeenAt,
			&s.ExposureState, &s.ExposureReason, &s.ExposureClassifiedAt,
			&s.GatewayName, &s.ConnectionName, &s.CapabilityCount, &s.DisabledCapabilityCount,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// BundleSnapshotServer is the per-server wire shape delivered in the policy
// bundle. Lean enough that shipping all of an org's servers on every cache
// miss stays cheap.
type BundleSnapshotServer struct {
	ID            uuid.UUID
	Name          string
	Address       string
	ExposureState string
}

// ListServersForBundle returns up to `limit` mcp_servers for the org so the
// shim can answer server.exposure_state locally for any observed invocation.
// Ordered by name for determinism so two consecutive bundles at the same
// version produce byte-identical wire output.
func (d *Discovery) ListServersForBundle(ctx context.Context, orgID uuid.UUID, limit int) ([]BundleSnapshotServer, error) {
	if limit <= 0 {
		limit = 5000
	}
	const q = `
		select id, name, address, exposure_state
		from mcp_servers
		where org_id = $1
		order by name
		limit $2`
	rows, err := d.Pool.Query(ctx, q, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BundleSnapshotServer{}
	for rows.Next() {
		var s BundleSnapshotServer
		if err := rows.Scan(&s.ID, &s.Name, &s.Address, &s.ExposureState); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *Discovery) GetServerDetail(ctx context.Context, orgID, id uuid.UUID) (*models.MCPServerDetail, error) {
	det := &models.MCPServerDetail{}
	const sq = `
		select s.id, s.org_id, s.gateway_id, s.connection_id, s.name, s.address, s.transport, s.version, s.metadata,
		       s.first_seen_at, s.last_seen_at,
		       s.exposure_state, s.exposure_reason, s.exposure_classified_at,
		       coalesce(g.name, ''), coalesce(mc.name, '')
		from mcp_servers s
		left join gateways g on g.id = s.gateway_id
		left join mcp_connections mc on mc.id = s.connection_id
		where s.org_id = $1 and s.id = $2`
	err := d.Pool.QueryRow(ctx, sq, orgID, id).Scan(
		&det.ID, &det.OrgID, &det.GatewayID, &det.ConnectionID, &det.Name, &det.Address, &det.Transport, &det.Version, &det.Metadata,
		&det.FirstSeenAt, &det.LastSeenAt,
		&det.ExposureState, &det.ExposureReason, &det.ExposureClassifiedAt,
		&det.GatewayName, &det.ConnectionName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	const cq = `
		select c.id, c.mcp_server_id, c.kind, c.name, c.description, c.schema,
		       c.first_seen_at, c.last_seen_at,
		       c.enabled, c.disabled_at, c.disabled_by,
		       coalesce(u.email, '') as disabled_by_email
		from mcp_capabilities c
		left join users u on u.id = c.disabled_by
		where c.mcp_server_id = $1
		order by c.kind, c.name`
	crows, err := d.Pool.Query(ctx, cq, id)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	det.Capabilities = []models.MCPCapability{}
	for crows.Next() {
		var c models.MCPCapability
		if err := crows.Scan(
			&c.ID, &c.MCPServerID, &c.Kind, &c.Name, &c.Description, &c.Schema,
			&c.FirstSeenAt, &c.LastSeenAt,
			&c.Enabled, &c.DisabledAt, &c.DisabledBy, &c.DisabledByEmail,
		); err != nil {
			return nil, err
		}
		det.Capabilities = append(det.Capabilities, c)
	}
	if err := crows.Err(); err != nil {
		return nil, err
	}

	const iq = `
		select id, org_id, mcp_server_id, capability_id, capability_kind, capability_name, caller, status, latency_ms, at
		from mcp_invocations where mcp_server_id = $1
		order by at desc limit 100`
	irows, err := d.Pool.Query(ctx, iq, id)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	det.Invocations = []models.MCPInvocation{}
	for irows.Next() {
		var inv models.MCPInvocation
		if err := irows.Scan(&inv.ID, &inv.OrgID, &inv.MCPServerID, &inv.CapabilityID, &inv.CapabilityKind, &inv.CapabilityName, &inv.Caller, &inv.Status, &inv.LatencyMs, &inv.At); err != nil {
			return nil, err
		}
		det.Invocations = append(det.Invocations, inv)
	}
	return det, irows.Err()
}

// CapabilityRow is a flat join of a capability with its server, used by the
// chat agent's list_capabilities tool so the model can answer cross-server
// capability questions in one round-trip.
type CapabilityRow struct {
	ID          uuid.UUID `json:"id"`
	ServerID    uuid.UUID `json:"serverId"`
	ServerName  string    `json:"serverName"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
}

// CapabilityFilter narrows ListCapabilitiesForOrg. Empty fields are ignored.
type CapabilityFilter struct {
	ServerID     *uuid.UUID
	Kind         string
	NameContains string
	Limit        int
}

// ListCapabilitiesForOrg returns capabilities across all the org's servers,
// optionally filtered by server, kind, and case-insensitive name substring.
// Caller is responsible for sane Limit; zero means "no limit" — chat tool
// clamps before calling.
func (d *Discovery) ListCapabilitiesForOrg(ctx context.Context, orgID uuid.UUID, f CapabilityFilter) ([]CapabilityRow, error) {
	args := []any{orgID}
	where := "s.org_id = $1"
	if f.ServerID != nil {
		args = append(args, *f.ServerID)
		where += " and s.id = $" + itoa(len(args))
	}
	if f.Kind != "" {
		args = append(args, f.Kind)
		where += " and c.kind = $" + itoa(len(args))
	}
	if f.NameContains != "" {
		args = append(args, "%"+f.NameContains+"%")
		where += " and c.name ilike $" + itoa(len(args))
	}
	q := `
		select c.id, s.id, s.name, c.kind, c.name, coalesce(c.description, ''), c.enabled
		from mcp_capabilities c
		join mcp_servers s on s.id = c.mcp_server_id
		where ` + where + `
		order by s.name asc, c.kind asc, c.name asc`
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += " limit $" + itoa(len(args))
	}
	rows, err := d.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CapabilityRow{}
	for rows.Next() {
		var r CapabilityRow
		if err := rows.Scan(&r.ID, &r.ServerID, &r.ServerName, &r.Kind, &r.Name, &r.Description, &r.Enabled); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// itoa is a local-tiny strconv.Itoa to avoid an extra import in this file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// nullableWorkload turns an optional workload bag into four *string values
// suitable for direct pgx parameter binding — empty strings (and a nil bag)
// become NULL so the mcp_invocations columns stay sparse for older / partial
// invocations.
func nullableWorkload(w *InvocationWorkload) (*string, *string, *string, *string) {
	if w == nil {
		return nil, nil, nil, nil
	}
	return strOrNil(w.Host), strOrNil(w.Cluster), strOrNil(w.Namespace), strOrNil(w.AgentClass)
}

func nullableNetwork(n *InvocationNetwork) (*string, *string, *string, *string) {
	if n == nil {
		return nil, nil, nil, nil
	}
	return strOrNil(n.Subnet), strOrNil(n.VPC), strOrNil(n.Zone), strOrNil(n.CallerIP)
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
