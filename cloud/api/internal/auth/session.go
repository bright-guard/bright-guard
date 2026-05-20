package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	SessionCookieName = "bg_session"
	stateCookieName   = "bg_oauth_state"

	BearerPrefix = "bg_cli_"
)

type ctxKey int

const (
	ctxKeyUser ctxKey = iota
	ctxKeySession
)

type CookieOpts struct {
	Secure   bool
	Domain   string
	SameSite http.SameSite
}

func SetSessionCookie(w http.ResponseWriter, sessionID uuid.UUID, expires time.Time, opts CookieOpts) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionID.String(),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: opts.SameSite,
		Domain:   opts.Domain,
	})
}

func ClearSessionCookie(w http.ResponseWriter, opts CookieOpts) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: opts.SameSite,
		Domain:   opts.Domain,
	})
}

// parseBearerToken decodes a "bg_cli_<uuid>.<secret>" bearer into its parts.
func parseBearerToken(tok string) (uuid.UUID, string, bool) {
	if !strings.HasPrefix(tok, BearerPrefix) {
		return uuid.Nil, "", false
	}
	rest := strings.TrimPrefix(tok, BearerPrefix)
	dot := strings.IndexByte(rest, '.')
	if dot < 0 {
		return uuid.Nil, "", false
	}
	id, err := uuid.Parse(rest[:dot])
	if err != nil {
		return uuid.Nil, "", false
	}
	secret := rest[dot+1:]
	if secret == "" {
		return uuid.Nil, "", false
	}
	return id, secret, true
}

func Middleware(sessions *store.Sessions, users *store.Users) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sess, user := tryCookie(r, sessions, users); sess != nil && user != nil {
				_ = sessions.Touch(r.Context(), sess.ID)
				ctx := context.WithValue(r.Context(), ctxKeyUser, user)
				ctx = context.WithValue(ctx, ctxKeySession, sess)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if sess, user := tryBearer(r, sessions, users); sess != nil && user != nil {
				_ = sessions.Touch(r.Context(), sess.ID)
				ctx := context.WithValue(r.Context(), ctxKeyUser, user)
				ctx = context.WithValue(ctx, ctxKeySession, sess)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func tryCookie(r *http.Request, sessions *store.Sessions, users *store.Users) (*models.Session, *models.User) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, nil
	}
	id, err := uuid.Parse(c.Value)
	if err != nil {
		return nil, nil
	}
	sess, err := sessions.Get(r.Context(), id)
	if err != nil {
		return nil, nil
	}
	if sess.Kind != "" && sess.Kind != "cookie" {
		// A cli session must not be accepted via cookie.
		return nil, nil
	}
	user, err := users.ByID(r.Context(), sess.UserID.String())
	if err != nil {
		return nil, nil
	}
	return sess, user
}

func tryBearer(r *http.Request, sessions *store.Sessions, users *store.Users) (*models.Session, *models.User) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, nil
	}
	tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	id, secret, ok := parseBearerToken(tok)
	if !ok {
		return nil, nil
	}
	sess, err := sessions.GetCLIByToken(r.Context(), id, secret)
	if err != nil {
		return nil, nil
	}
	user, err := users.ByID(r.Context(), sess.UserID.String())
	if err != nil {
		return nil, nil
	}
	return sess, user
}

func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func UserFromContext(ctx context.Context) *models.User {
	v, _ := ctx.Value(ctxKeyUser).(*models.User)
	return v
}

func SessionFromContext(ctx context.Context) *models.Session {
	v, _ := ctx.Value(ctxKeySession).(*models.Session)
	return v
}

// WithUserForTest is a tiny helper for handler tests that need to bypass the
// session-cookie middleware and inject a user directly into the request context.
func WithUserForTest(ctx context.Context, user *models.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser, user)
}
