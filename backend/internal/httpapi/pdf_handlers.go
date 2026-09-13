package httpapi

import (
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/render"
	"github.com/armature/armature/backend/internal/report"
)

// RenderShareTTL is how long the link the renderer opens stays valid: long
// enough to print, short enough that nothing else ever opens it.
const RenderShareTTL = 2 * time.Minute

// PDFRendersPerMinute is the brake on one address asking a shared dashboard
// for its PDF; a print is a whole browser's work.
const PDFRendersPerMinute = 6

var pdfRenders = newThrottle(PDFRendersPerMinute, time.Minute)

// printPath is the shared page as the renderer opens it: no header, light
// theme, whatever this browser would otherwise remember.
func printPath(secret string) string {
	return "/shared/" + secret + "?print=1&theme=light"
}

// handleDashboardPDF prints a dashboard as the signed-in reader filters it,
// through a link only the renderer holds, minted and revoked around the print.
func (s *Server) handleDashboardPDF(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "dashboardID", "dashboard")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	expires := time.Now().Add(RenderShareTTL)
	share, secret, lsn, err := s.Reports.CreateShare(r.Context(), id, report.ShareInput{
		Name: "PDF export", Query: r.URL.Query().Get("q"), ExpiresAt: &expires, Internal: true,
	}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	defer func() { _, _ = s.Reports.RevokeShare(r.Context(), id, share.ID) }()

	view, err := s.Reports.Shared(r.Context(), secret)
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.streamPDF(w, r, printPath(secret), view.Dashboard.ProjectKey+"-"+auth.Slugify(view.Dashboard.Name))
}

// handleSharedPDF prints a shared dashboard for whoever holds the link.
func (s *Server) handleSharedPDF(w http.ResponseWriter, r *http.Request) {
	if !pdfRenders.gate(w, r, "This dashboard was printed too often from here. Wait a minute and try again.") {
		return
	}
	token := r.PathValue("token")
	view, err := s.Reports.Shared(r.Context(), token)
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.streamPDF(w, r, printPath(token), view.Dashboard.ProjectKey+"-"+auth.Slugify(view.Dashboard.Name))
}

func (s *Server) streamPDF(w http.ResponseWriter, r *http.Request, path, name string) {
	renderer := s.Renderer
	if renderer == nil {
		renderer = render.Unavailable{}
	}
	body, err := renderer.PDF(r.Context(), path)
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer body.Close()
	filename := name + "-" + time.Now().Format("2006-01-02") + ".pdf"
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}
