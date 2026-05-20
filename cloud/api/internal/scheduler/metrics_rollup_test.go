package scheduler

import (
	"testing"
	"time"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

func TestNewMetricsRollup_Defaults(t *testing.T) {
	r := NewMetricsRollup(nil, nil, 0)
	if r.Interval != time.Hour {
		t.Errorf("Interval = %v, want 1h", r.Interval)
	}
	if r.backfilled {
		t.Error("backfilled should start false")
	}
}

func TestNewMetricsRollup_AcceptsInterval(t *testing.T) {
	r := NewMetricsRollup(nil, nil, 13*time.Minute)
	if r.Interval != 13*time.Minute {
		t.Errorf("Interval = %v, want 13m", r.Interval)
	}
}

// TestPostureScore_FullCoverage verifies the composite formula yields 100
// when every component is at its max.
func TestPostureScore_FullCoverage(t *testing.T) {
	got := store.PostureScore(
		10, 10, // 40 caps
		5, 5, // 30 callers
		3, 3, // 20 not-public
		2, 2, // 10 online
	)
	if got != 100 {
		t.Errorf("PostureScore = %d, want 100", got)
	}
}

// TestPostureScore_ZeroDenom shows the no-data fallback: each empty bucket
// awards its full weight (treats absence as healthy default).
func TestPostureScore_ZeroDenom(t *testing.T) {
	got := store.PostureScore(0, 0, 0, 0, 0, 0, 0, 0)
	if got != 100 {
		t.Errorf("zero denominators -> %d, want 100", got)
	}
}

// TestPostureScore_HalfFlagged hits the math precisely: 40 capabilities
// covered, 50% callers clean, all servers not-public, no gateways tracked.
func TestPostureScore_HalfFlagged(t *testing.T) {
	got := store.PostureScore(
		2, 2, // 40
		1, 2, // 30 * 0.5 = 15
		1, 1, // 20
		0, 0, // 10
	)
	want := 85
	if got != want {
		t.Errorf("PostureScore = %d, want %d", got, want)
	}
}

// TestPostureFromSnapshot_NoPolicies shows that the "no policies" case
// implicitly zeros the capability-coverage component.
func TestPostureFromSnapshot_NoPolicies(t *testing.T) {
	s := store.CurrentSnapshot{
		TotalServers:        10,
		TotalCapabilities:   100,
		PublicExposureCount: 0,
		GatewaysOnline:      2,
		GatewaysTotal:       2,
		CallersTotal:        5,
		CallersFlaggedNew:   0,
		PoliciesEnabled:     0,
	}
	got := store.PostureFromSnapshot(s)
	// 0 caps covered + 30 callers + 20 not-public + 10 gateways = 60
	if got != 60 {
		t.Errorf("posture = %d, want 60", got)
	}
}

// TestDayInUTC normalizes any timestamp to its UTC date floor.
func TestDayInUTC(t *testing.T) {
	in := time.Date(2026, 5, 20, 14, 32, 7, 0, time.UTC)
	got := store.DayInUTC(in)
	want := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("DayInUTC(%v) = %v, want %v", in, got, want)
	}
}
