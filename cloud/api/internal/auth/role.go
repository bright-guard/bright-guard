package auth

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

// RoleLookup is the dependency RequireOrgRole needs: given user+org, return
// the role or an error. Satisfied by store.Orgs.RoleFor.
type RoleLookup interface {
	RoleFor(ctx context.Context, userID, orgID uuid.UUID) (models.OrgRole, error)
}

type roleCtxKey int

const ctxKeyOrgRole roleCtxKey = 0

// RequireOrgRole returns a middleware that allows the request through only if
// the calling user's role in the resolved org is in `allowed`. `orgIDFromReq`
// extracts the org id from the request (typically a URL param). Role is also
// stashed in the request context so handlers can branch without re-querying.
func RequireOrgRole(lookup RoleLookup, orgIDFromReq func(r *http.Request) (uuid.UUID, bool), allowed ...models.OrgRole) func(http.Handler) http.Handler {
	allowSet := make(map[models.OrgRole]struct{}, len(allowed))
	for _, a := range allowed {
		allowSet[a] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			orgID, ok := orgIDFromReq(r)
			if !ok {
				http.Error(w, "invalid orgId", http.StatusBadRequest)
				return
			}
			role, err := lookup.RoleFor(r.Context(), user.ID, orgID)
			if err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if _, ok := allowSet[role]; !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyOrgRole, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OrgRoleFromContext returns the role injected by RequireOrgRole, if any.
func OrgRoleFromContext(ctx context.Context) (models.OrgRole, bool) {
	v, ok := ctx.Value(ctxKeyOrgRole).(models.OrgRole)
	return v, ok
}
