package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

type fakeRoleLookup struct {
	role models.OrgRole
	err  error
}

func (f fakeRoleLookup) RoleFor(_ context.Context, _, _ uuid.UUID) (models.OrgRole, error) {
	return f.role, f.err
}

func TestRequireOrgRole(t *testing.T) {
	user := &models.User{ID: uuid.New()}
	orgID := uuid.New()
	getOrg := func(_ *http.Request) (uuid.UUID, bool) { return orgID, true }

	t.Run("allows matching role", func(t *testing.T) {
		mw := RequireOrgRole(fakeRoleLookup{role: models.RoleOwner}, getOrg, models.RoleOwner, models.RoleAdmin)
		called := false
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			if got, ok := OrgRoleFromContext(r.Context()); !ok || got != models.RoleOwner {
				t.Errorf("role not in ctx: %v", got)
			}
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest("GET", "/x", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if !called || rr.Code != http.StatusOK {
			t.Fatalf("code=%d called=%v", rr.Code, called)
		}
	})

	t.Run("rejects non-matching role", func(t *testing.T) {
		mw := RequireOrgRole(fakeRoleLookup{role: models.RoleMember}, getOrg, models.RoleOwner, models.RoleAdmin)
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Fatal("handler should not run")
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest("GET", "/x", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("code=%d", rr.Code)
		}
	})

	t.Run("rejects when lookup errors", func(t *testing.T) {
		mw := RequireOrgRole(fakeRoleLookup{err: errors.New("not found")}, getOrg, models.RoleOwner)
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Fatal("handler should not run")
		}))
		req := httptest.NewRequest("GET", "/x", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("code=%d", rr.Code)
		}
	})

	t.Run("401 without user", func(t *testing.T) {
		mw := RequireOrgRole(fakeRoleLookup{}, getOrg, models.RoleOwner)
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Fatal("handler should not run")
		}))
		req := httptest.NewRequest("GET", "/x", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("code=%d", rr.Code)
		}
	})
}
