package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

type ctxKey int

const (
	ctxKeyOrgID ctxKey = iota
	ctxKeyGateway
)

const enrollmentTokenTTL = 24 * time.Hour
const onlineWindow = 5 * time.Minute

func (s *Server) orgMember(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		raw := chi.URLParam(r, "orgId")
		orgID, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_org_id", "invalid orgId")
			return
		}
		ok, err := s.Orgs.UserHasMembership(r.Context(), user.ID, orgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "membership check failed")
			return
		}
		if !ok {
			writeError(w, http.StatusForbidden, "forbidden", "forbidden")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyOrgID, orgID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func orgFromCtx(ctx context.Context) uuid.UUID {
	v, _ := ctx.Value(ctxKeyOrgID).(uuid.UUID)
	return v
}

func gatewayFromCtx(ctx context.Context) *models.Gateway {
	v, _ := ctx.Value(ctxKeyGateway).(*models.Gateway)
	return v
}

func (s *Server) gatewayBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		gw, err := s.Gateways.AuthenticateCredential(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credential")
			return
		}
		_ = s.Gateways.TouchSeen(r.Context(), gw.ID)
		_ = s.Gateways.CommitEnrollmentOnHeartbeat(r.Context(), gw.ID)
		ctx := context.WithValue(r.Context(), ctxKeyGateway, gw)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ── org-facing gateway handlers ──────────────────────────────────────────

type createGatewayReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type createGatewayResp struct {
	Gateway         *models.Gateway `json:"gateway"`
	EnrollmentToken string          `json:"enrollmentToken"`
	InstallCommand  string          `json:"installCommand"`
	ExpiresAt       time.Time       `json:"expiresAt"`
}

func (s *Server) buildInstallCommand(gw *models.Gateway, token string) string {
	base := strings.TrimRight(s.Cfg.AppBaseURL, "/")
	vol := "bg-shim-" + gw.ID.String()
	return "docker run -d --name bg-shim-" + gw.ID.String() + " --restart unless-stopped" +
		" -v " + vol + ":/data" +
		" -e BG_CONTROL_PLANE=" + base +
		" -e BG_ENROLLMENT_TOKEN=" + token +
		" us-central1-docker.pkg.dev/bright-guard-prod/bright-guard/bright-guard-shim:latest"
}

func (s *Server) handleCreateGateway(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	var req createGatewayReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	gw, err := s.Gateways.Create(r.Context(), orgID, user.ID, name, strings.TrimSpace(req.Description))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create gateway")
		return
	}
	token, err := s.Gateways.IssueEnrollmentToken(r.Context(), gw, user.ID, enrollmentTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, createGatewayResp{
		Gateway:         gw,
		EnrollmentToken: token,
		InstallCommand:  s.buildInstallCommand(gw, token),
		ExpiresAt:       time.Now().Add(enrollmentTokenTTL),
	})
}

func (s *Server) handleListGateways(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	gws, err := s.Gateways.List(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list gateways")
		return
	}
	writeJSON(w, http.StatusOK, gws)
}

func parseGatewayID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "gatewayId"))
}

func (s *Server) handleGetGateway(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := parseGatewayID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid gateway id")
		return
	}
	gw, err := s.Gateways.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	servers, err := s.Discovery.ListServersForGateway(r.Context(), orgID, gw.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list servers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"gateway":    gw,
		"mcpServers": servers,
	})
}

func (s *Server) handleDeleteGateway(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := parseGatewayID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid gateway id")
		return
	}
	if err := s.Gateways.Revoke(r.Context(), orgID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "revoke failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReissueEnrollment(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	id, err := parseGatewayID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid gateway id")
		return
	}
	gw, err := s.Gateways.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	token, err := s.Gateways.IssueEnrollmentToken(r.Context(), gw, user.ID, enrollmentTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, createGatewayResp{
		Gateway:         gw,
		EnrollmentToken: token,
		InstallCommand:  s.buildInstallCommand(gw, token),
		ExpiresAt:       time.Now().Add(enrollmentTokenTTL),
	})
}

// ── org-facing MCP server handlers ───────────────────────────────────────

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	out, err := s.Discovery.ListServers(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list servers")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "serverId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid server id")
		return
	}
	det, err := s.Discovery.GetServerDetail(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, det)
}

// ── gateway-facing handlers ──────────────────────────────────────────────

type gatewayRegisterReq struct {
	EnrollmentToken string `json:"enrollmentToken"`
}

type gatewayRegisterResp struct {
	GatewayID  string `json:"gatewayId"`
	Credential string `json:"credential"`
}

func (s *Server) handleGatewayRegister(w http.ResponseWriter, r *http.Request) {
	var req gatewayRegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	if strings.TrimSpace(req.EnrollmentToken) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "enrollmentToken required")
		return
	}
	gw, cred, err := s.Gateways.ClaimEnrollmentToken(r.Context(), req.EnrollmentToken)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidToken):
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
		case errors.Is(err, store.ErrTokenExpired):
			writeError(w, http.StatusUnauthorized, "expired", "token expired")
		case errors.Is(err, store.ErrTokenAlreadyUsed):
			writeError(w, http.StatusConflict, "conflict", "token already claimed")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "register failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, gatewayRegisterResp{
		GatewayID:  gw.ID.String(),
		Credential: cred,
	})
}

// heartbeatResp is the wire-protocol contract for any future real-agentgateway
// shim: control plane returns the current denylist so the shim can refresh its
// local policy cache without a separate roundtrip.
//
// PolicyBundle is included only when the client's cached version (X-Bundle-Version
// header or ?bundleVersion=N query) is strictly less than the org's current
// policy_bundle_version. Otherwise it's omitted entirely to keep ticks light.
type heartbeatResp struct {
	DisabledCapabilities []models.DisabledCapabilityRef `json:"disabledCapabilities"`
	PolicyBundle         *models.PolicyBundle           `json:"policyBundle,omitempty"`
}

func clientBundleVersion(r *http.Request) int64 {
	raw := r.Header.Get("X-Bundle-Version")
	if raw == "" {
		raw = r.URL.Query().Get("bundleVersion")
	}
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (s *Server) handleGatewayHeartbeat(w http.ResponseWriter, r *http.Request) {
	gw := gatewayFromCtx(r.Context())
	if gw == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing gateway")
		return
	}
	disabled, err := s.Discovery.ListDisabledCapabilitiesForGateway(r.Context(), gw.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list disabled capabilities")
		return
	}
	if disabled == nil {
		disabled = []models.DisabledCapabilityRef{}
	}
	resp := heartbeatResp{DisabledCapabilities: disabled}

	if s.Policies != nil {
		clientVer := clientBundleVersion(r)
		version, policies, err := s.Policies.BundleFor(r.Context(), gw.OrgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not load policy bundle")
			return
		}
		if clientVer < version {
			bundle := &models.PolicyBundle{
				Version:  version,
				Policies: make([]models.BundlePolicy, 0, len(policies)),
			}
			for _, p := range policies {
				bundle.Policies = append(bundle.Policies, models.BundlePolicy{
					ID:         p.ID,
					Name:       p.Name,
					Action:     p.Action,
					Expression: p.Expression,
				})
			}
			resp.PolicyBundle = bundle
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type observationServer struct {
	Name         string           `json:"name"`
	Address      string           `json:"address"`
	Transport    string           `json:"transport"`
	Version      string           `json:"version"`
	Metadata     json.RawMessage  `json:"metadata"`
	Capabilities []observationCap `json:"capabilities"`
}

type observationCap struct {
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type observationInvocation struct {
	Server         string                `json:"server"`
	CapabilityKind string                `json:"capabilityKind"`
	CapabilityName string                `json:"capabilityName"`
	Caller         json.RawMessage       `json:"caller"`
	Status         string                `json:"status"`
	LatencyMs      int                   `json:"latencyMs"`
	At             time.Time             `json:"at"`
	Decisions      []observationDecision `json:"decisions,omitempty"`
}

// observationDecision is a single per-policy verdict the shim ships with the
// invocation when it has a policy bundle loaded. matched=true means the policy
// fired; action is the policy's configured action ("deny" or "warn"). If the
// shim flips status=denied on its side it ships the decisions too so the
// server can persist them and skip the sweep for this row.
type observationDecision struct {
	PolicyID string `json:"policyId"`
	Action   string `json:"action"`
	Matched  bool   `json:"matched"`
}

type observationsReq struct {
	Servers     []observationServer     `json:"servers"`
	Invocations []observationInvocation `json:"invocations"`
}

func (s *Server) handleGatewayObservations(w http.ResponseWriter, r *http.Request) {
	gw := gatewayFromCtx(r.Context())
	if gw == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing gateway")
		return
	}
	var req observationsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}

	serverIDsByName := map[string]uuid.UUID{}
	for _, ss := range req.Servers {
		if strings.TrimSpace(ss.Name) == "" {
			continue
		}
		stored, err := s.Discovery.UpsertMCPServer(r.Context(), gw.OrgID, gw.ID, ss.Name, ss.Address, ss.Transport, ss.Version, ss.Metadata)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "upsert server failed")
			return
		}
		serverIDsByName[ss.Name] = stored.ID
		for _, cap := range ss.Capabilities {
			if cap.Kind == "" || cap.Name == "" {
				continue
			}
			if _, err := s.Discovery.UpsertCapability(r.Context(), stored.ID, cap.Kind, cap.Name, cap.Description, cap.Schema); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "upsert capability failed")
				return
			}
		}
	}

	for _, inv := range req.Invocations {
		sid, ok := serverIDsByName[inv.Server]
		if !ok {
			continue
		}
		at := inv.At
		if at.IsZero() {
			at = time.Now()
		}

		// If the shim shipped policy decisions, materialize them so we can
		// persist alongside the invocation in one transaction. The presence
		// of decision rows is the marker the sweep uses to skip already-
		// evaluated invocations — so this is also how we honor "client did
		// the eval, don't re-do it server-side".
		var opts []store.InsertInvocationOption
		if len(inv.Decisions) > 0 {
			decs := make([]store.InvocationDecision, 0, len(inv.Decisions))
			for _, d := range inv.Decisions {
				pid, err := uuid.Parse(d.PolicyID)
				if err != nil {
					continue
				}
				decs = append(decs, store.InvocationDecision{
					PolicyID: pid,
					Action:   models.PolicyAction(d.Action),
					Matched:  d.Matched,
				})
			}
			if len(decs) > 0 {
				opts = append(opts, store.WithDecisions(decs))
			}
		}

		if err := s.Discovery.InsertInvocation(r.Context(), gw.OrgID, sid, inv.CapabilityKind, inv.CapabilityName, inv.Caller, inv.Status, inv.LatencyMs, at, opts...); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "insert invocation failed")
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// ── per-capability toggle ────────────────────────────────────────────────

type setCapabilityEnabledReq struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleSetCapabilityEnabled(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	// serverId is bound by the route for tidy URLs but the cap-belongs-to-org check
	// is what enforces tenancy; the join also confirms the cap lives under that server.
	serverID, err := uuid.Parse(chi.URLParam(r, "serverId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid server id")
		return
	}
	capID, err := uuid.Parse(chi.URLParam(r, "capId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid capability id")
		return
	}
	var req setCapabilityEnabledReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	ok, err := s.Discovery.CapabilityBelongsToOrgServer(r.Context(), orgID, serverID, capID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "membership check failed")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err := s.Discovery.SetCapabilityEnabled(r.Context(), capID, req.Enabled, user.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		log.Printf("SetCapabilityEnabled cap=%s enabled=%v err=%v", capID, req.Enabled, err)
		writeError(w, http.StatusInternalServerError, "internal", "could not update capability")
		return
	}
	det, err := s.Discovery.GetServerDetail(r.Context(), orgID, serverID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not reload server")
		return
	}
	writeJSON(w, http.StatusOK, det)
}
