package mcp

import (
	"encoding/base64"
	"errors"
	"net/http"
)

// AuthSecret is the in-memory form of a connection's credential. Exactly one
// field is populated per `Method`; other fields must remain zero.
type AuthSecret struct {
	Method      string `json:"method"`
	HeaderName  string `json:"headerName,omitempty"`
	HeaderValue string `json:"headerValue,omitempty"`
	BearerToken string `json:"bearerToken,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	// TODO(#8): oauth2_authcode adds access/refresh tokens + expiry here.
}

// ErrAuthMethodUnsupported is returned when a transport is asked to use an
// auth method we have not implemented yet (e.g. oauth2_authcode).
var ErrAuthMethodUnsupported = errors.New("auth method not implemented")

// AuthRoundTripper wraps base with per-request credential injection.
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
	// Clone so we never mutate the caller's request headers.
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
		// TODO(#8): inject the access token from the stored OAuth state and
		// refresh via refresh_token when expired. For now treat as unsupported
		// at the call site (handled in Client.do).
	}
	return a.base.RoundTrip(r)
}
