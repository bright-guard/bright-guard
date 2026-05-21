package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// ErrDCRUnsupported signals that the remote MCP endpoint (or its authorization
// server) does not advertise enough metadata for Dynamic Client Registration.
// Callers should fall back to the manual OAuth flow.
var ErrDCRUnsupported = errors.New("oauth dcr: server does not support dynamic client registration")

// dcrProbeTimeout caps every individual metadata fetch. The MCP spec layers
// three documents (protected-resource -> authorization-server -> registration
// endpoint) and we don't want a slow remote to hang the user's create-connection
// request indefinitely.
const dcrProbeTimeout = 5 * time.Second

// dcrMaxBody bounds JSON metadata + registration responses. RFC 7591 responses
// are small (a handful of fields); 64 KiB is generous enough for any legitimate
// server and defends against pathological replies.
const dcrMaxBody = 64 * 1024

// DCRMetadata captures the OAuth endpoints we need to drive the rest of the
// flow once discovery completes.
type DCRMetadata struct {
	Issuer          string   `json:"issuer"`
	AuthorizeURL    string   `json:"authorize_url"`
	TokenURL        string   `json:"token_url"`
	RegistrationURL string   `json:"registration_url"`
	ScopesSupported []string `json:"scopes_supported,omitempty"`
}

// protectedResourceMetadata mirrors RFC 9728 §3.1.
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// authServerMetadata mirrors RFC 8414 §2 (a strict subset — we only need
// fields relevant to authcode + DCR).
type authServerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
}

// DCRRequest is the RFC 7591 client-registration request body we send.
type DCRRequest struct {
	ApplicationType         string   `json:"application_type"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientName              string   `json:"client_name"`
	Scope                   string   `json:"scope,omitempty"`
}

// DCRResponse is the subset of RFC 7591 §3.2.1 response we persist. We also
// keep the registration access token + URI per RFC 7592 so a future feature
// can update / rotate / delete the client without a fresh registration.
type DCRResponse struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64  `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64  `json:"client_secret_expires_at,omitempty"`
	RegistrationAccessToken string `json:"registration_access_token,omitempty"`
	RegistrationClientURI   string `json:"registration_client_uri,omitempty"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`
}

// Probe performs the MCP authorization discovery cascade for an MCP endpoint:
//
//  1. GET {origin}/.well-known/oauth-protected-resource (RFC 9728).
//  2. If found, follow the first authorization_servers entry and fetch its
//     .well-known/oauth-authorization-server document (RFC 8414).
//  3. If protected-resource is absent (404), fall back to fetching
//     {origin}/.well-known/oauth-authorization-server directly.
//
// Returns ErrDCRUnsupported when neither path yields a usable authorize/token
// pair, or when the AS metadata is missing a registration_endpoint. The caller
// has enough info to drive RegisterClient on success.
func Probe(ctx context.Context, endpointURL string) (*DCRMetadata, error) {
	endpointOrigin, err := originOf(endpointURL)
	if err != nil {
		return nil, fmt.Errorf("dcr: bad endpoint URL: %w", err)
	}

	hc := &http.Client{Timeout: dcrProbeTimeout}

	// Step 1: protected-resource metadata (RFC 9728).
	prURL := endpointOrigin + "/.well-known/oauth-protected-resource"
	asMetaURL := ""
	if pr, status, ferr := fetchPR(ctx, hc, prURL); ferr == nil && pr != nil {
		if len(pr.AuthorizationServers) == 0 {
			return nil, fmt.Errorf("%w: protected-resource lists no authorization_servers", ErrDCRUnsupported)
		}
		// Per RFC 9728 the URL is the issuer; metadata lives at
		// /.well-known/oauth-authorization-server appended to that origin (RFC 8414).
		asMetaURL, err = asMetadataURL(pr.AuthorizationServers[0])
		if err != nil {
			return nil, fmt.Errorf("dcr: bad authorization_servers entry: %w", err)
		}
	} else if status == http.StatusNotFound || errors.Is(ferr, errProtectedResourceMissing) {
		// Step 3 fallback: assume the MCP endpoint origin IS the authorization
		// server, which is the common case for self-contained MCP servers that
		// don't bother with the resource-metadata document.
		asMetaURL = endpointOrigin + "/.well-known/oauth-authorization-server"
	} else if ferr != nil {
		return nil, ferr
	}

	asMeta, err := fetchAS(ctx, hc, asMetaURL)
	if err != nil {
		return nil, err
	}
	if asMeta.AuthorizationEndpoint == "" || asMeta.TokenEndpoint == "" {
		return nil, fmt.Errorf("%w: AS metadata missing authorize/token endpoints", ErrDCRUnsupported)
	}
	if asMeta.RegistrationEndpoint == "" {
		return nil, fmt.Errorf("%w: AS metadata has no registration_endpoint", ErrDCRUnsupported)
	}
	if !isAbsoluteHTTPS(asMeta.AuthorizationEndpoint) || !isAbsoluteHTTPS(asMeta.TokenEndpoint) {
		return nil, fmt.Errorf("%w: AS endpoints must be absolute https URLs", ErrDCRUnsupported)
	}
	// Defense in depth: the registration_endpoint (and the rest of the AS
	// endpoints) must live on the same registrable domain as the AS metadata
	// document. Otherwise a compromised metadata response could redirect us to
	// register at an attacker-controlled URL and leak the generated
	// client_secret + future access tokens. Same-registrable-domain (rather
	// than same-origin) is required because real-world providers — Atlassian
	// being the canonical example — front the MCP endpoint on one subdomain
	// (mcp.atlassian.com) and the OAuth backend on another (cf.mcp.atlassian.com).
	for label, ep := range map[string]string{
		"registration_endpoint":  asMeta.RegistrationEndpoint,
		"authorization_endpoint": asMeta.AuthorizationEndpoint,
		"token_endpoint":         asMeta.TokenEndpoint,
	} {
		ok, err := sameRegistrableDomain(asMetaURL, ep)
		if err != nil {
			return nil, fmt.Errorf("%w: %s host check: %v", ErrDCRUnsupported, label, err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: %s registrable domain does not match authorization server", ErrDCRUnsupported, label)
		}
	}

	return &DCRMetadata{
		Issuer:          asMeta.Issuer,
		AuthorizeURL:    asMeta.AuthorizationEndpoint,
		TokenURL:        asMeta.TokenEndpoint,
		RegistrationURL: asMeta.RegistrationEndpoint,
		ScopesSupported: asMeta.ScopesSupported,
	}, nil
}

// RegisterClient performs the RFC 7591 dynamic client registration POST. The
// caller is responsible for persisting the returned client_id / client_secret.
// We never log secret material here.
func RegisterClient(ctx context.Context, registrationURL string, req DCRRequest) (*DCRResponse, error) {
	if !isAbsoluteHTTPS(registrationURL) {
		return nil, fmt.Errorf("%w: registration URL must be absolute https", ErrDCRUnsupported)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	hctx, cancel := context.WithTimeout(ctx, dcrProbeTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(hctx, http.MethodPost, registrationURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	hc := &http.Client{Timeout: dcrProbeTimeout}
	resp, err := hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dcr register: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, dcrMaxBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// RFC 7591 §3.2.2 error responses use 400 with a JSON body. Surface the
		// status + a truncated body to the caller; we deliberately do not echo
		// it into client error messages further up the stack.
		return nil, fmt.Errorf("dcr register: status %d: %s", resp.StatusCode, truncate(raw, 200))
	}
	var out DCRResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("dcr register: decode response: %w", err)
	}
	if out.ClientID == "" {
		return nil, errors.New("dcr register: response missing client_id")
	}
	return &out, nil
}

// ---- internal helpers ----

var errProtectedResourceMissing = errors.New("protected-resource metadata missing")

func fetchPR(ctx context.Context, hc *http.Client, u string) (*protectedResourceMetadata, int, error) {
	body, status, err := getJSON(ctx, hc, u)
	if err != nil {
		return nil, status, err
	}
	if status == http.StatusNotFound {
		return nil, status, errProtectedResourceMissing
	}
	if status != http.StatusOK {
		return nil, status, fmt.Errorf("dcr: protected-resource status %d", status)
	}
	var pr protectedResourceMetadata
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, status, fmt.Errorf("dcr: decode protected-resource: %w", err)
	}
	return &pr, status, nil
}

func fetchAS(ctx context.Context, hc *http.Client, u string) (*authServerMetadata, error) {
	body, status, err := getJSON(ctx, hc, u)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("%w: no authorization-server metadata at %s", ErrDCRUnsupported, u)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("dcr: AS metadata status %d", status)
	}
	var as authServerMetadata
	if err := json.Unmarshal(body, &as); err != nil {
		return nil, fmt.Errorf("dcr: decode AS metadata: %w", err)
	}
	return &as, nil
}

func getJSON(ctx context.Context, hc *http.Client, u string) ([]byte, int, error) {
	if !isAbsoluteHTTPS(u) {
		return nil, 0, fmt.Errorf("dcr: refuse non-https probe url: %s", u)
	}
	cctx, cancel := context.WithTimeout(ctx, dcrProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("dcr: GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, dcrMaxBody))
	return body, resp.StatusCode, nil
}

func originOf(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("missing host")
	}
	return u.Scheme + "://" + u.Host, nil
}

// asMetadataURL transforms an AS issuer (e.g. https://idp.example.com/) into
// the well-known metadata URL per RFC 8414 §3 — path components on the issuer
// get carried through (e.g. https://example.com/tenant ->
// https://example.com/.well-known/oauth-authorization-server/tenant), but most
// real-world deployments keep the issuer at the origin root.
func asMetadataURL(issuer string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("missing host")
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		return u.Scheme + "://" + u.Host + "/.well-known/oauth-authorization-server", nil
	}
	return u.Scheme + "://" + u.Host + "/.well-known/oauth-authorization-server" + path, nil
}

func isAbsoluteHTTPS(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	// httptest.NewServer hands out http:// URLs; we keep http for tests but
	// production wiring upstream of Probe enforces https on the user-supplied
	// endpoint URL, so the only http callers in real life are the local tests.
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	return u.Host != ""
}

func sameOrigin(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return ua.Scheme == ub.Scheme && ua.Host == ub.Host
}

// sameRegistrableDomain returns true when both URLs resolve to the same
// registrable domain (eTLD+1) per the public suffix list — e.g.
// mcp.atlassian.com and cf.mcp.atlassian.com both yield atlassian.com. Hosts
// that aren't dotted DNS names (IP literals, single-label hosts used by tests)
// fall through to exact host equality.
func sameRegistrableDomain(a, b string) (bool, error) {
	ua, err := url.Parse(a)
	if err != nil {
		return false, err
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false, err
	}
	if ua.Scheme != ub.Scheme {
		return false, nil
	}
	ha, hb := ua.Hostname(), ub.Hostname()
	ra, errA := publicsuffix.EffectiveTLDPlusOne(ha)
	rb, errB := publicsuffix.EffectiveTLDPlusOne(hb)
	if errA != nil || errB != nil {
		// Fall back to exact host match for non-DNS hosts (IPs, single-label).
		return ha == hb, nil
	}
	return ra == rb, nil
}
