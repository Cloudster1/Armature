package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/profile"
)

// A person's own account: their name, how dates are written for them, and
// their picture. The picture is served through the API like an attachment,
// so the session decides who sees it.

type updateProfileRequest struct {
	Name     *string `json:"name"`
	Timezone *string `json:"timezone"`
	Locale   *string `json:"locale"`
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req updateProfileRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())
	if err := s.Auth.UpdateProfile(r.Context(), p.User.ID, auth.ProfileInput{Name: req.Name, Timezone: req.Timezone, Locale: req.Locale}); err != nil {
		respondError(w, r, err)
		return
	}
	s.respondPrincipal(w, r)
}

// respondPrincipal re-reads the caller, so the answer is what the next request
// will see and not what this one hoped.
func (s *Server) respondPrincipal(w http.ResponseWriter, r *http.Request) {
	secret := credentialFrom(r, s.CookieName)
	if secret == "" {
		respondError(w, r, ErrUnauthorized(""))
		return
	}
	fresh, err := s.Auth.Authenticate(r.Context(), secret)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"principal": fresh})
}

func (s *Server) handleSetAvatar(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, profile.AvatarMaxBytes+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		respondError(w, r, ErrBadRequest("Send the picture as multipart form data in a part named file."))
		return
	}
	p := PrincipalFrom(r.Context())
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			respondError(w, r, ErrBadRequest("The upload has no part named file."))
			return
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				respondError(w, r, profile.ErrTooLarge)
				return
			}
			respondError(w, r, ErrBadRequest("The upload could not be read."))
			return
		}
		if part.FormName() != "file" {
			continue
		}
		if _, err := s.Profiles.Set(r.Context(), p.User.ID, part); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				err = profile.ErrTooLarge
			}
			respondError(w, r, err)
			return
		}
		s.respondPrincipal(w, r)
		return
	}
}

func (s *Server) handleRemoveAvatar(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if err := s.Profiles.Remove(r.Context(), p.User.ID); err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// handleAvatar streams a picture to anyone in an organization with its owner.
// The version in the URL changes with the picture, so the browser may keep it.
func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		respondError(w, r, ErrNotFound("There is no picture for that person."))
		return
	}
	p := PrincipalFrom(r.Context())
	shared, err := s.Auth.SharesOrganization(r.Context(), p.User.ID, id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !shared && p.User.ID != id {
		respondError(w, r, ErrNotFound("There is no picture for that person."))
		return
	}
	body, contentType, err := s.Profiles.Open(r.Context(), id)
	if err != nil {
		respondError(w, r, ErrNotFound("There is no picture for that person."))
		return
	}
	defer body.Close()
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}
