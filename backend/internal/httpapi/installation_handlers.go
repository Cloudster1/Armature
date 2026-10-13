package httpapi

import (
	"net/http"
)

type renameInstallationRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleInstallation(w http.ResponseWriter, r *http.Request) {
	out, err := s.Installation.Get(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"installation": out})
}

func (s *Server) handleRenameInstallation(w http.ResponseWriter, r *http.Request) {
	var req renameInstallationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	out, lsn, err := s.Installation.Rename(r.Context(), PrincipalFrom(r.Context()).User.ID, req.Name, clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"installation": out})
}
