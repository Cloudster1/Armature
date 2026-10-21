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

// handleSuiteDemo says how far the suite demo got, null until somebody added it.
func (s *Server) handleSuiteDemo(w http.ResponseWriter, r *http.Request) {
	out, err := demo.Suite(r.Context(), s.DB)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"demo": out})
}

// handleAddSuiteDemo asks the worker for the suite demo, which Exciter's and Stator's link to.
func (s *Server) handleAddSuiteDemo(w http.ResponseWriter, r *http.Request) {
	out, lsn, err := demo.QueueSuite(r.Context(), s.DB, PrincipalFrom(r.Context()), clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusAccepted, map[string]any{"demo": out})
}
