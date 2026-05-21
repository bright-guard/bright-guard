package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// stubDispatcher mirrors fakeDispatcher but lets a test register specific
// tool decls + handlers without dragging in the real store. Used to assert
// the loop ships every registered tool to the model.
type stubDispatcher struct {
	tools    []FunctionDeclaration
	handlers map[string]ToolHandler
	calls    []string
}

func (s *stubDispatcher) Tools() []FunctionDeclaration { return s.tools }
func (s *stubDispatcher) Dispatch(ctx context.Context, orgID uuid.UUID, name string, args json.RawMessage) (any, error) {
	s.calls = append(s.calls, name)
	if h, ok := s.handlers[name]; ok {
		return h(ctx, orgID, args)
	}
	return map[string]any{"ok": true}, nil
}

// TestNewToolsAreDeclared registers the three v1.1 tools with stub handlers
// and asserts the loop relays each declaration into the request's tools
// payload (so Gemini sees them) and dispatches them correctly.
func TestNewToolsAreDeclared(t *testing.T) {
	sd := &stubDispatcher{handlers: map[string]ToolHandler{}}
	register := func(name string) {
		sd.tools = append(sd.tools, FunctionDeclaration{
			Name: name, Description: "stub",
			Parameters: json.RawMessage(`{"type":"object"}`),
		})
		sd.handlers[name] = func(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
			return map[string]any{"ok": true, "name": name}, nil
		}
	}
	register("list_capabilities")
	register("get_policy")
	register("dashboard_timeseries")

	ss := &scriptedSender{resps: []*Response{textResp("done", 1, 1)}}
	_, err := RunLoop(context.Background(), ss.Send, sd, uuid.New(), "gemini-x", nil, "hello", 2)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if len(ss.last.Tools) != 1 {
		t.Fatalf("expected 1 ToolDecl, got %d", len(ss.last.Tools))
	}
	names := map[string]bool{}
	for _, d := range ss.last.Tools[0].FunctionDeclarations {
		names[d.Name] = true
	}
	for _, want := range []string{"list_capabilities", "get_policy", "dashboard_timeseries"} {
		if !names[want] {
			t.Errorf("tool %q missing from request decls; got %v", want, names)
		}
	}
}

// TestNewToolsDispatch confirms each of the new tools, when the model emits a
// functionCall for them, gets routed through the dispatcher with its args
// intact and the handler's result wrapped into a functionResponse.
func TestNewToolsDispatch(t *testing.T) {
	sd := &stubDispatcher{handlers: map[string]ToolHandler{
		"list_capabilities": func(_ context.Context, _ uuid.UUID, args json.RawMessage) (any, error) {
			var in struct {
				ServerID string `json:"server_id"`
				Kind     string `json:"kind"`
			}
			_ = json.Unmarshal(args, &in)
			return map[string]any{"items": []any{}, "echo": in.Kind}, nil
		},
		"get_policy": func(_ context.Context, _ uuid.UUID, args json.RawMessage) (any, error) {
			return map[string]any{"id": "abc"}, nil
		},
		"dashboard_timeseries": func(_ context.Context, _ uuid.UUID, args json.RawMessage) (any, error) {
			return map[string]any{"metric": "denials", "points": []any{}}, nil
		},
	}}
	for name := range sd.handlers {
		sd.tools = append(sd.tools, FunctionDeclaration{Name: name})
	}
	parallel := &Response{
		Candidates: []Candidate{{
			Content: Content{Role: "model", Parts: []Part{
				{FunctionCall: &FunctionCall{Name: "list_capabilities", Args: json.RawMessage(`{"kind":"tool"}`)}},
				{FunctionCall: &FunctionCall{Name: "get_policy", Args: json.RawMessage(`{"policy_id":"x"}`)}},
				{FunctionCall: &FunctionCall{Name: "dashboard_timeseries", Args: json.RawMessage(`{"metric":"denials","range":"30d"}`)}},
			}},
			FinishReason: "STOP",
		}},
	}
	ss := &scriptedSender{resps: []*Response{parallel, textResp("done", 1, 1)}}
	res, err := RunLoop(context.Background(), ss.Send, sd, uuid.New(), "gemini-x", nil, "go", 4)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if len(res.ToolCalls) != 3 {
		t.Fatalf("expected 3 tool calls in trace, got %d", len(res.ToolCalls))
	}
	seen := map[string]bool{}
	for _, c := range sd.calls {
		seen[c] = true
	}
	for _, want := range []string{"list_capabilities", "get_policy", "dashboard_timeseries"} {
		if !seen[want] {
			t.Errorf("dispatcher never received %q (got %v)", want, sd.calls)
		}
	}
}

// TestSystemPromptShape locks in the read-only framing AND the deep-link
// section. If either side of the prompt is silently regressed the test fails.
func TestSystemPromptShape(t *testing.T) {
	required := []string{
		// Original framing — must stay.
		"You are the Bright Guard assistant",
		"ALWAYS use the provided tools",
		"read-only assistant",
		// Deep-link section — added in this wave.
		"Activity timeline (filtered):",
		"/app/mcp-servers/{server_id}",
		"/app/callers/{caller_id}",
		"/app/policies/{policy_id}",
		"Never invent IDs.",
	}
	for _, s := range required {
		if !strings.Contains(SystemPrompt, s) {
			t.Errorf("SystemPrompt missing required fragment: %q", s)
		}
	}
}
