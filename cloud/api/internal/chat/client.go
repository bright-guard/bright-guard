// Package chat wires a small read-only AI agent to Vertex AI's generateContent
// endpoint. The surface is intentionally narrow: one Client, a tool Dispatcher,
// and a loop that drives "model -> tool -> model -> ..." until the model
// produces a final assistant text message (no more functionCall parts) or the
// iteration cap fires.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Client is a thin REST wrapper around Vertex AI's generateContent endpoint.
// Construct one at startup.
type Client struct {
	Project  string
	Location string
	Model    string
	HTTP     *http.Client
}

// NewClient resolves ADC and returns a ready Client. Callers should fall back
// gracefully (e.g. disable the chat endpoint) if NewClient returns an error.
func NewClient(ctx context.Context, project, location, model string) (*Client, error) {
	if project == "" {
		return nil, fmt.Errorf("chat: GOOGLE_CLOUD_PROJECT (or GCP_PROJECT) is required")
	}
	if location == "" {
		location = "us-central1"
	}
	if model == "" {
		return nil, fmt.Errorf("chat: CHAT_MODEL is required")
	}
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("chat: ADC: %w", err)
	}
	hc := oauth2.NewClient(ctx, creds.TokenSource)
	hc.Timeout = 60 * time.Second
	return &Client{
		Project:  project,
		Location: location,
		Model:    model,
		HTTP:     hc,
	}, nil
}

// Part is one item in a Content.Parts array. Exactly one of Text / FunctionCall
// / FunctionResponse will be set per Part.
type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

// FunctionCall is the model-emitted "please run tool" envelope.
type FunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// FunctionResponse is the caller-emitted "here is the tool result" envelope.
// Response is a JSON object — Gemini requires an object, not a bare array or
// scalar.
type FunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

// Content is one turn in the conversation. Role is "user" or "model".
type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// SystemInstruction is the system prompt wrapper Gemini expects.
type SystemInstruction struct {
	Parts []Part `json:"parts"`
}

// FunctionDeclaration is one tool schema entry.
type FunctionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolDecl is the wrapper Gemini expects around a list of function decls.
type ToolDecl struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations"`
}

// GenerationConfig tweaks decoding. We default to a wide maxOutputTokens
// because Gemini 2.5 counts "thinking" tokens against this budget and 1024
// truncates aggressively.
type GenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
}

// Request is the body POSTed to Vertex's generateContent endpoint.
type Request struct {
	Contents          []Content          `json:"contents"`
	SystemInstruction *SystemInstruction `json:"system_instruction,omitempty"`
	Tools             []ToolDecl         `json:"tools,omitempty"`
	GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
}

// UsageMetadata is the per-response token usage block. Gemini 2.5 reports a
// separate thoughtsTokenCount that is NOT included in candidatesTokenCount;
// total = prompt + candidates + thoughts.
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// Candidate is one model response candidate. We only ever inspect the first.
type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason"`
}

// Response is the parsed body from Vertex.
type Response struct {
	Candidates    []Candidate   `json:"candidates"`
	UsageMetadata UsageMetadata `json:"usageMetadata"`
}

// Send POSTs req to Vertex and parses the response.
func (c *Client) Send(ctx context.Context, req Request) (*Response, error) {
	if req.GenerationConfig == nil {
		req.GenerationConfig = &GenerationConfig{MaxOutputTokens: 4096, Temperature: 0.2}
	}
	buf, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		c.Location, c.Project, c.Location, c.Model,
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("chat: vertex post: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("chat: vertex returned %d: %s", resp.StatusCode, string(body))
	}
	var out Response
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("chat: parse: %w (body=%s)", err, string(body))
	}
	return &out, nil
}
