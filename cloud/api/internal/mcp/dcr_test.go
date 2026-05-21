package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dcrServers wires up three handlers — protected-resource metadata,
// authorization-server metadata, and the registration endpoint — onto a
// single httptest server and returns the URLs the test should probe.
type dcrServers struct {
	mux       *http.ServeMux
	srv       *httptest.Server
	endpoint  string
	registers int
}

func newDCRServers(t *testing.T, withPR, withAS, withRegistration bool) *dcrServers {
	t.Helper()
	d := &dcrServers{mux: http.NewServeMux()}
	d.srv = httptest.NewServer(d.mux)
	t.Cleanup(d.srv.Close)
	d.endpoint = d.srv.URL + "/mcp"

	if withPR {
		d.mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              d.srv.URL + "/",
				"authorization_servers": []string{d.srv.URL},
			})
		})
	}
	if withAS {
		d.mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
			payload := map[string]any{
				"issuer":                 d.srv.URL,
				"authorization_endpoint": d.srv.URL + "/oauth/authorize",
				"token_endpoint":         d.srv.URL + "/oauth/token",
				"scopes_supported":       []string{"read", "write"},
			}
			if withRegistration {
				payload["registration_endpoint"] = d.srv.URL + "/oauth/register"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(payload)
		})
	}
	if withRegistration {
		d.mux.HandleFunc("/oauth/register", func(w http.ResponseWriter, r *http.Request) {
			d.registers++
			body, _ := io.ReadAll(r.Body)
			var req DCRRequest
			if err := json.Unmarshal(body, &req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if req.ApplicationType != "web" {
				t.Errorf("application_type = %q", req.ApplicationType)
			}
			if len(req.RedirectURIs) == 0 || req.RedirectURIs[0] == "" {
				t.Errorf("redirect_uris must not be empty: %+v", req.RedirectURIs)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(DCRResponse{
				ClientID:                "generated-client",
				ClientSecret:            "generated-secret",
				RegistrationAccessToken: "reg-access-token",
				RegistrationClientURI:   d.srv.URL + "/oauth/register/generated-client",
				TokenEndpointAuthMethod: "client_secret_post",
			})
		})
	}
	return d
}

func TestProbe_HappyPath(t *testing.T) {
	d := newDCRServers(t, true, true, true)

	meta, err := Probe(context.Background(), d.endpoint)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if meta.AuthorizeURL == "" || meta.TokenURL == "" || meta.RegistrationURL == "" {
		t.Fatalf("incomplete metadata: %+v", meta)
	}
	if !strings.HasSuffix(meta.RegistrationURL, "/oauth/register") {
		t.Errorf("RegistrationURL = %q", meta.RegistrationURL)
	}
	if len(meta.ScopesSupported) != 2 {
		t.Errorf("ScopesSupported = %v", meta.ScopesSupported)
	}
}

func TestProbe_FallbackToDirectASMetadata(t *testing.T) {
	// No protected-resource doc — common pattern for MCP servers that *are*
	// their own authorization server.
	d := newDCRServers(t, false, true, true)

	meta, err := Probe(context.Background(), d.endpoint)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if meta.RegistrationURL == "" {
		t.Fatalf("expected registration URL, got %+v", meta)
	}
}

func TestProbe_NoASMetadata_ReturnsUnsupported(t *testing.T) {
	d := newDCRServers(t, false, false, false)

	_, err := Probe(context.Background(), d.endpoint)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrDCRUnsupported) {
		t.Fatalf("err = %v, want ErrDCRUnsupported", err)
	}
}

func TestProbe_ASMetadataWithoutRegistrationEndpoint(t *testing.T) {
	d := newDCRServers(t, true, true, false)

	_, err := Probe(context.Background(), d.endpoint)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrDCRUnsupported) {
		t.Fatalf("err = %v, want ErrDCRUnsupported", err)
	}
}

func TestProbe_RejectsCrossOriginRegistrationEndpoint(t *testing.T) {
	// Attacker-controlled metadata pointing at a different host.
	d := http.NewServeMux()
	d.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 "https://idp.example.com",
			"authorization_endpoint": "https://idp.example.com/authorize",
			"token_endpoint":         "https://idp.example.com/token",
			"registration_endpoint":  "https://evil.example.com/register",
		})
	})
	srv := httptest.NewServer(d)
	defer srv.Close()

	_, err := Probe(context.Background(), srv.URL+"/mcp")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrDCRUnsupported) {
		t.Fatalf("err = %v, want ErrDCRUnsupported", err)
	}
}

func TestRegisterClient_HappyPath(t *testing.T) {
	d := newDCRServers(t, true, true, true)
	meta, err := Probe(context.Background(), d.endpoint)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	resp, err := RegisterClient(context.Background(), meta.RegistrationURL, DCRRequest{
		ApplicationType:         "web",
		RedirectURIs:            []string{"https://app.example.dev/oauth/connect/callback"},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_post",
		ClientName:              "Bright Guard - Acme",
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if resp.ClientID != "generated-client" || resp.ClientSecret != "generated-secret" {
		t.Fatalf("bad creds: %+v", resp)
	}
	if resp.RegistrationAccessToken == "" || resp.RegistrationClientURI == "" {
		t.Errorf("missing RFC 7592 fields: %+v", resp)
	}
	if d.registers != 1 {
		t.Errorf("registers = %d", d.registers)
	}
}

func TestRegisterClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_redirect_uri"}`))
	}))
	defer srv.Close()
	_, err := RegisterClient(context.Background(), srv.URL, DCRRequest{
		ApplicationType: "web",
		RedirectURIs:    []string{"https://x.example.com/cb"},
		ClientName:      "x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSameRegistrableDomain(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		// Atlassian-style: MCP server at one subdomain, OAuth backend at another,
		// both rooted at atlassian.com. This is the load-bearing case.
		{
			"atlassian cross-subdomain",
			"https://mcp.atlassian.com/.well-known/oauth-authorization-server",
			"https://cf.mcp.atlassian.com/v1/register",
			true,
		},
		{
			"same host",
			"https://idp.example.com/.well-known/oauth-authorization-server",
			"https://idp.example.com/register",
			true,
		},
		// Attacker case — both have eTLD+1 but they differ.
		{
			"different registrable domain",
			"https://idp.example.com/.well-known/oauth-authorization-server",
			"https://attacker.example.org/register",
			false,
		},
		// IP literals fall back to exact-host equality.
		{
			"ip literal same host",
			"http://127.0.0.1:8080/.well-known/oauth-authorization-server",
			"http://127.0.0.1:8080/register",
			true,
		},
		{
			"ip literal different host",
			"http://127.0.0.1:8080/.well-known/oauth-authorization-server",
			"http://evil.example.com/register",
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sameRegistrableDomain(tc.a, tc.b)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Fatalf("sameRegistrableDomain(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
