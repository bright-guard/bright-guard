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
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type capability struct {
	Kind        string `yaml:"kind" json:"kind"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

type server struct {
	Name         string                 `yaml:"name" json:"name"`
	Address      string                 `yaml:"address" json:"address"`
	Transport    string                 `yaml:"transport" json:"transport"`
	Version      string                 `yaml:"version" json:"version"`
	Metadata     map[string]any         `yaml:"metadata" json:"metadata"`
	Capabilities []capability           `yaml:"capabilities" json:"capabilities"`
}

type shimConfig struct {
	Servers []server `yaml:"servers"`
}

type registerResp struct {
	GatewayID  string `json:"gatewayId"`
	Credential string `json:"credential"`
}

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
	log.Printf("shim ready: control_plane=%s servers=%d", controlPlane, len(cfg.Servers))

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	// First tick immediately so verification doesn't have to wait 30s.
	if err := emit(controlPlane, credential, cfg, rng); err != nil {
		log.Printf("tick error: %v", err)
	}
	for range tick.C {
		if err := emit(controlPlane, credential, cfg, rng); err != nil {
			log.Printf("tick error: %v", err)
		}
	}
}

func emit(controlPlane, credential string, cfg *shimConfig, rng *rand.Rand) error {
	if err := postNoBody(controlPlane+"/v1/gateway/heartbeat", credential); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
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
		body.Invocations = append(body.Invocations, obsInvocation{
			Server:         s.Name,
			CapabilityKind: c.Kind,
			CapabilityName: c.Name,
			Caller:         map[string]any{"agent": "demo-agent"},
			Status:         status,
			LatencyMs:      10 + rng.Intn(450),
			At:             time.Now().UTC(),
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
	log.Printf("tick: heartbeat ok, %d servers, %d invocations", len(body.Servers), len(body.Invocations))
	return nil
}

func postNoBody(url, credential string) error {
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, string(b))
	}
	return nil
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
