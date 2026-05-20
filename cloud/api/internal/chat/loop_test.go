package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeDispatcher struct {
	tools  []FunctionDeclaration
	calls  []string
	output map[string]any
}

func (f *fakeDispatcher) Tools() []FunctionDeclaration { return f.tools }
func (f *fakeDispatcher) Dispatch(ctx context.Context, orgID uuid.UUID, name string, args json.RawMessage) (any, error) {
	f.calls = append(f.calls, name)
	if v, ok := f.output[name]; ok {
		return v, nil
	}
	return map[string]any{"ok": true}, nil
}

// scriptedSender walks a canned sequence of responses; one per iteration.
type scriptedSender struct {
	resps []*Response
	i     int
	last  Request
}

func (s *scriptedSender) Send(ctx context.Context, req Request) (*Response, error) {
	s.last = req
	if s.i >= len(s.resps) {
		return &Response{Candidates: []Candidate{{
			Content:      Content{Role: "model", Parts: []Part{{Text: "done"}}},
			FinishReason: "STOP",
		}}}, nil
	}
	r := s.resps[s.i]
	s.i++
	return r, nil
}

func textResp(t string, prompt, candidates int) *Response {
	return &Response{
		Candidates: []Candidate{{
			Content:      Content{Role: "model", Parts: []Part{{Text: t}}},
			FinishReason: "STOP",
		}},
		UsageMetadata: UsageMetadata{PromptTokenCount: prompt, CandidatesTokenCount: candidates},
	}
}

func callResp(name string, args string, prompt, candidates int) *Response {
	return &Response{
		Candidates: []Candidate{{
			Content: Content{Role: "model", Parts: []Part{{
				FunctionCall: &FunctionCall{Name: name, Args: json.RawMessage(args)},
			}}},
			FinishReason: "STOP",
		}},
		UsageMetadata: UsageMetadata{PromptTokenCount: prompt, CandidatesTokenCount: candidates},
	}
}

func TestRunLoopFinishesWithoutTools(t *testing.T) {
	ss := &scriptedSender{resps: []*Response{textResp("Hello!", 10, 5)}}
	fd := &fakeDispatcher{}
	res, err := RunLoop(context.Background(), ss.Send, fd, uuid.New(), "gemini-x", nil, "hi", 4)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if res.AssistantText != "Hello!" {
		t.Fatalf("text = %q", res.AssistantText)
	}
	if res.InputTokens != 10 || res.OutputTokens != 5 {
		t.Fatalf("usage = %d/%d", res.InputTokens, res.OutputTokens)
	}
	if len(fd.calls) != 0 {
		t.Fatalf("expected no tool calls, got %v", fd.calls)
	}
}

func TestRunLoopDispatchesToolThenFinishes(t *testing.T) {
	ss := &scriptedSender{resps: []*Response{
		callResp("list_mcp_servers", `{"limit":5}`, 100, 20),
		textResp("You have 3 servers.", 150, 12),
	}}
	fd := &fakeDispatcher{
		output: map[string]any{"list_mcp_servers": map[string]any{"items": []any{1, 2, 3}, "total": 3}},
	}
	res, err := RunLoop(context.Background(), ss.Send, fd, uuid.New(), "gemini-x", nil, "how many servers?", 4)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if !strings.Contains(res.AssistantText, "3 servers") {
		t.Fatalf("text = %q", res.AssistantText)
	}
	if len(fd.calls) != 1 || fd.calls[0] != "list_mcp_servers" {
		t.Fatalf("expected list_mcp_servers call, got %v", fd.calls)
	}
	if res.InputTokens != 250 || res.OutputTokens != 32 {
		t.Fatalf("usage = %d/%d", res.InputTokens, res.OutputTokens)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(res.ToolCalls))
	}
}

func TestRunLoopHandlesParallelCalls(t *testing.T) {
	parallel := &Response{
		Candidates: []Candidate{{
			Content: Content{Role: "model", Parts: []Part{
				{FunctionCall: &FunctionCall{Name: "list_mcp_servers", Args: json.RawMessage(`{}`)}},
				{FunctionCall: &FunctionCall{Name: "list_gateways", Args: json.RawMessage(`{}`)}},
			}},
			FinishReason: "STOP",
		}},
		UsageMetadata: UsageMetadata{PromptTokenCount: 50, CandidatesTokenCount: 10},
	}
	ss := &scriptedSender{resps: []*Response{parallel, textResp("done", 60, 5)}}
	fd := &fakeDispatcher{}
	res, err := RunLoop(context.Background(), ss.Send, fd, uuid.New(), "gemini-x", nil, "two-things", 4)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if len(fd.calls) != 2 {
		t.Fatalf("expected 2 tool dispatches, got %v", fd.calls)
	}
	if len(res.ToolCalls) != 2 {
		t.Fatalf("expected 2 traces, got %d", len(res.ToolCalls))
	}
}

func TestRunLoopHitsIterationCap(t *testing.T) {
	loop := []*Response{
		callResp("list_mcp_servers", `{}`, 1, 1),
		callResp("list_mcp_servers", `{}`, 1, 1),
		callResp("list_mcp_servers", `{}`, 1, 1),
	}
	ss := &scriptedSender{resps: loop}
	fd := &fakeDispatcher{}
	res, err := RunLoop(context.Background(), ss.Send, fd, uuid.New(), "gemini-x", nil, "spin", 3)
	if err == nil {
		t.Fatalf("expected error after exhausting iterations")
	}
	if res == nil {
		t.Fatalf("expected non-nil result alongside error")
	}
	if !strings.Contains(res.AssistantText, "wasn't able") {
		t.Fatalf("expected fallback text, got %q", res.AssistantText)
	}
	if len(fd.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(fd.calls))
	}
}

func TestRunLoopCountsThoughtsTokens(t *testing.T) {
	ss := &scriptedSender{resps: []*Response{{
		Candidates: []Candidate{{
			Content:      Content{Role: "model", Parts: []Part{{Text: "ok"}}},
			FinishReason: "STOP",
		}},
		UsageMetadata: UsageMetadata{PromptTokenCount: 5, CandidatesTokenCount: 3, ThoughtsTokenCount: 4},
	}}}
	fd := &fakeDispatcher{}
	res, err := RunLoop(context.Background(), ss.Send, fd, uuid.New(), "gemini-x", nil, "hi", 2)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	// Thoughts tokens roll into OutputTokens for budget accounting.
	if res.OutputTokens != 7 {
		t.Fatalf("expected 7 output tokens (3 candidates + 4 thoughts), got %d", res.OutputTokens)
	}
}
