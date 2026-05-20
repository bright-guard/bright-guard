package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	deviceCodeTTL = 10 * time.Minute
	cliSessionTTL = 30 * 24 * time.Hour
	pollInterval  = 5

	userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXYZ23456789"
	userCodeLen      = 8 // formatted as XXXX-XXXX
)

type initiateReq struct {
	ClientLabel string `json:"clientLabel"`
}

type initiateResp struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

func randBase64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randUserCode() (string, error) {
	raw := make([]byte, userCodeLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, 0, userCodeLen+1)
	for i, b := range raw {
		out = append(out, userCodeAlphabet[int(b)%len(userCodeAlphabet)])
		if i == 3 {
			out = append(out, '-')
		}
	}
	return string(out), nil
}

func (s *Server) handleDeviceInitiate(w http.ResponseWriter, r *http.Request) {
	var req initiateReq
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	label := strings.TrimSpace(req.ClientLabel)
	if label == "" {
		label = "bg-cli"
	}
	if len(label) > 128 {
		label = label[:128]
	}

	secret, err := randBase64(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "rand failed")
		return
	}
	deviceCode := "bg_dev_" + secret

	var userCode string
	for i := 0; i < 5; i++ {
		userCode, err = randUserCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "rand failed")
			return
		}
		_, err = s.DeviceAuth.Create(r.Context(), deviceCode, userCode, label, deviceCodeTTL)
		if err == nil {
			break
		}
		userCode = ""
	}
	if userCode == "" {
		writeError(w, http.StatusInternalServerError, "internal", "could not create device authorization")
		return
	}

	base := strings.TrimRight(s.Cfg.WebBaseURL, "/")
	writeJSON(w, http.StatusOK, initiateResp{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationURI:         base + "/device",
		VerificationURIComplete: base + "/device?code=" + userCode,
		ExpiresIn:               int(deviceCodeTTL.Seconds()),
		Interval:                pollInterval,
	})
}

type pollReq struct {
	DeviceCode string `json:"deviceCode"`
}

type pollResp struct {
	AccessToken string    `json:"accessToken"`
	TokenType   string    `json:"tokenType"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func (s *Server) handleDevicePoll(w http.ResponseWriter, r *http.Request) {
	var req pollReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	if strings.TrimSpace(req.DeviceCode) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "deviceCode required")
		return
	}
	rec, err := s.DeviceAuth.GetByDeviceCode(r.Context(), req.DeviceCode)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusGone, "expired", "device code expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	if time.Now().After(rec.ExpiresAt) && rec.Status == "pending" {
		writeError(w, http.StatusGone, "expired", "device code expired")
		return
	}
	switch rec.Status {
	case "pending":
		writeError(w, http.StatusPreconditionRequired, "authorization_pending", "authorization pending")
	case "denied":
		writeError(w, http.StatusForbidden, "denied", "device authorization denied")
	case "approved":
		// Consume: read & clear the bearer plaintext, then delete the row so
		// the token can only be returned once.
		bearer, sessionID, ok, err := s.DeviceAuth.ConsumeApproved(r.Context(), rec.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "consume failed")
			return
		}
		if !ok {
			// Already consumed.
			writeError(w, http.StatusGone, "expired", "device code expired")
			return
		}
		sess, err := s.Sessions.Get(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "session lookup failed")
			return
		}
		writeJSON(w, http.StatusOK, pollResp{
			AccessToken: bearer,
			TokenType:   "Bearer",
			ExpiresAt:   sess.ExpiresAt,
		})
	default:
		writeError(w, http.StatusGone, "expired", "device code expired")
	}
}

type lookupResp struct {
	ClientLabel string    `json:"clientLabel"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func normalizeUserCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	// Accept both "ABCDWXYZ" and "ABCD-WXYZ".
	if len(s) == 8 {
		s = s[:4] + "-" + s[4:]
	}
	return s
}

func (s *Server) handleDeviceLookup(w http.ResponseWriter, r *http.Request) {
	code := normalizeUserCode(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "code required")
		return
	}
	rec, err := s.DeviceAuth.GetByUserCode(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, lookupResp{
		ClientLabel: rec.ClientLabel,
		Status:      rec.Status,
		ExpiresAt:   rec.ExpiresAt,
	})
}

type approveReq struct {
	UserCode string `json:"userCode"`
}

func (s *Server) handleDeviceApprove(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req approveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	code := normalizeUserCode(req.UserCode)
	if code == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "userCode required")
		return
	}
	rec, err := s.DeviceAuth.GetByUserCode(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	if rec.Status != "pending" {
		writeError(w, http.StatusConflict, "conflict", "already resolved")
		return
	}
	if time.Now().After(rec.ExpiresAt) {
		writeError(w, http.StatusGone, "expired", "device code expired")
		return
	}

	secret, err := randBase64(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "rand failed")
		return
	}
	sess, err := s.Sessions.CreateCLI(r.Context(), user.ID, rec.ClientLabel, secret, cliSessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "session create failed")
		return
	}
	bearer := auth.BearerPrefix + sess.ID.String() + "." + secret
	if err := s.DeviceAuth.ApproveWithBearer(r.Context(), rec.ID, user.ID, sess.ID, bearer); err != nil {
		// Roll the orphan session back.
		_ = s.Sessions.Delete(r.Context(), sess.ID)
		if errors.Is(err, store.ErrAlreadyResolved) {
			writeError(w, http.StatusConflict, "conflict", "already resolved")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "approve failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeviceDeny(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req approveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	code := normalizeUserCode(req.UserCode)
	if code == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "userCode required")
		return
	}
	rec, err := s.DeviceAuth.GetByUserCode(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	if err := s.DeviceAuth.Deny(r.Context(), rec.ID, user.ID); err != nil {
		if errors.Is(err, store.ErrAlreadyResolved) {
			writeError(w, http.StatusConflict, "conflict", "already resolved")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "deny failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── /api/sessions ────────────────────────────────────────────────────────

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	sessions, err := s.Sessions.ListForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list failed")
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	current := auth.SessionFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid session id")
		return
	}
	if current != nil && current.ID == id {
		writeError(w, http.StatusBadRequest, "invalid_request", "cannot revoke the current session")
		return
	}
	if err := s.Sessions.DeleteForUser(r.Context(), user.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "revoke failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
