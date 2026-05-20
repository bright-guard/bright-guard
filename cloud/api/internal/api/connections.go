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
	// OAuthDCR opts into Dynamic Client Registration (RFC 7591) — the server
	// is probed for .well-known/oauth-protected-resource and
	// .well-known/oauth-authorization-server, and a fresh client is registered
	// on the fly. When set, OAuthConfig (admin-supplied client_id/secret +
	// URLs) is ignored.
	OAuthDCR bool `json:"oauthDcr"`
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
		writeError(w, http.StatusInternalServerError, "internal", "could not list connections")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	var req createConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	name := strings.TrimSpace(req.Name)
	endpoint := strings.TrimSpace(req.EndpointURL)
	transport := strings.TrimSpace(req.Transport)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if endpoint == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "endpointUrl is required")
		return
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "endpointUrl must be an absolute http(s) URL")
		return
	}
	if !validTransport(transport) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid transport")
		return
	}
	method, ok := validAuthMethod(req.AuthMethod)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid authMethod")
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
		redirectURI := strings.TrimRight(s.Cfg.AppBaseURL, "/") + oauthCallbackPath
		if req.OAuthDCR {
			// Discovery + dynamic registration. On any failure we surface a 422
			// with code dcr_unsupported so the SPA can prompt the user to fall
			// through to the manual flow.
			dcrSecret, err := s.runOAuthDCR(r.Context(), orgID, endpoint, redirectURI)
			if err != nil {
				writeError(w, http.StatusUnprocessableEntity, "dcr_unsupported", err.Error())
				return
			}
			secret.ClientID = dcrSecret.ClientID
			secret.ClientSecret = dcrSecret.ClientSecret
			secret.AuthorizeURL = dcrSecret.AuthorizeURL
			secret.TokenURL = dcrSecret.TokenURL
			secret.Scopes = dcrSecret.Scopes
			secret.RedirectURI = redirectURI
			secret.DCRRegistrationURL = dcrSecret.DCRRegistrationURL
			secret.DCRRegistrationAccessToken = dcrSecret.DCRRegistrationAccessToken
			secret.DCRClientSecretExpiresAt = dcrSecret.DCRClientSecretExpiresAt
			oauthStatus = models.OAuthStatusPendingAuthorize
		} else {
			if req.OAuthConfig == nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "oauthConfig is required for oauth2_authcode")
				return
			}
			if err := validateOAuthConfig(req.OAuthConfig); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			secret.ClientID = strings.TrimSpace(req.OAuthConfig.ClientID)
			secret.ClientSecret = req.OAuthConfig.ClientSecret
			secret.AuthorizeURL = strings.TrimSpace(req.OAuthConfig.AuthorizeURL)
			secret.TokenURL = strings.TrimSpace(req.OAuthConfig.TokenURL)
			secret.Scopes = strings.TrimSpace(req.OAuthConfig.Scopes)
			secret.ExtraParams = req.OAuthConfig.ExtraParams
			secret.RedirectURI = redirectURI
			oauthStatus = models.OAuthStatusPendingAuthorize
		}
	} else {
		if err := validateSecret(method, secret); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
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
		writeError(w, http.StatusInternalServerError, "internal", "could not create connection")
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

// runOAuthDCR drives the Discovery + Dynamic Client Registration cascade
// for a new connection. Returns a fully-populated AuthSecret subset (the
// caller copies fields into the persisted secret) or an error suitable for
// rendering into a `dcr_unsupported` HTTP response.
func (s *Server) runOAuthDCR(ctx context.Context, orgID uuid.UUID, endpointURL, redirectURI string) (mcp.AuthSecret, error) {
	meta, err := mcp.Probe(ctx, endpointURL)
	if err != nil {
		return mcp.AuthSecret{}, err
	}

	clientName := "Bright Guard"
	if s.Orgs != nil {
		if org, oerr := s.Orgs.Get(ctx, orgID); oerr == nil && org != nil && org.Name != "" {
			clientName = "Bright Guard - " + org.Name
		}
	}

	scope := strings.Join(meta.ScopesSupported, " ")
	resp, err := mcp.RegisterClient(ctx, meta.RegistrationURL, mcp.DCRRequest{
		ApplicationType:         "web",
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_post",
		ClientName:              clientName,
		Scope:                   scope,
	})
	if err != nil {
		return mcp.AuthSecret{}, err
	}

	return mcp.AuthSecret{
		ClientID:                   resp.ClientID,
		ClientSecret:               resp.ClientSecret,
		AuthorizeURL:               meta.AuthorizeURL,
		TokenURL:                   meta.TokenURL,
		Scopes:                     scope,
		DCRRegistrationURL:         resp.RegistrationClientURI,
		DCRRegistrationAccessToken: resp.RegistrationAccessToken,
		DCRClientSecretExpiresAt:   resp.ClientSecretExpiresAt,
	}, nil
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
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	conn, err := s.Connections.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, conn)
}

func (s *Server) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	if err := s.Connections.Delete(r.Context(), orgID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDiscoverConnection(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	conn, err := s.Connections.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	if s.Scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler_unconfigured", "scheduler not configured")
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
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
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
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	conn, secret, err := s.Connections.GetWithSecret(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	if conn.OrgID != orgID {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if conn.AuthMethod != models.AuthMethodOAuth2Authcode {
		writeError(w, http.StatusBadRequest, "invalid_request", "connection is not oauth2_authcode")
		return
	}
	if secret.AuthorizeURL == "" || secret.TokenURL == "" || secret.ClientID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "oauth config incomplete")
		return
	}

	state, err := randomBase64URL(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "rng failed")
		return
	}
	verifier, err := randomBase64URL(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "rng failed")
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
		writeError(w, http.StatusInternalServerError, "internal", "could not start authorize")
		return
	}

	authorizeURL, err := buildAuthorizeURL(secret, state, challenge)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
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
		writeError(w, http.StatusBadRequest, "invalid_request", "missing state")
		return
	}

	st, err := s.Connections.TakeOAuthState(r.Context(), state)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "invalid_request", "unknown or expired state")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "state lookup failed")
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
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	if conn.OrgID != st.OrgID {
		writeError(w, http.StatusBadRequest, "invalid_request", "state mismatch")
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
		writeError(w, http.StatusInternalServerError, "internal", "persist failed")
		return
	}
	if err := s.Connections.UpdateOAuthStatus(r.Context(), conn.ID, models.OAuthStatusAuthorized); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "persist failed")
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
		writeError(w, http.StatusInternalServerError, "internal", "bad return_to")
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
