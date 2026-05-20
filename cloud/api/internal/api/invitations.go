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

// errorResp shapes the JSON 422 body documented for not_registered etc.
type errorResp struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResp{Error: errorBody{Code: code, Message: msg}})
}

// ── owner/admin-facing invitations ───────────────────────────────────────

func (s *Server) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	items, err := s.Invitations.ListForOrg(r.Context(), orgID, status)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
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
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		http.Error(w, "valid email is required", http.StatusBadRequest)
		return
	}
	role := req.Role
	if role == "" {
		role = models.RoleMember
	}
	switch role {
	case models.RoleOwner, models.RoleAdmin, models.RoleMember:
	default:
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}

	// Reject unregistered emails — invitations are existing-users only.
	invitee, err := s.Users.ByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusUnprocessableEntity, "not_registered",
				email+" isn't a Bright Guard user yet. Ask them to sign up at mcp-governance.infoblox.dev first.")
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}

	// 422 if the user is already a member of this org.
	if _, err := s.Orgs.RoleFor(r.Context(), invitee.ID, orgID); err == nil {
		writeErr(w, http.StatusUnprocessableEntity, "already_member", email+" is already a member of this org.")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}

	inv, err := s.Invitations.Create(r.Context(), orgID, user.ID, email, role)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			writeErr(w, http.StatusUnprocessableEntity, "already_invited", "An invitation is already pending for this email.")
			return
		}
		http.Error(w, "create failed", http.StatusInternalServerError)
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
		http.Error(w, "invalid invitation id", http.StatusBadRequest)
		return
	}
	if err := s.Invitations.Revoke(r.Context(), orgID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "revoke failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── invitee-facing routes (not orgMember-protected) ──────────────────────

func (s *Server) handleListMyInvitations(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	items, err := s.Invitations.ListPendingForEmail(r.Context(), user.Email)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid invitation id", http.StatusBadRequest)
		return
	}
	inv, err := s.Invitations.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if !strings.EqualFold(inv.Email, user.Email) {
		http.Error(w, "this invitation is addressed to a different email", http.StatusForbidden)
		return
	}
	if inv.Status != "pending" {
		http.Error(w, "invitation is "+inv.Status, http.StatusConflict)
		return
	}
	if err := s.Orgs.AddMember(r.Context(), inv.OrgID, user.ID, inv.Role); err != nil {
		http.Error(w, "add member failed", http.StatusInternalServerError)
		return
	}
	if err := s.Invitations.MarkAccepted(r.Context(), inv.ID); err != nil {
		// AddMember already succeeded; the inv state mismatch is the only realistic
		// failure here and a 500 here is fine — the next attempt will be a no-op.
		http.Error(w, "mark accepted failed", http.StatusInternalServerError)
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
		http.Error(w, "invalid invitation id", http.StatusBadRequest)
		return
	}
	inv, err := s.Invitations.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if !strings.EqualFold(inv.Email, user.Email) {
		http.Error(w, "this invitation is addressed to a different email", http.StatusForbidden)
		return
	}
	if inv.Status != "pending" {
		http.Error(w, "invitation is "+inv.Status, http.StatusConflict)
		return
	}
	if err := s.Invitations.MarkDeclined(r.Context(), inv.ID); err != nil {
		http.Error(w, "mark declined failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
