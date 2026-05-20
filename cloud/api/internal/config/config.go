package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port             string
	AppBaseURL       string
	WebBaseURL       string
	DatabaseURL      string
	SessionSecret    string
	CookieSecure     bool
	GoogleClientID   string
	GoogleSecret     string
	DevLoginEnabled  bool
	AllowedHosts     []string
	// Email
	EmailProvider    string // "stub" (default) or "gcp_email"
	EmailFrom        string // From address used by all outbound mail
	GCPProject       string // For email API; falls back to GOOGLE_CLOUD_PROJECT
	GCPEmailLocation string // For email API; defaults to "global"
	// PlatformAdminSeedEmails are auto-promoted to platform_admins on sign-in.
	PlatformAdminSeedEmails []string
}

// Default seed list of platform admins. Used when PLATFORM_ADMIN_SEED_EMAILS
// is unset.
var defaultPlatformAdminSeedEmails = []string{
	"daniel@danielgarcia.info",
	"dgarcia@infoblox.com",
}

func FromEnv() (*Config, error) {
	c := &Config{
		Port:             getenv("PORT", "8080"),
		AppBaseURL:       os.Getenv("APP_BASE_URL"),
		WebBaseURL:       os.Getenv("WEB_BASE_URL"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		SessionSecret:    os.Getenv("SESSION_SECRET"),
		GoogleClientID:   os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleSecret:     os.Getenv("GOOGLE_CLIENT_SECRET"),
		EmailProvider:    getenv("EMAIL_PROVIDER", "stub"),
		EmailFrom:        getenv("EMAIL_FROM", "noreply@mcp-governance.infoblox.dev"),
		GCPProject:       firstNonEmpty(os.Getenv("GCP_PROJECT"), os.Getenv("GOOGLE_CLOUD_PROJECT")),
		GCPEmailLocation: getenv("GCP_EMAIL_LOCATION", "global"),
	}
	if v := os.Getenv("SESSION_COOKIE_SECURE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("SESSION_COOKIE_SECURE: %w", err)
		}
		c.CookieSecure = b
	}
	if v := os.Getenv("DEV_LOGIN_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("DEV_LOGIN_ENABLED: %w", err)
		}
		c.DevLoginEnabled = b
	}

	required := map[string]string{
		"APP_BASE_URL":   c.AppBaseURL,
		"WEB_BASE_URL":   c.WebBaseURL,
		"DATABASE_URL":   c.DatabaseURL,
		"SESSION_SECRET": c.SessionSecret,
	}
	// Google credentials are only required when dev-login is off.
	if !c.DevLoginEnabled {
		required["GOOGLE_CLIENT_ID"] = c.GoogleClientID
		required["GOOGLE_CLIENT_SECRET"] = c.GoogleSecret
	}
	for k, v := range required {
		if v == "" {
			return nil, fmt.Errorf("%s is required", k)
		}
	}
	if len(c.SessionSecret) < 32 {
		return nil, fmt.Errorf("SESSION_SECRET must be at least 32 characters")
	}

	c.AllowedHosts = parseAllowedHosts(os.Getenv("ALLOWED_HOSTS"), c.AppBaseURL)
	c.PlatformAdminSeedEmails = parsePlatformAdminSeed(os.Getenv("PLATFORM_ADMIN_SEED_EMAILS"))
	return c, nil
}

func parsePlatformAdminSeed(env string) []string {
	if env == "" {
		out := make([]string, len(defaultPlatformAdminSeedEmails))
		for i, e := range defaultPlatformAdminSeedEmails {
			out[i] = strings.ToLower(strings.TrimSpace(e))
		}
		return out
	}
	out := []string{}
	for _, p := range strings.Split(env, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// IsPlatformAdminSeed reports whether email is on the seed list.
func (c *Config) IsPlatformAdminSeed(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	for _, s := range c.PlatformAdminSeedEmails {
		if s == e {
			return true
		}
	}
	return false
}

// GoogleConfigured reports whether Google OAuth credentials are present.
func (c *Config) GoogleConfigured() bool {
	return c.GoogleClientID != "" && c.GoogleSecret != ""
}

// IsAllowedHost reports whether h matches one of the configured AllowedHosts.
// Host port is compared as-is; "example.com" and "example.com:443" differ.
func (c *Config) IsAllowedHost(h string) bool {
	if h == "" {
		return false
	}
	for _, allowed := range c.AllowedHosts {
		if allowed == h {
			return true
		}
	}
	return false
}

// parseAllowedHosts returns a normalized list of allowed hosts. If env is empty,
// it falls back to the host parsed from appBaseURL.
func parseAllowedHosts(env, appBaseURL string) []string {
	out := []string{}
	for _, p := range strings.Split(env, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		if h := parseHost(appBaseURL); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func parseHost(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
