package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/mcp"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const connectionDiscoveryTimeout = 8 * time.Second

type createConnectionReq struct {
	Name        string `json:"name"`
	EndpointURL string `json:"endpointUrl"`
	Transport   string `json:"transport"`
	AuthMethod  string `json:"authMethod"`
	AuthSecret  struct {
		HeaderName  string `json:"headerName"`
		HeaderValue string `json:"headerValue"`
		BearerToken string `json:"bearerToken"`
		Username    string `json:"username"`
		Password    string `json:"password"`
	} `json:"authSecret"`
}

func validTransport(t string) bool {
	switch t {
	case "streamable-http", "sse", "http":
		return true
	}
	return false
}

func validAuthMethod(m string) (models.AuthMethod, bool) {
	switch models.AuthMethod(m) {
	case models.AuthMethodAPIKeyHeader, models.AuthMethodBearer, models.AuthMethodBasic:
		return models.AuthMethod(m), true
	case models.AuthMethodOAuth2Authcode:
		// TODO(#8): once the OAuth2 authcode flow lands, accept this method here.
		return "", false
	}
	return "", false
}

func (s *Server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	out, err := s.Connections.List(r.Context(), orgID)
	if err != nil {
		http.Error(w, "could not list connections", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	var req createConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	endpoint := strings.TrimSpace(req.EndpointURL)
	transport := strings.TrimSpace(req.Transport)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if endpoint == "" {
		http.Error(w, "endpointUrl is required", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "endpointUrl must be an absolute http(s) URL", http.StatusBadRequest)
		return
	}
	if !validTransport(transport) {
		http.Error(w, "invalid transport", http.StatusBadRequest)
		return
	}
	method, ok := validAuthMethod(req.AuthMethod)
	if !ok {
		http.Error(w, "invalid or unsupported authMethod", http.StatusBadRequest)
		return
	}
	secret := mcp.AuthSecret{
		Method:      string(method),
		HeaderName:  strings.TrimSpace(req.AuthSecret.HeaderName),
		HeaderValue: req.AuthSecret.HeaderValue,
		BearerToken: req.AuthSecret.BearerToken,
		Username:    req.AuthSecret.Username,
		Password:    req.AuthSecret.Password,
	}
	if err := validateSecret(method, secret); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	conn, err := s.Connections.Create(r.Context(), orgID, user.ID, name, endpoint, transport, method, secret)
	if err != nil {
		http.Error(w, "could not create connection", http.StatusInternalServerError)
		return
	}

	// Best-effort synchronous discovery. The connection is persisted regardless
	// of the outcome — failures show up as status=error/unauthorized.
	if s.Scheduler != nil {
		dctx, cancel := context.WithTimeout(r.Context(), connectionDiscoveryTimeout)
		defer cancel()
		_ = s.Scheduler.Discover(dctx, conn.ID)
		// Reload so the response carries the post-discovery status.
		if reloaded, err := s.Connections.Get(r.Context(), orgID, conn.ID); err == nil {
			conn = reloaded
		}
	}
	writeJSON(w, http.StatusOK, conn)
}

func validateSecret(method models.AuthMethod, s mcp.AuthSecret) error {
	switch method {
	case models.AuthMethodAPIKeyHeader:
		if s.HeaderName == "" || s.HeaderValue == "" {
			return errors.New("headerName and headerValue are required")
		}
	case models.AuthMethodBearer:
		if s.BearerToken == "" {
			return errors.New("bearerToken is required")
		}
	case models.AuthMethodBasic:
		if s.Username == "" {
			return errors.New("username is required")
		}
	}
	return nil
}

func (s *Server) handleGetConnection(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	conn, err := s.Connections.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, conn)
}

func (s *Server) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.Connections.Delete(r.Context(), orgID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDiscoverConnection(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	conn, err := s.Connections.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if s.Scheduler == nil {
		http.Error(w, "scheduler not configured", http.StatusServiceUnavailable)
		return
	}
	dctx, cancel := context.WithTimeout(r.Context(), connectionDiscoveryTimeout)
	defer cancel()
	_ = s.Scheduler.Discover(dctx, conn.ID)
	reloaded, err := s.Connections.Get(r.Context(), orgID, conn.ID)
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, reloaded)
}
