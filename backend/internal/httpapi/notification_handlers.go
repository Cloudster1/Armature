package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/notify"
)

// The inbox: what a person was told, and how they want to be told. Every
// route here is the caller's own; nobody reads another person's inbox.

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	unread := r.URL.Query().Get("unread") == "true"
	limit := queryInt(r, "limit", notify.DefaultInboxLimit, 1, notify.MaxInboxLimit)
	items, err := s.Notify.Inbox(r.Context(), userFrom(r), unread, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"notifications": items})
}

type unreadCount struct {
	Unread int `json:"unread"`
}

func (s *Server) handleUnreadCount(w http.ResponseWriter, r *http.Request) {
	n, err := s.Notify.Unread(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, unreadCount{Unread: n})
}

// markReadRequest names rows, or says all of them.
type markReadRequest struct {
	IDs []uuid.UUID `json:"ids,omitempty"`
	All bool        `json:"all,omitempty"`
}

func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	var req markReadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !req.All && len(req.IDs) == 0 {
		respondError(w, r, ErrBadRequest("Say which notifications to mark read, or all of them."))
		return
	}
	var (
		err error
		lsn db.LSN
	)
	if req.All {
		lsn, err = s.Notify.MarkAllRead(r.Context(), userFrom(r))
	} else {
		lsn, err = s.Notify.MarkRead(r.Context(), userFrom(r), req.IDs)
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := s.Notify.Preferences(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"preferences": p})
}

func (s *Server) handleSaveNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	var req notify.Preferences
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	lsn, err := s.Notify.SavePreferences(r.Context(), userFrom(r), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	p, err := s.Notify.Preferences(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"preferences": p})
}
