package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

// Audit action constants — kept here so the set is discoverable in one place.
const (
	actionUserSuspend   = "user.suspend"
	actionUserUnsuspend = "user.unsuspend"
	actionUserDelete    = "user.delete"
	actionUserPromote   = "user.promote"
	actionUserDemote    = "user.demote"
	actionOrgSuspend    = "org.suspend"
	actionOrgDelete     = "org.delete"
)

func (s *Server) handlePlatformOverview(w http.ResponseWriter, r *http.Request) {
	o, err := s.Platform.Overview(r.Context())
	if err != nil {
		log.Printf("platform overview: %v", err)
		http.Error(w, "could not load overview", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) handlePlatformActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.ActivityFilter{
		Cursor: q.Get("cursor"),
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = t
		}
	}
	rows, next, err := s.Activity.ActivityCrossOrg(r.Context(), f)
	if err != nil {
		log.Printf("platform activity: %v", err)
		http.Error(w, "could not list activity", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      rows,
		"nextCursor": nullableString(next),
	})
}

func (s *Server) handlePlatformListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	users, next, err := s.Platform.ListUsers(r.Context(), q.Get("q"), limit, q.Get("cursor"))
	if err != nil {
		log.Printf("platform list users: %v", err)
		http.Error(w, "could not list users", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      users,
		"nextCursor": nullableString(next),
	})
}

func (s *Server) handlePlatformGetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	u, err := s.Platform.UserDetail(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		log.Printf("platform get user: %v", err)
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handlePlatformSuspendUser(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if actor.ID == id {
		http.Error(w, "cannot suspend yourself", http.StatusBadRequest)
		return
	}
	if err := s.Platform.SuspendUser(r.Context(), id, actor.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		log.Printf("suspend user: %v", err)
		http.Error(w, "could not suspend user", http.StatusInternalServerError)
		return
	}
	if err := s.Platform.Audit(r.Context(), actor.ID, actionUserSuspend, "user", id, nil); err != nil {
		log.Printf("audit user.suspend: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlatformUnsuspendUser(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := s.Platform.UnsuspendUser(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		log.Printf("unsuspend user: %v", err)
		http.Error(w, "could not unsuspend user", http.StatusInternalServerError)
		return
	}
	if err := s.Platform.Audit(r.Context(), actor.ID, actionUserUnsuspend, "user", id, nil); err != nil {
		log.Printf("audit user.unsuspend: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

type confirmReq struct {
	Confirm string `json:"confirm"`
}

// checkConfirm reads {"confirm": "<phrase>"} from the body and compares against
// expected (case-insensitive, trimmed). Writes 400 + returns false on mismatch.
func checkConfirm(w http.ResponseWriter, r *http.Request, expected string) bool {
	var c confirmReq
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	if strings.TrimSpace(strings.ToLower(c.Confirm)) != strings.ToLower(expected) {
		http.Error(w, "confirmation phrase mismatch; expected: "+expected, http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) handlePlatformDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if actor.ID == id {
		http.Error(w, "cannot delete yourself", http.StatusBadRequest)
		return
	}
	email, err := s.Platform.UserEmail(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		log.Printf("delete user lookup: %v", err)
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}
	if !checkConfirm(w, r, "delete user "+email) {
		return
	}
	if _, err := s.Platform.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		log.Printf("delete user: %v", err)
		http.Error(w, "could not delete user", http.StatusInternalServerError)
		return
	}
	if err := s.Platform.Audit(r.Context(), actor.ID, actionUserDelete, "user", id, map[string]any{"email": email}); err != nil {
		log.Printf("audit user.delete: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlatformPromote(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	// Fail-fast: target user must exist so we don't insert a dangling row.
	email, err := s.Platform.UserEmail(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		log.Printf("promote lookup: %v", err)
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}
	if err := s.Platform.Promote(r.Context(), id, actor.ID); err != nil {
		log.Printf("promote: %v", err)
		http.Error(w, "could not promote", http.StatusInternalServerError)
		return
	}
	if err := s.Platform.Audit(r.Context(), actor.ID, actionUserPromote, "platform_admin", id, map[string]any{"email": email}); err != nil {
		log.Printf("audit user.promote: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlatformDemote(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	// Block demoting yourself if you're the only active admin: otherwise
	// the platform locks itself out.
	if actor.ID == id {
		n, err := s.Platform.CountActiveAdmins(r.Context())
		if err != nil {
			log.Printf("count active admins: %v", err)
			http.Error(w, "could not check admin count", http.StatusInternalServerError)
			return
		}
		if !canDemoteSelf(n) {
			http.Error(w, "cannot demote the last active platform admin", http.StatusBadRequest)
			return
		}
	}
	if err := s.Platform.Demote(r.Context(), id); err != nil {
		log.Printf("demote: %v", err)
		http.Error(w, "could not demote", http.StatusInternalServerError)
		return
	}
	if err := s.Platform.Audit(r.Context(), actor.ID, actionUserDemote, "platform_admin", id, nil); err != nil {
		log.Printf("audit user.demote: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlatformListOrgs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	orgs, next, err := s.Platform.ListOrgs(r.Context(), q.Get("q"), limit, q.Get("cursor"))
	if err != nil {
		log.Printf("platform list orgs: %v", err)
		http.Error(w, "could not list orgs", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      orgs,
		"nextCursor": nullableString(next),
	})
}

func (s *Server) handlePlatformGetOrg(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	o, err := s.Platform.OrgDetail(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "org not found", http.StatusNotFound)
			return
		}
		log.Printf("platform get org: %v", err)
		http.Error(w, "could not load org", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) handlePlatformSuspendOrg(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := s.Platform.SuspendOrg(r.Context(), id, actor.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "org not found", http.StatusNotFound)
			return
		}
		log.Printf("suspend org: %v", err)
		http.Error(w, "could not suspend org", http.StatusInternalServerError)
		return
	}
	if err := s.Platform.Audit(r.Context(), actor.ID, actionOrgSuspend, "org", id, nil); err != nil {
		log.Printf("audit org.suspend: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlatformDeleteOrg(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	slug, err := s.Platform.OrgSlug(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "org not found", http.StatusNotFound)
			return
		}
		log.Printf("delete org lookup: %v", err)
		http.Error(w, "could not load org", http.StatusInternalServerError)
		return
	}
	if !checkConfirm(w, r, "delete org "+slug) {
		return
	}
	if _, err := s.Platform.DeleteOrg(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "org not found", http.StatusNotFound)
			return
		}
		log.Printf("delete org: %v", err)
		http.Error(w, "could not delete org", http.StatusInternalServerError)
		return
	}
	if err := s.Platform.Audit(r.Context(), actor.ID, actionOrgDelete, "org", id, map[string]any{"slug": slug}); err != nil {
		log.Printf("audit org.delete: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlatformListAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := s.Platform.ListAdmins(r.Context())
	if err != nil {
		log.Printf("list admins: %v", err)
		http.Error(w, "could not list admins", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": admins})
}

func (s *Server) handlePlatformAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	entries, next, err := s.Platform.ListAudit(r.Context(), limit, q.Get("cursor"))
	if err != nil {
		log.Printf("list audit: %v", err)
		http.Error(w, "could not list audit", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      entries,
		"nextCursor": nullableString(next),
	})
}

// parseUUIDParam reads a chi path param and writes 400 on parse failure.
func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		http.Error(w, "invalid "+name, http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}

// nullableString returns nil for an empty string so JSON serializes as `null`
// rather than "". Matches the existing activity/callers cursor convention.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// canDemoteSelf encodes the lockout-prevention rule: a self-demote is only
// permitted while at least one *other* active admin remains. Extracted so it
// can be unit-tested without a DB.
func canDemoteSelf(activeCount int) bool {
	return activeCount >= 2
}
