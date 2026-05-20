package config

import (
	"fmt"
	"os"
	"strconv"
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
}

func FromEnv() (*Config, error) {
	c := &Config{
		Port:           getenv("PORT", "8080"),
		AppBaseURL:     os.Getenv("APP_BASE_URL"),
		WebBaseURL:     os.Getenv("WEB_BASE_URL"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		SessionSecret:  os.Getenv("SESSION_SECRET"),
		GoogleClientID: os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleSecret:   os.Getenv("GOOGLE_CLIENT_SECRET"),
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
	return c, nil
}

// GoogleConfigured reports whether Google OAuth credentials are present.
func (c *Config) GoogleConfigured() bool {
	return c.GoogleClientID != "" && c.GoogleSecret != ""
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
