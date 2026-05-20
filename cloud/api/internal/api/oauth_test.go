package api

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/bright-guard/bright-guard/cloud/api/internal/mcp"
)

func TestPKCEChallengeRoundTrip(t *testing.T) {
	// Whatever verifier we generate, the S256 challenge must match a fresh
	// SHA-256 done outside this package. This is the contract upstream IdPs
	// rely on to bind /authorize and /token.
	v, err := randomBase64URL(32)
	if err != nil {
		t.Fatalf("randomBase64URL: %v", err)
	}
	if len(v) < 32 {
		t.Fatalf("verifier too short: %s", v)
	}
	got := pkceChallenge(v)
	sum := sha256.Sum256([]byte(v))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("pkceChallenge = %s, want %s", got, want)
	}
	// Verifier must be URL-safe base64 (no padding, no +/).
	if strings.ContainsAny(v, "+/=") {
		t.Errorf("verifier contains disallowed chars: %s", v)
	}
}

func TestBuildAuthorizeURLIncludesPKCEAndExtraParams(t *testing.T) {
	secret := mcp.AuthSecret{
		ClientID:     "abc",
		AuthorizeURL: "https://idp.example.com/authorize",
		Scopes:       "read:jira-work write:jira-work",
		RedirectURI:  "https://mcp.example.dev/oauth/connect/callback",
		ExtraParams:  map[string]string{"audience": "api.atlassian.com", "prompt": "consent"},
	}
	out, err := buildAuthorizeURL(secret, "STATE", "CHALLENGE")
	if err != nil {
		t.Fatalf("buildAuthorizeURL: %v", err)
	}
	u, err := url.Parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("client_id") != "abc" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("state") != "STATE" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if q.Get("code_challenge") != "CHALLENGE" || q.Get("code_challenge_method") != "S256" {
		t.Errorf("missing PKCE on URL: %s", out)
	}
	if q.Get("scope") != secret.Scopes {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if q.Get("audience") != "api.atlassian.com" {
		t.Errorf("audience = %q", q.Get("audience"))
	}
	if q.Get("prompt") != "consent" {
		t.Errorf("prompt = %q", q.Get("prompt"))
	}
}

func TestValidAuthMethod_AcceptsOAuth(t *testing.T) {
	if _, ok := validAuthMethod("oauth2_authcode"); !ok {
		t.Fatal("oauth2_authcode should be valid")
	}
	if _, ok := validAuthMethod("garbage"); ok {
		t.Fatal("garbage should not be valid")
	}
}
