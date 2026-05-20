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
	"github.com/bright-guard/bright-guard/cloud/api/internal/config"
	"github.com/bright-guard/bright-guard/cloud/api/internal/email"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
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
	Scheduler   *scheduler.Scheduler
	Google      *auth.Google // may be nil if not configured
	Dev         *auth.DevLogin
	Cookie      auth.CookieOpts
	ServeSPA    bool // when true, mount the embedded SPA as a catch-all
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
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
			http.Error(w, "Google OAuth is not configured. Set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET.", http.StatusServiceUnavailable)
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

			// Org membership & invitations. Read endpoints are open to any
			// member; the write endpoints below require owner/admin.
			r.Get("/members", s.handleListMembers)
			r.Get("/invitations", s.handleListInvitations)
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireOrgRole(s.Orgs, orgIDFromURL, models.RoleOwner, models.RoleAdmin))
				r.Post("/invitations", s.handleCreateInvitation)
				r.Delete("/invitations/{id}", s.handleRevokeInvitation)
			})
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
		http.Error(w, "could not list memberships", http.StatusInternalServerError)
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
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	org, err := s.Orgs.Create(r.Context(), strings.TrimSpace(req.Name), user.ID)
	if err != nil {
		http.Error(w, "could not create org", http.StatusInternalServerError)
		return
	}
	if err := s.Sessions.SetActiveOrg(r.Context(), sess.ID, org.ID); err != nil {
		http.Error(w, "could not set active org", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (s *Server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	memberships, err := s.Orgs.ListForUser(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "could not list orgs", http.StatusInternalServerError)
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
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	orgID, err := uuid.Parse(req.OrgID)
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
		http.Error(w, "not a member", http.StatusForbidden)
		return
	}
	if err := s.Sessions.SetActiveOrg(r.Context(), sess.ID, orgID); err != nil {
		http.Error(w, "could not set active org", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
