package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestPlatformIsSeed(t *testing.T) {
	p := &Platform{SeedEmails: []string{"daniel@danielgarcia.info", "dgarcia@infoblox.com"}}
	cases := map[string]bool{
		"daniel@danielgarcia.info": true,
		"DGARCIA@infoblox.com":     true,
		"  dgarcia@infoblox.com  ": true,
		"someoneelse@example.com":  false,
		"":                         false,
	}
	for in, want := range cases {
		if got := p.isSeed(in); got != want {
			t.Errorf("isSeed(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPlatformIsSeedEmptyList(t *testing.T) {
	p := &Platform{}
	if p.isSeed("daniel@danielgarcia.info") {
		t.Fatal("empty seed list should match nothing")
	}
}

// TestMaybeBootstrap_NonSeedShortCircuit verifies the no-pool path: when the
// email isn't on the seed list we never touch the DB (Pool == nil here would
// panic if we did). Asserts MaybeBootstrap is safe to call on every upsert.
func TestMaybeBootstrap_NonSeedShortCircuit(t *testing.T) {
	p := &Platform{SeedEmails: []string{"admin@example.com"}}
	if err := p.MaybeBootstrap(context.Background(), uuid.New(), "tenant@example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
