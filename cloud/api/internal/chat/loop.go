package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SenderFunc abstracts the chat API call so tests can swap a mock in without
// needing real network. The real client's Send matches this signature.
type SenderFunc func(ctx context.Context, req Request) (*Response, error)

// DispatcherInterface lets the loop test pass a fake without dragging in a DB.
type DispatcherInterface interface {
	Tools() []FunctionDeclaration
	Dispatch(ctx context.Context, orgID uuid.UUID, name string, args json.RawMessage) (any, error)
}

// LoopResult is what the handler returns to its caller. AssistantText is the
// final user-visible text; ToolCalls is the trace of tools the model ran;
// ConversationDelta is the new messages to append to the persistent thread.
type LoopResult struct {
	AssistantText     string
	ToolCalls         []ToolCallTrace
	InputTokens       int
	OutputTokens      int
	StopReason        string
	ConversationDelta []Content
}

// ToolCallTrace is one row of "what the model asked for". Stored as part of
// the assistant message's tool_calls JSON so we can render compact "Looked up:
// list_mcp_servers · 12 results" blocks in the SPA.
type ToolCallTrace struct {
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	DurationMs int64           `json:"durationMs"`
	Error      string          `json:"error,omitempty"`
	OutputSize int             `json:"outputSize"`
}

// RunLoop drives one user-turn end-to-end. priorMessages is the existing
// conversation in Gemini Contents form (oldest first). userText is the new
// user message that's already been persisted by the caller.
//
// The loop:
//  1. Build a Request with system + tools + the full history.
//  2. Send.
//  3. If the model's response includes any functionCall parts: dispatch each,
//     append a user turn whose parts are the matching functionResponse parts,
//     go back to step 2.
//  4. Otherwise we have a final assistant text — extract and return.
//  5. Cap iterations to avoid runaway loops.
func RunLoop(
	ctx context.Context,
	send SenderFunc,
	disp DispatcherInterface,
	orgID uuid.UUID,
	model string,
	priorMessages []Content,
	userText string,
	maxIterations int,
) (*LoopResult, error) {
	if maxIterations <= 0 {
		maxIterations = 8
	}

	messages := append([]Content{}, priorMessages...)
	messages = append(messages, Content{
		Role:  "user",
		Parts: []Part{{Text: userText}},
	})
	newMessages := []Content{messages[len(messages)-1]}

	tools := []ToolDecl{}
	if decls := disp.Tools(); len(decls) > 0 {
		tools = append(tools, ToolDecl{FunctionDeclarations: decls})
	}

	result := &LoopResult{}

	for i := 0; i < maxIterations; i++ {
		req := Request{
			Contents:          messages,
			SystemInstruction: &SystemInstruction{Parts: []Part{{Text: SystemPrompt}}},
			Tools:             tools,
			GenerationConfig:  &GenerationConfig{MaxOutputTokens: 4096, Temperature: 0.2},
		}
		resp, err := send(ctx, req)
		if err != nil {
			return nil, err
		}
		// Token accounting: Gemini 2.5 reports thoughtsTokenCount separately
		// from candidatesTokenCount. We charge the org for both.
		result.InputTokens += resp.UsageMetadata.PromptTokenCount
		result.OutputTokens += resp.UsageMetadata.CandidatesTokenCount + resp.UsageMetadata.ThoughtsTokenCount

		if len(resp.Candidates) == 0 {
			result.AssistantText = "Sorry — I couldn't generate a response."
			result.ConversationDelta = newMessages
			return result, fmt.Errorf("chat: no candidates in response")
		}
		cand := resp.Candidates[0]
		result.StopReason = cand.FinishReason

		assistant := Content{Role: "model", Parts: cand.Content.Parts}
		messages = append(messages, assistant)
		newMessages = append(newMessages, assistant)

		// Collect functionCall parts. Gemini can emit multiple per turn.
		var calls []FunctionCall
		for _, p := range cand.Content.Parts {
			if p.FunctionCall != nil {
				calls = append(calls, *p.FunctionCall)
			}
		}

		if len(calls) == 0 {
			result.AssistantText = collectText(cand.Content.Parts)
			result.ConversationDelta = newMessages
			return result, nil
		}

		// Dispatch each function call and build the user turn carrying the
		// matching functionResponse parts.
		respParts := make([]Part, 0, len(calls))
		for _, fc := range calls {
			start := time.Now()
			out, err := disp.Dispatch(ctx, orgID, fc.Name, fc.Args)
			dur := time.Since(start)
			trace := ToolCallTrace{
				Name:       fc.Name,
				Input:      fc.Args,
				DurationMs: dur.Milliseconds(),
			}
			var payload json.RawMessage
			if err != nil {
				trace.Error = err.Error()
				payload, _ = json.Marshal(map[string]string{"error": err.Error()})
			} else {
				wrapped, mErr := wrapToolOutput(out)
				if mErr != nil {
					trace.Error = mErr.Error()
					payload, _ = json.Marshal(map[string]string{"error": mErr.Error()})
				} else {
					payload = wrapped
					trace.OutputSize = len(payload)
				}
			}
			respParts = append(respParts, Part{FunctionResponse: &FunctionResponse{
				Name:     fc.Name,
				Response: payload,
			}})
			result.ToolCalls = append(result.ToolCalls, trace)
		}
		userTurn := Content{Role: "user", Parts: respParts}
		messages = append(messages, userTurn)
		newMessages = append(newMessages, userTurn)
	}

	result.AssistantText = "Sorry — I wasn't able to finish answering that within my tool-use budget. Try a more specific question."
	result.ConversationDelta = newMessages
	return result, fmt.Errorf("chat: hit max iterations (%d)", maxIterations)
}

func collectText(parts []Part) string {
	out := ""
	for _, p := range parts {
		if p.Text != "" {
			if out != "" {
				out += "\n\n"
			}
			out += p.Text
		}
	}
	return out
}

// wrapToolOutput coerces a tool's return value into a JSON object. Gemini
// rejects functionResponse.response that is not a JSON object (arrays /
// strings / numbers / bools / null all fail). Our tools already return maps,
// but this is a safety net.
func wrapToolOutput(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 && raw[0] == '{' {
		return raw, nil
	}
	return json.Marshal(map[string]any{"result": v})
}
