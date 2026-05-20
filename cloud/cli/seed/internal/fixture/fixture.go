package fixture

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// File is the top-level fixture document.
type File struct {
	Org      Org       `yaml:"org"`
	Servers  []Server  `yaml:"servers"`
	Gateways []Gateway `yaml:"gateways"`
}

type Org struct {
	Name string `yaml:"name"`
}

type Capability struct {
	Kind        string `yaml:"kind"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type Caller struct {
	Agent     string `yaml:"agent"`
	UserEmail string `yaml:"userEmail"`
	Weight    int    `yaml:"weight"`
}

// Server is a logical MCP-server definition; multiple gateways can reference it.
type Server struct {
	Key          string         `yaml:"key"`
	Name         string         `yaml:"name"`
	Address      string         `yaml:"address"`
	Transport    string         `yaml:"transport"`
	Version      string         `yaml:"version"`
	Metadata     map[string]any `yaml:"metadata"`
	Capabilities []Capability   `yaml:"capabilities"`
	Callers      []Caller       `yaml:"callers"`
}

type Gateway struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Observes    []ObservedServer `yaml:"observes"`
}

// ObservedServer ties a server (by key) to a traffic profile for a gateway.
type ObservedServer struct {
	ServerKey string  `yaml:"server"`
	Traffic   Traffic `yaml:"traffic"`
}

// Traffic describes how to procedurally generate invocations.
type Traffic struct {
	HoursBack         float64  `yaml:"hours_back"`
	TotalInvocations  int      `yaml:"total_invocations"`
	RecentHourWeight  float64  `yaml:"recent_hour_weight"` // 0..1 fraction in last hour
	ErrorRate         float64  `yaml:"error_rate"`         // 0..1
	DeniedRate        float64  `yaml:"denied_rate"`        // 0..1
	LatencyMedianMs   int      `yaml:"latency_median_ms"`
	LatencySigma      float64  `yaml:"latency_sigma"`
	CapabilityWeights []CapWt  `yaml:"capability_weights"`
}

type CapWt struct {
	Name   string `yaml:"name"`
	Weight int    `yaml:"weight"`
}

// Load reads and validates a fixture file.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture: %w", err)
	}
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse fixture: %w", err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *File) Validate() error {
	seen := map[string]bool{}
	for i, s := range f.Servers {
		if strings.TrimSpace(s.Key) == "" {
			return fmt.Errorf("servers[%d]: key required", i)
		}
		if seen[s.Key] {
			return fmt.Errorf("servers[%d]: duplicate key %q", i, s.Key)
		}
		seen[s.Key] = true
		if s.Name == "" {
			return fmt.Errorf("servers[%d]: name required", i)
		}
		if len(s.Capabilities) == 0 {
			return fmt.Errorf("servers[%d] (%s): at least one capability required", i, s.Key)
		}
	}
	if len(f.Gateways) == 0 {
		return fmt.Errorf("at least one gateway required")
	}
	for i, gw := range f.Gateways {
		if strings.TrimSpace(gw.Name) == "" {
			return fmt.Errorf("gateways[%d]: name required", i)
		}
		for j, obs := range gw.Observes {
			if !seen[obs.ServerKey] {
				return fmt.Errorf("gateways[%d].observes[%d]: unknown server key %q", i, j, obs.ServerKey)
			}
		}
	}
	return nil
}

// LookupServer returns the server definition with the given key.
func (f *File) LookupServer(key string) *Server {
	for i := range f.Servers {
		if f.Servers[i].Key == key {
			return &f.Servers[i]
		}
	}
	return nil
}
