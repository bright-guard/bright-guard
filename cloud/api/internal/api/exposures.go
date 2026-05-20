package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/exposure"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

// handleListExposures returns per-state counts for the org.
func (s *Server) handleListExposures(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	counts, err := s.Discovery.CountExposuresByState(r.Context(), orgID)
	if err != nil {
		http.Error(w, "could not count exposures", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"counts": counts})
}

// handleReclassifyExposure reclassifies a single mcp_server's exposure state.
func (s *Server) handleReclassifyExposure(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	addr, err := s.Discovery.GetServerAddress(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	state, reason := exposure.Classify(addr)
	if err := s.Discovery.SetExposure(r.Context(), id, state, reason); err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	det, err := s.Discovery.GetServerDetail(r.Context(), orgID, id)
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, det)
}
