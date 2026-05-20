package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ExposureRow is the minimal projection needed by the sweep: an id and the
// address to classify.
type ExposureRow struct {
	ID      uuid.UUID
	Address string
}

// ListExposureDue returns up to limit servers whose exposure has never been
// classified or is older than staleBefore.
func (d *Discovery) ListExposureDue(ctx context.Context, staleBefore time.Time, limit int) ([]ExposureRow, error) {
	const q = `
		select id, address
		from mcp_servers
		where exposure_classified_at is null
		   or exposure_classified_at < $1
		order by coalesce(exposure_classified_at, to_timestamp(0)) asc
		limit $2`
	rows, err := d.Pool.Query(ctx, q, staleBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExposureRow{}
	for rows.Next() {
		var r ExposureRow
		if err := rows.Scan(&r.ID, &r.Address); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetExposure persists a single classification result.
func (d *Discovery) SetExposure(ctx context.Context, id uuid.UUID, state, reason string) error {
	const q = `
		update mcp_servers
		set exposure_state = $2,
		    exposure_reason = $3,
		    exposure_classified_at = now()
		where id = $1`
	_, err := d.Pool.Exec(ctx, q, id, state, reason)
	return err
}

// GetServerAddress fetches the address for one server, scoped to an org.
func (d *Discovery) GetServerAddress(ctx context.Context, orgID, id uuid.UUID) (string, error) {
	var addr string
	const q = `select address from mcp_servers where org_id = $1 and id = $2`
	err := d.Pool.QueryRow(ctx, q, orgID, id).Scan(&addr)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return addr, err
}

// ExposureCount is a (state, count) pair.
type ExposureCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

// CountExposuresByState returns counts of each exposure_state for an org.
// States with zero rows are included as zero so the UI can render a stable
// shape.
func (d *Discovery) CountExposuresByState(ctx context.Context, orgID uuid.UUID) ([]ExposureCount, error) {
	const q = `
		select exposure_state, count(*)::int
		from mcp_servers
		where org_id = $1
		group by exposure_state`
	rows, err := d.Pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]int{}
	for rows.Next() {
		var c ExposureCount
		if err := rows.Scan(&c.State, &c.Count); err != nil {
			return nil, err
		}
		seen[c.State] = c.Count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []ExposureCount{}
	for _, s := range []string{"public", "cloud_internal", "internal", "unreachable", "unknown"} {
		out = append(out, ExposureCount{State: s, Count: seen[s]})
	}
	return out, nil
}
