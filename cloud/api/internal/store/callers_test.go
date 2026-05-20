package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCanonicalizeCaller_EmptyVariants(t *testing.T) {
	cases := []json.RawMessage{
		nil,
		json.RawMessage(``),
		json.RawMessage(`null`),
		json.RawMessage(`{}`),
		json.RawMessage(`  `),
	}
	for _, c := range cases {
		got := CanonicalizeCaller(c)
		if got != anonymousMarker {
			t.Errorf("CanonicalizeCaller(%q) = %q, want %q", string(c), got, anonymousMarker)
		}
	}
}

func TestCanonicalizeCaller_KeyOrderStable(t *testing.T) {
	a := json.RawMessage(`{"agent":"copilot","userEmail":"alice@acme.example"}`)
	b := json.RawMessage(`{"userEmail":"alice@acme.example","agent":"copilot"}`)
	if SignatureFor(a) != SignatureFor(b) {
		t.Fatalf("signatures should match across key orders")
	}
}

func TestCanonicalizeCaller_NestedObjects(t *testing.T) {
	a := json.RawMessage(`{"agent":"x","meta":{"a":1,"b":2}}`)
	b := json.RawMessage(`{"meta":{"b":2,"a":1},"agent":"x"}`)
	if SignatureFor(a) != SignatureFor(b) {
		t.Fatalf("nested key order should not change signature")
	}
}

func TestCanonicalizeCaller_Arrays(t *testing.T) {
	// Arrays preserve order (semantically meaningful) — so different orders
	// MUST produce different signatures.
	a := json.RawMessage(`{"scopes":["read","write"]}`)
	b := json.RawMessage(`{"scopes":["write","read"]}`)
	if SignatureFor(a) == SignatureFor(b) {
		t.Fatalf("array order should be preserved in signature")
	}
}

func TestSignatureFor_IsHex(t *testing.T) {
	sig := SignatureFor(json.RawMessage(`{"agent":"x"}`))
	if len(sig) != 64 {
		t.Fatalf("expected 64-char hex digest, got %d (%q)", len(sig), sig)
	}
	for _, c := range sig {
		if !strings.Contains("0123456789abcdef", string(c)) {
			t.Fatalf("non-hex char in signature: %q", sig)
		}
	}
}

func TestLabelFor_PrefersAgent(t *testing.T) {
	l := LabelFor(json.RawMessage(`{"agent":"copilot","user":"alice"}`), "abc123def")
	if l != "copilot" {
		t.Errorf("label = %q, want copilot", l)
	}
}

func TestLabelFor_FallsBackToUser(t *testing.T) {
	l := LabelFor(json.RawMessage(`{"user":"alice"}`), "abc123def")
	if l != "alice" {
		t.Errorf("label = %q, want alice", l)
	}
}

func TestLabelFor_FallsBackToUserEmail(t *testing.T) {
	l := LabelFor(json.RawMessage(`{"userEmail":"alice@acme.example"}`), "abc123def")
	if l != "alice@acme.example" {
		t.Errorf("label = %q", l)
	}
}

func TestLabelFor_HashFallback(t *testing.T) {
	l := LabelFor(json.RawMessage(`{"unknown":"x"}`), "deadbeefcafe")
	if l != "caller_deadbe" {
		t.Errorf("label = %q, want caller_deadbe", l)
	}
}

func TestLabelFor_AnonymousFallback(t *testing.T) {
	l := LabelFor(nil, "")
	if l != "caller_anon" {
		t.Errorf("label = %q", l)
	}
}

func TestBuildCallerWhere(t *testing.T) {
	// Reuse the activity test pattern with a fresh uuid.
	where, args := buildCallerWhere(uuid.New(), CallerFilter{FlaggedOnly: true, Q: "Foo"})
	if !strings.Contains(where, "flagged_new = true") {
		t.Errorf("missing flagged clause: %s", where)
	}
	if !strings.Contains(where, "lower(label) like") {
		t.Errorf("missing q clause: %s", where)
	}
	if len(args) != 2 { // org + q
		t.Errorf("args = %#v", args)
	}
	if got := args[1].(string); got != "%foo%" {
		t.Errorf("q arg = %q", got)
	}
}
