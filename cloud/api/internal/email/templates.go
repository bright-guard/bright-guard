package email

import (
	"bytes"
	"fmt"
	htmltmpl "html/template"
	texttmpl "text/template"
	"time"
)

// InvitationVars feeds the invitation templates.
type InvitationVars struct {
	ID            string
	OrgName       string
	InviterName   string
	InviterEmail  string
	AcceptURL     string
	ExpiresAt     time.Time
}

const inviteSubjectTmpl = `You've been invited to {{.OrgName}} on Bright Guard`

// Stdlib templates keep us off any extra dependency. The HTML version mirrors
// the text version so even strict mail clients render the same content.
const inviteTextTmpl = `{{.InviterName}} ({{.InviterEmail}}) invited you to join the Bright Guard org "{{.OrgName}}".

Open {{.AcceptURL}} to accept or decline.

This invitation expires {{.ExpiresAt.Format "Mon, 02 Jan 2006 15:04 MST"}}.
`

const inviteHTMLTmpl = `<p>{{.InviterName}} ({{.InviterEmail}}) invited you to join the Bright Guard org "{{.OrgName}}".</p>
<p>Open <a href="{{.AcceptURL}}">{{.AcceptURL}}</a> to accept or decline.</p>
<p>This invitation expires {{.ExpiresAt.Format "Mon, 02 Jan 2006 15:04 MST"}}.</p>
`

// RenderInvitation returns subject, html, text for the given vars.
func RenderInvitation(v InvitationVars) (subject, html, text string, err error) {
	sub, err := execText("subject", inviteSubjectTmpl, v)
	if err != nil {
		return "", "", "", fmt.Errorf("subject: %w", err)
	}
	t, err := execText("text", inviteTextTmpl, v)
	if err != nil {
		return "", "", "", fmt.Errorf("text: %w", err)
	}
	h, err := execHTML("html", inviteHTMLTmpl, v)
	if err != nil {
		return "", "", "", fmt.Errorf("html: %w", err)
	}
	return sub, h, t, nil
}

func execText(name, src string, v any) (string, error) {
	tmpl, err := texttmpl.New(name).Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func execHTML(name, src string, v any) (string, error) {
	tmpl, err := htmltmpl.New(name).Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		return "", err
	}
	return buf.String(), nil
}
