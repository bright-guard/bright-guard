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
	oauth     *oauth2.Config
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
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleSecret,
		RedirectURL:  cfg.AppBaseURL + "/auth/google/callback",
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.GoogleClientID})
	return &Google{
		cfg:       cfg,
		oauth:     oauthCfg,
		verifier:  verifier,
		users:     users,
		orgs:      orgs,
		sessions:  sessions,
		cookieOpt: cookieOpt,
	}, nil
}

func (g *Google) StartHandler(w http.ResponseWriter, r *http.Request) {
	state, err := randState(32)
	if err != nil {
		http.Error(w, "could not generate state", http.StatusInternalServerError)
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
	http.Redirect(w, r, g.oauth.AuthCodeURL(state, oauth2.AccessTypeOnline), http.StatusFound)
}

func (g *Google) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
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
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	tok, err := g.oauth.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		http.Error(w, "no id_token in response", http.StatusBadGateway)
		return
	}
	idTok, err := g.verifier.Verify(ctx, rawID)
	if err != nil {
		http.Error(w, "id_token verify failed", http.StatusBadGateway)
		return
	}
	var claims struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := idTok.Claims(&claims); err != nil {
		http.Error(w, "bad claims", http.StatusBadGateway)
		return
	}
	if claims.Sub == "" || claims.Email == "" {
		http.Error(w, "missing required claims", http.StatusBadGateway)
		return
	}

	user, err := g.users.UpsertByGoogle(ctx, claims.Sub, claims.Email, claims.Name, claims.Picture)
	if err != nil {
		http.Error(w, "could not upsert user", http.StatusInternalServerError)
		return
	}
	sess, err := g.sessions.Create(ctx, user.ID, r.UserAgent())
	if err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	SetSessionCookie(w, sess.ID, sess.ExpiresAt, g.cookieOpt)

	memberships, err := g.orgs.ListForUser(ctx, user.ID)
	if err != nil {
		http.Error(w, "could not load memberships", http.StatusInternalServerError)
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
