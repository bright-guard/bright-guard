package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseActivityFilter_Defaults(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET", "/api/orgs/x/activity", nil)
	f, err := s.parseActivityFilter(r)
	if err != nil {
		t.Fatal(err)
	}
	if f.Limit != defaultActivityLimit {
		t.Errorf("default limit: %d", f.Limit)
	}
	if f.From.IsZero() || f.To.IsZero() {
		t.Errorf("from/to should default to non-zero")
	}
	if d := f.To.Sub(f.From); d < 23*time.Hour || d > 25*time.Hour {
		t.Errorf("default window: %v", d)
	}
	if len(f.CapabilityKinds) != 0 || len(f.Statuses) != 0 {
		t.Errorf("filters should be empty by default")
	}
}

func TestParseActivityFilter_Repeatable(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET",
		"/api/orgs/x/activity?capabilityKind=tool&capabilityKind=Resource&status=ok&status=error&q=foo&limit=200",
		nil)
	f, err := s.parseActivityFilter(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.CapabilityKinds; len(got) != 2 || got[0] != "tool" || got[1] != "resource" {
		t.Errorf("kinds: %#v", got)
	}
	if got := f.Statuses; len(got) != 2 || got[0] != "ok" || got[1] != "error" {
		t.Errorf("statuses: %#v", got)
	}
	if f.Q != "foo" {
		t.Errorf("q: %q", f.Q)
	}
	if f.Limit != 200 {
		t.Errorf("limit: %d", f.Limit)
	}
}

func TestParseActivityFilter_LimitClamp(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET", "/api/orgs/x/activity?limit=9999", nil)
	f, err := s.parseActivityFilter(r)
	if err != nil {
		t.Fatal(err)
	}
	if f.Limit != maxActivityLimit {
		t.Errorf("clamp: %d", f.Limit)
	}
}

func TestParseActivityFilter_InvalidUUID(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET", "/api/orgs/x/activity?mcpServerId=not-a-uuid", nil)
	if _, err := s.parseActivityFilter(r); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseActivityFilter_InvalidTime(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET", "/api/orgs/x/activity?from=not-a-time", nil)
	if _, err := s.parseActivityFilter(r); err == nil {
		t.Fatal("expected error")
	}
}
