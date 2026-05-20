package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	defaultCallerLimit = 100
	maxCallerLimit     = 500
)

func (s *Server) parseCallerFilter(r *http.Request) (store.CallerFilter, error) {
	f := store.CallerFilter{Limit: defaultCallerLimit}
	q := r.URL.Query()

	if t, _, err := parseTimeOr(q.Get("from"), time.Time{}); err != nil {
		return f, err
	} else {
		f.From = t
	}
	if t, _, err := parseTimeOr(q.Get("to"), time.Time{}); err != nil {
		return f, err
	} else {
		f.To = t
	}
	if raw := strings.TrimSpace(q.Get("flagged")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return f, err
		}
		f.FlaggedOnly = v
	}
	f.Q = q.Get("q")
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return f, err
		}
		if n > maxCallerLimit {
			n = maxCallerLimit
		}
		if n > 0 {
			f.Limit = n
		}
	}
	f.Cursor = q.Get("cursor")
	return f, nil
}

type callerListResp struct {
	Items      []models.OrgCaller  `json:"items"`
	NextCursor *string             `json:"nextCursor"`
	Totals     store.CallerTotals  `json:"totals"`
}

func (s *Server) handleListCallers(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	f, err := s.parseCallerFilter(r)
	if err != nil {
		http.Error(w, "invalid query: "+err.Error(), http.StatusBadRequest)
		return
	}
	items, next, totals, err := s.Callers.List(r.Context(), orgID, f)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	resp := callerListResp{Items: items, Totals: totals}
	if next != "" {
		resp.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetCaller(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	det, err := s.Callers.Get(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleAcknowledgeCaller(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.Callers.Acknowledge(r.Context(), orgID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "acknowledge failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
