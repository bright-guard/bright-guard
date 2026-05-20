package policy

// Template is a starter policy customers can author with one click. Listed at
// GET /api/policy/templates and surfaced as a "Start from a template" picker
// on the PoliciesPage. The CEL source is what gets persisted unmodified into
// the policies.expression column when a customer clicks "Use this template".
//
// Wave N+8 ships two: one each for the headline UC8 (exposure) and UC9
// (credentials) use cases. New templates can be appended without changing the
// wire format.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Expression  string `json:"expression"`
	Action      string `json:"action"` // deny | warn
	// UseCase identifies the vision use case this template advances. Used by
	// the UI to render the "Enforces UC8/UC9" badge so customers can tell
	// real-deny policies apart from audit-only ones at a glance.
	UseCase string `json:"useCase"`
}

// Templates is the static list returned by the API. Stable order; the UI
// renders them in the listed sequence.
func Templates() []Template {
	return []Template{
		{
			ID:          "block-public-exposure",
			Name:        "Block public-exposure servers",
			Description: "Deny any invocation to a server classified as public. Backs vision UC8: internal MCP servers reachable externally are governed back behind your perimeter.",
			Expression:  `server.exposure_state == "public"`,
			Action:      "deny",
			UseCase:     "UC8",
		},
		{
			ID:          "block-unapproved-callers",
			Name:        "Block unapproved callers",
			Description: "Deny invocations from new callers that have not yet been acknowledged by an operator. Backs vision UC9: credential & identity governance — agents from unapproved identities are denied with an audit trail.",
			Expression:  `caller.flagged_new && !caller.acknowledged`,
			Action:      "deny",
			UseCase:     "UC9",
		},
	}
}
