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
		http.Error(w, "rand failed", http.StatusInternalServerError)
		return
	}
	deviceCode := "bg_dev_" + secret

	var userCode string
	for i := 0; i < 5; i++ {
		userCode, err = randUserCode()
		if err != nil {
			http.Error(w, "rand failed", http.StatusInternalServerError)
			return
		}
		_, err = s.DeviceAuth.Create(r.Context(), deviceCode, userCode, label, deviceCodeTTL)
		if err == nil {
			break
		}
		userCode = ""
	}
	if userCode == "" {
		http.Error(w, "could not create device authorization", http.StatusInternalServerError)
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
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.DeviceCode) == "" {
		http.Error(w, "deviceCode required", http.StatusBadRequest)
		return
	}
	rec, err := s.DeviceAuth.GetByDeviceCode(r.Context(), req.DeviceCode)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusGone, map[string]string{"error": "expired"})
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if time.Now().After(rec.ExpiresAt) && rec.Status == "pending" {
		writeJSON(w, http.StatusGone, map[string]string{"error": "expired"})
		return
	}
	switch rec.Status {
	case "pending":
		writeJSON(w, http.StatusPreconditionRequired, map[string]string{"error": "authorization_pending"})
	case "denied":
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "denied"})
	case "approved":
		// Consume: read & clear the bearer plaintext, then delete the row so
		// the token can only be returned once.
		bearer, sessionID, ok, err := s.DeviceAuth.ConsumeApproved(r.Context(), rec.ID)
		if err != nil {
			http.Error(w, "consume failed", http.StatusInternalServerError)
			return
		}
		if !ok {
			// Already consumed.
			writeJSON(w, http.StatusGone, map[string]string{"error": "expired"})
			return
		}
		sess, err := s.Sessions.Get(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "session lookup failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, pollResp{
			AccessToken: bearer,
			TokenType:   "Bearer",
			ExpiresAt:   sess.ExpiresAt,
		})
	default:
		writeJSON(w, http.StatusGone, map[string]string{"error": "expired"})
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
		http.Error(w, "code required", http.StatusBadRequest)
		return
	}
	rec, err := s.DeviceAuth.GetByUserCode(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req approveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	code := normalizeUserCode(req.UserCode)
	if code == "" {
		http.Error(w, "userCode required", http.StatusBadRequest)
		return
	}
	rec, err := s.DeviceAuth.GetByUserCode(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if rec.Status != "pending" {
		http.Error(w, "already resolved", http.StatusConflict)
		return
	}
	if time.Now().After(rec.ExpiresAt) {
		http.Error(w, "expired", http.StatusGone)
		return
	}

	secret, err := randBase64(32)
	if err != nil {
		http.Error(w, "rand failed", http.StatusInternalServerError)
		return
	}
	sess, err := s.Sessions.CreateCLI(r.Context(), user.ID, rec.ClientLabel, secret, cliSessionTTL)
	if err != nil {
		http.Error(w, "session create failed", http.StatusInternalServerError)
		return
	}
	bearer := auth.BearerPrefix + sess.ID.String() + "." + secret
	if err := s.DeviceAuth.ApproveWithBearer(r.Context(), rec.ID, user.ID, sess.ID, bearer); err != nil {
		// Roll the orphan session back.
		_ = s.Sessions.Delete(r.Context(), sess.ID)
		if errors.Is(err, store.ErrAlreadyResolved) {
			http.Error(w, "already resolved", http.StatusConflict)
			return
		}
		http.Error(w, "approve failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeviceDeny(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req approveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	code := normalizeUserCode(req.UserCode)
	if code == "" {
		http.Error(w, "userCode required", http.StatusBadRequest)
		return
	}
	rec, err := s.DeviceAuth.GetByUserCode(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if err := s.DeviceAuth.Deny(r.Context(), rec.ID, user.ID); err != nil {
		if errors.Is(err, store.ErrAlreadyResolved) {
			http.Error(w, "already resolved", http.StatusConflict)
			return
		}
		http.Error(w, "deny failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── /api/sessions ────────────────────────────────────────────────────────

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	sessions, err := s.Sessions.ListForUser(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	current := auth.SessionFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
	if current != nil && current.ID == id {
		http.Error(w, "cannot revoke the current session", http.StatusBadRequest)
		return
	}
	if err := s.Sessions.DeleteForUser(r.Context(), user.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "revoke failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
