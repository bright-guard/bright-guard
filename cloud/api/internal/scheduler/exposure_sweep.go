package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/bright-guard/bright-guard/cloud/api/internal/exposure"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	exposureLockKey   = "bg-exposure-sweep"
	exposureStaleTTL  = 24 * time.Hour
	exposureBatchSize = 500
)

// ExposureSweep periodically (re)classifies mcp_servers.exposure_state based
// on the address column. It does NO network IO — see exposure.Classify.
type ExposureSweep struct {
	Connections *store.Connections // for advisory lock only
	Discovery   *store.Discovery
	Interval    time.Duration
}

// NewExposureSweep builds a sweep with sensible defaults.
func NewExposureSweep(conns *store.Connections, disc *store.Discovery, interval time.Duration) *ExposureSweep {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	return &ExposureSweep{
		Connections: conns,
		Discovery:   disc,
		Interval:    interval,
	}
}

// Run blocks until ctx is cancelled, running one tick on start and then on
// each interval.
func (e *ExposureSweep) Run(ctx context.Context) {
	t := time.NewTicker(e.Interval)
	defer t.Stop()
	e.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.tick(ctx)
		}
	}
}

func (e *ExposureSweep) tick(ctx context.Context) {
	ok, err := e.Connections.TryAdvisoryLock(ctx, exposureLockKey)
	if err != nil {
		log.Printf("exposure: advisory lock check failed: %v", err)
		return
	}
	if !ok {
		return
	}
	due, err := e.Discovery.ListExposureDue(ctx, time.Now().Add(-exposureStaleTTL), exposureBatchSize)
	if err != nil {
		log.Printf("exposure: list due failed: %v", err)
		return
	}
	for _, row := range due {
		if err := ctx.Err(); err != nil {
			return
		}
		state, reason := exposure.Classify(row.Address)
		if err := e.Discovery.SetExposure(ctx, row.ID, state, reason); err != nil {
			log.Printf("exposure: set %s: %v", row.ID, err)
		}
	}
}
