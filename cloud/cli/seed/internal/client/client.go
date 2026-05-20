package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin wrapper around the Bright Guard control-plane API.
type Client struct {
	BaseURL string
	Token   string // CLI bearer; empty means cookie auth
	Cookie  string // bg_session value; empty means bearer
	HTTP    *http.Client
}

// New constructs a client. Exactly one of token/cookie should be set.
func New(baseURL, token, cookie string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("control-plane URL required")
	}
	if token == "" && cookie == "" {
		return nil, fmt.Errorf("either --token or --cookie required")
	}
	if token != "" && cookie != "" {
		return nil, fmt.Errorf("--token and --cookie are mutually exclusive")
	}
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		Cookie:  cookie,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// do issues a request with user auth. If body is non-nil it's JSON-encoded.
// out, if non-nil, is JSON-decoded from the response body.
func (c *Client) do(method, path string, body, out any) error {
	return c.doAuth(method, path, c.userAuth, body, out)
}

// doBearer issues a request with an explicit gateway bearer credential.
func (c *Client) doBearer(method, path, bearer string, body, out any) error {
	apply := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.doAuth(method, path, apply, body, out)
}

// doUnauth issues a request with no auth headers (e.g. gateway register).
func (c *Client) doUnauth(method, path string, body, out any) error {
	return c.doAuth(method, path, func(*http.Request) {}, body, out)
}

func (c *Client) userAuth(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
		return
	}
	req.AddCookie(&http.Cookie{Name: "bg_session", Value: c.Cookie})
}

func (c *Client) doAuth(method, path string, apply func(*http.Request), body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal %s %s: %w", method, path, err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	apply(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, truncate(string(respBytes), 400))
	}
	if out == nil || len(respBytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBytes, out); err != nil {
		return fmt.Errorf("decode %s %s: %w (body=%s)", method, path, err, truncate(string(respBytes), 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ── typed wrappers for the endpoints the seeder needs ────────────────────

type Org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Membership struct {
	Org  Org    `json:"org"`
	Role string `json:"role"`
}

func (c *Client) ListOrgs() ([]Membership, error) {
	var ms []Membership
	if err := c.do(http.MethodGet, "/api/orgs", nil, &ms); err != nil {
		return nil, err
	}
	return ms, nil
}

func (c *Client) CreateOrg(name string) (*Org, error) {
	var org Org
	if err := c.do(http.MethodPost, "/api/orgs", map[string]string{"name": name}, &org); err != nil {
		return nil, err
	}
	return &org, nil
}

func (c *Client) SetActiveOrg(orgID string) error {
	return c.do(http.MethodPost, "/api/sessions/active-org", map[string]string{"orgId": orgID}, nil)
}

type CreateGatewayResp struct {
	Gateway struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"gateway"`
	EnrollmentToken string `json:"enrollmentToken"`
	InstallCommand  string `json:"installCommand"`
}

func (c *Client) CreateGateway(orgID, name, description string) (*CreateGatewayResp, error) {
	var out CreateGatewayResp
	body := map[string]string{"name": name, "description": description}
	if err := c.do(http.MethodPost, "/api/orgs/"+orgID+"/gateways", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type GatewaySummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) ListGateways(orgID string) ([]GatewaySummary, error) {
	var gws []GatewaySummary
	if err := c.do(http.MethodGet, "/api/orgs/"+orgID+"/gateways", nil, &gws); err != nil {
		return nil, err
	}
	return gws, nil
}

type RegisterResp struct {
	GatewayID  string `json:"gatewayId"`
	Credential string `json:"credential"`
}

func (c *Client) RegisterGateway(enrollmentToken string) (*RegisterResp, error) {
	var out RegisterResp
	body := map[string]string{"enrollmentToken": enrollmentToken}
	if err := c.doUnauth(http.MethodPost, "/v1/gateway/register", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Capability used in observations payloads (wire shape).
type Capability struct {
	Kind        string         `json:"kind"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema,omitempty"`
}

type ObsServer struct {
	Name         string         `json:"name"`
	Address      string         `json:"address"`
	Transport    string         `json:"transport"`
	Version      string         `json:"version"`
	Metadata     map[string]any `json:"metadata"`
	Capabilities []Capability   `json:"capabilities"`
}

type ObsInvocation struct {
	Server         string         `json:"server"`
	CapabilityKind string         `json:"capabilityKind"`
	CapabilityName string         `json:"capabilityName"`
	Caller         map[string]any `json:"caller"`
	Status         string         `json:"status"`
	LatencyMs      int            `json:"latencyMs"`
	At             time.Time      `json:"at"`
}

type ObservationsBody struct {
	Servers     []ObsServer     `json:"servers"`
	Invocations []ObsInvocation `json:"invocations"`
}

func (c *Client) PostObservations(credential string, body *ObservationsBody) error {
	return c.doBearer(http.MethodPost, "/v1/gateway/observations", credential, body, nil)
}

func (c *Client) Heartbeat(credential string) error {
	return c.doBearer(http.MethodPost, "/v1/gateway/heartbeat", credential, nil, nil)
}
