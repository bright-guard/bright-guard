package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	defaultDashboardRange = 30
	dashboardGatewayWindow = 5 * time.Minute
)

// parseRangeDays parses a query like "7d", "30d", "90d" into a day count.
// Anything unparseable falls back to defaultDashboardRange. Capped at 365.
func parseRangeDays(q string) int {
	s := strings.TrimSpace(strings.ToLower(q))
	if s == "" {
		return defaultDashboardRange
	}
	s = strings.TrimSuffix(s, "d")
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return defaultDashboardRange
		}
		n = n*10 + int(ch-'0')
		if n > 365 {
			return 365
		}
	}
	if n <= 0 {
		return defaultDashboardRange
	}
	return n
}

// kpiTile is the response shape for one of the six dashboard tiles. Both
// Current and Prior are pre-computed by the backend so the SPA only needs to
// render — no math beyond formatting the delta percentage.
type kpiTile struct {
	Key           string  `json:"key"`
	Current       float64 `json:"current"`
	Prior         float64 `json:"prior"`
	DeltaPercent  float64 `json:"deltaPercent"`
	HigherIsBetter bool   `json:"higherIsBetter"`
	Sparkline     []float64 `json:"sparkline"`
	Extra         map[string]any `json:"extra,omitempty"`
}

type kpisResp struct {
	RangeDays int       `json:"rangeDays"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	Tiles     []kpiTile `json:"tiles"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Server) handleDashboardKPIs(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	days := parseRangeDays(r.URL.Query().Get("range"))
	now := time.Now().UTC()
	to := now
	from := to.Add(-time.Duration(days) * 24 * time.Hour)
	priorFrom := from.Add(-time.Duration(days) * 24 * time.Hour)
	priorTo := from

	curInv, err := s.Dashboard.CountInvocations(r.Context(), orgID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "kpi: count invocations")
		return
	}
	priorInv, err := s.Dashboard.CountInvocations(r.Context(), orgID, priorFrom, priorTo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "kpi: count invocations (prior)")
		return
	}
	curCallers, curNewCallers, err := s.Dashboard.CountDistinctCallers(r.Context(), orgID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "kpi: callers")
		return
	}
	priorCallers, _, err := s.Dashboard.CountDistinctCallers(r.Context(), orgID, priorFrom, priorTo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "kpi: callers prior")
		return
	}
	curNewServers, err := s.Dashboard.CountNewServers(r.Context(), orgID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "kpi: new servers")
		return
	}
	priorNewServers, err := s.Dashboard.CountNewServers(r.Context(), orgID, priorFrom, priorTo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "kpi: new servers prior")
		return
	}
	daily, err := s.Dashboard.ListDaily(r.Context(), orgID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "kpi: daily")
		return
	}
	snap, err := s.Dashboard.Snapshot(r.Context(), orgID, now.Add(-dashboardGatewayWindow))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "kpi: snapshot")
		return
	}

	// Sparkline series (length = days, one value per UTC day). Missing days
	// fill with zero so the line is continuous.
	sparkInv := dailySeries(daily, days, from, func(d store.DailyMetric) float64 {
		return float64(d.InvocationsAllowed + d.InvocationsAudited + d.InvocationsDenied)
	})
	sparkDenials := dailySeries(daily, days, from, func(d store.DailyMetric) float64 {
		return float64(d.InvocationsDenied)
	})
	sparkPublic := dailySeries(daily, days, from, func(d store.DailyMetric) float64 {
		return float64(d.PublicExposureCount)
	})
	sparkServers := dailySeries(daily, days, from, func(d store.DailyMetric) float64 {
		return float64(d.TotalServers)
	})
	sparkCallers := dailySeries(daily, days, from, func(d store.DailyMetric) float64 {
		return float64(d.NewCallers)
	})
	sparkPosture := dailySeries(daily, days, from, func(d store.DailyMetric) float64 {
		return float64(d.PostureScore)
	})

	posture := store.PostureFromSnapshot(snap)
	// "prior posture" pulled from the row at the start of the window.
	priorPosture := 0
	if len(daily) > 0 {
		priorPosture = daily[0].PostureScore
	}

	curInvTotal := float64(curInv.Total())
	priorInvTotal := float64(priorInv.Total())

	tiles := []kpiTile{
		{
			Key:            "posture",
			Current:        float64(posture),
			Prior:          float64(priorPosture),
			DeltaPercent:   delta(float64(posture), float64(priorPosture)),
			HigherIsBetter: true,
			Sparkline:      sparkPosture,
			Extra:          map[string]any{"max": 100},
		},
		{
			Key:            "footprint",
			Current:        float64(snap.TotalServers),
			Prior:          float64(snap.TotalServers - curNewServers),
			DeltaPercent:   delta(float64(snap.TotalServers), float64(snap.TotalServers-curNewServers)),
			HigherIsBetter: true,
			Sparkline:      sparkServers,
			Extra: map[string]any{
				"totalCapabilities": snap.TotalCapabilities,
				"newServers":        curNewServers,
				"priorNewServers":   priorNewServers,
			},
		},
		{
			Key:            "invocations",
			Current:        curInvTotal,
			Prior:          priorInvTotal,
			DeltaPercent:   delta(curInvTotal, priorInvTotal),
			HigherIsBetter: true,
			Sparkline:      sparkInv,
			Extra: map[string]any{
				"allowed": curInv.Allowed,
				"audited": curInv.Audited,
				"denied":  curInv.Denied,
			},
		},
		{
			Key:            "denials",
			Current:        float64(curInv.Denied),
			Prior:          float64(priorInv.Denied),
			DeltaPercent:   delta(float64(curInv.Denied), float64(priorInv.Denied)),
			HigherIsBetter: true, // more denials = enforcement working
			Sparkline:      sparkDenials,
		},
		{
			Key:            "publicExposure",
			Current:        float64(snap.PublicExposureCount),
			Prior:          priorPublicFromSeries(sparkPublic),
			DeltaPercent:   delta(float64(snap.PublicExposureCount), priorPublicFromSeries(sparkPublic)),
			HigherIsBetter: false,
			Sparkline:      sparkPublic,
		},
		{
			Key:            "activeCallers",
			Current:        float64(curCallers),
			Prior:          float64(priorCallers),
			DeltaPercent:   delta(float64(curCallers), float64(priorCallers)),
			HigherIsBetter: true,
			Sparkline:      sparkCallers,
			Extra: map[string]any{
				"newCallers": curNewCallers,
			},
		},
	}
	writeJSON(w, http.StatusOK, kpisResp{
		RangeDays: days, From: from, To: to, Tiles: tiles, UpdatedAt: now,
	})
}

// priorPublicFromSeries treats the first day in the sparkline as the
// "prior" value for the public-exposure tile. The series is in chronological
// order; index 0 is the oldest sample.
func priorPublicFromSeries(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	return s[0]
}

// dailySeries builds a length-`days` float slice anchored on `from` (UTC start
// of day). Days without a row in `rows` contribute zero. Slightly defensive:
// rows may include days outside the window, those are ignored.
func dailySeries(rows []store.DailyMetric, days int, from time.Time, pick func(store.DailyMetric) float64) []float64 {
	out := make([]float64, days)
	start := store.DayInUTC(from)
	index := map[string]float64{}
	for _, r := range rows {
		index[r.Day.UTC().Format("2006-01-02")] = pick(r)
	}
	for i := 0; i < days; i++ {
		key := start.Add(time.Duration(i) * 24 * time.Hour).Format("2006-01-02")
		out[i] = index[key]
	}
	return out
}

func delta(cur, prior float64) float64 {
	if prior == 0 {
		if cur == 0 {
			return 0
		}
		return 100
	}
	return ((cur - prior) / prior) * 100
}

type timeseriesResp struct {
	Metric    string      `json:"metric"`
	RangeDays int         `json:"rangeDays"`
	From      time.Time   `json:"from"`
	To        time.Time   `json:"to"`
	Series    []seriesRow `json:"series"`
}

type seriesRow struct {
	Day     string `json:"day"`
	Allowed int64  `json:"allowed,omitempty"`
	Audited int64  `json:"audited,omitempty"`
	Denied  int64  `json:"denied,omitempty"`
	Value   int64  `json:"value,omitempty"`
}

func (s *Server) handleDashboardTimeseries(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	days := parseRangeDays(r.URL.Query().Get("range"))
	metric := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("metric")))
	if metric == "" {
		metric = "invocations"
	}
	now := time.Now().UTC()
	to := store.DayInUTC(now).Add(24 * time.Hour) // include today
	from := to.Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := s.Dashboard.ListDaily(r.Context(), orgID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "timeseries")
		return
	}
	index := map[string]store.DailyMetric{}
	for _, r := range rows {
		index[r.Day.UTC().Format("2006-01-02")] = r
	}
	out := make([]seriesRow, 0, days)
	for i := 0; i < days; i++ {
		day := from.Add(time.Duration(i) * 24 * time.Hour).Format("2006-01-02")
		r := index[day]
		switch metric {
		case "invocations":
			out = append(out, seriesRow{
				Day:     day,
				Allowed: r.InvocationsAllowed,
				Audited: r.InvocationsAudited,
				Denied:  r.InvocationsDenied,
			})
		case "denials":
			out = append(out, seriesRow{Day: day, Value: r.InvocationsDenied})
		case "new_callers":
			out = append(out, seriesRow{Day: day, Value: r.NewCallers})
		case "new_servers":
			out = append(out, seriesRow{Day: day, Value: r.NewServers})
		case "public_exposure":
			out = append(out, seriesRow{Day: day, Value: r.PublicExposureCount})
		case "posture":
			out = append(out, seriesRow{Day: day, Value: int64(r.PostureScore)})
		default:
			writeError(w, http.StatusBadRequest, "invalid_metric", "unknown metric")
			return
		}
	}
	writeJSON(w, http.StatusOK, timeseriesResp{
		Metric: metric, RangeDays: days, From: from, To: to, Series: out,
	})
}

type highlightsResp struct {
	From            time.Time              `json:"from"`
	To              time.Time              `json:"to"`
	RangeDays       int                    `json:"rangeDays"`
	TopCapabilities []store.DashTopCapability `json:"topCapabilities"`
	TopCallers      []store.DashTopCaller     `json:"topCallers"`
	RecentDenied    []store.RecentDenied   `json:"recentDenied"`
}

func (s *Server) handleDashboardHighlights(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	days := parseRangeDays(r.URL.Query().Get("range"))
	now := time.Now().UTC()
	to := now
	from := to.Add(-time.Duration(days) * 24 * time.Hour)
	caps, callers, denied, err := s.Dashboard.Highlights(r.Context(), orgID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "highlights")
		return
	}
	writeJSON(w, http.StatusOK, highlightsResp{
		From: from, To: to, RangeDays: days,
		TopCapabilities: caps, TopCallers: callers, RecentDenied: denied,
	})
}

type calloutsResp struct {
	PublicExposureServers int64 `json:"publicExposureServers"`
	FlaggedNewCallers     int64 `json:"flaggedNewCallers"`
	CapabilitiesNoPolicy  int64 `json:"capabilitiesNoPolicy"`
}

func (s *Server) handleDashboardCallouts(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	c, err := s.Dashboard.Callouts(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "callouts")
		return
	}
	writeJSON(w, http.StatusOK, calloutsResp{
		PublicExposureServers: c.PublicExposureServers,
		FlaggedNewCallers:     c.FlaggedNewCallers,
		CapabilitiesNoPolicy:  c.CapabilitiesNoPolicy,
	})
}

// orgIDForDashboardURL is exported only so the routes file can route via
// chi without importing internals. Currently unused; the handler uses
// orgFromCtx via the orgMember middleware.
var _ = uuid.Nil
