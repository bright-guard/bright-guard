package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/policy"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	policySimulateMaxRows = 5000
	policySimulateMaxHrs  = 24 * 7 // hard cap on the window to keep one request bounded
	policyBackfillWindow  = 24 * time.Hour
	policyBackfillLimit   = 5000
)

type policyReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Expression  string `json:"expression"`
	Action      string `json:"action"`
	Enabled     *bool  `json:"enabled"`
}

func validPolicyAction(s string) (models.PolicyAction, bool) {
	switch models.PolicyAction(s) {
	case models.PolicyActionDeny, models.PolicyActionWarn:
		return models.PolicyAction(s), true
	}
	return "", false
}

// handleListPolicyTemplates returns the static starter-policy catalog. Used
// by the "Start from a template" affordance on PoliciesPage; one click pre-
// fills the new-policy modal with the CEL source.
func (s *Server) handleListPolicyTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": policy.Templates()})
}

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	out, err := s.Policies.List(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list policies")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	var req policyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	name := strings.TrimSpace(req.Name)
	expr := strings.TrimSpace(req.Expression)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if expr == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "expression is required")
		return
	}
	action := models.PolicyActionDeny
	if req.Action != "" {
		a, ok := validPolicyAction(req.Action)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid action")
			return
		}
		action = a
	}
	// Compile up front so the user gets the CEL error at create time, not
	// at the next sweep tick.
	if s.PolicyEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "policy_engine_unconfigured", "policy engine not configured")
		return
	}
	if _, err := s.PolicyEngine.Compile(expr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_expression", err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	out, err := s.Policies.Create(r.Context(), store.PolicyCreate{
		OrgID:       orgID,
		CreatedBy:   user.ID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Expression:  expr,
		Action:      action,
		Enabled:     enabled,
	})
	if err != nil {
		// Surface unique-name collision as 409 — every other error is opaque.
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "name_conflict", "a policy with that name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not create policy")
		return
	}
	// Best-effort post-create backfill against the last 24h. Don't block.
	if s.PolicySweep != nil && out.Enabled {
		go s.PolicySweep.BackfillPolicy(context.Background(), *out, policyBackfillWindow, policyBackfillLimit)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	out, err := s.Policies.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	var req policyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	existing, err := s.Policies.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	patch := store.PolicyPatch{}
	if v := strings.TrimSpace(req.Name); v != "" && v != existing.Name {
		patch.Name = &v
	}
	if req.Description != "" || (req.Description == "" && existing.Description != "") {
		d := strings.TrimSpace(req.Description)
		patch.Description = &d
	}
	exprChanged := false
	if v := strings.TrimSpace(req.Expression); v != "" && v != existing.Expression {
		if s.PolicyEngine == nil {
			writeError(w, http.StatusServiceUnavailable, "policy_engine_unconfigured", "policy engine not configured")
			return
		}
		if _, err := s.PolicyEngine.Compile(v); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_expression", err.Error())
			return
		}
		patch.Expression = &v
		exprChanged = true
	}
	if req.Action != "" {
		a, ok := validPolicyAction(req.Action)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid action")
			return
		}
		if a != existing.Action {
			patch.Action = &a
		}
	}
	if req.Enabled != nil && *req.Enabled != existing.Enabled {
		patch.Enabled = req.Enabled
	}

	out, err := s.Policies.Update(r.Context(), orgID, id, patch)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "name_conflict", "a policy with that name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "update failed")
		return
	}
	// Re-evaluate against the last 24h when the expression changed so the
	// activity view stays consistent. Fire-and-forget so the API response
	// isn't blocked on (potentially slow) replay.
	if exprChanged && s.PolicySweep != nil && out.Enabled {
		go s.PolicySweep.BackfillPolicy(context.Background(), *out, policyBackfillWindow, policyBackfillLimit)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	if err := s.Policies.Delete(r.Context(), orgID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type simulateReq struct {
	// Optional override — defaults to the persisted expression so the UI can
	// re-run the same policy without sending it back over the wire.
	Expression string    `json:"expression"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
}

type simulateMatch struct {
	InvocationID   uuid.UUID       `json:"invocationId"`
	At             time.Time       `json:"at"`
	ServerName     string          `json:"serverName"`
	CapabilityKind string          `json:"capabilityKind"`
	CapabilityName string          `json:"capabilityName"`
	Status         string          `json:"status"`
	Caller         json.RawMessage `json:"caller"`
}

type simulateResp struct {
	Scanned int             `json:"scanned"`
	Matches []simulateMatch `json:"matches"`
	From    time.Time       `json:"from"`
	To      time.Time       `json:"to"`
}

func (s *Server) handleSimulatePolicy(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	pol, err := s.Policies.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	var req simulateReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	expr := strings.TrimSpace(req.Expression)
	if expr == "" {
		expr = pol.Expression
	}
	if s.PolicyEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "policy_engine_unconfigured", "policy engine not configured")
		return
	}
	prg, err := s.PolicyEngine.Compile(expr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_expression", err.Error())
		return
	}

	to := req.To
	if to.IsZero() {
		to = time.Now().UTC()
	}
	from := req.From
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	if from.After(to) {
		writeError(w, http.StatusBadRequest, "invalid_request", "from must be <= to")
		return
	}
	if to.Sub(from) > time.Duration(policySimulateMaxHrs)*time.Hour {
		writeError(w, http.StatusBadRequest, "invalid_request", "window too large; max 7 days")
		return
	}

	invs, err := s.Policies.ListInvocationsInWindow(r.Context(), orgID, from, to, policySimulateMaxRows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list invocations failed")
		return
	}
	resp := simulateResp{Scanned: len(invs), Matches: []simulateMatch{}, From: from, To: to}
	for _, inv := range invs {
		ic := policy.InvocationContext{
			At:         inv.At,
			Status:     inv.Status,
			Caller:     inv.Caller,
			Server:     inv.Server,
			Capability: inv.Capability,
			Workload:   inv.Workload,
			Network:    inv.Network,
		}
		matched, err := prg.Evaluate(r.Context(), ic)
		if err != nil {
			continue
		}
		if !matched {
			continue
		}
		resp.Matches = append(resp.Matches, simulateMatch{
			InvocationID:   inv.ID,
			At:             inv.At,
			ServerName:     inv.Server["name"],
			CapabilityKind: inv.Capability["kind"],
			CapabilityName: inv.Capability["name"],
			Status:         inv.Status,
			Caller:         inv.Caller,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// isUniqueViolation returns true when err is a PostgreSQL unique-violation.
// We're loose with the detection here because the only unique constraint we
// surface to the user is (org_id, name).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "23505") || strings.Contains(s, "duplicate key") || strings.Contains(s, "policies_org_id_name_key")
}

// --- UC5 — Pre-mortem / post-mortem policy simulator ---
//
// Distinct from handleSimulatePolicy above (which is keyed by an existing
// policy id): this is the org-level "what would this CEL block?" endpoint that
// drives the "Simulate" tab on the new-policy modal and the "What is this
// blocking?" panel on PolicyDetailPage. Synchronous; bounded to keep
// sub-second on the largest current orgs.

type orgSimulateExprReq struct {
	Expression string `json:"expression"`
	Action     string `json:"action"`
}

type orgSimulateReq struct {
	Expression string              `json:"expression"`
	Action     string              `json:"action"`
	Range      string              `json:"range"` // 7d | 30d | 90d
	Comparison *orgSimulateExprReq `json:"comparison"`
}

type orgSimulateResp struct {
	*policy.SimulationResult
	Comparison *policy.SimulationResult `json:"comparison"`
}

func parseSimulationRange(s string) (time.Duration, bool) {
	switch s {
	case "", "30d":
		return 30 * 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "90d":
		return 90 * 24 * time.Hour, true
	}
	return 0, false
}

// handleOrgSimulatePolicy is the new UC5 entry point. Loads the org's recent
// invocations once and evaluates both the primary expression and (optionally)
// a comparison expression against the same set so the diff is apples-to-apples.
func (s *Server) handleOrgSimulatePolicy(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	var req orgSimulateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	expr := strings.TrimSpace(req.Expression)
	if expr == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "expression is required")
		return
	}
	action, ok := validPolicyAction(req.Action)
	if !ok {
		// Default to deny when omitted so the most common pre-mortem case is
		// a one-field POST.
		if req.Action == "" {
			action = models.PolicyActionDeny
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid action")
			return
		}
	}
	window, ok := parseSimulationRange(req.Range)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "range must be 7d, 30d, or 90d")
		return
	}
	if s.PolicyEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "policy_engine_unconfigured", "policy engine not configured")
		return
	}
	primaryPrg, err := s.PolicyEngine.Compile(expr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_expression", err.Error())
		return
	}
	var compPrg *policy.PolicyProgram
	var compAction policy.SimulationAction
	if req.Comparison != nil && strings.TrimSpace(req.Comparison.Expression) != "" {
		cexpr := strings.TrimSpace(req.Comparison.Expression)
		ca, cok := validPolicyAction(req.Comparison.Action)
		if !cok {
			if req.Comparison.Action == "" {
				ca = models.PolicyActionDeny
			} else {
				writeError(w, http.StatusBadRequest, "invalid_request", "invalid comparison action")
				return
			}
		}
		prg, err := s.PolicyEngine.Compile(cexpr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_comparison_expression", err.Error())
			return
		}
		compPrg = prg
		compAction = toSimulationAction(ca)
	}

	since := time.Now().UTC().Add(-window)
	invs, err := s.Policies.ListInvocationsForSimulation(r.Context(), orgID, since, policy.SimulationMaxRows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list invocations failed")
		return
	}
	truncated := len(invs) >= policy.SimulationMaxRows
	if truncated {
		log.Printf("policy simulate: org %s range=%s hit %d cap", orgID, req.Range, policy.SimulationMaxRows)
	}
	inputs := buildSimulationInputs(invs)

	result := policy.Simulate(r.Context(), primaryPrg, toSimulationAction(action), inputs)
	result.Truncated = truncated
	resp := orgSimulateResp{SimulationResult: &result}
	if compPrg != nil {
		cmp := policy.Simulate(r.Context(), compPrg, compAction, inputs)
		cmp.Truncated = truncated
		resp.Comparison = &cmp
	}
	writeJSON(w, http.StatusOK, resp)
}

// buildSimulationInputs lifts the store rows into the engine's input shape.
// CallerKey collapses caller identity to a single human-friendly token, using
// the enriched signature/label/agent fields in priority order. Anything else
// degrades to "(unknown)" upstream in Simulate.
func buildSimulationInputs(invs []store.InvocationContext) []policy.SimulationInput {
	out := make([]policy.SimulationInput, 0, len(invs))
	for _, inv := range invs {
		serverName := inv.Server["name"]
		capKind := inv.Capability["kind"]
		capName := inv.Capability["name"]
		capKey := capName
		if capKind != "" && capName != "" {
			capKey = capKind + "/" + capName
		}
		out = append(out, policy.SimulationInput{
			InvocationID:  inv.ID,
			At:            inv.At,
			ServerName:    serverName,
			CapabilityKey: capKey,
			CallerKey:     callerKeyFromJSON(inv.Caller),
			IC: policy.InvocationContext{
				At:         inv.At,
				Status:     inv.Status,
				Caller:     inv.Caller,
				Server:     inv.Server,
				Capability: inv.Capability,
			},
		})
	}
	return out
}

// callerKeyFromJSON extracts the most-useful identity field from the enriched
// caller blob. Order: label → signature → agent → user → "(unknown)".
func callerKeyFromJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	for _, k := range []string{"label", "signature", "agent", "user"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func toSimulationAction(a models.PolicyAction) policy.SimulationAction {
	if a == models.PolicyActionWarn {
		return policy.SimulationActionWarn
	}
	return policy.SimulationActionDeny
}

