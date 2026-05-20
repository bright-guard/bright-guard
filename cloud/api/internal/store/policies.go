package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

// Policies is the data layer for CEL policies and their per-invocation
// decisions. Audit-only — nothing here ever blocks a request.
type Policies struct {
	Pool *pgxpool.Pool
}

type PolicyCreate struct {
	OrgID       uuid.UUID
	CreatedBy   uuid.UUID
	Name        string
	Description string
	Expression  string
	Action      models.PolicyAction
	Enabled     bool
}

func (p *Policies) Create(ctx context.Context, in PolicyCreate) (*models.Policy, error) {
	const q = `
		insert into policies (org_id, name, description, expression, action, enabled, created_by)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning id, org_id, name, description, expression, action, enabled, created_by, created_at, updated_at`
	out := &models.Policy{}
	err := p.Pool.QueryRow(ctx, q,
		in.OrgID, in.Name, in.Description, in.Expression, string(in.Action), in.Enabled, in.CreatedBy,
	).Scan(
		&out.ID, &out.OrgID, &out.Name, &out.Description, &out.Expression, &out.Action,
		&out.Enabled, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Policies) Get(ctx context.Context, orgID, id uuid.UUID) (*models.Policy, error) {
	const q = `
		select id, org_id, name, description, expression, action, enabled, created_by, created_at, updated_at
		from policies where org_id = $1 and id = $2`
	out := &models.Policy{}
	err := p.Pool.QueryRow(ctx, q, orgID, id).Scan(
		&out.ID, &out.OrgID, &out.Name, &out.Description, &out.Expression, &out.Action,
		&out.Enabled, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// Populate last24h match count for the detail view.
	const matchQ = `
		select count(*) from mcp_invocation_decisions
		where policy_id = $1 and matched = true and at > now() - interval '24 hours'`
	_ = p.Pool.QueryRow(ctx, matchQ, id).Scan(&out.Last24hMatches)
	return out, nil
}

// List returns all policies for an org, newest first, with a recent-match count
// joined inline so the list view can show "Last 24h: N matches".
func (p *Policies) List(ctx context.Context, orgID uuid.UUID) ([]models.Policy, error) {
	const q = `
		select p.id, p.org_id, p.name, p.description, p.expression, p.action, p.enabled,
		       p.created_by, p.created_at, p.updated_at,
		       coalesce(m.cnt, 0)
		from policies p
		left join (
			select policy_id, count(*) as cnt
			from mcp_invocation_decisions
			where matched = true and at > now() - interval '24 hours'
			group by policy_id
		) m on m.policy_id = p.id
		where p.org_id = $1
		order by p.created_at desc`
	rows, err := p.Pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Policy{}
	for rows.Next() {
		var m models.Policy
		if err := rows.Scan(
			&m.ID, &m.OrgID, &m.Name, &m.Description, &m.Expression, &m.Action, &m.Enabled,
			&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.Last24hMatches,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PolicyPatch is a sparse update; nil fields are not touched.
type PolicyPatch struct {
	Name        *string
	Description *string
	Expression  *string
	Action      *models.PolicyAction
	Enabled     *bool
}

// Update applies a partial patch. Returns the updated row.
// The caller is responsible for compiling the new expression first so the API
// can surface a CEL compile error as a 400 before any DB write.
func (p *Policies) Update(ctx context.Context, orgID, id uuid.UUID, patch PolicyPatch) (*models.Policy, error) {
	const q = `
		update policies set
		  name        = coalesce($3, name),
		  description = coalesce($4, description),
		  expression  = coalesce($5, expression),
		  action      = coalesce($6, action),
		  enabled     = coalesce($7, enabled),
		  updated_at  = now()
		where org_id = $1 and id = $2
		returning id, org_id, name, description, expression, action, enabled, created_by, created_at, updated_at`
	out := &models.Policy{}
	var action *string
	if patch.Action != nil {
		s := string(*patch.Action)
		action = &s
	}
	err := p.Pool.QueryRow(ctx, q,
		orgID, id, patch.Name, patch.Description, patch.Expression, action, patch.Enabled,
	).Scan(
		&out.ID, &out.OrgID, &out.Name, &out.Description, &out.Expression, &out.Action,
		&out.Enabled, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Policies) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	const q = `delete from policies where org_id = $1 and id = $2`
	tag, err := p.Pool.Exec(ctx, q, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// BundleFor returns the active (enabled) policies for an org along with the
// org's current policy_bundle_version. Used by the heartbeat handler to decide
// whether to ship a fresh bundle to a shim that's behind on its cached version.
func (p *Policies) BundleFor(ctx context.Context, orgID uuid.UUID) (int64, []models.Policy, error) {
	var version int64
	if err := p.Pool.QueryRow(ctx,
		`select policy_bundle_version from orgs where id = $1`, orgID).Scan(&version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, ErrNotFound
		}
		return 0, nil, err
	}
	policies, err := p.ListEnabledForOrg(ctx, orgID)
	if err != nil {
		return 0, nil, err
	}
	return version, policies, nil
}

// ListEnabledForOrg returns enabled policies for an org. Used by the sweep
// goroutine, which compiles each per tick.
func (p *Policies) ListEnabledForOrg(ctx context.Context, orgID uuid.UUID) ([]models.Policy, error) {
	const q = `
		select id, org_id, name, description, expression, action, enabled, created_by, created_at, updated_at
		from policies where org_id = $1 and enabled = true`
	rows, err := p.Pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Policy{}
	for rows.Next() {
		var m models.Policy
		if err := rows.Scan(
			&m.ID, &m.OrgID, &m.Name, &m.Description, &m.Expression, &m.Action, &m.Enabled,
			&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DecisionRow is one row queued for bulk insert.
type DecisionRow struct {
	InvocationID uuid.UUID
	PolicyID     uuid.UUID
	Matched      bool
	Action       models.PolicyAction
}

// RecordDecisions bulk-inserts decisions, upserting on (invocation_id, policy_id)
// so a re-evaluation overwrites prior verdicts (e.g. after a policy expression
// change). Pure additive — no decisions ever cause an invocation to fail.
func (p *Policies) RecordDecisions(ctx context.Context, decisions []DecisionRow) error {
	if len(decisions) == 0 {
		return nil
	}
	// pgx CopyFrom would be ideal here; for now we batch into one INSERT with
	// multiple value rows, which is plenty for the scheduler's per-tick caps.
	args := make([]any, 0, len(decisions)*4)
	values := make([]string, 0, len(decisions))
	for i, d := range decisions {
		base := i*4 + 1
		values = append(values, fmt.Sprintf("($%d, $%d, $%d, $%d)", base, base+1, base+2, base+3))
		args = append(args, d.InvocationID, d.PolicyID, d.Matched, string(d.Action))
	}
	q := `
		insert into mcp_invocation_decisions (invocation_id, policy_id, matched, action)
		values ` + join(values, ", ") + `
		on conflict (invocation_id, policy_id) do update
		  set matched = excluded.matched, action = excluded.action, at = now()`
	_, err := p.Pool.Exec(ctx, q, args...)
	return err
}

// DecisionsForInvocations returns the matched-only decisions for a set of
// invocation ids. The activity store also has its own equivalent query inline
// for performance; this method exists for direct callers (replay tooling).
func (p *Policies) DecisionsForInvocations(
	ctx context.Context, orgID uuid.UUID, invocationIDs []uuid.UUID,
) (map[uuid.UUID][]models.Decision, error) {
	out := map[uuid.UUID][]models.Decision{}
	if len(invocationIDs) == 0 {
		return out, nil
	}
	const q = `
		select d.invocation_id, d.policy_id, p.name, d.matched, d.action, d.at
		from mcp_invocation_decisions d
		join policies p on p.id = d.policy_id
		where p.org_id = $1 and d.invocation_id = any($2::uuid[]) and d.matched = true
		order by d.at desc`
	rows, err := p.Pool.Query(ctx, q, orgID, invocationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d models.Decision
		var action string
		if err := rows.Scan(&d.InvocationID, &d.PolicyID, &d.PolicyName, &d.Matched, &action, &d.At); err != nil {
			return nil, err
		}
		d.Action = models.PolicyAction(action)
		out[d.InvocationID] = append(out[d.InvocationID], d)
	}
	return out, rows.Err()
}

// ListOrgsWithDuePolicies returns up to `limit` org_ids that have enabled
// policies. Used by the sweep tick to bound work per cycle.
func (p *Policies) ListOrgsWithDuePolicies(ctx context.Context, limit int) ([]uuid.UUID, error) {
	const q = `
		select distinct org_id from policies where enabled = true
		order by org_id
		limit $1`
	rows, err := p.Pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SweepWatermark returns the per-org watermark (max invocations.at already
// processed). Defaults to epoch if no row exists yet.
func (p *Policies) SweepWatermark(ctx context.Context, orgID uuid.UUID) (time.Time, error) {
	const q = `select watermark from policy_sweep_state where org_id = $1`
	var w time.Time
	err := p.Pool.QueryRow(ctx, q, orgID).Scan(&w)
	if errors.Is(err, pgx.ErrNoRows) {
		// Backstop: epoch in UTC.
		return time.Unix(0, 0).UTC(), nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return w, nil
}

// SetSweepWatermark advances the org watermark monotonically forward.
func (p *Policies) SetSweepWatermark(ctx context.Context, orgID uuid.UUID, at time.Time) error {
	const q = `
		insert into policy_sweep_state (org_id, watermark, updated_at)
		values ($1, $2, now())
		on conflict (org_id) do update
		  set watermark = greatest(policy_sweep_state.watermark, excluded.watermark),
		      updated_at = now()`
	_, err := p.Pool.Exec(ctx, q, orgID, at)
	return err
}

// InvocationContext is the snapshot the sweep + simulate paths feed to the CEL
// engine. Kept as a generic any-map so the engine package can stay
// store-independent.
type InvocationContext struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	At         time.Time
	Status     string
	Caller     json.RawMessage
	Server     map[string]string
	Capability map[string]string
}

// ListInvocationsForSweep loads invocations strictly newer than the watermark,
// up to `limit`. Each row is joined with its server + capability metadata so
// the eval path doesn't need any extra round-trips per row.
//
// Rows that already have decisions persisted are skipped — the shim ships
// decisions with each invocation when it has a bundle loaded, and the
// presence of those rows is the marker that says "client already evaluated,
// don't redo it server-side".
func (p *Policies) ListInvocationsForSweep(
	ctx context.Context, orgID uuid.UUID, after time.Time, limit int,
) ([]InvocationContext, error) {
	const q = `
		select i.id, i.org_id, i.at, i.status, i.caller,
		       s.id::text, s.name, s.transport, s.address, s.exposure_state,
		       i.capability_kind, i.capability_name,
		       coalesce(c.description, ''),
		       coalesce(oc.signature, ''), coalesce(oc.label, ''),
		       coalesce(oc.flagged_new, false),
		       (oc.acknowledged_at is not null)
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		left join mcp_capabilities c on c.id = i.capability_id
		left join org_callers oc on oc.org_id = i.org_id and oc.caller = i.caller
		where i.org_id = $1 and i.at > $2
		  and not exists (
		    select 1 from mcp_invocation_decisions d where d.invocation_id = i.id
		  )
		order by i.at asc
		limit $3`
	rows, err := p.Pool.Query(ctx, q, orgID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InvocationContext{}
	for rows.Next() {
		ic := InvocationContext{
			Server:     map[string]string{},
			Capability: map[string]string{},
		}
		var sID, sName, sTrans, sAddr, sExp string
		var capKind, capName, capDesc string
		var cSig, cLabel string
		var cFlagged, cAck bool
		if err := rows.Scan(
			&ic.ID, &ic.OrgID, &ic.At, &ic.Status, &ic.Caller,
			&sID, &sName, &sTrans, &sAddr, &sExp,
			&capKind, &capName, &capDesc,
			&cSig, &cLabel, &cFlagged, &cAck,
		); err != nil {
			return nil, err
		}
		ic.Server["id"] = sID
		ic.Server["name"] = sName
		ic.Server["transport"] = sTrans
		ic.Server["address"] = sAddr
		ic.Server["exposureState"] = sExp
		ic.Server["exposure_state"] = sExp
		ic.Capability["kind"] = capKind
		ic.Capability["name"] = capName
		ic.Capability["description"] = capDesc
		// Enrich the caller JSON with signature/label/flagged_new/acknowledged
		// so the CEL env exposes them as first-class fields. Done in-place on
		// a decoded copy so the original raw JSON survives for sites that
		// only need the inbound caller payload.
		ic.Caller = enrichCallerForCEL(ic.Caller, cSig, cLabel, cFlagged, cAck)
		out = append(out, ic)
	}
	return out, rows.Err()
}

// ListInvocationsInWindow is the simulate-mode loader. It loads up to `limit`
// invocations whose at falls in [from, to). Order is descending so simulate
// surfaces the most recent matches first.
func (p *Policies) ListInvocationsInWindow(
	ctx context.Context, orgID uuid.UUID, from, to time.Time, limit int,
) ([]InvocationContext, error) {
	const q = `
		select i.id, i.org_id, i.at, i.status, i.caller,
		       s.id::text, s.name, s.transport, s.address, s.exposure_state,
		       i.capability_kind, i.capability_name,
		       coalesce(c.description, ''),
		       coalesce(oc.signature, ''), coalesce(oc.label, ''),
		       coalesce(oc.flagged_new, false),
		       (oc.acknowledged_at is not null)
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		left join mcp_capabilities c on c.id = i.capability_id
		left join org_callers oc on oc.org_id = i.org_id and oc.caller = i.caller
		where i.org_id = $1 and i.at >= $2 and i.at < $3
		order by i.at desc
		limit $4`
	rows, err := p.Pool.Query(ctx, q, orgID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InvocationContext{}
	for rows.Next() {
		ic := InvocationContext{
			Server:     map[string]string{},
			Capability: map[string]string{},
		}
		var sID, sName, sTrans, sAddr, sExp string
		var capKind, capName, capDesc string
		var cSig, cLabel string
		var cFlagged, cAck bool
		if err := rows.Scan(
			&ic.ID, &ic.OrgID, &ic.At, &ic.Status, &ic.Caller,
			&sID, &sName, &sTrans, &sAddr, &sExp,
			&capKind, &capName, &capDesc,
			&cSig, &cLabel, &cFlagged, &cAck,
		); err != nil {
			return nil, err
		}
		ic.Server["id"] = sID
		ic.Server["name"] = sName
		ic.Server["transport"] = sTrans
		ic.Server["address"] = sAddr
		ic.Server["exposureState"] = sExp
		ic.Server["exposure_state"] = sExp
		ic.Capability["kind"] = capKind
		ic.Capability["name"] = capName
		ic.Capability["description"] = capDesc
		ic.Caller = enrichCallerForCEL(ic.Caller, cSig, cLabel, cFlagged, cAck)
		out = append(out, ic)
	}
	return out, rows.Err()
}

// enrichCallerForCEL merges signature/label/flagged_new/acknowledged into the
// raw caller jsonb so the CEL env exposes them as first-class fields under
// `caller`. The original keys are preserved when present (the inbound payload
// already had them); enrichment is additive.
func enrichCallerForCEL(raw json.RawMessage, sig, label string, flagged, acknowledged bool) json.RawMessage {
	m := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	if _, ok := m["signature"]; !ok && sig != "" {
		m["signature"] = sig
	}
	if _, ok := m["label"]; !ok && label != "" {
		m["label"] = label
	}
	if _, ok := m["flagged_new"]; !ok {
		m["flagged_new"] = flagged
	}
	if _, ok := m["acknowledged"]; !ok {
		m["acknowledged"] = acknowledged
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// join is a tiny strings.Join shim local to this file so we don't grow the
// already-busy import block above. Hand-rolled to keep the file self-contained.
func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	n := len(sep) * (len(parts) - 1)
	for _, s := range parts {
		n += len(s)
	}
	buf := make([]byte, 0, n)
	buf = append(buf, parts[0]...)
	for _, s := range parts[1:] {
		buf = append(buf, sep...)
		buf = append(buf, s...)
	}
	return string(buf)
}
