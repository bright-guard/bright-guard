package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

const anonymousMarker = "_anonymous_"

// Callers is the data layer for org_callers (UC9 detection).
type Callers struct {
	Pool *pgxpool.Pool
}

// CallerFilter matches the List endpoint query.
type CallerFilter struct {
	From         time.Time
	To           time.Time
	FlaggedOnly  bool
	Q            string
	Limit        int
	Cursor       string
}

// CallerTotals are the cursor-independent totals for the filtered window.
type CallerTotals struct {
	Total      int `json:"total"`
	FlaggedNew int `json:"flaggedNew"`
}

// SeenRow describes one observed caller event for batch upsert during backfill.
type SeenRow struct {
	OrgID      uuid.UUID
	Signature  string
	Label      string
	CallerJSON json.RawMessage
	At         time.Time
}

// CanonicalizeCaller returns a stable, key-sorted JSON encoding suitable for
// hashing. Empty/null inputs collapse to the literal "_anonymous_".
func CanonicalizeCaller(raw json.RawMessage) string {
	if len(raw) == 0 {
		return anonymousMarker
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return anonymousMarker
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// Not valid JSON; fall back to a literal-byte representation. This is
		// rare since the column is jsonb but defensible.
		return string(raw)
	}
	if v == nil {
		return anonymousMarker
	}
	if m, ok := v.(map[string]any); ok && len(m) == 0 {
		return anonymousMarker
	}
	return canonicalEncode(v)
}

func canonicalEncode(v any) string {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			b.WriteString(canonicalEncode(t[k]))
		}
		b.WriteByte('}')
		return b.String()
	case []any:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(canonicalEncode(e))
		}
		b.WriteByte(']')
		return b.String()
	default:
		out, _ := json.Marshal(t)
		return string(out)
	}
}

// SignatureFor produces the hex SHA256 of the canonicalized caller payload.
func SignatureFor(raw json.RawMessage) string {
	c := CanonicalizeCaller(raw)
	sum := sha256.Sum256([]byte(c))
	return hex.EncodeToString(sum[:])
}

// LabelFor extracts a human-friendly identifier from a caller blob. If no
// known field is present, returns "caller_<first-6-of-sig>".
func LabelFor(raw json.RawMessage, signature string) string {
	if len(raw) > 0 {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err == nil {
			for _, k := range []string{"agent", "user", "userEmail", "client"} {
				if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
	}
	if signature == "" {
		return "caller_anon"
	}
	if len(signature) >= 6 {
		return "caller_" + signature[:6]
	}
	return "caller_" + signature
}

// Upsert inserts or bumps a caller row. last_seen_at moves to max(existing,at);
// invocation_count increments by 1; label only updates if currently empty.
func (c *Callers) Upsert(ctx context.Context, orgID uuid.UUID, signature, label string, callerJSON json.RawMessage, at time.Time) error {
	const q = `
		insert into org_callers (org_id, signature, label, caller, first_seen_at, last_seen_at, invocation_count, flagged_new)
		values ($1, $2, $3, $4, $5, $5, 1, true)
		on conflict (org_id, signature) do update set
		  last_seen_at = greatest(org_callers.last_seen_at, excluded.last_seen_at),
		  invocation_count = org_callers.invocation_count + 1,
		  label = case when org_callers.label = '' then excluded.label else org_callers.label end`
	_, err := c.Pool.Exec(ctx, q, orgID, signature, label, jsonOrEmpty(callerJSON), at.UTC())
	return err
}

// MarkSeenBatch processes a slice of observed callers in one transaction. The
// upsert is idempotent: if a signature appears N times in `rows`, its
// invocation_count grows by N.
func (c *Callers) MarkSeenBatch(ctx context.Context, rows []SeenRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := c.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	const q = `
		insert into org_callers (org_id, signature, label, caller, first_seen_at, last_seen_at, invocation_count, flagged_new)
		values ($1, $2, $3, $4, $5, $5, 1, true)
		on conflict (org_id, signature) do update set
		  last_seen_at = greatest(org_callers.last_seen_at, excluded.last_seen_at),
		  invocation_count = org_callers.invocation_count + 1,
		  label = case when org_callers.label = '' then excluded.label else org_callers.label end`
	for _, r := range rows {
		if _, err := tx.Exec(ctx, q, r.OrgID, r.Signature, r.Label, jsonOrEmpty(r.CallerJSON), r.At.UTC()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// FlagAgeRollover clears flagged_new for any caller whose first_seen_at is
// older than `age`.
func (c *Callers) FlagAgeRollover(ctx context.Context, age time.Duration) error {
	const q = `update org_callers set flagged_new = false where flagged_new = true and first_seen_at < now() - $1::interval`
	// Pass age as a Postgres interval string.
	interval := fmt.Sprintf("%d seconds", int64(age.Seconds()))
	_, err := c.Pool.Exec(ctx, q, interval)
	return err
}

func buildCallerWhere(orgID uuid.UUID, f CallerFilter) (string, []any) {
	clauses := []string{"org_id = $1"}
	args := []any{orgID}
	if !f.From.IsZero() {
		args = append(args, f.From)
		clauses = append(clauses, fmt.Sprintf("last_seen_at >= $%d", len(args)))
	}
	if !f.To.IsZero() {
		args = append(args, f.To)
		clauses = append(clauses, fmt.Sprintf("last_seen_at < $%d", len(args)))
	}
	if f.FlaggedOnly {
		clauses = append(clauses, "flagged_new = true")
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		clauses = append(clauses, fmt.Sprintf("lower(label) like $%d", len(args)))
	}
	return strings.Join(clauses, " and "), args
}

func (c *Callers) List(ctx context.Context, orgID uuid.UUID, f CallerFilter) ([]models.OrgCaller, string, CallerTotals, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	where, args := buildCallerWhere(orgID, f)

	totalsQ := `
		select
		  count(*),
		  count(*) filter (where flagged_new)
		from org_callers
		where ` + where
	var totals CallerTotals
	if err := c.Pool.QueryRow(ctx, totalsQ, args...).Scan(&totals.Total, &totals.FlaggedNew); err != nil {
		return nil, "", CallerTotals{}, err
	}

	pageArgs := append([]any{}, args...)
	pageWhere := where
	if f.Cursor != "" {
		curAt, curID, err := DecodeCursor(f.Cursor)
		if err != nil {
			return nil, "", CallerTotals{}, fmt.Errorf("invalid cursor: %w", err)
		}
		pageArgs = append(pageArgs, curAt, curID)
		pageWhere += fmt.Sprintf(" and (last_seen_at, id) < ($%d, $%d)", len(pageArgs)-1, len(pageArgs))
	}
	pageArgs = append(pageArgs, limit+1)

	listQ := `
		select id, org_id, signature, label, caller, first_seen_at, last_seen_at, invocation_count, flagged_new
		from org_callers
		where ` + pageWhere + `
		order by last_seen_at desc, id desc
		limit $` + fmt.Sprintf("%d", len(pageArgs))

	rows, err := c.Pool.Query(ctx, listQ, pageArgs...)
	if err != nil {
		return nil, "", CallerTotals{}, err
	}
	defer rows.Close()

	out := make([]models.OrgCaller, 0, limit)
	for rows.Next() {
		var r models.OrgCaller
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Signature, &r.Label, &r.Caller,
			&r.FirstSeenAt, &r.LastSeenAt, &r.InvocationCount, &r.FlaggedNew); err != nil {
			return nil, "", CallerTotals{}, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", CallerTotals{}, err
	}

	nextCursor := ""
	if len(out) > limit {
		last := out[limit-1]
		nextCursor = EncodeCursor(last.LastSeenAt, last.ID)
		out = out[:limit]
	}
	return out, nextCursor, totals, nil
}

// Get loads a caller row plus its top servers + recent invocations. Top
// servers and recent invocations are looked up by (org, signature) against
// mcp_invocations re-hashed on the fly — cheap because invocations is already
// org-indexed and the per-caller volume is small.
func (c *Callers) Get(ctx context.Context, orgID, id uuid.UUID) (*models.OrgCallerDetail, error) {
	det := &models.OrgCallerDetail{}
	const sq = `
		select id, org_id, signature, label, caller, first_seen_at, last_seen_at, invocation_count, flagged_new
		from org_callers where org_id = $1 and id = $2`
	err := c.Pool.QueryRow(ctx, sq, orgID, id).Scan(
		&det.ID, &det.OrgID, &det.Signature, &det.Label, &det.Caller,
		&det.FirstSeenAt, &det.LastSeenAt, &det.InvocationCount, &det.FlaggedNew,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Top servers — match invocations to this caller by signature using the
	// stored caller JSON. Re-hashing in SQL is awkward; instead match on
	// caller::text = stored caller::text. This is exact for callers that the
	// shim emits with stable key order; for variant key orders we still match
	// because mcp_invocations.caller stored via pg jsonb normalizes keys.
	det.TopServers = []models.OrgCallerTopServer{}
	const topQ = `
		select s.id, s.name, count(*) as c
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		where i.org_id = $1 and i.caller = $2::jsonb
		group by s.id, s.name
		order by c desc, s.name
		limit 10`
	trows, err := c.Pool.Query(ctx, topQ, orgID, string(det.Caller))
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var t models.OrgCallerTopServer
		if err := trows.Scan(&t.MCPServerID, &t.Name, &t.Count); err != nil {
			return nil, err
		}
		det.TopServers = append(det.TopServers, t)
	}
	if err := trows.Err(); err != nil {
		return nil, err
	}

	det.RecentInvocations = []models.MCPInvocation{}
	const recentQ = `
		select id, org_id, mcp_server_id, capability_id, capability_kind, capability_name, caller, status, latency_ms, at
		from mcp_invocations
		where org_id = $1 and caller = $2::jsonb
		order by at desc
		limit 50`
	rrows, err := c.Pool.Query(ctx, recentQ, orgID, string(det.Caller))
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var inv models.MCPInvocation
		if err := rrows.Scan(&inv.ID, &inv.OrgID, &inv.MCPServerID, &inv.CapabilityID,
			&inv.CapabilityKind, &inv.CapabilityName, &inv.Caller, &inv.Status, &inv.LatencyMs, &inv.At); err != nil {
			return nil, err
		}
		det.RecentInvocations = append(det.RecentInvocations, inv)
	}
	return det, rrows.Err()
}

// Acknowledge clears flagged_new for a single caller.
func (c *Callers) Acknowledge(ctx context.Context, orgID, id uuid.UUID) error {
	const q = `update org_callers set flagged_new = false where org_id = $1 and id = $2`
	tag, err := c.Pool.Exec(ctx, q, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SweepNew scans mcp_invocations newer than each org's current watermark and
// upserts caller rows. The watermark is derived as
// (max(last_seen_at) over org_callers for that org) minus a 1-minute buffer.
// Orgs with no callers yet sweep all-time history (the initial backfill).
func (c *Callers) SweepNew(ctx context.Context, buffer time.Duration) error {
	// One pass per org. The set of orgs is the union of orgs visible in
	// mcp_invocations + orgs that already have callers; we just join from
	// mcp_invocations since callers without invocations would be a no-op.
	const orgsQ = `
		select distinct i.org_id,
		       coalesce(
		         (select max(last_seen_at) from org_callers oc where oc.org_id = i.org_id),
		         'epoch'::timestamptz
		       ) as watermark
		from mcp_invocations i`
	orgRows, err := c.Pool.Query(ctx, orgsQ)
	if err != nil {
		return err
	}
	type orgWM struct {
		ID uuid.UUID
		WM time.Time
	}
	var orgs []orgWM
	for orgRows.Next() {
		var ow orgWM
		if err := orgRows.Scan(&ow.ID, &ow.WM); err != nil {
			orgRows.Close()
			return err
		}
		orgs = append(orgs, ow)
	}
	orgRows.Close()
	if err := orgRows.Err(); err != nil {
		return err
	}

	for _, ow := range orgs {
		since := ow.WM.Add(-buffer)
		const invQ = `
			select caller, at from mcp_invocations
			where org_id = $1 and at > $2
			order by at asc`
		rows, err := c.Pool.Query(ctx, invQ, ow.ID, since)
		if err != nil {
			return err
		}
		var batch []SeenRow
		for rows.Next() {
			var caller json.RawMessage
			var at time.Time
			if err := rows.Scan(&caller, &at); err != nil {
				rows.Close()
				return err
			}
			sig := SignatureFor(caller)
			label := LabelFor(caller, sig)
			batch = append(batch, SeenRow{
				OrgID:      ow.ID,
				Signature:  sig,
				Label:      label,
				CallerJSON: caller,
				At:         at,
			})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if err := c.MarkSeenBatch(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}
