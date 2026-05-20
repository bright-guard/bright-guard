package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/chat"
	"github.com/bright-guard/bright-guard/cloud/api/internal/config"
	"github.com/bright-guard/bright-guard/cloud/api/internal/email"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/policy"
	"github.com/bright-guard/bright-guard/cloud/api/internal/scheduler"
	"github.com/bright-guard/bright-guard/cloud/api/internal/spa"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

type Server struct {
	Cfg         *config.Config
	Users       *store.Users
	Orgs        *store.Orgs
	Sessions    *store.Sessions
	Gateways    *store.Gateways
	Discovery   *store.Discovery
	Activity    *store.Activity
	DeviceAuth  *store.DeviceAuth
	Connections *store.Connections
	Callers     *store.Callers
	Invitations *store.Invitations
	Email       email.Sender
	Platform    *store.Platform
	Policies    *store.Policies
	Dashboard   *store.Dashboard
	Chat           *store.Chat
	ChatClient     *chat.Client
	ChatDispatcher *chat.Dispatcher
	Scheduler   *scheduler.Scheduler
	PolicySweep *scheduler.PolicySweeper
	PolicyEngine *policy.Engine
	Google      *auth.Google // may be nil if not configured
	Dev         *auth.DevLogin
	Cookie      auth.CookieOpts
	ServeSPA    bool // when true, mount the embedded SPA as a catch-all
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	// Security headers run first so they're applied to every response, including
	// CORS preflights, the SPA, and error responses produced higher in the chain.
	r.Use(securityHeadersMiddleware)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(s.corsMiddleware())
	r.Use(auth.Middleware(s.Sessions, s.Users))

	r.Get("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	// Public dev-only endpoint that the SPA can check to decide whether to show
	// the dev-login UI block.
	r.Get("/api/dev/enabled", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": s.Cfg.DevLoginEnabled})
	})

	if s.Google != nil {
		r.Get("/auth/google/start", s.Google.StartHandler)
		r.Get("/auth/google/callback", s.Google.CallbackHandler)
	} else {
		// Friendly error so the SPA's "Continue with Google" doesn't 404 on dev.
		r.Get("/auth/google/start", func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusServiceUnavailable, "google_oauth_unconfigured",
				"Google OAuth is not configured. Set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET.")
		})
	}

	if s.Cfg.DevLoginEnabled && s.Dev != nil {
		r.Post("/auth/dev/login", s.Dev.Handler)
	}

	r.Post("/auth/logout", s.handleLogout)

	// Device-auth (CLI-facing, no auth required to start/poll).
	r.Post("/oauth/device", s.handleDeviceInitiate)
	r.Post("/oauth/device/poll", s.handleDevicePoll)

	// OAuth2 authorization-code callback. Reached by the user's browser
	// directly from the upstream provider; we don't require our session
	// cookie because the `state` token is the trust anchor.
	r.Get("/oauth/connect/callback", s.handleOAuthCallback)

	r.Group(func(r chi.Router) {
		r.Use(auth.RequireUser)
		r.Get("/api/me", s.handleMe)
		// Static catalog of starter CEL policies (UC8/UC9). Org-independent so
		// it lives at the auth-required root, not under /api/orgs/{orgId}.
		r.Get("/api/policy/templates", s.handleListPolicyTemplates)
		r.Post("/api/orgs", s.handleCreateOrg)
		r.Get("/api/orgs", s.handleListOrgs)
		r.Post("/api/sessions/active-org", s.handleSetActiveOrg)

		// Device-auth (SPA-facing, cookie/bearer protected).
		r.Get("/api/device/lookup", s.handleDeviceLookup)
		r.Post("/api/device/approve", s.handleDeviceApprove)
		r.Post("/api/device/deny", s.handleDeviceDeny)

		// Session management.
		r.Get("/api/sessions", s.handleListSessions)
		r.Delete("/api/sessions/{id}", s.handleRevokeSession)

		r.Route("/api/orgs/{orgId}", func(r chi.Router) {
			r.Use(s.orgMember)
			r.Get("/gateways", s.handleListGateways)
			r.Post("/gateways", s.handleCreateGateway)
			r.Get("/gateways/{gatewayId}", s.handleGetGateway)
			r.Delete("/gateways/{gatewayId}", s.handleDeleteGateway)
			r.Post("/gateways/{gatewayId}/enrollment-tokens", s.handleReissueEnrollment)

			r.Get("/mcp-servers", s.handleListServers)
			r.Get("/mcp-servers/{serverId}", s.handleGetServer)
			r.Patch("/mcp-servers/{serverId}/capabilities/{capId}", s.handleSetCapabilityEnabled)
			r.Post("/mcp-servers/{id}/reclassify-exposure", s.handleReclassifyExposure)

			r.Get("/exposures", s.handleListExposures)

			r.Get("/mcp-connections", s.handleListConnections)
			r.Post("/mcp-connections", s.handleCreateConnection)
			r.Get("/mcp-connections/{id}", s.handleGetConnection)
			r.Delete("/mcp-connections/{id}", s.handleDeleteConnection)
			r.Post("/mcp-connections/{id}/discover", s.handleDiscoverConnection)
			r.Get("/mcp-connections/{id}/authorize", s.handleStartOAuthAuthorize)

			r.Get("/activity", s.handleListActivity)
			r.Get("/activity/summary", s.handleActivitySummary)

			r.Get("/callers", s.handleListCallers)
			r.Get("/callers/{id}", s.handleGetCaller)
			r.Post("/callers/{id}/acknowledge", s.handleAcknowledgeCaller)

			r.Get("/policies", s.handleListPolicies)
			r.Post("/policies", s.handleCreatePolicy)
			r.Get("/policies/{id}", s.handleGetPolicy)
			r.Patch("/policies/{id}", s.handleUpdatePolicy)
			r.Delete("/policies/{id}", s.handleDeletePolicy)
			r.Post("/policies/{id}/simulate", s.handleSimulatePolicy)

			// Org membership & invitations. Read endpoints are open to any
			// member; the write endpoints below require owner/admin.
			r.Get("/members", s.handleListMembers)
			r.Get("/invitations", s.handleListInvitations)
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireOrgRole(s.Orgs, orgIDFromURL, models.RoleOwner, models.RoleAdmin))
				r.Post("/invitations", s.handleCreateInvitation)
				r.Delete("/invitations/{id}", s.handleRevokeInvitation)
			})

			r.Get("/dashboard/kpis", s.handleDashboardKPIs)
			r.Get("/dashboard/timeseries", s.handleDashboardTimeseries)
			r.Get("/dashboard/highlights", s.handleDashboardHighlights)
			r.Get("/dashboard/callouts", s.handleDashboardCallouts)

			// In-product chat assistant. Read-only Q&A over the org's MCP /
			// gateway / caller / activity / policy data.
			r.Post("/chat/sessions", s.handleCreateChatSession)
			r.Get("/chat/sessions", s.handleListChatSessions)
			r.Get("/chat/sessions/{sid}", s.handleGetChatSession)
			r.Delete("/chat/sessions/{sid}", s.handleDeleteChatSession)
			r.Post("/chat/sessions/{sid}/messages", s.handlePostChatMessage)

			// UC5 — pre/post-mortem policy simulator (org-level, no policy id).
			// Placed after /policies/{id}/simulate so chi resolves the static
			// "simulate" suffix without an id present to this route.
			r.Post("/policies/simulate", s.handleOrgSimulatePolicy)
		})

		// Invitee-facing routes. These are NOT under orgMember because the
		// caller may not yet be a member of the inviting org.
		r.Get("/api/me/invitations", s.handleListMyInvitations)
		r.Post("/api/invitations/{id}/accept", s.handleAcceptInvitation)
		r.Post("/api/invitations/{id}/decline", s.handleDeclineInvitation)

		// Platform-admin (backoffice) routes. RequirePlatformAdmin is stacked
		// on RequireUser so unauthenticated callers get 401 and non-admin
		// tenants get 403.
		r.Route("/api/platform", func(r chi.Router) {
			r.Use(auth.RequirePlatformAdmin(s.Platform))
			r.Get("/overview", s.handlePlatformOverview)
			r.Get("/activity", s.handlePlatformActivity)
			r.Get("/users", s.handlePlatformListUsers)
			r.Get("/users/{id}", s.handlePlatformGetUser)
			r.Post("/users/{id}/suspend", s.handlePlatformSuspendUser)
			r.Post("/users/{id}/unsuspend", s.handlePlatformUnsuspendUser)
			r.Delete("/users/{id}", s.handlePlatformDeleteUser)
			r.Post("/users/{id}/promote", s.handlePlatformPromote)
			r.Post("/users/{id}/demote", s.handlePlatformDemote)
			r.Get("/orgs", s.handlePlatformListOrgs)
			r.Get("/orgs/{id}", s.handlePlatformGetOrg)
			r.Post("/orgs/{id}/suspend", s.handlePlatformSuspendOrg)
			r.Delete("/orgs/{id}", s.handlePlatformDeleteOrg)
			r.Get("/admins", s.handlePlatformListAdmins)
			r.Get("/audit", s.handlePlatformAudit)
		})
	})

	// Gateway-facing routes (bearer auth, no cookies).
	r.Post("/v1/gateway/register", s.handleGatewayRegister)
	r.Group(func(r chi.Router) {
		r.Use(s.gatewayBearer)
		r.Post("/v1/gateway/heartbeat", s.handleGatewayHeartbeat)
		r.Post("/v1/gateway/observations", s.handleGatewayObservations)
	})

	// SPA catch-all. Anything not matched above falls through to the embedded
	// React bundle (or its placeholder in local dev).
	if s.ServeSPA {
		r.Mount("/", spa.Handler())
	}

	return r
}

func (s *Server) corsMiddleware() func(http.Handler) http.Handler {
	allowed := strings.TrimRight(s.Cfg.WebBaseURL, "/")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && origin == allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess := auth.SessionFromContext(r.Context()); sess != nil {
		if err := s.Sessions.Delete(r.Context(), sess.ID); err != nil {
			log.Printf("logout: delete session: %v", err)
		}
	}
	auth.ClearSessionCookie(w, s.Cookie)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	sess := auth.SessionFromContext(r.Context())
	memberships, err := s.Orgs.ListForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list memberships")
		return
	}
	if memberships == nil {
		memberships = []models.Membership{}
	}
	platformAdmin := false
	if s.Platform != nil {
		// Best-effort: a DB blip shouldn't make /api/me fail.
		if ok, err := s.Platform.IsActiveAdmin(r.Context(), user.ID); err == nil {
			platformAdmin = ok
		} else {
			log.Printf("me: platform admin check: %v", err)
		}
	}
	resp := map[string]any{
		"user":          user,
		"memberships":   memberships,
		"activeOrgId":   sess.ActiveOrgID,
		"platformAdmin": platformAdmin,
	}
	writeJSON(w, http.StatusOK, resp)
}

type createOrgReq struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	sess := auth.SessionFromContext(r.Context())
	var req createOrgReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	org, err := s.Orgs.Create(r.Context(), strings.TrimSpace(req.Name), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create org")
		return
	}
	if err := s.Sessions.SetActiveOrg(r.Context(), sess.ID, org.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not set active org")
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (s *Server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	memberships, err := s.Orgs.ListForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list orgs")
		return
	}
	writeJSON(w, http.StatusOK, memberships)
}

type setActiveOrgReq struct {
	OrgID string `json:"orgId"`
}

func (s *Server) handleSetActiveOrg(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	sess := auth.SessionFromContext(r.Context())
	var req setActiveOrgReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	orgID, err := uuid.Parse(req.OrgID)
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
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err := s.Sessions.SetActiveOrg(r.Context(), sess.ID, orgID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not set active org")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// apiError is the inner shape returned on any non-2xx; apiErrorEnvelope wraps
// it so the wire format is always {"error":{"code":..., "message":...}}.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

// writeError is the single chokepoint for non-2xx responses. Pick a stable,
// machine-readable code; the message is free-form and human-facing. Do not
// include secret material (token fragments, DB connection strings, etc.).
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorEnvelope{Error: apiError{Code: code, Message: message}})
}

// securityHeadersMiddleware sets a conservative baseline of HTTP security
// headers on every response. CSP allows Google Fonts (used by the SPA) and
// data: image URIs (favicons / inline SVG); everything else is same-origin.
// If the SPA ever needs to fetch from an additional origin, extend the
// relevant directive here rather than per-route.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"img-src 'self' data: https:; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
