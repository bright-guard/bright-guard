package chat

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

// BudgetStatus reports whether the org has burned through its daily token
// quota. ResetAt is midnight UTC of the next day — a quick & honest signal
// the SPA can render to the user.
type BudgetStatus struct {
	Used     int64     `json:"used"`
	Budget   int64     `json:"budget"`
	OverBudget bool    `json:"overBudget"`
	ResetAt  time.Time `json:"resetAt"`
}

// CheckBudget reads today's usage and compares to budget. Budget <= 0 is
// treated as "no limit" (useful for local-dev).
func CheckBudget(ctx context.Context, c *store.Chat, orgID uuid.UUID, budget int64, now time.Time) (BudgetStatus, error) {
	in, out, err := c.DailyUsage(ctx, orgID, now)
	if err != nil {
		return BudgetStatus{}, err
	}
	used := in + out
	r := nextUTCMidnight(now)
	return BudgetStatus{
		Used:       used,
		Budget:     budget,
		OverBudget: budget > 0 && used >= budget,
		ResetAt:    r,
	}, nil
}

// RecordUsage bumps today's per-org token counters.
func RecordUsage(ctx context.Context, c *store.Chat, orgID uuid.UUID, now time.Time, in, out int) error {
	return c.AddUsage(ctx, orgID, now, int64(in), int64(out))
}

func nextUTCMidnight(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day()+1, 0, 0, 0, 0, time.UTC)
}
