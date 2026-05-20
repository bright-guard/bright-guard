package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockServer is a tiny MCP-style JSON-RPC handler used by these tests. Each
// method we care about returns a deterministic result so we can assert on it.
func mockServer(t *testing.T, expectAuth func(*http.Request) error) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if expectAuth != nil {
			if err := expectAuth(r); err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		body, _ := io.ReadAll(r.Body)
		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("bad json from client: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{
					"name":    "mock-server",
					"version": "1.2.3",
				},
				"instructions": "be nice",
			}
		case "tools/list":
			result = map[string]any{
				"tools": []map[string]any{
					{"name": "echo", "description": "echoes input", "inputSchema": map[string]any{"type": "object"}},
					{"name": "ping", "description": "responds pong"},
				},
			}
		case "resources/list":
			result = map[string]any{"resources": []map[string]any{}}
		case "prompts/list":
			result = map[string]any{"prompts": []map[string]any{}}
		case "ping":
			result = map[string]any{}
		default:
			resp := rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32601, Message: "method not found"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		raw, _ := json.Marshal(result)
		_ = json.NewEncoder(w).Encode(rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  raw,
		})
	}))
}

func TestClientInitializeAndListTools(t *testing.T) {
	srv := mockServer(t, nil)
	defer srv.Close()

	c := New(srv.URL, "streamable-http", AuthSecret{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	info, err := c.Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if info.Name != "mock-server" {
		t.Errorf("server name = %q, want mock-server", info.Name)
	}
	if info.Version != "1.2.3" {
		t.Errorf("server version = %q, want 1.2.3", info.Version)
	}
	if info.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocol version = %q, want %q", info.ProtocolVersion, ProtocolVersion)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("tools[0].Name = %q, want echo", tools[0].Name)
	}
}

func TestClientBearerAuthInjected(t *testing.T) {
	srv := mockServer(t, func(r *http.Request) error {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			return io.EOF
		}
		return nil
	})
	defer srv.Close()

	c := New(srv.URL, "http", AuthSecret{Method: "bearer", BearerToken: "s3cret"})
	ctx := context.Background()
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
}

func TestClientAPIKeyHeaderAuthInjected(t *testing.T) {
	srv := mockServer(t, func(r *http.Request) error {
		if r.Header.Get("X-Api-Key") != "abc123" {
			return io.EOF
		}
		return nil
	})
	defer srv.Close()

	c := New(srv.URL, "http", AuthSecret{
		Method:      "api_key_header",
		HeaderName:  "X-Api-Key",
		HeaderValue: "abc123",
	})
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestClientUnauthorizedMaps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, "http", AuthSecret{Method: "bearer", BearerToken: "bad"})
	_, err := c.Initialize(context.Background())
	if err != ErrUnauthorized {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestClientSSEResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Read the request id so our reply can match it.
		body, _ := io.ReadAll(r.Body)
		var req rpcRequest
		_ = json.Unmarshal(body, &req)
		// Notification (no id) the client should ignore.
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notify\",\"params\":{}}\n\n")
		// Real response.
		raw, _ := json.Marshal(rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"tools":[{"name":"hello"}]}`),
		})
		_, _ = io.WriteString(w, "data: "+string(raw)+"\n\n")
		// Make sure the stream gets flushed.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "streamable-http", AuthSecret{})
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "hello" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestClientRetriesOn5xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req rpcRequest
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{}`),
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "http", AuthSecret{})
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestEndpointURLValidated(t *testing.T) {
	// Sanity check: the client doesn't validate URLs itself — it just hands them
	// to http.NewRequestWithContext, which will reject obvious garbage. This
	// guards against a refactor accidentally swallowing the error.
	c := New("not a url", "http", AuthSecret{})
	_, err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected an error for invalid URL")
	}
	if !strings.Contains(err.Error(), "URL") && !strings.Contains(err.Error(), "url") {
		// Different Go versions phrase this differently, but it should mention url.
		t.Logf("got error: %v (not asserting wording)", err)
	}
}
