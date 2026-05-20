package store

import (
	"reflect"
	"strings"
	"testing"

	"github.com/bright-guard/bright-guard/cloud/api/internal/mcp"
)

func newTestConnections(t *testing.T) *Connections {
	t.Helper()
	aead, err := NewAEAD([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAEAD: %v", err)
	}
	return &Connections{AEAD: aead}
}

func TestAEADRoundtrip(t *testing.T) {
	c := newTestConnections(t)
	want := mcp.AuthSecret{
		Method:      "api_key_header",
		HeaderName:  "X-Api-Key",
		HeaderValue: "supersecret",
	}
	blob, err := c.Encrypt(want)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("Encrypt returned empty blob")
	}
	if strings.Contains(string(blob), "supersecret") {
		t.Fatal("ciphertext leaks plaintext")
	}
	got, err := c.Decrypt(blob)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Decrypt = %+v, want %+v", got, want)
	}
}

func TestAEADDecryptEmpty(t *testing.T) {
	c := newTestConnections(t)
	got, err := c.Decrypt(nil)
	if err != nil {
		t.Fatalf("Decrypt(nil): %v", err)
	}
	if !reflect.DeepEqual(got, mcp.AuthSecret{}) {
		t.Fatalf("Decrypt(nil) = %+v, want zero", got)
	}
}

func TestAEADTampering(t *testing.T) {
	c := newTestConnections(t)
	blob, err := c.Encrypt(mcp.AuthSecret{Method: "bearer", BearerToken: "abc"})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	blob[len(blob)-1] ^= 0xff
	if _, err := c.Decrypt(blob); err == nil {
		t.Fatal("expected decrypt to fail on tampered ciphertext")
	}
}

func TestAEADKeyDerivedFromSecret(t *testing.T) {
	// A different secret must produce a different key (so two control planes
	// with separate SESSION_SECRETs can't read each other's auth_state).
	a, err := NewAEAD([]byte("secret-one-secret-one-secret-one"))
	if err != nil {
		t.Fatalf("NewAEAD a: %v", err)
	}
	b, err := NewAEAD([]byte("secret-two-secret-two-secret-two"))
	if err != nil {
		t.Fatalf("NewAEAD b: %v", err)
	}
	ca := &Connections{AEAD: a}
	cb := &Connections{AEAD: b}
	blob, err := ca.Encrypt(mcp.AuthSecret{Method: "bearer", BearerToken: "z"})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := cb.Decrypt(blob); err == nil {
		t.Fatal("decrypt with different secret unexpectedly succeeded")
	}
}
