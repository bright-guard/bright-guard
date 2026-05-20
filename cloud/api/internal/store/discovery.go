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
		returning id, mcp_server_id, kind, name, description, schema, first_seen_at, last_seen_at`
	c := &models.MCPCapability{}
	err := d.Pool.QueryRow(ctx, q, mcpServerID, kind, name, description, jsonOrEmpty(schema)).Scan(
		&c.ID, &c.MCPServerID, &c.Kind, &c.Name, &c.Description, &c.Schema, &c.FirstSeenAt, &c.LastSeenAt,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (d *Discovery) InsertInvocation(ctx context.Context, orgID, mcpServerID uuid.UUID, capabilityKind, capabilityName string, caller json.RawMessage, status string, latencyMs int, at time.Time) error {
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

	const q = `
		insert into mcp_invocations (org_id, mcp_server_id, capability_id, capability_kind, capability_name, caller, status, latency_ms, at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err = d.Pool.Exec(ctx, q, orgID, mcpServerID, capID, capabilityKind, capabilityName, jsonOrEmpty(caller), status, latencyMs, at)
	return err
}

func (d *Discovery) ListServers(ctx context.Context, orgID uuid.UUID) ([]models.MCPServerWithCounts, error) {
	const q = `
		select s.id, s.org_id, s.gateway_id, s.connection_id, s.name, s.address, s.transport, s.version, s.metadata,
		       s.first_seen_at, s.last_seen_at,
		       coalesce(g.name, '') as gateway_name,
		       coalesce(mc.name, '') as connection_name,
		       coalesce(c.cnt, 0) as capability_count
		from mcp_servers s
		left join gateways g on g.id = s.gateway_id
		left join mcp_connections mc on mc.id = s.connection_id
		left join (
			select mcp_server_id, count(*) as cnt
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
			&s.GatewayName, &s.ConnectionName, &s.CapabilityCount,
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
		       g.name as gateway_name,
		       '' as connection_name,
		       coalesce(c.cnt, 0) as capability_count
		from mcp_servers s
		join gateways g on g.id = s.gateway_id
		left join (
			select mcp_server_id, count(*) as cnt
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
			&s.GatewayName, &s.ConnectionName, &s.CapabilityCount,
		); err != nil {
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
		       coalesce(g.name, ''), coalesce(mc.name, '')
		from mcp_servers s
		left join gateways g on g.id = s.gateway_id
		left join mcp_connections mc on mc.id = s.connection_id
		where s.org_id = $1 and s.id = $2`
	err := d.Pool.QueryRow(ctx, sq, orgID, id).Scan(
		&det.ID, &det.OrgID, &det.GatewayID, &det.ConnectionID, &det.Name, &det.Address, &det.Transport, &det.Version, &det.Metadata,
		&det.FirstSeenAt, &det.LastSeenAt, &det.GatewayName, &det.ConnectionName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	const cq = `
		select id, mcp_server_id, kind, name, description, schema, first_seen_at, last_seen_at
		from mcp_capabilities where mcp_server_id = $1
		order by kind, name`
	crows, err := d.Pool.Query(ctx, cq, id)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	det.Capabilities = []models.MCPCapability{}
	for crows.Next() {
		var c models.MCPCapability
		if err := crows.Scan(&c.ID, &c.MCPServerID, &c.Kind, &c.Name, &c.Description, &c.Schema, &c.FirstSeenAt, &c.LastSeenAt); err != nil {
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
