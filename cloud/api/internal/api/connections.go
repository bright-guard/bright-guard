package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

const (
	connectionDiscoveryTimeout = 8 * time.Second
	oauthStateTTL              = 10 * time.Minute
	oauthCallbackPath          = "/oauth/connect/callback"
)

type oauthConfigReq struct {
	AuthorizeURL string            `json:"authorizeUrl"`
	TokenURL     string            `json:"tokenUrl"`
	ClientID     string            `json:"clientId"`
	ClientSecret string            `json:"clientSecret"`
	Scopes       string            `json:"scopes"`
	ExtraParams  map[string]string `json:"extraParams"`
}

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
	OAuthConfig *oauthConfigReq `json:"oauthConfig"`
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
	case models.AuthMethodAPIKeyHeader, models.AuthMethodBearer, models.AuthMethodBasic, models.AuthMethodOAuth2Authcode:
		return models.AuthMethod(m), true
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
		http.Error(w, "invalid authMethod", http.StatusBadRequest)
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

	oauthStatus := ""
	if method == models.AuthMethodOAuth2Authcode {
		if req.OAuthConfig == nil {
			http.Error(w, "oauthConfig is required for oauth2_authcode", http.StatusBadRequest)
			return
		}
		if err := validateOAuthConfig(req.OAuthConfig); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		secret.ClientID = strings.TrimSpace(req.OAuthConfig.ClientID)
		secret.ClientSecret = req.OAuthConfig.ClientSecret
		secret.AuthorizeURL = strings.TrimSpace(req.OAuthConfig.AuthorizeURL)
		secret.TokenURL = strings.TrimSpace(req.OAuthConfig.TokenURL)
		secret.Scopes = strings.TrimSpace(req.OAuthConfig.Scopes)
		secret.ExtraParams = req.OAuthConfig.ExtraParams
		secret.RedirectURI = strings.TrimRight(s.Cfg.AppBaseURL, "/") + oauthCallbackPath
		oauthStatus = models.OAuthStatusPendingAuthorize
	} else {
		if err := validateSecret(method, secret); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	conn, err := s.Connections.CreateWithOpts(r.Context(), store.CreateOpts{
		OrgID:       orgID,
		CreatedBy:   user.ID,
		Name:        name,
		Endpoint:    endpoint,
		Transport:   transport,
		Method:      method,
		Secret:      secret,
		OAuthStatus: oauthStatus,
	})
	if err != nil {
		http.Error(w, "could not create connection", http.StatusInternalServerError)
		return
	}

	// OAuth connections cannot be discovered until the user finishes the dance.
	// Everything else gets a best-effort sync discovery so the UI shows status
	// immediately.
	if method != models.AuthMethodOAuth2Authcode && s.Scheduler != nil {
		dctx, cancel := context.WithTimeout(r.Context(), connectionDiscoveryTimeout)
		defer cancel()
		_ = s.Scheduler.Discover(dctx, conn.ID)
		if reloaded, err := s.Connections.Get(r.Context(), orgID, conn.ID); err == nil {
			conn = reloaded
		}
	}
	writeJSON(w, http.StatusOK, conn)
}

func validateOAuthConfig(c *oauthConfigReq) error {
	if strings.TrimSpace(c.ClientID) == "" {
		return errors.New("oauthConfig.clientId is required")
	}
	if !isHTTPSURL(c.AuthorizeURL) {
		return errors.New("oauthConfig.authorizeUrl must be an https URL")
	}
	if !isHTTPSURL(c.TokenURL) {
		return errors.New("oauthConfig.tokenUrl must be an https URL")
	}
	return nil
}

func isHTTPSURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http"
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
	// Skip discovery for OAuth connections that haven't completed the dance —
	// otherwise we'd just hit the remote with no Authorization header.
	if conn.AuthMethod == models.AuthMethodOAuth2Authcode && conn.OAuthStatus != models.OAuthStatusAuthorized {
		writeJSON(w, http.StatusOK, conn)
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

// ----- OAuth2 authorization-code dance -----

type authorizeResp struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

// handleStartOAuthAuthorize creates the PKCE state and returns the provider
// authorize URL the SPA should redirect the user to.
func (s *Server) handleStartOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	conn, secret, err := s.Connections.GetWithSecret(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if conn.OrgID != orgID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if conn.AuthMethod != models.AuthMethodOAuth2Authcode {
		http.Error(w, "connection is not oauth2_authcode", http.StatusBadRequest)
		return
	}
	if secret.AuthorizeURL == "" || secret.TokenURL == "" || secret.ClientID == "" {
		http.Error(w, "oauth config incomplete", http.StatusBadRequest)
		return
	}

	state, err := randomBase64URL(32)
	if err != nil {
		http.Error(w, "rng failed", http.StatusInternalServerError)
		return
	}
	verifier, err := randomBase64URL(32)
	if err != nil {
		http.Error(w, "rng failed", http.StatusInternalServerError)
		return
	}
	challenge := pkceChallenge(verifier)

	returnTo := r.URL.Query().Get("returnTo")
	if returnTo == "" {
		returnTo = "/app/mcp-connections"
	}

	if err := s.Connections.PutOAuthState(r.Context(), store.OAuthState{
		State:        state,
		ConnectionID: conn.ID,
		OrgID:        conn.OrgID,
		UserID:       user.ID,
		CodeVerifier: verifier,
		ReturnTo:     returnTo,
		ExpiresAt:    time.Now().Add(oauthStateTTL),
	}); err != nil {
		http.Error(w, "could not start authorize", http.StatusInternalServerError)
		return
	}

	authorizeURL, err := buildAuthorizeURL(secret, state, challenge)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, authorizeResp{AuthorizeURL: authorizeURL})
}

// buildAuthorizeURL constructs the provider's authorize URL with PKCE params.
func buildAuthorizeURL(s mcp.AuthSecret, state, challenge string) (string, error) {
	u, err := url.Parse(s.AuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("bad authorize URL: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", s.ClientID)
	if s.RedirectURI != "" {
		q.Set("redirect_uri", s.RedirectURI)
	}
	if s.Scopes != "" {
		q.Set("scope", s.Scopes)
	}
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	for k, v := range s.ExtraParams {
		if k == "" {
			continue
		}
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// handleOAuthCallback is the unauthenticated endpoint the provider redirects
// to. State validates org/user; we exchange + persist tokens server-side and
// then 302 the browser back to the SPA.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	code := q.Get("code")
	provError := q.Get("error")

	if state == "" {
		http.Error(w, "missing state", http.StatusBadRequest)
		return
	}

	st, err := s.Connections.TakeOAuthState(r.Context(), state)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "unknown or expired state", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "state lookup failed", http.StatusInternalServerError)
		return
	}
	if time.Now().After(st.ExpiresAt) {
		s.redirectCallback(w, r, st.ReturnTo, st.ConnectionID, "error", "state_expired")
		return
	}

	if provError != "" {
		_ = s.Connections.UpdateOAuthStatus(r.Context(), st.ConnectionID, models.OAuthStatusNeedsReauth)
		s.redirectCallback(w, r, st.ReturnTo, st.ConnectionID, "error", provError)
		return
	}
	if code == "" {
		s.redirectCallback(w, r, st.ReturnTo, st.ConnectionID, "error", "missing_code")
		return
	}

	conn, secret, err := s.Connections.GetWithSecret(r.Context(), st.ConnectionID)
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if conn.OrgID != st.OrgID {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	updated, err := exchangeCodeForTokens(r.Context(), secret, code, st.CodeVerifier)
	if err != nil {
		log.Printf("oauth callback: token exchange for %s: %v", conn.ID, err)
		_ = s.Connections.UpdateOAuthStatus(r.Context(), conn.ID, models.OAuthStatusNeedsReauth)
		s.redirectCallback(w, r, st.ReturnTo, conn.ID, "error", "token_exchange_failed")
		return
	}
	updated.Method = string(models.AuthMethodOAuth2Authcode)
	if err := s.Connections.UpdateAuthState(r.Context(), conn.ID, updated); err != nil {
		http.Error(w, "persist failed", http.StatusInternalServerError)
		return
	}
	if err := s.Connections.UpdateOAuthStatus(r.Context(), conn.ID, models.OAuthStatusAuthorized); err != nil {
		http.Error(w, "persist failed", http.StatusInternalServerError)
		return
	}

	// Best-effort post-authorize discovery.
	if s.Scheduler != nil {
		dctx, cancel := context.WithTimeout(r.Context(), connectionDiscoveryTimeout)
		_ = s.Scheduler.Discover(dctx, conn.ID)
		cancel()
	}

	s.redirectCallback(w, r, st.ReturnTo, conn.ID, "ok", "")
}

// exchangeCodeForTokens POSTs to the token endpoint and returns an updated
// AuthSecret containing the new access/refresh tokens.
func exchangeCodeForTokens(ctx context.Context, s mcp.AuthSecret, code, verifier string) (mcp.AuthSecret, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", s.ClientID)
	if s.ClientSecret != "" {
		form.Set("client_secret", s.ClientSecret)
	}
	if s.RedirectURI != "" {
		form.Set("redirect_uri", s.RedirectURI)
	}
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return s, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	hc := &http.Client{Timeout: 15 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return s, fmt.Errorf("token endpoint status %d", resp.StatusCode)
	}
	return mcp.ApplyTokenResponse(s, body)
}

func (s *Server) redirectCallback(w http.ResponseWriter, r *http.Request, returnTo string, connID uuid.UUID, status, reason string) {
	base := strings.TrimRight(s.Cfg.WebBaseURL, "/")
	target := returnTo
	if !strings.HasPrefix(target, "/") {
		target = "/app/mcp-connections"
	}
	u, err := url.Parse(base + target)
	if err != nil {
		http.Error(w, "bad return_to", http.StatusInternalServerError)
		return
	}
	q := u.Query()
	q.Set("connection", connID.String())
	q.Set("status", status)
	if reason != "" {
		q.Set("reason", reason)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func randomBase64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// pkceChallenge derives the S256 code_challenge from a code_verifier.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
