package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
)

// Attachments: files on an issue, uploaded as multipart form data and streamed
// back through the API so that the session, not a bucket policy, decides who
// may read them.

// uploadSlack is how much larger than the file the whole multipart body may be:
// the boundaries and the headers around the part.
const uploadSlack = 64 << 10

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	found, err := s.Attachments.List(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"attachments": found})
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	in, err := readUploadedFile(w, r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	created, lsn, err := s.Attachments.Upload(r.Context(), r.PathValue("issueKey"), in, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(uploadError(err)))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"attachment": created})
}

// readUploadedFile finds the part named file and hands its stream on, so the
// bytes go to the bucket without a copy on disk. The body is capped first.
func readUploadedFile(w http.ResponseWriter, r *http.Request) (attachment.UploadInput, error) {
	// Refusing at the connection is cheaper than reading a gigabyte to say no.
	r.Body = http.MaxBytesReader(w, r.Body, attachment.MaxSize+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		return attachment.UploadInput{}, ErrBadRequest("Send the file as multipart form data in a part named file.")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return attachment.UploadInput{}, ErrBadRequest("The upload has no part named file.")
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return attachment.UploadInput{}, attachment.ErrTooLarge
			}
			return attachment.UploadInput{}, ErrBadRequest("The upload could not be read.")
		}
		if part.FormName() != "file" {
			continue
		}
		return attachment.UploadInput{
			FileName:    part.FileName(),
			ContentType: part.Header.Get("Content-Type"),
			Body:        part,
		}, nil
	}
}

// uploadError turns the body cap tripping mid-stream into the size refusal.
func uploadError(err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return attachment.ErrTooLarge
	}
	return err
}

// handleDownloadAttachment streams the bytes. Everything is sent as a download
// with the stored type; an HTML file is never rendered from this origin.
func (s *Server) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("attachmentID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid attachment id."))
		return
	}
	found, body, err := s.Attachments.Open(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	serveAttachment(w, r, found, body)
}

// serveAttachment writes the download headers and streams the bytes, closing
// the reader. Every route that hands a file out says the same things about it.
func serveAttachment(w http.ResponseWriter, r *http.Request, found *attachment.Attachment, body io.ReadCloser) {
	defer body.Close()
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" && isSafeInline(found.ContentType) {
		disposition = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", found.ContentType)
	h.Set("Content-Length", strconv.FormatInt(found.Size, 10))
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": found.FileName}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=0")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// isSafeInline says which types a browser may show in place: images and PDFs,
// which cannot run script against this origin. Anything else downloads.
func isSafeInline(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/plain":
		return true
	}
	return false
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("attachmentID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid attachment id."))
		return
	}
	found, err := s.Attachments.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	// The route knows the caller may write issues somewhere; this is the
	// check that it is this issue's project.
	projectKey, _, err := issue.ParseKey(found.IssueKey)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !PermsFrom(r.Context()).Can(perm.IssueWrite, projectKey) {
		respondError(w, r, forbidden(perm.IssueWrite))
		return
	}
	lsn, err := s.Attachments.Delete(r.Context(), id, actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
