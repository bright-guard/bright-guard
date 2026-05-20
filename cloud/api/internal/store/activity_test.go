package store

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	want := time.Date(2026, 5, 19, 12, 34, 56, 0, time.UTC)
	id := uuid.New()
	c := EncodeCursor(want, id)
	if c == "" {
		t.Fatal("encoded cursor should not be empty")
	}
	gotAt, gotID, err := DecodeCursor(c)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !gotAt.Equal(want) {
		t.Fatalf("at: got %v want %v", gotAt, want)
	}
	if gotID != id {
		t.Fatalf("id: got %v want %v", gotID, id)
	}
}

func TestCursorDecodeErrors(t *testing.T) {
	cases := []string{"", "not!base64!!!", "abcdef"}
	for _, c := range cases {
		if _, _, err := DecodeCursor(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestBuildActivityWhere_BaseOrgOnly(t *testing.T) {
	org := uuid.New()
	where, args, err := buildActivityWhere(org, ActivityFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if where != "i.org_id = $1" {
		t.Fatalf("where: %q", where)
	}
	if len(args) != 1 || args[0] != org {
		t.Fatalf("args: %#v", args)
	}
}

func TestBuildActivityWhere_AllFilters(t *testing.T) {
	org := uuid.New()
	sid := uuid.New()
	from := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	f := ActivityFilter{
		From:            from,
		To:              to,
		CapabilityKinds: []string{"tool", "resource"},
		Statuses:        []string{"ok", "error"},
		MCPServerID:     &sid,
		Q:               "search-me",
	}
	where, args, err := buildActivityWhere(org, f)
	if err != nil {
		t.Fatal(err)
	}
	parts := []string{
		"i.org_id = $1",
		"i.at >= $2",
		"i.at < $3",
		"i.capability_kind = any($4::text[])",
		"i.status = any($5::text[])",
		"i.mcp_server_id = $6",
		"(lower(i.capability_name) like $7 or lower(s.name) like $7)",
	}
	for _, p := range parts {
		if !strings.Contains(where, p) {
			t.Errorf("where missing %q\nfull: %s", p, where)
		}
	}
	if got := len(args); got != 7 {
		t.Fatalf("args len = %d, want 7: %#v", got, args)
	}
	if args[0] != org || args[1] != from || args[2] != to {
		t.Errorf("ordered args mismatch: %#v", args[:3])
	}
	if got := args[6].(string); got != "%search-me%" {
		t.Errorf("q arg got %q", got)
	}
}

func TestBuildActivityWhere_TrimsAndSkipsEmptyQ(t *testing.T) {
	org := uuid.New()
	where, args, _ := buildActivityWhere(org, ActivityFilter{Q: "   "})
	if strings.Contains(where, "like") {
		t.Errorf("blank q should not add a like clause: %s", where)
	}
	if len(args) != 1 {
		t.Errorf("args should be just org: %#v", args)
	}
}
