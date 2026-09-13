package httpapi

import (
	"net/http"
	"time"

	"github.com/armature/armature/backend/internal/desk"
)

// The desk's extras: articles, canned responses, ratings, business hours.

// RatingsPerMinute brakes one address on the public rating door.
const RatingsPerMinute = 10

var ratings = newThrottle(RatingsPerMinute, time.Minute)

func (s *Server) handleListArticles(w http.ResponseWriter, r *http.Request) {
	articles, err := s.Desk.Articles(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"articles": articles})
}

func (s *Server) handleCreateArticle(w http.ResponseWriter, r *http.Request) {
	var req desk.ArticleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Desk.CreateArticle(r.Context(), r.PathValue("projectKey"), req, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"article": made})
}

func (s *Server) handleUpdateArticle(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "articleID", "article")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req desk.ArticleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Desk.UpdateArticle(r.Context(), id, req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"article": updated})
}

func (s *Server) handleDeleteArticle(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "articleID", "article")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Desk.DeleteArticle(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handlePortalArticles is what a customer finds before raising a request.
func (s *Server) handlePortalArticles(w http.ResponseWriter, r *http.Request) {
	articles, err := s.Desk.SearchArticles(r.Context(), r.PathValue("projectKey"), r.URL.Query().Get("q"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"articles": articles})
}

func (s *Server) handlePortalArticle(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "articleID", "article")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	article, err := s.Desk.PublishedArticle(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"article": article})
}

func (s *Server) handleListCanned(w http.ResponseWriter, r *http.Request) {
	responses, err := s.Desk.CannedResponses(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"responses": responses})
}

func (s *Server) handleCreateCanned(w http.ResponseWriter, r *http.Request) {
	var req desk.CannedInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Desk.CreateCanned(r.Context(), r.PathValue("projectKey"), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"response": made})
}

func (s *Server) handleUpdateCanned(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "responseID", "canned response")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req desk.CannedInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Desk.UpdateCanned(r.Context(), id, req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"response": updated})
}

func (s *Server) handleDeleteCanned(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "responseID", "canned response")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Desk.DeleteCanned(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// renderRequest names the request a canned response is filled in for.
type renderRequest struct {
	IssueKey string `json:"issueKey"`
}

type rendered struct {
	Text string `json:"text"`
}

func (s *Server) handleRenderCanned(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "responseID", "canned response")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req renderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.IssueKey == "" {
		respondError(w, r, ErrBadRequest("Say which request the reply is for."))
		return
	}
	text, err := s.Desk.RenderCanned(r.Context(), id, req.IssueKey, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, rendered{Text: text})
}

// The rating door: no session, the token is the whole of it.

func (s *Server) handleRatingPage(w http.ResponseWriter, r *http.Request) {
	page, err := s.Desk.RatingPage(r.Context(), r.PathValue("token"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rating": page})
}

type rateRequest struct {
	Score   int    `json:"score"`
	Comment string `json:"comment,omitempty"`
}

func (s *Server) handleRate(w http.ResponseWriter, r *http.Request) {
	if !ratings.gate(w, r, "Too many ratings from here. Wait a minute and try again.") {
		return
	}
	var req rateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	rating, lsn, err := s.Desk.Rate(r.Context(), r.PathValue("token"), req.Score, req.Comment)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"rating": rating})
}

func (s *Server) handleIssueRating(w http.ResponseWriter, r *http.Request) {
	rating, err := s.Desk.Rating(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rating": rating})
}

func (s *Server) handleGetCalendar(w http.ResponseWriter, r *http.Request) {
	cal, err := s.Desk.Calendar(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"calendar": cal})
}

func (s *Server) handleSaveCalendar(w http.ResponseWriter, r *http.Request) {
	var req desk.Calendar
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	cal, lsn, err := s.Desk.SaveCalendar(r.Context(), r.PathValue("projectKey"), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"calendar": cal})
}
