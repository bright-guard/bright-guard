package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	// ProtocolVersion is the MCP protocol revision we negotiate with.
	ProtocolVersion = "2025-03-26"
	clientName      = "bright-guard"
	clientVersion   = "0.1.0"

	maxAttempts = 3
)

// Transport is the wire-level invoker each transport implementation satisfies.
type Transport interface {
	Invoke(ctx context.Context, req rpcRequest) (*rpcResponse, error)
}

// Client is a minimal MCP JSON-RPC client. It is safe to use from a single
// goroutine; concurrent use would need a real id allocator + response demux.
type Client struct {
	Endpoint  string
	Transport string // streamable-http | sse | http
	HTTP      *http.Client
	Auth      AuthSecret

	// nextID is atomically incremented per RPC for the JSON-RPC `id` field.
	nextID atomic.Int64

	tp Transport
}

// New builds a Client. The HTTP client is wrapped with an AuthRoundTripper so
// every request carries the configured credential.
func New(endpoint, transport string, auth AuthSecret) *Client {
	hc := &http.Client{
		Timeout: 30 * time.Second,
		Transport: AuthRoundTripper(http.DefaultTransport, auth),
	}
	c := &Client{
		Endpoint:  endpoint,
		Transport: transport,
		HTTP:      hc,
		Auth:      auth,
	}
	switch transport {
	case "http":
		c.tp = newHTTPTransport(endpoint, hc)
	default:
		// streamable-http handles SSE responses opportunistically; legacy
		// "sse" falls back to it (see transport_sse.go).
		c.tp = newStreamableTransport(endpoint, hc)
	}
	return c
}

// ErrUnauthorized is returned when the remote rejects our credentials.
var ErrUnauthorized = errors.New("mcp: unauthorized")

func (c *Client) do(ctx context.Context, method string, params any, out any) error {
	if c.Auth.Method == "oauth2_authcode" {
		return ErrAuthMethodUnsupported
	}
	id := int(c.nextID.Add(1))
	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	var lastErr error
	delay := 250 * time.Millisecond
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := c.tp.Invoke(ctx, req)
		if err != nil {
			lastErr = err
			var herr *HTTPError
			if errors.As(err, &herr) {
				if herr.Status == 401 || herr.Status == 403 {
					return ErrUnauthorized
				}
				if herr.Status >= 500 && herr.Status < 600 {
					// retryable
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(delay):
					}
					delay *= 2
					continue
				}
			}
			return err
		}
		if resp.Error != nil {
			return fmt.Errorf("mcp rpc %s: %s", method, resp.Error.Message)
		}
		if out != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("decode %s result: %w", method, err)
			}
		}
		return nil
	}
	return lastErr
}

// Initialize performs the MCP initialize handshake and returns server info.
// The MCP spec requires a `notifications/initialized` notification afterwards;
// we send it on a best-effort basis (transports don't currently support
// fire-and-forget, so any error is ignored).
func (c *Client) Initialize(ctx context.Context) (*ServerInfo, error) {
	params := initializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      clientInfo{Name: clientName, Version: clientVersion},
	}
	var raw struct {
		ProtocolVersion string          `json:"protocolVersion"`
		Capabilities    json.RawMessage `json:"capabilities"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Instructions string `json:"instructions"`
	}
	if err := c.do(ctx, "initialize", params, &raw); err != nil {
		return nil, err
	}
	return &ServerInfo{
		Name:            raw.ServerInfo.Name,
		Version:         raw.ServerInfo.Version,
		ProtocolVersion: raw.ProtocolVersion,
		Capabilities:    raw.Capabilities,
		Instructions:    raw.Instructions,
	}, nil
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var out listToolsResult
	if err := c.do(ctx, "tools/list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var out listResourcesResult
	if err := c.do(ctx, "resources/list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Resources, nil
}

func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var out listPromptsResult
	if err := c.do(ctx, "prompts/list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Prompts, nil
}

// Ping issues a JSON-RPC `ping` which servers must answer with an empty result.
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, "ping", map[string]any{}, nil)
}
