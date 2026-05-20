package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/email"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

// orgIDFromURL parses the chi URL param `orgId`. Used by auth.RequireOrgRole.
func orgIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "orgId"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// ── owner/admin-facing invitations ───────────────────────────────────────

func (s *Server) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	items, err := s.Invitations.ListForOrg(r.Context(), orgID, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list failed")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

type createInvitationReq struct {
	Email string         `json:"email"`
	Role  models.OrgRole `json:"role"`
}

func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	var req createInvitationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "invalid_request", "valid email is required")
		return
	}
	role := req.Role
	if role == "" {
		role = models.RoleMember
	}
	switch role {
	case models.RoleOwner, models.RoleAdmin, models.RoleMember:
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid role")
		return
	}

	// Reject unregistered emails — invitations are existing-users only.
	invitee, err := s.Users.ByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnprocessableEntity, "not_registered",
				email+" isn't a Bright Guard user yet. Ask them to sign up at mcp-governance.infoblox.dev first.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}

	// 422 if the user is already a member of this org.
	if _, err := s.Orgs.RoleFor(r.Context(), invitee.ID, orgID); err == nil {
		writeError(w, http.StatusUnprocessableEntity, "already_member", email+" is already a member of this org.")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}

	inv, err := s.Invitations.Create(r.Context(), orgID, user.ID, email, role)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			writeError(w, http.StatusUnprocessableEntity, "already_invited", "An invitation is already pending for this email.")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "create failed")
		return
	}

	s.sendInvitationEmail(r, inv)
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) sendInvitationEmail(r *http.Request, inv *models.Invitation) {
	if s.Email == nil {
		return
	}
	inviterName := inv.InviterName
	if inviterName == "" {
		inviterName = inv.InviterEmail
	}
	acceptURL := strings.TrimRight(s.Cfg.WebBaseURL, "/") + "/app/invitations/" + inv.ID.String()
	subject, html, text, err := email.RenderInvitation(email.InvitationVars{
		ID:           inv.ID.String(),
		OrgName:      inv.OrgName,
		InviterName:  inviterName,
		InviterEmail: inv.InviterEmail,
		AcceptURL:    acceptURL,
		ExpiresAt:    inv.ExpiresAt,
	})
	if err != nil {
		log.Printf("invitations: render: %v", err)
		return
	}
	// Send synchronously but ignore failure — the invite row still exists in DB
	// and the SPA can re-trigger via a follow-up "resend" if needed.
	if err := s.Email.Send(r.Context(), inv.Email, subject, html, text); err != nil {
		log.Printf("invitations: send to %s: %v", inv.Email, err)
	}
}

func (s *Server) handleRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid invitation id")
		return
	}
	if err := s.Invitations.Revoke(r.Context(), orgID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "revoke failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── invitee-facing routes (not orgMember-protected) ──────────────────────

func (s *Server) handleListMyInvitations(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	items, err := s.Invitations.ListPendingForEmail(r.Context(), user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list failed")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid invitation id")
		return
	}
	inv, err := s.Invitations.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	if !strings.EqualFold(inv.Email, user.Email) {
		writeError(w, http.StatusForbidden, "forbidden", "this invitation is addressed to a different email")
		return
	}
	if inv.Status != "pending" {
		writeError(w, http.StatusConflict, "conflict", "invitation is "+inv.Status)
		return
	}
	if err := s.Orgs.AddMember(r.Context(), inv.OrgID, user.ID, inv.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "add member failed")
		return
	}
	if err := s.Invitations.MarkAccepted(r.Context(), inv.ID); err != nil {
		// AddMember already succeeded; the inv state mismatch is the only realistic
		// failure here and a 500 here is fine — the next attempt will be a no-op.
		writeError(w, http.StatusInternalServerError, "internal", "mark accepted failed")
		return
	}
	// Switch the session's active org to the one they just joined.
	if sess := auth.SessionFromContext(r.Context()); sess != nil {
		_ = s.Sessions.SetActiveOrg(r.Context(), sess.ID, inv.OrgID)
	}
	fresh, err := s.Invitations.Get(r.Context(), inv.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, inv)
		return
	}
	writeJSON(w, http.StatusOK, fresh)
}

func (s *Server) handleDeclineInvitation(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid invitation id")
		return
	}
	inv, err := s.Invitations.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	if !strings.EqualFold(inv.Email, user.Email) {
		writeError(w, http.StatusForbidden, "forbidden", "this invitation is addressed to a different email")
		return
	}
	if inv.Status != "pending" {
		writeError(w, http.StatusConflict, "conflict", "invitation is "+inv.Status)
		return
	}
	if err := s.Invitations.MarkDeclined(r.Context(), inv.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "mark declined failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
