package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// httpTransport is the plain JSON-over-HTTP POST transport. Each request is a
// single POST; the response body is exactly one JSON-RPC response object.
type httpTransport struct {
	endpoint string
	http     *http.Client
}

func newHTTPTransport(endpoint string, hc *http.Client) *httpTransport {
	return &httpTransport{endpoint: endpoint, http: hc}
}

func (t *httpTransport) Invoke(ctx context.Context, req rpcRequest) (*rpcResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := t.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}

	var out rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// HTTPError carries a non-2xx response so callers can distinguish 401/403 from
// 5xx (which the client retries on).
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http %d: %s", e.Status, e.Body)
}
