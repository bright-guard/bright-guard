package seeder

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/bright-guard/bright-guard/cloud/cli/seed/internal/client"
	"github.com/bright-guard/bright-guard/cloud/cli/seed/internal/fixture"
)

// Summary captures what was seeded so the CLI can render a table.
type Summary struct {
	OrgName        string
	OrgID          string
	GatewayResults []GatewayResult
}

type GatewayResult struct {
	Name        string
	ID          string
	Servers     int
	Capabilities int
	Invocations int
}

// Options bundles inputs to Run.
type Options struct {
	Fixture *fixture.File
	OrgName string
	RNGSeed int64
	Logger  *log.Logger
	// BatchSize caps invocations per observations POST.
	BatchSize int
}

const defaultBatchSize = 200

// Run executes the full seeding flow.
func Run(c *client.Client, opts Options) (*Summary, error) {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = defaultBatchSize
	}
	orgName := strings.TrimSpace(opts.OrgName)
	if orgName == "" {
		orgName = strings.TrimSpace(opts.Fixture.Org.Name)
	}
	if orgName == "" {
		return nil, fmt.Errorf("org name required (either --org-name or fixture.org.name)")
	}

	org, err := ensureOrg(c, orgName, opts.Logger)
	if err != nil {
		return nil, err
	}
	opts.Logger.Printf("org ready: %s (%s)", org.Name, org.ID)

	if err := c.SetActiveOrg(org.ID); err != nil {
		return nil, fmt.Errorf("set active org: %w", err)
	}

	// Avoid double-creating gateways if the seeder is re-run.
	existing, err := c.ListGateways(org.ID)
	if err != nil {
		return nil, fmt.Errorf("list gateways: %w", err)
	}
	existingByName := map[string]bool{}
	for _, gw := range existing {
		existingByName[gw.Name] = true
	}

	rng := rand.New(rand.NewSource(opts.RNGSeed))
	summary := &Summary{OrgName: org.Name, OrgID: org.ID}

	for _, gw := range opts.Fixture.Gateways {
		if existingByName[gw.Name] {
			opts.Logger.Printf("gateway %q already exists; skipping create (re-run-safe path not implemented for observations on existing gateways)", gw.Name)
			summary.GatewayResults = append(summary.GatewayResults, GatewayResult{Name: gw.Name})
			continue
		}
		res, err := seedGateway(c, org.ID, gw, opts.Fixture, rng, opts.BatchSize, opts.Logger)
		if err != nil {
			return nil, fmt.Errorf("gateway %q: %w", gw.Name, err)
		}
		summary.GatewayResults = append(summary.GatewayResults, *res)
	}
	return summary, nil
}

func ensureOrg(c *client.Client, name string, logger *log.Logger) (*client.Org, error) {
	memberships, err := c.ListOrgs()
	if err != nil {
		return nil, fmt.Errorf("list orgs: %w", err)
	}
	for _, m := range memberships {
		if strings.EqualFold(m.Org.Name, name) {
			return &m.Org, nil
		}
	}
	logger.Printf("creating org %q", name)
	return c.CreateOrg(name)
}

func seedGateway(c *client.Client, orgID string, gw fixture.Gateway, fx *fixture.File, rng *rand.Rand, batchSize int, logger *log.Logger) (*GatewayResult, error) {
	logger.Printf("creating gateway %q", gw.Name)
	created, err := c.CreateGateway(orgID, gw.Name, gw.Description)
	if err != nil {
		return nil, fmt.Errorf("create gateway: %w", err)
	}
	reg, err := c.RegisterGateway(created.EnrollmentToken)
	if err != nil {
		return nil, fmt.Errorf("register gateway: %w", err)
	}
	logger.Printf("gateway registered: id=%s", reg.GatewayID)

	// Build the server/capability declarations once for this gateway.
	var obsServers []client.ObsServer
	capCount := 0
	for _, obs := range gw.Observes {
		s := fx.LookupServer(obs.ServerKey)
		if s == nil {
			continue
		}
		var caps []client.Capability
		for _, cap := range s.Capabilities {
			caps = append(caps, client.Capability{
				Kind:        cap.Kind,
				Name:        cap.Name,
				Description: cap.Description,
			})
		}
		capCount += len(caps)
		obsServers = append(obsServers, client.ObsServer{
			Name:         s.Name,
			Address:      s.Address,
			Transport:    s.Transport,
			Version:      s.Version,
			Metadata:     s.Metadata,
			Capabilities: caps,
		})
	}

	// First call: declare every server with its capabilities, no invocations yet.
	declOnly := &client.ObservationsBody{Servers: obsServers}
	if err := c.PostObservations(reg.Credential, declOnly); err != nil {
		return nil, fmt.Errorf("declare servers: %w", err)
	}

	// Generate invocations for each observed server.
	invocations := generateInvocations(gw, fx, rng)
	logger.Printf("gateway %q: %d servers, %d capabilities, %d invocations queued",
		gw.Name, len(obsServers), capCount, len(invocations))

	// Push in batches; redeclare servers each batch so the request stays valid.
	for start := 0; start < len(invocations); start += batchSize {
		end := start + batchSize
		if end > len(invocations) {
			end = len(invocations)
		}
		body := &client.ObservationsBody{
			Servers:     obsServers,
			Invocations: invocations[start:end],
		}
		if err := c.PostObservations(reg.Credential, body); err != nil {
			return nil, fmt.Errorf("post observations batch %d-%d: %w", start, end, err)
		}
	}
	if err := c.Heartbeat(reg.Credential); err != nil {
		logger.Printf("heartbeat (non-fatal): %v", err)
	}

	return &GatewayResult{
		Name:         gw.Name,
		ID:           reg.GatewayID,
		Servers:      len(obsServers),
		Capabilities: capCount,
		Invocations:  len(invocations),
	}, nil
}

func generateInvocations(gw fixture.Gateway, fx *fixture.File, rng *rand.Rand) []client.ObsInvocation {
	now := time.Now().UTC()
	var out []client.ObsInvocation
	for _, obs := range gw.Observes {
		s := fx.LookupServer(obs.ServerKey)
		if s == nil {
			continue
		}
		t := obs.Traffic
		if t.TotalInvocations <= 0 {
			continue
		}
		hoursBack := t.HoursBack
		if hoursBack <= 0 {
			hoursBack = 24
		}
		recentWeight := t.RecentHourWeight
		if recentWeight <= 0 {
			recentWeight = 0.5
		}
		recentCount := int(float64(t.TotalInvocations) * recentWeight)

		errRate := t.ErrorRate
		if errRate < 0 {
			errRate = 0
		}
		denRate := t.DeniedRate
		if denRate < 0 {
			denRate = 0
		}
		medLat := t.LatencyMedianMs
		if medLat <= 0 {
			medLat = 80
		}
		sigma := t.LatencySigma
		if sigma <= 0 {
			sigma = 0.6
		}

		for i := 0; i < t.TotalInvocations; i++ {
			var at time.Time
			if i < recentCount {
				at = now.Add(-time.Duration(rng.Float64() * float64(time.Hour)))
			} else {
				// Spread the remainder across [now-hoursBack, now-1h].
				offsetHours := 1 + rng.Float64()*(hoursBack-1)
				at = now.Add(-time.Duration(offsetHours * float64(time.Hour)))
			}
			cap := pickCapability(s, t.CapabilityWeights, rng)
			caller := pickCaller(s.Callers, rng)
			status := "ok"
			roll := rng.Float64()
			if roll < denRate {
				status = "denied"
			} else if roll < denRate+errRate {
				status = "error"
			}
			out = append(out, client.ObsInvocation{
				Server:         s.Name,
				CapabilityKind: cap.Kind,
				CapabilityName: cap.Name,
				Caller:         caller,
				Status:         status,
				LatencyMs:      sampleLatency(rng, medLat, sigma),
				At:             at,
			})
		}
	}
	// Sort oldest -> newest so dashboards render a natural timeline.
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func pickCapability(s *fixture.Server, weights []fixture.CapWt, rng *rand.Rand) fixture.Capability {
	if len(weights) == 0 {
		return s.Capabilities[rng.Intn(len(s.Capabilities))]
	}
	total := 0
	for _, w := range weights {
		total += w.Weight
	}
	if total <= 0 {
		return s.Capabilities[rng.Intn(len(s.Capabilities))]
	}
	r := rng.Intn(total)
	for _, w := range weights {
		r -= w.Weight
		if r < 0 {
			for _, c := range s.Capabilities {
				if c.Name == w.Name {
					return c
				}
			}
		}
	}
	return s.Capabilities[rng.Intn(len(s.Capabilities))]
}

func pickCaller(callers []fixture.Caller, rng *rand.Rand) map[string]any {
	if len(callers) == 0 {
		return map[string]any{"agent": "demo-agent"}
	}
	total := 0
	for _, c := range callers {
		w := c.Weight
		if w <= 0 {
			w = 1
		}
		total += w
	}
	r := rng.Intn(total)
	for _, c := range callers {
		w := c.Weight
		if w <= 0 {
			w = 1
		}
		r -= w
		if r < 0 {
			out := map[string]any{}
			if c.Agent != "" {
				out["agent"] = c.Agent
			}
			if c.UserEmail != "" {
				out["userEmail"] = c.UserEmail
			}
			if len(out) == 0 {
				out["agent"] = "demo-agent"
			}
			return out
		}
	}
	return map[string]any{"agent": "demo-agent"}
}

// sampleLatency draws from a log-normal-ish distribution clamped to sane bounds.
func sampleLatency(rng *rand.Rand, median int, sigma float64) int {
	if median < 1 {
		median = 50
	}
	mu := math.Log(float64(median))
	v := math.Exp(mu + sigma*rng.NormFloat64())
	if v < 1 {
		v = 1
	}
	if v > 30000 {
		v = 30000
	}
	return int(v)
}
