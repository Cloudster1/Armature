package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/theme"
)

// Themes: a person's own redefinition of the interface, shared with the
// organization when they say so. Every route reads as the caller; changing
// one is the owner's, or an administrator's once it is shared.

type chooseThemeRequest struct {
	ThemeID *uuid.UUID `json:"themeId"`
}

func (s *Server) administers(r *http.Request) bool {
	return PermsFrom(r.Context()).CanInOrg(perm.OrgAdminister)
}

func (s *Server) handleListThemes(w http.ResponseWriter, r *http.Request) {
	themes, err := s.Themes.List(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"themes": themes})
}

func (s *Server) handleCreateTheme(w http.ResponseWriter, r *http.Request) {
	var req theme.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Themes.Create(r.Context(), userFrom(r), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"theme": made})
}

func (s *Server) handleActiveTheme(w http.ResponseWriter, r *http.Request) {
	active, err := s.Themes.Active(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": active})
}

func (s *Server) handleChooseTheme(w http.ResponseWriter, r *http.Request) {
	var req chooseThemeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	chosen, lsn, err := s.Themes.Choose(r.Context(), userFrom(r), req.ThemeID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": chosen})
}

func (s *Server) handleGetTheme(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.Themes.Get(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": found})
}

func (s *Server) handleUpdateTheme(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req theme.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Themes.Update(r.Context(), id, userFrom(r), s.administers(r), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": updated})
}

func (s *Server) handleDeleteTheme(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Themes.Delete(r.Context(), id, userFrom(r), s.administers(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleUploadThemeAsset(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, theme.MaxAssetBytes+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		respondError(w, r, ErrBadRequest("Send the file as multipart form data in a part named file."))
		return
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			respondError(w, r, ErrBadRequest("The upload has no part named file."))
			return
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				respondError(w, r, theme.ErrAssetTooLarge)
				return
			}
			respondError(w, r, ErrBadRequest("The upload could not be read."))
			return
		}
		if part.FormName() != "file" {
			continue
		}
		asset, lsn, err := s.Themes.UploadAsset(r.Context(), id, userFrom(r), s.administers(r), part.FileName(), part)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				err = theme.ErrAssetTooLarge
			}
			respondError(w, r, err)
			return
		}
		NoteWrite(r.Context(), lsn)
		respondJSON(w, r, http.StatusCreated, map[string]any{"asset": asset})
		return
	}
}

// handleThemeAsset streams a file to anyone who may see the theme. It is
// offered as a download so an SVG never renders as a page of ours, and the
// id never changes, so the browser may keep it.
func (s *Server) handleThemeAsset(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	assetID, apiErr := pathUUID(r, "assetID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	body, asset, err := s.Themes.OpenAsset(r.Context(), id, assetID, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer body.Close()
	h := w.Header()
	h.Set("Content-Type", asset.ContentType)
	h.Set("Content-Length", strconv.FormatInt(asset.Size, 10))
	h.Set("Content-Disposition", `attachment; filename="`+asset.Name+`"`)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

func (s *Server) handleDeleteThemeAsset(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "themeID", "theme")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	assetID, apiErr := pathUUID(r, "assetID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Themes.DeleteAsset(r.Context(), id, assetID, userFrom(r), s.administers(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
