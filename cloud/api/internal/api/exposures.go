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
		writeError(w, http.StatusInternalServerError, "internal", "could not count exposures")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"counts": counts})
}

// handleReclassifyExposure reclassifies a single mcp_server's exposure state.
func (s *Server) handleReclassifyExposure(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid id")
		return
	}
	addr, err := s.Discovery.GetServerAddress(r.Context(), orgID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	state, reason := exposure.Classify(addr)
	if err := s.Discovery.SetExposure(r.Context(), id, state, reason); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "save failed")
		return
	}
	det, err := s.Discovery.GetServerDetail(r.Context(), orgID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, det)
}
