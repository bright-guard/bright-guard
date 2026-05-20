package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		raw := chi.URLParam(r, "orgId")
		orgID, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid orgId", http.StatusBadRequest)
			return
		}
		ok, err := s.Orgs.UserHasMembership(r.Context(), user.ID, orgID)
		if err != nil {
			http.Error(w, "membership check failed", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
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
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		gw, err := s.Gateways.AuthenticateCredential(r.Context(), token)
		if err != nil {
			http.Error(w, "invalid credential", http.StatusUnauthorized)
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
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	gw, err := s.Gateways.Create(r.Context(), orgID, user.ID, name, strings.TrimSpace(req.Description))
	if err != nil {
		http.Error(w, "could not create gateway", http.StatusInternalServerError)
		return
	}
	token, err := s.Gateways.IssueEnrollmentToken(r.Context(), gw, user.ID, enrollmentTokenTTL)
	if err != nil {
		http.Error(w, "could not issue token", http.StatusInternalServerError)
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
		http.Error(w, "could not list gateways", http.StatusInternalServerError)
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
		http.Error(w, "invalid gateway id", http.StatusBadRequest)
		return
	}
	gw, err := s.Gateways.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	servers, err := s.Discovery.ListServersForGateway(r.Context(), orgID, gw.ID)
	if err != nil {
		http.Error(w, "could not list servers", http.StatusInternalServerError)
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
		http.Error(w, "invalid gateway id", http.StatusBadRequest)
		return
	}
	if err := s.Gateways.Revoke(r.Context(), orgID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "revoke failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReissueEnrollment(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	id, err := parseGatewayID(r)
	if err != nil {
		http.Error(w, "invalid gateway id", http.StatusBadRequest)
		return
	}
	gw, err := s.Gateways.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	token, err := s.Gateways.IssueEnrollmentToken(r.Context(), gw, user.ID, enrollmentTokenTTL)
	if err != nil {
		http.Error(w, "could not issue token", http.StatusInternalServerError)
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
		http.Error(w, "could not list servers", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "serverId"))
	if err != nil {
		http.Error(w, "invalid server id", http.StatusBadRequest)
		return
	}
	det, err := s.Discovery.GetServerDetail(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
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
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.EnrollmentToken) == "" {
		http.Error(w, "enrollmentToken required", http.StatusBadRequest)
		return
	}
	gw, cred, err := s.Gateways.ClaimEnrollmentToken(r.Context(), req.EnrollmentToken)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidToken):
			http.Error(w, "invalid token", http.StatusUnauthorized)
		case errors.Is(err, store.ErrTokenExpired):
			http.Error(w, "token expired", http.StatusUnauthorized)
		case errors.Is(err, store.ErrTokenAlreadyUsed):
			http.Error(w, "token already claimed", http.StatusConflict)
		default:
			http.Error(w, "register failed", http.StatusInternalServerError)
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
type heartbeatResp struct {
	DisabledCapabilities []models.DisabledCapabilityRef `json:"disabledCapabilities"`
}

func (s *Server) handleGatewayHeartbeat(w http.ResponseWriter, r *http.Request) {
	gw := gatewayFromCtx(r.Context())
	if gw == nil {
		http.Error(w, "missing gateway", http.StatusUnauthorized)
		return
	}
	disabled, err := s.Discovery.ListDisabledCapabilitiesForGateway(r.Context(), gw.ID)
	if err != nil {
		http.Error(w, "could not list disabled capabilities", http.StatusInternalServerError)
		return
	}
	if disabled == nil {
		disabled = []models.DisabledCapabilityRef{}
	}
	writeJSON(w, http.StatusOK, heartbeatResp{DisabledCapabilities: disabled})
}

type observationServer struct {
	Name         string             `json:"name"`
	Address      string             `json:"address"`
	Transport    string             `json:"transport"`
	Version      string             `json:"version"`
	Metadata     json.RawMessage    `json:"metadata"`
	Capabilities []observationCap   `json:"capabilities"`
}

type observationCap struct {
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type observationInvocation struct {
	Server         string          `json:"server"`
	CapabilityKind string          `json:"capabilityKind"`
	CapabilityName string          `json:"capabilityName"`
	Caller         json.RawMessage `json:"caller"`
	Status         string          `json:"status"`
	LatencyMs      int             `json:"latencyMs"`
	At             time.Time       `json:"at"`
}

type observationsReq struct {
	Servers     []observationServer     `json:"servers"`
	Invocations []observationInvocation `json:"invocations"`
}

func (s *Server) handleGatewayObservations(w http.ResponseWriter, r *http.Request) {
	gw := gatewayFromCtx(r.Context())
	if gw == nil {
		http.Error(w, "missing gateway", http.StatusUnauthorized)
		return
	}
	var req observationsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	serverIDsByName := map[string]uuid.UUID{}
	for _, ss := range req.Servers {
		if strings.TrimSpace(ss.Name) == "" {
			continue
		}
		stored, err := s.Discovery.UpsertMCPServer(r.Context(), gw.OrgID, gw.ID, ss.Name, ss.Address, ss.Transport, ss.Version, ss.Metadata)
		if err != nil {
			http.Error(w, "upsert server failed", http.StatusInternalServerError)
			return
		}
		serverIDsByName[ss.Name] = stored.ID
		for _, cap := range ss.Capabilities {
			if cap.Kind == "" || cap.Name == "" {
				continue
			}
			if _, err := s.Discovery.UpsertCapability(r.Context(), stored.ID, cap.Kind, cap.Name, cap.Description, cap.Schema); err != nil {
				http.Error(w, "upsert capability failed", http.StatusInternalServerError)
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
		if err := s.Discovery.InsertInvocation(r.Context(), gw.OrgID, sid, inv.CapabilityKind, inv.CapabilityName, inv.Caller, inv.Status, inv.LatencyMs, at); err != nil {
			http.Error(w, "insert invocation failed", http.StatusInternalServerError)
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// serverId is bound by the route for tidy URLs but the cap-belongs-to-org check
	// is what enforces tenancy; the join also confirms the cap lives under that server.
	serverID, err := uuid.Parse(chi.URLParam(r, "serverId"))
	if err != nil {
		http.Error(w, "invalid server id", http.StatusBadRequest)
		return
	}
	capID, err := uuid.Parse(chi.URLParam(r, "capId"))
	if err != nil {
		http.Error(w, "invalid capability id", http.StatusBadRequest)
		return
	}
	var req setCapabilityEnabledReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	ok, err := s.Discovery.CapabilityBelongsToOrgServer(r.Context(), orgID, serverID, capID)
	if err != nil {
		http.Error(w, "membership check failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := s.Discovery.SetCapabilityEnabled(r.Context(), capID, req.Enabled, user.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "could not update capability", http.StatusInternalServerError)
		return
	}
	det, err := s.Discovery.GetServerDetail(r.Context(), orgID, serverID)
	if err != nil {
		http.Error(w, "could not reload server", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, det)
}
