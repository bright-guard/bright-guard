package api

import (
	"net/http"
)

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	members, err := s.Orgs.ListMembers(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list members failed")
		return
	}
	writeJSON(w, http.StatusOK, members)
}
