package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

// ToolHandler is the signature every dispatcher entry implements. The
// dispatcher passes the path-scoped orgID through; tools never see (or
// receive) an org override from the LLM. Result is a JSON-serializable value
// returned to the model as the functionResponse payload.
type ToolHandler func(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error)

// ToolDef is the internal record we keep per registered tool: the Gemini
// FunctionDeclaration plus its handler.
type ToolDef struct {
	Decl    FunctionDeclaration
	Handler ToolHandler
}

// Dispatcher routes tool calls by name. Constructed once at startup with
// references to the same store packages the regular handlers use — no query
// duplication.
type Dispatcher struct {
	Activity  *store.Activity
	Callers   *store.Callers
	Dashboard *store.Dashboard
	Discovery *store.Discovery
	Gateways  *store.Gateways
	Policies  *store.Policies

	decls    []FunctionDeclaration
	handlers map[string]ToolHandler
}

// Tools returns the function declarations to ship to the model. Stable order.
func (d *Dispatcher) Tools() []FunctionDeclaration { return d.decls }

// Dispatch runs the named tool against args. Unknown name -> error.
func (d *Dispatcher) Dispatch(ctx context.Context, orgID uuid.UUID, name string, args json.RawMessage) (any, error) {
	h, ok := d.handlers[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return h(ctx, orgID, args)
}

// NewDispatcher wires every v1 tool. Adding a tool: declare the schema below
// in `register`, add the handler in tools_*.go, and call register here.
func NewDispatcher(
	activity *store.Activity,
	callers *store.Callers,
	dashboard *store.Dashboard,
	discovery *store.Discovery,
	gateways *store.Gateways,
	policies *store.Policies,
) *Dispatcher {
	d := &Dispatcher{
		Activity:  activity,
		Callers:   callers,
		Dashboard: dashboard,
		Discovery: discovery,
		Gateways:  gateways,
		Policies:  policies,
		handlers:  map[string]ToolHandler{},
	}
	d.registerServersTools()
	d.registerGatewaysTools()
	d.registerCallersTools()
	d.registerActivityTools()
	d.registerDashboardTools()
	d.registerPoliciesTools()
	d.registerCapabilitiesTools()
	return d
}

// Tool is the legacy shape the per-tool registration files use. We keep the
// same fields and translate to FunctionDeclaration at register time so the
// schema authors don't have to think about the wire format.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

func (d *Dispatcher) register(t Tool, h ToolHandler) {
	fd := FunctionDeclaration{
		Name:        t.Name,
		Description: t.Description,
		Parameters:  t.InputSchema,
	}
	d.decls = append(d.decls, fd)
	d.handlers[t.Name] = h
}

// SystemPrompt is shipped with every request. It frames the agent's role and
// constrains it to use the tools rather than fabricate data.
const SystemPrompt = `You are the Bright Guard assistant. You help users understand what is happening in their MCP/AI-tooling tenant by answering questions in plain language, grounded in their own data.

Rules:
- ALWAYS use the provided tools to look up live data before answering. Do not make up server names, caller identities, counts, or invocation details.
- Tools are auto-scoped to the user's active organization. You do not need an orgId; the system injects it.
- When a question is ambiguous (e.g. unspecified time range), default to the last 7 days and say so.
- Be concise. Prefer short bulleted lists and small tables when listing entities.
- If a tool returns zero results, say so plainly instead of speculating.
- For counts and aggregates, cite the time window you used.
- This is a read-only assistant: you cannot toggle capabilities, create policies, or change anything. If asked, say so and point to the relevant page in the SPA.

Deep links:
When you reference specific items in your answer, include a markdown link to the relevant page in the Bright Guard SPA. Use these URL patterns exactly:

  - Activity timeline (filtered):
    /app/activity?range=7d&status=denied&serverId={server_id}
  - Specific MCP server:
    /app/mcp-servers/{server_id}
  - Specific caller:
    /app/callers/{caller_id}
  - Specific gateway:
    /app/gateways/{gateway_id}
  - Specific policy:
    /app/policies/{policy_id}
  - Policies list:
    /app/policies
  - Servers list (with exposure filter):
    /app/mcp-servers?exposure=public

Render links with concise, natural-language anchor text. Example:
  "Three of your servers are publicly exposed: [github-mcp](/app/mcp-servers/abc-123), [jira-mcp](/app/mcp-servers/def-456), [slack-mcp](/app/mcp-servers/ghi-789)."

Only link to items that actually exist in your tool-call results. Never invent IDs.`

// schema is a tiny helper to turn a Go literal into the JSON-Schema raw bytes
// the request body expects.
func schema(v map[string]any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
