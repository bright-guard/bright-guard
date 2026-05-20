package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckConfirm_Match(t *testing.T) {
	body := `{"confirm":"delete user dgarcia@infoblox.com"}`
	r := httptest.NewRequest("DELETE", "/x", strings.NewReader(body))
	w := httptest.NewRecorder()
	if !checkConfirm(w, r, "delete user dgarcia@infoblox.com") {
		t.Fatalf("expected match; status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCheckConfirm_CaseInsensitive(t *testing.T) {
	body := `{"confirm":"  Delete User dgarcia@infoblox.com  "}`
	r := httptest.NewRequest("DELETE", "/x", strings.NewReader(body))
	w := httptest.NewRecorder()
	if !checkConfirm(w, r, "delete user dgarcia@infoblox.com") {
		t.Fatalf("trim+lowercase should match; status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCheckConfirm_Mismatch(t *testing.T) {
	body := `{"confirm":"delete user wrong@example.com"}`
	r := httptest.NewRequest("DELETE", "/x", strings.NewReader(body))
	w := httptest.NewRecorder()
	if checkConfirm(w, r, "delete user dgarcia@infoblox.com") {
		t.Fatal("mismatch should return false")
	}
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "delete user dgarcia@infoblox.com") {
		t.Errorf("error body should echo the expected phrase: %s", w.Body.String())
	}
}

func TestCheckConfirm_BadJSON(t *testing.T) {
	r := httptest.NewRequest("DELETE", "/x", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	if checkConfirm(w, r, "delete user x") {
		t.Fatal("bad json should return false")
	}
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestNullableString(t *testing.T) {
	if nullableString("") != nil {
		t.Error("empty should be nil")
	}
	if v := nullableString("abc"); v != "abc" {
		t.Errorf("non-empty should be string: %v", v)
	}
}

func TestAuditActionNames(t *testing.T) {
	// Every state-changing endpoint must emit a unique, namespaced action
	// string. The catalog is small enough to enumerate; this guards against
	// accidental rename / duplication when adding new endpoints.
	all := []string{
		actionUserSuspend, actionUserUnsuspend, actionUserDelete,
		actionUserPromote, actionUserDemote,
		actionOrgSuspend, actionOrgDelete,
	}
	seen := map[string]bool{}
	for _, a := range all {
		if a == "" {
			t.Error("empty action constant")
		}
		if !strings.Contains(a, ".") {
			t.Errorf("action %q must be namespaced (kind.verb)", a)
		}
		if seen[a] {
			t.Errorf("duplicate action constant %q", a)
		}
		seen[a] = true
	}
}

func TestCanDemoteSelf(t *testing.T) {
	if canDemoteSelf(1) {
		t.Error("with one active admin, self-demote must be blocked")
	}
	if canDemoteSelf(0) {
		t.Error("zero is also blocked (shouldn't happen, but guard against)")
	}
	if !canDemoteSelf(2) {
		t.Error("two active admins → safe to demote self")
	}
}
