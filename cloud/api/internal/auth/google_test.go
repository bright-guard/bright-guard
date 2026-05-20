package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/oauth2"

	"github.com/bright-guard/bright-guard/cloud/api/internal/config"
)

func newTestGoogle(allowed []string) *Google {
	cfg := &config.Config{
		AppBaseURL:     "https://primary.example.com",
		WebBaseURL:     "https://web.example.com",
		GoogleClientID: "test-client-id",
		GoogleSecret:   "test-client-secret",
		AllowedHosts:   allowed,
	}
	return &Google{
		cfg: cfg,
		endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
	}
}

func TestStartHandler_DisallowedHost(t *testing.T) {
	g := newTestGoogle([]string{"primary.example.com"})
	r := httptest.NewRequest(http.MethodGet, "https://other.example.com/auth/google/start", nil)
	r.Host = "other.example.com"
	w := httptest.NewRecorder()

	g.StartHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestStartHandler_AllowedHostRedirects(t *testing.T) {
	g := newTestGoogle([]string{"primary.example.com", "alt.example.com"})
	r := httptest.NewRequest(http.MethodGet, "https://alt.example.com/auth/google/start", nil)
	r.Host = "alt.example.com"
	w := httptest.NewRecorder()

	g.StartHandler(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status: got %d, want %d (body=%s)", w.Code, http.StatusFound, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if loc == "" {
		t.Fatal("missing Location header")
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := u.Query().Get("redirect_uri"); got != "https://alt.example.com/auth/google/callback" {
		t.Errorf("redirect_uri: got %q", got)
	}
	if got := u.Query().Get("client_id"); got != "test-client-id" {
		t.Errorf("client_id: got %q", got)
	}
	// state cookie must be set on the inbound host so it round-trips.
	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == stateCookieName {
			found = true
			if c.Value == "" {
				t.Error("state cookie value empty")
			}
		}
	}
	if !found {
		t.Error("state cookie not set")
	}
}

func TestCallbackHandler_DisallowedHost(t *testing.T) {
	g := newTestGoogle([]string{"primary.example.com"})
	r := httptest.NewRequest(http.MethodGet,
		"https://other.example.com/auth/google/callback?state=x&code=y", nil)
	r.Host = "other.example.com"
	w := httptest.NewRecorder()

	g.CallbackHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestConfig_AllowedHosts_FromEnv(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("APP_BASE_URL", "https://fallback.example.com")
	t.Setenv("WEB_BASE_URL", "https://web.example.com")
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("DEV_LOGIN_ENABLED", "true")
	t.Setenv("ALLOWED_HOSTS", "a.example.com, b.example.com ,c.example.com")

	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	want := []string{"a.example.com", "b.example.com", "c.example.com"}
	if len(cfg.AllowedHosts) != len(want) {
		t.Fatalf("AllowedHosts: got %v, want %v", cfg.AllowedHosts, want)
	}
	for i, h := range want {
		if cfg.AllowedHosts[i] != h {
			t.Errorf("AllowedHosts[%d] = %q, want %q", i, cfg.AllowedHosts[i], h)
		}
	}
	for _, h := range want {
		if !cfg.IsAllowedHost(h) {
			t.Errorf("IsAllowedHost(%q) = false", h)
		}
	}
	if cfg.IsAllowedHost("nope.example.com") {
		t.Error("IsAllowedHost(nope) = true, want false")
	}
}

func TestConfig_AllowedHosts_FallsBackToAppBaseURL(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("APP_BASE_URL", "https://fallback.example.com")
	t.Setenv("WEB_BASE_URL", "https://web.example.com")
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("DEV_LOGIN_ENABLED", "true")
	t.Setenv("ALLOWED_HOSTS", "")

	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if len(cfg.AllowedHosts) != 1 || cfg.AllowedHosts[0] != "fallback.example.com" {
		t.Fatalf("AllowedHosts fallback: got %v", cfg.AllowedHosts)
	}
	if !cfg.IsAllowedHost("fallback.example.com") {
		t.Error("IsAllowedHost(fallback) = false")
	}
}
