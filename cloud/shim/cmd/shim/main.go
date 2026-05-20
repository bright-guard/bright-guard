package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
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
	Version  int64              `json:"version"`
	Policies []bundlePolicyWire `json:"policies"`
}

type bundlePolicyWire struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Action     string `json:"action"`
	Expression string `json:"expression"`
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

		// Second denial source: CEL policy bundle. Evaluate every policy;
		// record matches; deny-action policies flip status to "denied" if not
		// already denied.
		decisions := evaluatePolicies(progs, evalContext{
			caller:     caller,
			server:     map[string]string{"name": s.Name, "transport": s.Transport, "address": s.Address},
			capability: map[string]string{"kind": c.Kind, "name": c.Name, "description": c.Description},
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

// policyCache holds the compiled bundle and its version. Goroutine-safe so
// future eval paths could read while heartbeat updates concurrently.
type policyCache struct {
	mu    sync.RWMutex
	ver   int64
	progs []compiledPolicy
}

func newPolicyCache() *policyCache { return &policyCache{} }

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
	c.mu.Lock()
	c.ver = b.Version
	c.progs = progs
	c.mu.Unlock()
	log.Printf("policy bundle v%d applied: %d programs", b.Version, len(progs))
}
