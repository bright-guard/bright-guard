package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/bright-guard/bright-guard/cloud/api/internal/config"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

type Google struct {
	cfg       *config.Config
	endpoint  oauth2.Endpoint
	verifier  *oidc.IDTokenVerifier
	users     *store.Users
	orgs      *store.Orgs
	sessions  *store.Sessions
	cookieOpt CookieOpts
}

func NewGoogle(
	ctx context.Context,
	cfg *config.Config,
	users *store.Users,
	orgs *store.Orgs,
	sessions *store.Sessions,
	cookieOpt CookieOpts,
) (*Google, error) {
	if !cfg.GoogleConfigured() {
		return nil, nil
	}
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.GoogleClientID})
	return &Google{
		cfg:       cfg,
		endpoint:  provider.Endpoint(),
		verifier:  verifier,
		users:     users,
		orgs:      orgs,
		sessions:  sessions,
		cookieOpt: cookieOpt,
	}, nil
}

// redirectURIFor returns the OAuth callback URI for the inbound request, scoped
// to the same host the user is currently on so the state cookie matches.
// Scheme is always https — Cloud Run / production must terminate TLS.
func (g *Google) redirectURIFor(r *http.Request) (string, bool) {
	host := r.Host
	if !g.cfg.IsAllowedHost(host) {
		return "", false
	}
	return "https://" + host + "/auth/google/callback", true
}

// oauthConfigFor builds a per-request *oauth2.Config bound to the request's host.
func (g *Google) oauthConfigFor(redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     g.cfg.GoogleClientID,
		ClientSecret: g.cfg.GoogleSecret,
		RedirectURL:  redirectURI,
		Endpoint:     g.endpoint,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
}

func (g *Google) StartHandler(w http.ResponseWriter, r *http.Request) {
	redirectURI, ok := g.redirectURIFor(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "host not allowed")
		return
	}
	state, err := randState(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not generate state")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		Expires:  time.Now().Add(10 * time.Minute),
		HttpOnly: true,
		Secure:   g.cookieOpt.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	oauthCfg := g.oauthConfigFor(redirectURI)
	http.Redirect(w, r, oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOnline), http.StatusFound)
}

func (g *Google) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	redirectURI, ok := g.redirectURIFor(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "host not allowed")
		return
	}
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "missing state cookie")
		return
	}
	// One-time use: clear it.
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   g.cookieOpt.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	if r.URL.Query().Get("state") != stateCookie.Value {
		writeError(w, http.StatusBadRequest, "invalid_request", "state mismatch")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "missing code")
		return
	}
	oauthCfg := g.oauthConfigFor(redirectURI)
	tok, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_failure", "token exchange failed")
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		writeError(w, http.StatusBadGateway, "upstream_failure", "no id_token in response")
		return
	}
	idTok, err := g.verifier.Verify(ctx, rawID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_failure", "id_token verify failed")
		return
	}
	var claims struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := idTok.Claims(&claims); err != nil {
		writeError(w, http.StatusBadGateway, "upstream_failure", "bad claims")
		return
	}
	if claims.Sub == "" || claims.Email == "" {
		writeError(w, http.StatusBadGateway, "upstream_failure", "missing required claims")
		return
	}

	user, err := g.users.UpsertByGoogle(ctx, claims.Sub, claims.Email, claims.Name, claims.Picture)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not upsert user")
		return
	}
	sess, err := g.sessions.Create(ctx, user.ID, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create session")
		return
	}
	SetSessionCookie(w, sess.ID, sess.ExpiresAt, g.cookieOpt)

	memberships, err := g.orgs.ListForUser(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load memberships")
		return
	}
	dest := g.cfg.WebBaseURL + "/app"
	if len(memberships) == 0 {
		dest = g.cfg.WebBaseURL + "/onboarding"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

func randState(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// JSON helper used by other auth handlers in this package.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// apiErrorEnvelope mirrors the api package's error shape so /auth/* and
// middleware responses use the same wire format as /api/*.
type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiErrorEnvelope{Error: apiError{Code: code, Message: message}})
}
