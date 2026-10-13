package httpapi

import (
	"net/http"

	"github.com/armature/armature/backend/internal/demo"
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

// handleAddDemoProject fills the installation with the demo, so an
// administrator can show the product with something in it.
func (s *Server) handleAddDemoProject(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	made, lsn, err := demo.Add(r.Context(), s.DB, p, clientIP(r), s.Log)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"project": made})
}
