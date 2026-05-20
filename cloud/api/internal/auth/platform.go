package auth

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// ctxKeyPlatformAdmin marks the request as coming from an active platform
// admin. Set by RequirePlatformAdmin so downstream handlers can short-circuit
// "I am admin" decisions without a second DB hit.
const ctxKeyPlatformAdmin ctxKey = 100

// PlatformAdminChecker is the narrow contract RequirePlatformAdmin needs.
// *store.Platform satisfies it via IsActiveAdmin; tests fake it in-process.
type PlatformAdminChecker interface {
	IsActiveAdmin(ctx context.Context, userID uuid.UUID) (bool, error)
}

// RequirePlatformAdmin gates a route group on the calling user being an active
// platform_admins row. Must be chained after RequireUser so the user is on the
// request context.
func RequirePlatformAdmin(p PlatformAdminChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			ok, err := p.IsActiveAdmin(r.Context(), user.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "platform admin check failed")
				return
			}
			if !ok {
				writeError(w, http.StatusForbidden, "forbidden", "forbidden")
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyPlatformAdmin, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IsPlatformAdmin returns true when the request context was tagged by
// RequirePlatformAdmin. Cheap accessor for handlers.
func IsPlatformAdmin(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeyPlatformAdmin).(bool)
	return v
}
