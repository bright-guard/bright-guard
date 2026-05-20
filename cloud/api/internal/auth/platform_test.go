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

type fakeAdminChecker struct {
	active bool
	err    error
	calls  int
}

func (f *fakeAdminChecker) IsActiveAdmin(_ context.Context, _ uuid.UUID) (bool, error) {
	f.calls++
	return f.active, f.err
}

func TestRequirePlatformAdmin_NoUser401(t *testing.T) {
	fc := &fakeAdminChecker{active: true}
	h := RequirePlatformAdmin(fc)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not run")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/platform/overview", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
	if fc.calls != 0 {
		t.Errorf("DB should not be called when there is no user: %d calls", fc.calls)
	}
}

func TestRequirePlatformAdmin_NotAdmin403(t *testing.T) {
	fc := &fakeAdminChecker{active: false}
	h := RequirePlatformAdmin(fc)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not run")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/platform/overview", nil)
	user := &models.User{ID: uuid.New(), Email: "alice@example.com"}
	r = r.WithContext(context.WithValue(r.Context(), ctxKeyUser, user))
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestRequirePlatformAdmin_AdminPasses(t *testing.T) {
	fc := &fakeAdminChecker{active: true}
	inner := false
	h := RequirePlatformAdmin(fc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner = true
		// Context must carry the platform-admin flag for downstream handlers.
		if !IsPlatformAdmin(r.Context()) {
			t.Error("expected IsPlatformAdmin to be true on the inner ctx")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/platform/overview", nil)
	user := &models.User{ID: uuid.New(), Email: "admin@example.com"}
	r = r.WithContext(context.WithValue(r.Context(), ctxKeyUser, user))
	h.ServeHTTP(w, r)
	if !inner {
		t.Fatal("inner handler never ran")
	}
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestRequirePlatformAdmin_DBError500(t *testing.T) {
	fc := &fakeAdminChecker{err: errors.New("boom")}
	h := RequirePlatformAdmin(fc)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not run")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/platform/overview", nil)
	user := &models.User{ID: uuid.New()}
	r = r.WithContext(context.WithValue(r.Context(), ctxKeyUser, user))
	h.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", w.Code)
	}
}
