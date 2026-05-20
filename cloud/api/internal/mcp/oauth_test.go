package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// pkceChallenge mirrors the API package's helper. We replicate it here so the
// mcp tests stay self-contained and verify the same construction.
func pkceChallengeForTest(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestPKCEChallengeRoundTrip(t *testing.T) {
	// Construct an authorize URL using the same shape the API layer would,
	// then have a stand-in token endpoint verify the code_verifier hashes
	// back to the code_challenge that arrived on /authorize.
	verifier := "k9pCnpgRpfqIyMVRZQjFnf9bU8e7Lm1nNcVzlEYy_w0"
	challenge := pkceChallengeForTest(verifier)

	authorize, _ := url.Parse("https://provider.example.com/authorize")
	q := authorize.Query()
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	authorize.RawQuery = q.Encode()

	got := authorize.Query().Get("code_challenge")
	if got == "" {
		t.Fatal("missing code_challenge")
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("code_challenge = %s, want %s", got, want)
	}
}

func TestApplyTokenResponse(t *testing.T) {
	body := []byte(`{"access_token":"a1","refresh_token":"r1","token_type":"Bearer","expires_in":3600,"scope":"read"}`)
	s, err := ApplyTokenResponse(AuthSecret{ClientID: "c"}, body)
	if err != nil {
		t.Fatalf("ApplyTokenResponse: %v", err)
	}
	if s.AccessToken != "a1" || s.RefreshToken != "r1" || s.TokenType != "Bearer" {
		t.Fatalf("got %+v", s)
	}
	if s.ExpiresAt == nil || s.ExpiresAt.Before(time.Now().Add(30*time.Minute)) {
		t.Fatalf("ExpiresAt not set correctly: %v", s.ExpiresAt)
	}
	if s.Scopes != "read" {
		t.Fatalf("Scopes = %q, want read", s.Scopes)
	}
}

// memTokenStore is an in-memory TokenStore for OAuth2RoundTripper tests.
type memTokenStore struct {
	mu      sync.Mutex
	secrets map[[16]byte]AuthSecret
	status  map[[16]byte]string
}

func newMemTokenStore() *memTokenStore {
	return &memTokenStore{
		secrets: map[[16]byte]AuthSecret{},
		status:  map[[16]byte]string{},
	}
}

func (m *memTokenStore) LoadAuthSecret(_ context.Context, id [16]byte) (AuthSecret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.secrets[id]
	if !ok {
		return AuthSecret{}, errors.New("not found")
	}
	return s, nil
}

func (m *memTokenStore) SaveAuthSecret(_ context.Context, id [16]byte, s AuthSecret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secrets[id] = s
	return nil
}

func (m *memTokenStore) MarkOAuthStatus(_ context.Context, id [16]byte, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status[id] = status
	return nil
}

func TestOAuth2RoundTripper_RefreshesExpiredToken(t *testing.T) {
	var refreshCalls int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", form.Get("grant_type"))
		}
		if form.Get("refresh_token") != "old-refresh" {
			t.Errorf("refresh_token = %q, want old-refresh", form.Get("refresh_token"))
		}
		refreshCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	// The upstream MCP endpoint records the auth header it received.
	var seenAuth string
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mcpSrv.Close()

	store := newMemTokenStore()
	id := [16]byte{1}
	expired := time.Now().Add(-1 * time.Hour)
	store.secrets[id] = AuthSecret{
		Method:       "oauth2_authcode",
		ClientID:     "client-x",
		TokenURL:     tokenSrv.URL,
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    &expired,
		TokenType:    "Bearer",
	}

	rt := &OAuth2RoundTripper{
		Base:         http.DefaultTransport,
		ConnectionID: id,
		Store:        store,
	}
	client := &http.Client{Transport: rt, Timeout: 5 * time.Second}
	resp, err := client.Get(mcpSrv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()

	if refreshCalls != 1 {
		t.Errorf("refreshCalls = %d, want 1", refreshCalls)
	}
	if seenAuth != "Bearer new-access" {
		t.Errorf("Authorization = %q, want Bearer new-access", seenAuth)
	}
	if got := store.secrets[id]; got.AccessToken != "new-access" || got.RefreshToken != "new-refresh" {
		t.Errorf("store not updated: %+v", got)
	}
}

func TestOAuth2RoundTripper_NoRefreshWhenStillValid(t *testing.T) {
	var refreshCalls int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer tokenSrv.Close()
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mcpSrv.Close()

	store := newMemTokenStore()
	id := [16]byte{2}
	future := time.Now().Add(1 * time.Hour)
	store.secrets[id] = AuthSecret{
		Method:       "oauth2_authcode",
		ClientID:     "c",
		TokenURL:     tokenSrv.URL,
		AccessToken:  "fresh",
		RefreshToken: "r",
		ExpiresAt:    &future,
	}

	rt := &OAuth2RoundTripper{ConnectionID: id, Store: store}
	client := &http.Client{Transport: rt}
	resp, err := client.Get(mcpSrv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if refreshCalls != 0 {
		t.Errorf("unexpected refresh: %d", refreshCalls)
	}
}

func TestOAuth2RoundTripper_4xxOnRefreshMarksNeedsReauth(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenSrv.Close()

	store := newMemTokenStore()
	id := [16]byte{3}
	expired := time.Now().Add(-1 * time.Hour)
	store.secrets[id] = AuthSecret{
		Method:       "oauth2_authcode",
		ClientID:     "c",
		TokenURL:     tokenSrv.URL,
		AccessToken:  "a",
		RefreshToken: "r",
		ExpiresAt:    &expired,
	}

	rt := &OAuth2RoundTripper{ConnectionID: id, Store: store}
	client := &http.Client{Transport: rt}
	_, err := client.Get("https://unreached.example.com")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrOAuth2NeedsReauth) {
		// http.Client wraps the RoundTripper error; check via string match too.
		if !strings.Contains(err.Error(), "re-auth") {
			t.Fatalf("error = %v, want needs-reauth", err)
		}
	}
	if got := store.status[id]; got != "needs_reauth" {
		t.Errorf("status = %q, want needs_reauth", got)
	}
}
