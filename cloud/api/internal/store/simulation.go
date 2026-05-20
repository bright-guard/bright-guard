package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ListInvocationsForSimulation is the UC5 simulator's bulk loader. It returns
// up to `limit` invocations for `orgID` whose at is in [since, now), with all
// the context columns the policy CEL env consumes already joined in.
//
// Implementation note: this delegates to ListInvocationsInWindow (which the
// audit sweep also uses) so any future enrichment (workload, network, …) added
// to the sweep loader automatically flows to the simulator with no duplication.
// Keep this method here, not on Policies, so the policies.go file stays owned
// by its primary author for parallel work.
func (p *Policies) ListInvocationsForSimulation(
	ctx context.Context, orgID uuid.UUID, since time.Time, limit int,
) ([]InvocationContext, error) {
	now := time.Now().UTC()
	if !since.Before(now) {
		return []InvocationContext{}, nil
	}
	return p.ListInvocationsInWindow(ctx, orgID, since, now, limit)
}
