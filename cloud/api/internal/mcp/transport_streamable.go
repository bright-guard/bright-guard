package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// streamableTransport implements the MCP "Streamable HTTP" transport. The
// client POSTs each JSON-RPC request; the server may respond with either:
//   - Content-Type: application/json — a single response object, or
//   - Content-Type: text/event-stream — an SSE stream where the first `data:`
//     event with a matching `id` is the response (later events are notifications
//     we ignore here).
type streamableTransport struct {
	endpoint string
	http     *http.Client
}

func newStreamableTransport(endpoint string, hc *http.Client) *streamableTransport {
	return &streamableTransport{endpoint: endpoint, http: hc}
}

func (t *streamableTransport) Invoke(ctx context.Context, req rpcRequest) (*rpcResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := t.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}

	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		return readSSEResponse(resp.Body, req.ID)
	}
	var out rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// readSSEResponse scans the SSE stream for the first `data:` payload whose id
// matches the request. It ignores notifications (id == 0/null) and unrelated ids.
func readSSEResponse(r io.Reader, wantID int) (*rpcResponse, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	var dataBuf bytes.Buffer
	flush := func() (*rpcResponse, bool, error) {
		if dataBuf.Len() == 0 {
			return nil, false, nil
		}
		raw := bytes.TrimRight(dataBuf.Bytes(), "\n")
		dataBuf.Reset()
		var out rpcResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			// Not a JSON-RPC message — skip.
			return nil, false, nil
		}
		if out.ID != wantID {
			return nil, false, nil
		}
		return &out, true, nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			resp, done, err := flush()
			if err != nil || done {
				return resp, err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataBuf.WriteString(strings.TrimPrefix(line, "data:"))
			dataBuf.WriteByte('\n')
		}
		// Ignore `event:` / `id:` / `:` comments.
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("sse read: %w", err)
	}
	if resp, done, err := flush(); done {
		return resp, err
	}
	return nil, fmt.Errorf("sse stream ended without response for id %d", wantID)
}
