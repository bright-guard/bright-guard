package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

type DevLogin struct {
	Users     *store.Users
	Sessions  *store.Sessions
	CookieOpt CookieOpts
}

type devLoginReq struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (d *DevLogin) Handler(w http.ResponseWriter, r *http.Request) {
	var req devLoginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json body")
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid email")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = email
	}
	subject := "dev:" + email
	user, err := d.Users.UpsertByGoogle(r.Context(), subject, email, name, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not upsert user")
		return
	}
	sess, err := d.Sessions.Create(r.Context(), user.ID, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create session")
		return
	}
	SetSessionCookie(w, sess.ID, sess.ExpiresAt, d.CookieOpt)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}
