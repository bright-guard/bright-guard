package spa

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesRobotsTxt(t *testing.T) {
	h := Handler()
	r := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type=%q want text/plain*", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "User-agent: *") || !strings.Contains(body, "Disallow: /api/") {
		t.Errorf("unexpected body: %s", body)
	}
}
