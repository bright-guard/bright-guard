package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Activity struct {
	Pool *pgxpool.Pool
}

type ActivityFilter struct {
	From            time.Time
	To              time.Time
	CapabilityKinds []string
	Statuses        []string
	MCPServerID     *uuid.UUID
	Q               string
	Limit           int
	Cursor          string
}

type ActivityRowServer struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type ActivityRow struct {
	ID             uuid.UUID             `json:"id"`
	At             time.Time             `json:"at"`
	MCPServer      ActivityRowServer     `json:"mcpServer"`
	CapabilityKind string                `json:"capabilityKind"`
	CapabilityName string                `json:"capabilityName"`
	Status         string                `json:"status"`
	LatencyMs      int                   `json:"latencyMs"`
	Caller         json.RawMessage       `json:"caller"`
	Decisions      []ActivityRowDecision `json:"decisions"`
}

// ActivityRowDecision is the per-row chip payload — only matched=true rows are
// returned so the frontend can show a "by Policy {name}" tag without a second
// roundtrip.
type ActivityRowDecision struct {
	PolicyID   uuid.UUID `json:"policyId"`
	PolicyName string    `json:"policyName"`
	Action     string    `json:"action"`
}

type StatusTotals struct {
	OK     int `json:"ok"`
	Error  int `json:"error"`
	Denied int `json:"denied"`
}

type CapabilityKindTotals struct {
	Tool     int `json:"tool"`
	Resource int `json:"resource"`
	Prompt   int `json:"prompt"`
}

type TopCapability struct {
	CapabilityKind string `json:"capabilityKind"`
	CapabilityName string `json:"capabilityName"`
	Count          int    `json:"count"`
}

type TopCaller struct {
	Caller json.RawMessage `json:"caller"`
	Count  int             `json:"count"`
}

type Summary struct {
	TotalInvocations int                  `json:"totalInvocations"`
	ByStatus         StatusTotals         `json:"byStatus"`
	ByCapabilityKind CapabilityKindTotals `json:"byCapabilityKind"`
	TopCapabilities  []TopCapability      `json:"topCapabilities"`
	TopCallers       []TopCaller          `json:"topCallers"`
}

type cursorPayload struct {
	At time.Time `json:"at"`
	ID uuid.UUID `json:"id"`
}

func EncodeCursor(at time.Time, id uuid.UUID) string {
	b, _ := json.Marshal(cursorPayload{At: at.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeCursor(s string) (time.Time, uuid.UUID, error) {
	if s == "" {
		return time.Time{}, uuid.Nil, errors.New("empty cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	var p cursorPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return time.Time{}, uuid.Nil, err
	}
	return p.At, p.ID, nil
}

// buildActivityWhere composes the SQL where-clause + bind args for the filter.
// Exposed (lower-case in test) — keeps query construction unit-testable.
func buildActivityWhere(orgID uuid.UUID, f ActivityFilter) (string, []any, error) {
	var clauses []string
	args := []any{orgID}
	clauses = append(clauses, "i.org_id = $1")

	if !f.From.IsZero() {
		args = append(args, f.From)
		clauses = append(clauses, fmt.Sprintf("i.at >= $%d", len(args)))
	}
	if !f.To.IsZero() {
		args = append(args, f.To)
		clauses = append(clauses, fmt.Sprintf("i.at < $%d", len(args)))
	}
	if len(f.CapabilityKinds) > 0 {
		args = append(args, f.CapabilityKinds)
		clauses = append(clauses, fmt.Sprintf("i.capability_kind = any($%d::text[])", len(args)))
	}
	if len(f.Statuses) > 0 {
		args = append(args, f.Statuses)
		clauses = append(clauses, fmt.Sprintf("i.status = any($%d::text[])", len(args)))
	}
	if f.MCPServerID != nil {
		args = append(args, *f.MCPServerID)
		clauses = append(clauses, fmt.Sprintf("i.mcp_server_id = $%d", len(args)))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		idx := len(args)
		clauses = append(clauses, fmt.Sprintf("(lower(i.capability_name) like $%d or lower(s.name) like $%d)", idx, idx))
	}
	return strings.Join(clauses, " and "), args, nil
}

func (a *Activity) List(ctx context.Context, orgID uuid.UUID, f ActivityFilter) ([]ActivityRow, string, StatusTotals, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	where, args, err := buildActivityWhere(orgID, f)
	if err != nil {
		return nil, "", StatusTotals{}, err
	}

	// totals across the whole filtered window (cursor-independent)
	totalsQ := `
		select
			count(*) filter (where i.status = 'ok')     as ok,
			count(*) filter (where i.status = 'error')  as err,
			count(*) filter (where i.status = 'denied') as denied
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		where ` + where
	var totals StatusTotals
	if err := a.Pool.QueryRow(ctx, totalsQ, args...).Scan(&totals.OK, &totals.Error, &totals.Denied); err != nil {
		return nil, "", StatusTotals{}, err
	}

	// page query — apply cursor on top of base filter
	pageArgs := append([]any{}, args...)
	pageWhere := where
	if f.Cursor != "" {
		curAt, curID, err := DecodeCursor(f.Cursor)
		if err != nil {
			return nil, "", StatusTotals{}, fmt.Errorf("invalid cursor: %w", err)
		}
		pageArgs = append(pageArgs, curAt, curID)
		pageWhere += fmt.Sprintf(" and (i.at, i.id) < ($%d, $%d)", len(pageArgs)-1, len(pageArgs))
	}
	pageArgs = append(pageArgs, limit+1)

	listQ := `
		select i.id, i.at, s.id, s.name, i.capability_kind, i.capability_name,
		       i.status, i.latency_ms, i.caller
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		where ` + pageWhere + `
		order by i.at desc, i.id desc
		limit $` + fmt.Sprintf("%d", len(pageArgs))

	rows, err := a.Pool.Query(ctx, listQ, pageArgs...)
	if err != nil {
		return nil, "", StatusTotals{}, err
	}
	defer rows.Close()

	out := make([]ActivityRow, 0, limit)
	ids := make([]uuid.UUID, 0, limit)
	for rows.Next() {
		var r ActivityRow
		if err := rows.Scan(&r.ID, &r.At, &r.MCPServer.ID, &r.MCPServer.Name,
			&r.CapabilityKind, &r.CapabilityName, &r.Status, &r.LatencyMs, &r.Caller); err != nil {
			return nil, "", StatusTotals{}, err
		}
		r.Decisions = []ActivityRowDecision{}
		out = append(out, r)
		ids = append(ids, r.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, "", StatusTotals{}, err
	}

	nextCursor := ""
	if len(out) > limit {
		last := out[limit-1]
		nextCursor = EncodeCursor(last.At, last.ID)
		out = out[:limit]
		ids = ids[:limit]
	}

	if len(ids) > 0 {
		decisionsByInv, err := a.matchedDecisions(ctx, orgID, ids)
		if err != nil {
			return nil, "", StatusTotals{}, err
		}
		for i := range out {
			if decs, ok := decisionsByInv[out[i].ID]; ok {
				out[i].Decisions = decs
			}
		}
	}
	return out, nextCursor, totals, nil
}

// matchedDecisions returns the per-invocation list of matched policy chips for
// the given invocation ids. Single round-trip; only matched=true rows.
func (a *Activity) matchedDecisions(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]ActivityRowDecision, error) {
	const q = `
		select d.invocation_id, d.policy_id, p.name, d.action
		from mcp_invocation_decisions d
		join policies p on p.id = d.policy_id
		where p.org_id = $1 and d.invocation_id = any($2::uuid[]) and d.matched = true
		order by d.at asc`
	rows, err := a.Pool.Query(ctx, q, orgID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]ActivityRowDecision{}
	for rows.Next() {
		var invID, polID uuid.UUID
		var name, action string
		if err := rows.Scan(&invID, &polID, &name, &action); err != nil {
			return nil, err
		}
		out[invID] = append(out[invID], ActivityRowDecision{
			PolicyID:   polID,
			PolicyName: name,
			Action:     action,
		})
	}
	return out, rows.Err()
}

func (a *Activity) Summary(ctx context.Context, orgID uuid.UUID, from, to time.Time) (Summary, error) {
	f := ActivityFilter{From: from, To: to}
	where, args, err := buildActivityWhere(orgID, f)
	if err != nil {
		return Summary{}, err
	}

	var sum Summary
	totalsQ := `
		select
			count(*)                                       as total,
			count(*) filter (where i.status = 'ok')        as ok,
			count(*) filter (where i.status = 'error')     as err,
			count(*) filter (where i.status = 'denied')    as denied,
			count(*) filter (where i.capability_kind = 'tool')     as tool,
			count(*) filter (where i.capability_kind = 'resource') as resource,
			count(*) filter (where i.capability_kind = 'prompt')   as prompt
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		where ` + where
	if err := a.Pool.QueryRow(ctx, totalsQ, args...).Scan(
		&sum.TotalInvocations,
		&sum.ByStatus.OK, &sum.ByStatus.Error, &sum.ByStatus.Denied,
		&sum.ByCapabilityKind.Tool, &sum.ByCapabilityKind.Resource, &sum.ByCapabilityKind.Prompt,
	); err != nil {
		return Summary{}, err
	}

	capQ := `
		select i.capability_kind, i.capability_name, count(*) as c
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		where ` + where + `
		group by i.capability_kind, i.capability_name
		order by c desc, i.capability_kind, i.capability_name
		limit 10`
	crows, err := a.Pool.Query(ctx, capQ, args...)
	if err != nil {
		return Summary{}, err
	}
	defer crows.Close()
	sum.TopCapabilities = []TopCapability{}
	for crows.Next() {
		var tc TopCapability
		if err := crows.Scan(&tc.CapabilityKind, &tc.CapabilityName, &tc.Count); err != nil {
			return Summary{}, err
		}
		sum.TopCapabilities = append(sum.TopCapabilities, tc)
	}
	if err := crows.Err(); err != nil {
		return Summary{}, err
	}

	// Group by jsonb caller — cast to text for grouping, return raw json.
	callerQ := `
		select i.caller, count(*) as c
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		where ` + where + `
		group by i.caller::text, i.caller
		order by c desc
		limit 10`
	krows, err := a.Pool.Query(ctx, callerQ, args...)
	if err != nil {
		return Summary{}, err
	}
	defer krows.Close()
	sum.TopCallers = []TopCaller{}
	for krows.Next() {
		var tc TopCaller
		if err := krows.Scan(&tc.Caller, &tc.Count); err != nil {
			return Summary{}, err
		}
		sum.TopCallers = append(sum.TopCallers, tc)
	}
	return sum, krows.Err()
}
