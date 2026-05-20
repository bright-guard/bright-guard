package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type capability struct {
	Kind        string `yaml:"kind" json:"kind"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

type server struct {
	Name         string         `yaml:"name" json:"name"`
	Address      string         `yaml:"address" json:"address"`
	Transport    string         `yaml:"transport" json:"transport"`
	Version      string         `yaml:"version" json:"version"`
	Metadata     map[string]any `yaml:"metadata" json:"metadata"`
	Capabilities []capability   `yaml:"capabilities" json:"capabilities"`
}

type shimConfig struct {
	Servers []server `yaml:"servers"`
}

type registerResp struct {
	GatewayID  string `json:"gatewayId"`
	Credential string `json:"credential"`
}

// observationsBody mirrors cloud/api observationsReq. The decisions slice is
// only attached when the shim's bundle is live and a policy matched.
type observationsBody struct {
	Servers     []obsServer     `json:"servers"`
	Invocations []obsInvocation `json:"invocations"`
}

type obsServer struct {
	Name         string         `json:"name"`
	Address      string         `json:"address"`
	Transport    string         `json:"transport"`
	Version      string         `json:"version"`
	Metadata     map[string]any `json:"metadata"`
	Capabilities []capability   `json:"capabilities"`
}

type obsInvocation struct {
	Server         string         `json:"server"`
	CapabilityKind string         `json:"capabilityKind"`
	CapabilityName string         `json:"capabilityName"`
	Caller         map[string]any `json:"caller"`
	Status         string         `json:"status"`
	LatencyMs      int            `json:"latencyMs"`
	At             time.Time      `json:"at"`
	Decisions      []obsDecision  `json:"decisions,omitempty"`
	// Wave N+9 (UC6/UC7) optional per-invocation subject + network context.
	// omitempty so older control planes that don't know the keys still parse
	// the payload, and so the ~20% of invocations the shim leaves blank do
	// not push uninformative empty objects over the wire.
	Workload *obsWorkload `json:"workload,omitempty"`
	Network  *obsNetwork  `json:"network,omitempty"`
}

// obsWorkload mirrors cloud/api observationWorkload — the per-invocation
// subject context the gateway / shim reports for UC6.
type obsWorkload struct {
	Host       string `json:"host,omitempty"`
	Cluster    string `json:"cluster,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	AgentClass string `json:"agentClass,omitempty"`
}

// obsNetwork mirrors cloud/api observationNetwork — per-invocation network
// position for UC7.
type obsNetwork struct {
	Subnet   string `json:"subnet,omitempty"`
	VPC      string `json:"vpc,omitempty"`
	Zone     string `json:"zone,omitempty"`
	CallerIP string `json:"callerIp,omitempty"`
}

type obsDecision struct {
	PolicyID string `json:"policyId"`
	Action   string `json:"action"`
	Matched  bool   `json:"matched"`
}

// heartbeatResp matches cloud/api heartbeatResp. PolicyBundle is nil/omitted
// when the server thinks we're already up to date.
type heartbeatResp struct {
	DisabledCapabilities []disabledCapabilityRef `json:"disabledCapabilities"`
	PolicyBundle         *policyBundleWire       `json:"policyBundle"`
}

type disabledCapabilityRef struct {
	ServerName string `json:"serverName"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type policyBundleWire struct {
	Version  int64               `json:"version"`
	Policies []bundlePolicyWire  `json:"policies"`
	Servers  []bundleServerWire  `json:"servers,omitempty"`
	Callers  []bundleCallerWire  `json:"callers,omitempty"`
}

type bundlePolicyWire struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Action     string `json:"action"`
	Expression string `json:"expression"`
}

// bundleServerWire is the per-server snapshot the shim's CEL eval reads when a
// policy references server.exposure_state. Delivered on heartbeat; cached
// alongside the compiled policy programs and refreshed on bundle-version bump.
type bundleServerWire struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	ExposureState string `json:"exposureState"`
}

// bundleCallerWire is the per-caller snapshot. Signature is the canonical SHA256
// hash the control plane uses to deduplicate callers; the shim recomputes the
// signature from the invocation's caller payload to look up flagged_new /
// acknowledged.
type bundleCallerWire struct {
	Signature    string `json:"signature"`
	Label        string `json:"label"`
	FlaggedNew   bool   `json:"flaggedNew"`
	Acknowledged bool   `json:"acknowledged"`
}

func main() {
	controlPlane := strings.TrimRight(mustEnv("BG_CONTROL_PLANE"), "/")
	cfgPath := envOr("BG_CONFIG", "/etc/brightguard/shim.yaml")
	stateDir := envOr("BG_STATE_DIR", "/data")

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if len(cfg.Servers) == 0 {
		log.Fatal("config has no servers")
	}

	credential, err := ensureCredential(controlPlane, stateDir)
	if err != nil {
		log.Fatalf("enroll: %v", err)
	}

	policies := newPolicyCache()
	log.Printf("shim ready: control_plane=%s servers=%d", controlPlane, len(cfg.Servers))

	// Optional HTTP listener so the shim survives as a Cloud Run service.
	// Cloud Run requires the container to listen on $PORT; without it the
	// service is killed during startup probes. /healthz always returns 200.
	if port := os.Getenv("PORT"); port != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		go func() {
			srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			log.Printf("healthz listening on :%s", port)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("healthz: %v", err)
			}
		}()
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	// First tick immediately so verification doesn't have to wait 30s.
	if err := emit(controlPlane, credential, cfg, policies, rng); err != nil {
		log.Printf("tick error: %v", err)
	}
	for range tick.C {
		if err := emit(controlPlane, credential, cfg, policies, rng); err != nil {
			log.Printf("tick error: %v", err)
		}
	}
}

func emit(controlPlane, credential string, cfg *shimConfig, policies *policyCache, rng *rand.Rand) error {
	disabled, bundle, err := heartbeat(controlPlane, credential, policies.version())
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	if bundle != nil {
		// applyBundle is fail-closed: if compile fails for the new bundle we
		// keep the previous one active. Never drop enforcement because of a
		// bad new bundle.
		policies.apply(bundle)
	}

	disabledSet := map[string]bool{}
	for _, d := range disabled {
		disabledSet[d.ServerName+"|"+d.Kind+"|"+d.Name] = true
	}

	body := observationsBody{}
	for _, s := range cfg.Servers {
		body.Servers = append(body.Servers, obsServer{
			Name:         s.Name,
			Address:      s.Address,
			Transport:    s.Transport,
			Version:      s.Version,
			Metadata:     s.Metadata,
			Capabilities: s.Capabilities,
		})
	}

	progs := policies.programs()

	n := 1 + rng.Intn(3)
	for i := 0; i < n; i++ {
		s := cfg.Servers[rng.Intn(len(cfg.Servers))]
		if len(s.Capabilities) == 0 {
			continue
		}
		c := s.Capabilities[rng.Intn(len(s.Capabilities))]
		status := "ok"
		if rng.Intn(10) == 0 {
			status = "error"
		}
		caller := map[string]any{"agent": "demo-agent"}

		// First denial source: control-plane-set disabled capabilities.
		if disabledSet[s.Name+"|"+c.Kind+"|"+c.Name] {
			status = "denied"
		}

		// Synthesise workload + network context for ~80% of invocations so
		// the UC6/UC7 templates have realistic data to match against. The
		// remaining ~20% have neither context attached, exercising the
		// "missing values fall back to empty strings" path on both the
		// control-plane eval and the shim's local eval.
		wl, nw := synthesiseContext(rng)

		// Build the eval context: enrich `server` from the bundle's per-server
		// snapshot (exposure_state, id) and `caller` from the bundle's per-
		// caller snapshot (signature, flagged_new, acknowledged). The caller's
		// canonical signature is the lookup key so the shim mirrors how the
		// control plane deduplicates identities.
		sig := callerSignature(caller)
		evalCaller := enrichCallerForEval(caller, sig, policies.callerBySignature(sig))
		evalServer := buildEvalServer(s, policies.serverByName(s.Name))

		// Second denial source: CEL policy bundle. Evaluate every policy;
		// record matches; deny-action policies flip status to "denied" if not
		// already denied.
		decisions := evaluatePolicies(progs, evalContext{
			caller:     evalCaller,
			server:     evalServer,
			capability: map[string]string{"kind": c.Kind, "name": c.Name, "description": c.Description},
			workload:   workloadEvalMap(wl),
			network:    networkEvalMap(nw),
			at:         time.Now().UTC(),
			status:     status,
		})
		for _, d := range decisions {
			if d.Action == "deny" && status != "denied" {
				status = "denied"
			}
		}

		body.Invocations = append(body.Invocations, obsInvocation{
			Server:         s.Name,
			CapabilityKind: c.Kind,
			CapabilityName: c.Name,
			Caller:         caller,
			Status:         status,
			LatencyMs:      10 + rng.Intn(450),
			At:             time.Now().UTC(),
			Decisions:      decisions,
			Workload:       wl,
			Network:        nw,
		})
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, controlPlane+"/v1/gateway/observations", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("observations: %s %s", resp.Status, string(b))
	}
	log.Printf("tick: heartbeat ok, bundle_version=%d, %d servers, %d invocations",
		policies.version(), len(body.Servers), len(body.Invocations))
	return nil
}

func heartbeat(controlPlane, credential string, bundleVersion int64) ([]disabledCapabilityRef, *policyBundleWire, error) {
	req, err := http.NewRequest(http.MethodPost, controlPlane+"/v1/gateway/heartbeat", nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("X-Bundle-Version", strconv.FormatInt(bundleVersion, 10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("%s: %s", resp.Status, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if len(body) == 0 {
		return nil, nil, nil
	}
	var out heartbeatResp
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, nil, err
	}
	return out.DisabledCapabilities, out.PolicyBundle, nil
}

func ensureCredential(controlPlane, stateDir string) (string, error) {
	if c := os.Getenv("BG_CREDENTIAL"); c != "" {
		return c, nil
	}
	credPath := filepath.Join(stateDir, "credential")
	if b, err := os.ReadFile(credPath); err == nil {
		c := strings.TrimSpace(string(b))
		if c != "" {
			return c, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	token := os.Getenv("BG_ENROLLMENT_TOKEN")
	if token == "" {
		return "", errors.New("no BG_CREDENTIAL and no BG_ENROLLMENT_TOKEN")
	}
	body, err := json.Marshal(map[string]string{"enrollmentToken": token})
	if err != nil {
		return "", err
	}
	resp, err := http.Post(controlPlane+"/v1/gateway/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("register: %s %s", resp.Status, string(respBytes))
	}
	var out registerResp
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return "", err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(credPath, []byte(out.Credential), 0o600); err != nil {
		return "", err
	}
	log.Printf("enrolled: gateway=%s credential persisted at %s", out.GatewayID, credPath)
	return out.Credential, nil
}

func loadConfig(path string) (*shimConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c shimConfig
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("%s is required", k)
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// callerSignature mirrors cloud/api/internal/store/callers.SignatureFor:
// hex(sha256(canonical-json(caller))). Stable key order is the load-bearing
// detail — two callers with the same content but different key order MUST
// hash identically, otherwise the bundle's caller snapshot is unreachable.
// Empty / nil callers collapse to the literal "_anonymous_".
func callerSignature(caller map[string]any) string {
	enc := canonicalEncode(caller)
	if enc == "{}" || enc == "" {
		enc = "_anonymous_"
	}
	sum := sha256.Sum256([]byte(enc))
	return hex.EncodeToString(sum[:])
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

// enrichCallerForEval folds the bundle-side flagged_new / acknowledged /
// signature / label fields into the caller map so a CEL expression like
// `caller.flagged_new && !caller.acknowledged` resolves without the policy
// author needing to know how the control plane categorizes the identity.
func enrichCallerForEval(caller map[string]any, signature string, bundle bundleCallerWire) map[string]any {
	out := make(map[string]any, len(caller)+4)
	for k, v := range caller {
		out[k] = v
	}
	out["signature"] = signature
	out["flagged_new"] = bundle.FlaggedNew
	out["acknowledged"] = bundle.Acknowledged
	if bundle.Label != "" {
		out["label"] = bundle.Label
	}
	return out
}

// buildEvalServer lifts the local server config into a CEL-friendly map,
// overlaying the bundle's exposure_state + id when known. Falls back to
// "unknown" exposure_state when the bundle hasn't seen this server (e.g. the
// first heartbeat after a fresh enrollment).
func buildEvalServer(s server, bundle bundleServerWire) map[string]any {
	exposure := bundle.ExposureState
	if exposure == "" {
		exposure = "unknown"
	}
	return map[string]any{
		"id":             bundle.ID,
		"name":           s.Name,
		"address":        s.Address,
		"transport":      s.Transport,
		"exposure_state": exposure,
		// camelCase alias preserves any policy authored against the original
		// env shape before Wave N+8 renamed the field.
		"exposureState": exposure,
	}
}

// policyCache holds the compiled bundle and its version. Goroutine-safe so
// future eval paths could read while heartbeat updates concurrently.
//
// Wave N+8 added the servers and callers snapshots so the shim's local eval
// can resolve server.exposure_state and caller.flagged_new without a
// per-invocation round trip to the control plane.
type policyCache struct {
	mu      sync.RWMutex
	ver     int64
	progs   []compiledPolicy
	servers map[string]bundleServerWire // keyed by server name
	callers map[string]bundleCallerWire // keyed by signature
}

func newPolicyCache() *policyCache {
	return &policyCache{
		servers: map[string]bundleServerWire{},
		callers: map[string]bundleCallerWire{},
	}
}

func (c *policyCache) version() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ver
}

func (c *policyCache) programs() []compiledPolicy {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.progs) == 0 {
		return nil
	}
	cp := make([]compiledPolicy, len(c.progs))
	copy(cp, c.progs)
	return cp
}

// serverByName returns the cached server snapshot for the given local server
// name, or an empty value if the bundle hasn't seen this server. Empty values
// mean any policy reading server.exposure_state sees "" (which doesn't match
// the "public" template); preferred over returning ok=false because the CEL
// eval must remain side-effect-free and never fail open on missing data.
func (c *policyCache) serverByName(name string) bundleServerWire {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.servers[name]
}

// callerBySignature returns the cached caller snapshot keyed by signature.
// Empty when the caller hasn't been observed by the control plane yet —
// flagged_new=false in that case is correct because a brand-new caller hasn't
// been classified yet (the caller sweeper will catch it on its next run).
func (c *policyCache) callerBySignature(sig string) bundleCallerWire {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.callers[sig]
}

// apply swaps in a new bundle. Fail-closed: if any policy fails to compile
// we keep the previous bundle live. We compile every policy up-front to spot
// errors; on partial failure we log and abort the swap.
func (c *policyCache) apply(b *policyBundleWire) {
	if b == nil {
		return
	}
	progs := make([]compiledPolicy, 0, len(b.Policies))
	for _, p := range b.Policies {
		cp, err := compilePolicy(p)
		if err != nil {
			log.Printf("policy bundle v%d: compile %s (%s) failed: %v — keeping previous bundle",
				b.Version, p.ID, p.Name, err)
			return
		}
		progs = append(progs, cp)
	}
	servers := make(map[string]bundleServerWire, len(b.Servers))
	for _, s := range b.Servers {
		servers[s.Name] = s
	}
	callers := make(map[string]bundleCallerWire, len(b.Callers))
	for _, ca := range b.Callers {
		callers[ca.Signature] = ca
	}
	c.mu.Lock()
	c.ver = b.Version
	c.progs = progs
	c.servers = servers
	c.callers = callers
	c.mu.Unlock()
	log.Printf("policy bundle v%d applied: %d programs, %d servers, %d callers",
		b.Version, len(progs), len(servers), len(callers))
}

// fakeClusters / fakeNamespaces / fakeAgentClasses / fakeSubnets / fakeVPCs /
// fakeZones are the round-robin pools the shim draws synthetic UC6/UC7
// context from. Chosen so the two shipped templates (block-prod-to-public,
// block-outside-corp-net) have realistic match + non-match populations.
var (
	fakeClusters     = []string{"prod-us-east", "prod-us-west", "staging", "dev"}
	fakeNamespaces   = []string{"default", "data", "ml-platform"}
	fakeAgentClasses = []string{"assistant", "copilot", "agent-runtime"}
	fakeSubnets      = []string{"10.0.1.0/24", "10.0.2.0/24", "172.16.0.0/16", "192.168.1.0/24"}
	fakeVPCs         = []string{"vpc-prod", "vpc-staging", "vpc-dev"}
	fakeZones        = []string{"us-east-1a", "us-east-1b", "us-west-2a"}
)

// synthesiseContext returns a workload + network pair, or (nil, nil) for
// roughly 20% of calls. The bucket choice is independent so we also generate
// "workload-only" / "network-only" invocations occasionally — useful to
// confirm the templates handle partial context correctly.
//
// "Cluster derivation" is intentional: the cluster name is the load-bearing
// field for the prod-to-public template, so we sample it once and key the
// rest of the workload off it for plausible mock data.
func synthesiseContext(rng *rand.Rand) (*obsWorkload, *obsNetwork) {
	// 20% of invocations have no context at all (mimics older / observability-
	// only agents that don't report subject + network metadata yet).
	if rng.Intn(5) == 0 {
		return nil, nil
	}
	cluster := fakeClusters[rng.Intn(len(fakeClusters))]
	ns := fakeNamespaces[rng.Intn(len(fakeNamespaces))]
	ac := fakeAgentClasses[rng.Intn(len(fakeAgentClasses))]
	host := cluster + "-pod-" + strconv.Itoa(rng.Intn(20))

	subnet := fakeSubnets[rng.Intn(len(fakeSubnets))]
	vpc := fakeVPCs[rng.Intn(len(fakeVPCs))]
	zone := fakeZones[rng.Intn(len(fakeZones))]
	ip := callerIPInSubnet(subnet, rng)

	wl := &obsWorkload{Host: host, Cluster: cluster, Namespace: ns, AgentClass: ac}
	nw := &obsNetwork{Subnet: subnet, VPC: vpc, Zone: zone, CallerIP: ip}
	return wl, nw
}

// callerIPInSubnet returns an IPv4 string from inside the given CIDR. Only the
// /24 and /16 shapes used in fakeSubnets are recognised — anything else just
// returns "10.0.0.1". Demo-grade: we're not trying to honor real CIDR maths,
// just to put a plausible address into the payload.
func callerIPInSubnet(cidr string, rng *rand.Rand) string {
	switch cidr {
	case "10.0.1.0/24":
		return "10.0.1." + strconv.Itoa(1+rng.Intn(250))
	case "10.0.2.0/24":
		return "10.0.2." + strconv.Itoa(1+rng.Intn(250))
	case "172.16.0.0/16":
		return "172.16." + strconv.Itoa(rng.Intn(254)) + "." + strconv.Itoa(1+rng.Intn(250))
	case "192.168.1.0/24":
		return "192.168.1." + strconv.Itoa(1+rng.Intn(250))
	default:
		return "10.0.0.1"
	}
}

// workloadEvalMap / networkEvalMap lift the wire-shape obsWorkload / obsNetwork
// into the map[string]any shape the eval context expects. Empty / nil inputs
// produce a fully-empty map (the env normaliser fills the four expected keys
// with "").
func workloadEvalMap(w *obsWorkload) map[string]any {
	if w == nil {
		return nil
	}
	return map[string]any{
		"host":        w.Host,
		"cluster":     w.Cluster,
		"namespace":   w.Namespace,
		"agent_class": w.AgentClass,
	}
}

func networkEvalMap(n *obsNetwork) map[string]any {
	if n == nil {
		return nil
	}
	return map[string]any{
		"subnet":    n.Subnet,
		"vpc":       n.VPC,
		"zone":      n.Zone,
		"caller_ip": n.CallerIP,
	}
}
