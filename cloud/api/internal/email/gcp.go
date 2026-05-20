package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GCPSender posts to the GCP Cloud Email REST API. Auth uses ADC, so any
// environment that runs the regular gcloud client (Cloud Run service account,
// `gcloud auth application-default login` locally) works without config.
//
// Reference: https://cloud.google.com/email-api/docs
//
// TODO: the Cloud Email API surface is still in beta and the exact request
// schema/response codes may shift. This implementation issues the documented
// v1beta `POST .../emails` and surfaces non-2xx responses as errors. If the
// API moves to GA with a breaking change, update the URL and body shape here.
type GCPSender struct {
	Project  string // GCP project id; required.
	Location string // e.g. "global" or a region; "global" is the documented default.
	From     string // RFC 5322 address used in `from`.
	Client   *http.Client
}

// NewGCPSender builds a GCPSender authenticated with ADC. Returns an error if
// no credentials are available — callers should fall back to the stub.
func NewGCPSender(ctx context.Context, project, location, from string) (*GCPSender, error) {
	if project == "" {
		return nil, fmt.Errorf("email: GCP project is required")
	}
	if location == "" {
		location = "global"
	}
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("email: ADC: %w", err)
	}
	// oauth2.NewClient wraps the default http.Transport with a token-injecting
	// RoundTripper. Tokens auto-refresh.
	hc := oauth2.NewClient(ctx, creds.TokenSource)
	hc.Timeout = 15 * time.Second
	return &GCPSender{
		Project:  project,
		Location: location,
		From:     from,
		Client:   hc,
	}, nil
}

// gcpEmailReq is the documented v1beta body. If GA introduces breaking
// changes the fields here are the first thing to update.
type gcpEmailReq struct {
	From    gcpAddr   `json:"from"`
	To      []gcpAddr `json:"to"`
	Subject string    `json:"subject"`
	Body    gcpBody   `json:"body"`
}

type gcpAddr struct {
	Address string `json:"address"`
}

type gcpBody struct {
	Text string `json:"text,omitempty"`
	HTML string `json:"html,omitempty"`
}

func (g *GCPSender) Send(ctx context.Context, to, subject, html, text string) error {
	if g == nil {
		return fmt.Errorf("email: nil sender")
	}
	body := gcpEmailReq{
		From:    gcpAddr{Address: g.From},
		To:      []gcpAddr{{Address: to}},
		Subject: subject,
		Body:    gcpBody{Text: text, HTML: html},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://emails.googleapis.com/v1beta/projects/%s/locations/%s/emails",
		g.Project, g.Location)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.Client.Do(req)
	if err != nil {
		return fmt.Errorf("email: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// Body may contain the rejected address; truncate so we don't dump it
		// into long-lived log retention by accident.
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("email: gcp returned %d: %s", resp.StatusCode, string(preview))
	}
	return nil
}
