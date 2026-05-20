package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	defaultActivityWindow = 24 * time.Hour
	defaultActivityLimit  = 100
	maxActivityLimit      = 500
)

func parseTimeOr(v string, fallback time.Time) (time.Time, bool, error) {
	if strings.TrimSpace(v) == "" {
		return fallback, false, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

func dedupeLower(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func (s *Server) parseActivityFilter(r *http.Request) (store.ActivityFilter, error) {
	now := time.Now().UTC()
	f := store.ActivityFilter{
		From:  now.Add(-defaultActivityWindow),
		To:    now,
		Limit: defaultActivityLimit,
	}
	q := r.URL.Query()

	if t, _, err := parseTimeOr(q.Get("from"), f.From); err != nil {
		return f, err
	} else {
		f.From = t
	}
	if t, _, err := parseTimeOr(q.Get("to"), f.To); err != nil {
		return f, err
	} else {
		f.To = t
	}

	f.CapabilityKinds = dedupeLower(q["capabilityKind"])
	f.Statuses = dedupeLower(q["status"])

	if raw := q.Get("mcpServerId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return f, err
		}
		f.MCPServerID = &id
	}

	f.Q = q.Get("q")

	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return f, err
		}
		if n > maxActivityLimit {
			n = maxActivityLimit
		}
		if n > 0 {
			f.Limit = n
		}
	}
	f.Cursor = q.Get("cursor")
	return f, nil
}

type activityListResp struct {
	Items      []store.ActivityRow `json:"items"`
	NextCursor *string             `json:"nextCursor"`
	Totals     store.StatusTotals  `json:"totals"`
}

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	f, err := s.parseActivityFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid query: "+err.Error())
		return
	}
	items, next, totals, err := s.Activity.List(r.Context(), orgID, f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list failed")
		return
	}
	resp := activityListResp{Items: items, Totals: totals}
	if next != "" {
		resp.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleActivitySummary(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	now := time.Now().UTC()
	from := now.Add(-defaultActivityWindow)
	to := now
	if t, _, err := parseTimeOr(r.URL.Query().Get("from"), from); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid from")
		return
	} else {
		from = t
	}
	if t, _, err := parseTimeOr(r.URL.Query().Get("to"), to); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid to")
		return
	} else {
		to = t
	}
	sum, err := s.Activity.Summary(r.Context(), orgID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "summary failed")
		return
	}
	writeJSON(w, http.StatusOK, sum)
}
