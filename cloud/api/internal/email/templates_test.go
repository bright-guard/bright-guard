package email

import (
	"strings"
	"testing"
	"time"
)

func TestRenderInvitation(t *testing.T) {
	v := InvitationVars{
		ID:           "abc-123",
		OrgName:      "Acme Corp",
		InviterName:  "Alice",
		InviterEmail: "alice@acme.example",
		AcceptURL:    "https://app.example/app/invitations/abc-123",
		ExpiresAt:    time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC),
	}
	subject, html, text, err := RenderInvitation(v)
	if err != nil {
		t.Fatalf("RenderInvitation: %v", err)
	}
	if subject != `You've been invited to Acme Corp on Bright Guard` {
		t.Errorf("subject: %q", subject)
	}
	for _, want := range []string{"Alice", "alice@acme.example", "Acme Corp", v.AcceptURL} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q: %q", want, text)
		}
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q: %q", want, html)
		}
	}
	if !strings.Contains(html, "<a href=") {
		t.Errorf("html should contain anchor: %q", html)
	}
}
