package httpapi

import (
	"net/http"
	"time"

	"github.com/armature/armature/backend/internal/report"
)

// SharedReadsPerMinute is the brake on one address opening shared dashboards:
// a wall of monitors behind one router refreshing every minute fits under it.
const SharedReadsPerMinute = 240

var sharedReads = newThrottle(SharedReadsPerMinute, time.Minute)

type createShareRequest struct {
	Name string `json:"name"`
	// Query is the dashboard's filter as the sharer sees it, frozen into the link.
	Query     string     `json:"query"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// handleCreateShare mints a link. The secret is in this answer and nowhere
// else afterwards; the row keeps a digest.
func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "dashboardID", "dashboard")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req createShareRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	share, secret, lsn, err := s.Reports.CreateShare(r.Context(), id, report.ShareInput{Name: req.Name, Query: req.Query, ExpiresAt: req.ExpiresAt}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"share": share, "url": s.AppBaseURL + "/shared/" + secret})
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "dashboardID", "dashboard")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	shares, err := s.Reports.Shares(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"shares": shares})
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	dashboardID, apiErr := pathUUID(r, "dashboardID", "dashboard")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	shareID, apiErr := pathUUID(r, "shareID", "link")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Reports.RevokeShare(r.Context(), dashboardID, shareID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// sharedGate is what every public read of a shared dashboard passes: the
// brake, and a header that keeps the numbers out of any cache on the way.
func (s *Server) sharedGate(w http.ResponseWriter, r *http.Request) bool {
	if !sharedReads.gate(w, r, "This dashboard was opened too often from here. Wait a minute and try again.") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}

// handleShared opens a link: the dashboard's arrangement and whose it is.
func (s *Server) handleShared(w http.ResponseWriter, r *http.Request) {
	if !s.sharedGate(w, r) {
		return
	}
	view, err := s.Reports.Shared(r.Context(), r.PathValue("token"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, view)
}

// handleSharedWidget answers one widget of a shared dashboard, with its own
// settings and the link's frozen query; the visitor sends no parameters.
func (s *Server) handleSharedWidget(w http.ResponseWriter, r *http.Request) {
	if !s.sharedGate(w, r) {
		return
	}
	widgetID, apiErr := pathUUID(r, "widgetID", "widget")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	out, err := s.Reports.SharedWidget(r.Context(), r.PathValue("token"), widgetID)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, out)
}
