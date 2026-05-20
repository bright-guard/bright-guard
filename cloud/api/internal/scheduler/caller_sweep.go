package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	callerSweepInterval = 5 * time.Minute
	callerFlagAge       = 7 * 24 * time.Hour
	callerSweepBuffer   = 1 * time.Minute
	callerLockKey       = "bg-callers"
)

// CallerSweeper periodically re-scans mcp_invocations and updates the
// org_callers registry. It uses its own advisory-lock key, distinct from
// the discovery scheduler's, so the two run in parallel without blocking.
type CallerSweeper struct {
	Callers     *store.Callers
	Connections *store.Connections
	Interval    time.Duration
}

func NewCallerSweeper(callers *store.Callers, conns *store.Connections, interval time.Duration) *CallerSweeper {
	if interval <= 0 {
		interval = callerSweepInterval
	}
	return &CallerSweeper{Callers: callers, Connections: conns, Interval: interval}
}

func (s *CallerSweeper) Run(ctx context.Context) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *CallerSweeper) tick(ctx context.Context) {
	ok, err := s.Connections.TryAdvisoryLock(ctx, callerLockKey)
	if err != nil {
		log.Printf("caller sweep: advisory lock check failed: %v", err)
		return
	}
	if !ok {
		return
	}
	if err := s.Callers.SweepNew(ctx, callerSweepBuffer); err != nil {
		log.Printf("caller sweep: SweepNew failed: %v", err)
	}
	if err := s.Callers.FlagAgeRollover(ctx, callerFlagAge); err != nil {
		log.Printf("caller sweep: FlagAgeRollover failed: %v", err)
	}
}
