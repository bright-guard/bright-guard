package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AuthSecret is the in-memory form of a connection's credential. For non-oauth
// methods exactly one credential field is populated per `Method`. For
// `oauth2_authcode` the embedded OAuth2 fields hold both the static client
// config and the (rotating) tokens.
type AuthSecret struct {
	Method      string `json:"method"`
	HeaderName  string `json:"headerName,omitempty"`
	HeaderValue string `json:"headerValue,omitempty"`
	BearerToken string `json:"bearerToken,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`

	// OAuth2 authorization-code fields. These are persisted on the same encrypted
	// blob as the credential above so a single AEAD round-trip carries everything
	// the RoundTripper needs.
	ClientID     string            `json:"client_id,omitempty"`
	ClientSecret string            `json:"client_secret,omitempty"`
	AuthorizeURL string            `json:"authorize_url,omitempty"`
	TokenURL     string            `json:"token_url,omitempty"`
	Scopes       string            `json:"scopes,omitempty"`
	RedirectURI  string            `json:"redirect_uri,omitempty"`
	ExtraParams  map[string]string `json:"extra_params,omitempty"`
	AccessToken  string            `json:"access_token,omitempty"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	ExpiresAt    *time.Time        `json:"expires_at,omitempty"`
	TokenType    string            `json:"token_type,omitempty"`

	// Dynamic Client Registration (RFC 7591 / 7592) provenance. These are
	// populated only when the client_id/secret above were minted via DCR rather
	// than typed in by an admin. The registration_access_token + URL are kept
	// so a future feature can re-register / rotate / delete the client without
	// requiring a fresh discovery cycle.
	DCRRegistrationURL         string `json:"dcr_registration_url,omitempty"`
	DCRRegistrationAccessToken string `json:"dcr_registration_access_token,omitempty"`
	DCRClientSecretExpiresAt   int64  `json:"dcr_client_secret_expires_at,omitempty"`
}

// ErrAuthMethodUnsupported is returned when a transport is asked to use an
// auth method we have not implemented yet.
var ErrAuthMethodUnsupported = errors.New("auth method not implemented")

// ErrOAuth2NeedsReauth signals that the stored refresh token no longer works
// and the user must redo the authorize dance.
var ErrOAuth2NeedsReauth = errors.New("oauth2: refresh token rejected, re-auth required")

// AuthRoundTripper wraps base with per-request credential injection for the
// static auth methods (api_key_header / bearer / basic). For oauth2_authcode
// callers must use OAuth2RoundTripper instead, since it needs live DB access
// to read + persist rotating tokens.
func AuthRoundTripper(base http.RoundTripper, secret AuthSecret) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &authRT{base: base, s: secret}
}

type authRT struct {
	base http.RoundTripper
	s    AuthSecret
}

func (a *authRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	switch a.s.Method {
	case "bearer":
		if a.s.BearerToken != "" {
			r.Header.Set("Authorization", "Bearer "+a.s.BearerToken)
		}
	case "basic":
		if a.s.Username != "" || a.s.Password != "" {
			enc := base64.StdEncoding.EncodeToString([]byte(a.s.Username + ":" + a.s.Password))
			r.Header.Set("Authorization", "Basic "+enc)
		}
	case "api_key_header":
		if a.s.HeaderName != "" {
			r.Header.Set(a.s.HeaderName, a.s.HeaderValue)
		}
	case "oauth2_authcode":
		// For oauth, callers should wrap with OAuth2RoundTripper. If they didn't,
		// fall back to whatever access_token happens to be in the secret — useful
		// for one-shot tests but unsafe in production because it never refreshes.
		if a.s.AccessToken != "" {
			r.Header.Set("Authorization", a.bearerWord()+" "+a.s.AccessToken)
		}
	}
	return a.base.RoundTrip(r)
}

func (a *authRT) bearerWord() string {
	if a.s.TokenType != "" {
		return a.s.TokenType
	}
	return "Bearer"
}

// TokenStore is the narrow interface OAuth2RoundTripper needs to read + persist
// the rotating token blob for a connection. *store.Connections satisfies it.
type TokenStore interface {
	LoadAuthSecret(ctx context.Context, connectionID [16]byte) (AuthSecret, error)
	SaveAuthSecret(ctx context.Context, connectionID [16]byte, secret AuthSecret) error
	MarkOAuthStatus(ctx context.Context, connectionID [16]byte, status string) error
}

// OAuth2RoundTripper injects a fresh access token on every outbound request,
// refreshing via the token endpoint when the cached one is about to expire.
type OAuth2RoundTripper struct {
	Base         http.RoundTripper
	ConnectionID [16]byte
	Store        TokenStore
	HTTP         *http.Client // for token endpoint; defaults to http.DefaultClient

	mu sync.Mutex
}

func (o *OAuth2RoundTripper) base() http.RoundTripper {
	if o.Base != nil {
		return o.Base
	}
	return http.DefaultTransport
}

func (o *OAuth2RoundTripper) tokenHTTP() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return http.DefaultClient
}

// RoundTrip serializes refreshes — concurrent requests for the same connection
// would otherwise race on the token endpoint and clobber each other's blobs.
func (o *OAuth2RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	o.mu.Lock()
	secret, err := o.Store.LoadAuthSecret(req.Context(), o.ConnectionID)
	if err != nil {
		o.mu.Unlock()
		return nil, err
	}
	if tokenNeedsRefresh(secret) {
		refreshed, rerr := refreshToken(req.Context(), o.tokenHTTP(), secret)
		if rerr != nil {
			_ = o.Store.MarkOAuthStatus(req.Context(), o.ConnectionID, "needs_reauth")
			o.mu.Unlock()
			return nil, rerr
		}
		secret = refreshed
		if err := o.Store.SaveAuthSecret(req.Context(), o.ConnectionID, secret); err != nil {
			o.mu.Unlock()
			return nil, err
		}
	}
	o.mu.Unlock()

	r := req.Clone(req.Context())
	if secret.AccessToken != "" {
		tt := secret.TokenType
		if tt == "" {
			tt = "Bearer"
		}
		r.Header.Set("Authorization", tt+" "+secret.AccessToken)
	}
	return o.base().RoundTrip(r)
}

func tokenNeedsRefresh(s AuthSecret) bool {
	if s.AccessToken == "" {
		return s.RefreshToken != ""
	}
	if s.ExpiresAt == nil {
		return false
	}
	// 60s skew gives us margin against clock drift + in-flight requests.
	return time.Until(*s.ExpiresAt) < 60*time.Second
}

// refreshToken exchanges the stored refresh_token for a new access token.
// Returns the updated AuthSecret (caller persists it). A 4xx response from
// the provider is translated into ErrOAuth2NeedsReauth.
func refreshToken(ctx context.Context, hc *http.Client, s AuthSecret) (AuthSecret, error) {
	if s.RefreshToken == "" || s.TokenURL == "" || s.ClientID == "" {
		return s, errors.New("oauth2: missing refresh token or client config")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", s.RefreshToken)
	form.Set("client_id", s.ClientID)
	if s.ClientSecret != "" {
		form.Set("client_secret", s.ClientSecret)
	}
	if s.Scopes != "" {
		form.Set("scope", s.Scopes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return s, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return s, fmt.Errorf("%w: status %d: %s", ErrOAuth2NeedsReauth, resp.StatusCode, truncate(body, 200))
	}
	if resp.StatusCode >= 500 {
		return s, fmt.Errorf("oauth2 refresh: status %d", resp.StatusCode)
	}
	return ApplyTokenResponse(s, body)
}

// ApplyTokenResponse parses a standard RFC 6749 token endpoint JSON body and
// returns the secret with access/refresh/expiry filled in. Refresh-token
// rotation: providers may issue a new refresh_token on refresh; if they do
// we adopt it, otherwise we keep the previous one.
func ApplyTokenResponse(s AuthSecret, body []byte) (AuthSecret, error) {
	var raw struct {
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		TokenType    string      `json:"token_type"`
		ExpiresIn    json.Number `json:"expires_in"`
		Scope        string      `json:"scope"`
		Error        string      `json:"error"`
		ErrorDesc    string      `json:"error_description"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return s, fmt.Errorf("oauth2: decode token response: %w", err)
	}
	if raw.Error != "" {
		return s, fmt.Errorf("oauth2 token error: %s: %s", raw.Error, raw.ErrorDesc)
	}
	if raw.AccessToken == "" {
		return s, errors.New("oauth2: token response missing access_token")
	}
	s.AccessToken = raw.AccessToken
	if raw.RefreshToken != "" {
		s.RefreshToken = raw.RefreshToken
	}
	if raw.TokenType != "" {
		s.TokenType = raw.TokenType
	}
	if raw.ExpiresIn != "" {
		if secs, err := strconv.ParseInt(string(raw.ExpiresIn), 10, 64); err == nil && secs > 0 {
			t := time.Now().Add(time.Duration(secs) * time.Second)
			s.ExpiresAt = &t
		}
	}
	if raw.Scope != "" {
		s.Scopes = raw.Scope
	}
	return s, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
