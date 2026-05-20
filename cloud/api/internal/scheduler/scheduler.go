package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/mcp"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	advisoryLockKey = "bg-discovery"
	defaultStaleTTL = 6 * time.Hour
	batchSize       = 20
)

// Scheduler periodically rediscovers MCP servers behind every saved connection.
type Scheduler struct {
	Connections *store.Connections
	Discovery   *store.Discovery
	Interval    time.Duration

	// NewClient is overridable for tests. When nil, mcp.New is used.
	NewClient func(endpoint, transport string, auth mcp.AuthSecret) ClientLike
	// NewOAuthClient is overridable for tests. When nil, mcp.NewWithTransport
	// is used with an OAuth2RoundTripper backed by Connections.
	NewOAuthClient func(endpoint, transport string, connID uuid.UUID, auth mcp.AuthSecret) ClientLike
}

// ClientLike is the subset of *mcp.Client the scheduler uses; pulled out so
// tests can stub it.
type ClientLike interface {
	Initialize(ctx context.Context) (*mcp.ServerInfo, error)
	ListTools(ctx context.Context) ([]mcp.Tool, error)
	ListResources(ctx context.Context) ([]mcp.Resource, error)
	ListPrompts(ctx context.Context) ([]mcp.Prompt, error)
}

// New builds a Scheduler with the production client factory.
func New(conns *store.Connections, disc *store.Discovery, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = time.Hour
	}
	return &Scheduler{
		Connections: conns,
		Discovery:   disc,
		Interval:    interval,
		NewClient: func(endpoint, transport string, auth mcp.AuthSecret) ClientLike {
			return mcp.New(endpoint, transport, auth)
		},
		NewOAuthClient: func(endpoint, transport string, connID uuid.UUID, auth mcp.AuthSecret) ClientLike {
			rt := &mcp.OAuth2RoundTripper{
				Base:         http.DefaultTransport,
				ConnectionID: connID,
				Store:        conns,
			}
			return mcp.NewWithTransport(endpoint, transport, rt, auth)
		},
	}
}

// Run blocks until ctx is cancelled. Only the leader (advisory lock holder)
// actually performs discovery; other replicas spin idly.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	// Kick once on start so newly-deployed instances don't wait a full tick.
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

func (s *Scheduler) tick(ctx context.Context) {
	ok, err := s.Connections.TryAdvisoryLock(ctx, advisoryLockKey)
	if err != nil {
		log.Printf("scheduler: advisory lock check failed: %v", err)
		return
	}
	if !ok {
		// Another replica holds the lock.
		return
	}
	due, err := s.Connections.ListDue(ctx, time.Now().Add(-defaultStaleTTL), batchSize)
	if err != nil {
		log.Printf("scheduler: list due failed: %v", err)
		return
	}
	for _, conn := range due {
		if err := ctx.Err(); err != nil {
			return
		}
		s.runOne(ctx, conn.ID)
	}
}

// runOne fetches secrets and runs discovery against a single connection.
func (s *Scheduler) runOne(ctx context.Context, connID uuid.UUID) {
	rcCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.Discover(rcCtx, connID); err != nil {
		log.Printf("scheduler: discovery for %s failed: %v", connID, err)
	}
}

// Discover is the one-shot entrypoint used by both the scheduler and the
// connection create handler. It loads + decrypts the auth secret, runs an MCP
// initialize + capability listing, and updates mcp_servers / mcp_capabilities.
func (s *Scheduler) Discover(ctx context.Context, connID uuid.UUID) error {
	conn, secret, err := s.Connections.GetWithSecret(ctx, connID)
	if err != nil {
		return err
	}
	// OAuth connections that haven't completed the authorize dance can't be
	// probed — there's no token to send. Treat as a no-op (the ListDue query
	// also filters these out for the periodic sweep, but the create-handler
	// short-circuits here when it lands on a pending row).
	if conn.AuthMethod == models.AuthMethodOAuth2Authcode && conn.OAuthStatus != models.OAuthStatusAuthorized {
		return nil
	}
	var cli ClientLike
	if conn.AuthMethod == models.AuthMethodOAuth2Authcode && s.NewOAuthClient != nil {
		cli = s.NewOAuthClient(conn.EndpointURL, conn.Transport, conn.ID, secret)
	} else {
		cli = s.NewClient(conn.EndpointURL, conn.Transport, secret)
	}

	info, err := cli.Initialize(ctx)
	if err != nil {
		return s.recordError(ctx, conn, err)
	}

	serverName := info.Name
	if serverName == "" {
		serverName = conn.Name
	}
	metadata, _ := json.Marshal(map[string]any{
		"source":          "connection",
		"connectionId":    conn.ID,
		"protocolVersion": info.ProtocolVersion,
		"instructions":    info.Instructions,
	})

	srv, err := s.Discovery.UpsertMCPServerForConnection(
		ctx, conn.OrgID, conn.ID, serverName, conn.EndpointURL, conn.Transport, info.Version, metadata,
	)
	if err != nil {
		return s.recordError(ctx, conn, err)
	}

	// Tools, resources, prompts — best-effort each, since not every server
	// implements all three. Errors are logged but don't fail the whole sweep.
	if tools, terr := cli.ListTools(ctx); terr == nil {
		for _, t := range tools {
			if _, err := s.Discovery.UpsertCapability(ctx, srv.ID, "tool", t.Name, t.Description, t.InputSchema); err != nil {
				log.Printf("scheduler: upsert tool %s: %v", t.Name, err)
			}
		}
	} else if !isMethodNotFound(terr) {
		log.Printf("scheduler: list tools for %s: %v", conn.Name, terr)
	}
	if resources, rerr := cli.ListResources(ctx); rerr == nil {
		for _, r := range resources {
			schema, _ := json.Marshal(map[string]any{"uri": r.URI, "mimeType": r.MimeType})
			if _, err := s.Discovery.UpsertCapability(ctx, srv.ID, "resource", r.Name, r.Description, schema); err != nil {
				log.Printf("scheduler: upsert resource %s: %v", r.Name, err)
			}
		}
	} else if !isMethodNotFound(rerr) {
		log.Printf("scheduler: list resources for %s: %v", conn.Name, rerr)
	}
	if prompts, perr := cli.ListPrompts(ctx); perr == nil {
		for _, p := range prompts {
			if _, err := s.Discovery.UpsertCapability(ctx, srv.ID, "prompt", p.Name, p.Description, p.Arguments); err != nil {
				log.Printf("scheduler: upsert prompt %s: %v", p.Name, err)
			}
		}
	} else if !isMethodNotFound(perr) {
		log.Printf("scheduler: list prompts for %s: %v", conn.Name, perr)
	}

	// TODO: capability diff alerts (#7 follow-up). For now we record additions
	// implicitly via first_seen_at and don't delete removed rows.
	id := srv.ID
	if err := s.Connections.UpdateAfterDiscovery(ctx, conn.ID, &id, "healthy", ""); err != nil {
		log.Printf("scheduler: update after discovery: %v", err)
	}
	return nil
}

func (s *Scheduler) recordError(ctx context.Context, conn *models.MCPConnection, err error) error {
	status := "error"
	if errors.Is(err, mcp.ErrUnauthorized) {
		status = "unauthorized"
	}
	if errors.Is(err, mcp.ErrOAuth2NeedsReauth) {
		status = "unauthorized"
		_ = s.Connections.UpdateOAuthStatus(ctx, conn.ID, models.OAuthStatusNeedsReauth)
	}
	if err2 := s.Connections.UpdateAfterDiscovery(ctx, conn.ID, nil, status, err.Error()); err2 != nil {
		log.Printf("scheduler: record-error update: %v", err2)
	}
	return err
}

// isMethodNotFound returns true when the server responds with JSON-RPC method
// not found (-32601). We treat this as "no capabilities of this kind" rather
// than a failure.
func isMethodNotFound(err error) bool {
	if err == nil {
		return false
	}
	return containsString(err.Error(), "method not found") || containsString(err.Error(), "-32601")
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
