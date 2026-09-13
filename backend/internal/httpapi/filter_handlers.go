package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bulk"
	"github.com/armature/armature/backend/internal/csvio"
	"github.com/armature/armature/backend/internal/filter"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/project"
)

// Saved filters: a query with a name, the reader's own or shared with the
// organization. Every route reads as the caller.

func (s *Server) handleListFilters(w http.ResponseWriter, r *http.Request) {
	filters, err := s.Filters.List(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"filters": filters})
}

func (s *Server) handleCreateFilter(w http.ResponseWriter, r *http.Request) {
	var req filter.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Filters.Create(r.Context(), userFrom(r), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"filter": made})
}

func (s *Server) handleGetFilter(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "filterID", "filter")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.Filters.Get(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"filter": found})
}

func (s *Server) handleUpdateFilter(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "filterID", "filter")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req filter.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Filters.Update(r.Context(), id, userFrom(r), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"filter": updated})
}

func (s *Server) handleDeleteFilter(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "filterID", "filter")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Filters.Delete(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleStarFilter(w http.ResponseWriter, r *http.Request) {
	s.starFilter(w, r, true)
}

func (s *Server) handleUnstarFilter(w http.ResponseWriter, r *http.Request) {
	s.starFilter(w, r, false)
}

func (s *Server) starFilter(w http.ResponseWriter, r *http.Request, on bool) {
	id, apiErr := pathUUID(r, "filterID", "filter")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	updated, lsn, err := s.Filters.Star(r.Context(), id, userFrom(r), on)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"filter": updated})
}

func (s *Server) handleSubscribeFilter(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "filterID", "filter")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req filter.Subscription
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Filters.Subscribe(r.Context(), id, userFrom(r), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"filter": updated})
}

func (s *Server) handleUnsubscribeFilter(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "filterID", "filter")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	updated, lsn, err := s.Filters.Unsubscribe(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"filter": updated})
}

// handleRunFilter lists what a saved filter matches, compiled as the caller.
func (s *Server) handleRunFilter(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "filterID", "filter")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.Filters.Get(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	compiled, err := compileQuery(found.Query, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	result, err := s.Issues.List(r.Context(), readableFilter(r, issue.Filter{Query: compiled}), issue.Page{Limit: queryInt(r, "limit", 50, 1, 200), Offset: queryInt(r, "offset", 0, 0, 1<<20)})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, result)
}

// Export: the search's result as a file.

func (s *Server) handleExportIssues(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	f := readableFilter(r, issue.Filter{ProjectKey: query.Get("project")})
	if q := query.Get("q"); q != "" {
		compiled, err := compileQuery(q, userFrom(r))
		if err != nil {
			respondError(w, r, err)
			return
		}
		f.Query = compiled
	}
	var columns []string
	if raw := strings.TrimSpace(query.Get("columns")); raw != "" {
		for _, c := range strings.Split(raw, ",") {
			if c = strings.TrimSpace(c); c != "" {
				columns = append(columns, c)
			}
		}
	}
	var buf bytes.Buffer
	if err := s.CSV.Export(r.Context(), &buf, f, columns); err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="issues.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// Bulk: many issues, one request, each its own transaction.

type bulkRequest struct {
	Keys   []string   `json:"keys"`
	Change bulk.Input `json:"change"`
}

func (s *Server) handleBulkEdit(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	// Every key has to be in a project the caller may make this change in: the
	// same permissions the one issue routes ask for, not IssueWrite for all.
	needed := []perm.Permission{perm.IssueWrite}
	if req.Change.SprintID != nil {
		needed = append(needed, perm.SprintManage)
	}
	if req.Change.TeamID != nil {
		needed = append(needed, perm.TeamManage)
	}
	if strings.TrimSpace(req.Change.Transition) != "" {
		needed = append(needed, perm.IssueTransition)
	}
	perms := PermsFrom(r.Context())
	for _, key := range req.Keys {
		projectKey, _, err := issue.ParseKey(key)
		if err != nil {
			respondError(w, r, ErrBadRequest(capitalize(err.Error())+"."))
			return
		}
		for _, required := range needed {
			if perms.Can(required, project.NormalizeKey(projectKey)) {
				continue
			}
			if required == perm.IssueWrite {
				respondError(w, r, ErrForbidden("You cannot change issues in "+projectKey+"."))
			} else {
				respondError(w, r, forbidden(required))
			}
			return
		}
	}
	result, lsn, err := s.Bulk.Edit(r.Context(), req.Keys, req.Change, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, result)
}

// Import: a file in, mapped, tried dry first.

// maxImportUpload bounds the multipart body around the file itself.
const maxImportUpload = csvio.MaxImportBytes + 64<<10

// upload is the file and the decisions sent beside it.
type upload struct {
	data    []byte
	name    string
	mapping csvio.Mapping
	values  csvio.Values
	people  csvio.People
}

func (s *Server) readImportFile(w http.ResponseWriter, r *http.Request) (*upload, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportUpload)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, ErrBadRequest("Send the file as multipart form data in a part named file.")
	}
	out := &upload{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrBadRequest("The upload could not be read.")
		}
		raw, err := io.ReadAll(part)
		if err != nil {
			return nil, ErrBadRequest("The file is too large; keep it under 8 MB.")
		}
		switch part.FormName() {
		case "file":
			out.data, out.name = raw, part.FileName()
		case "mapping":
			if err := decodePart(raw, &out.mapping); err != nil {
				return nil, ErrBadRequest("The mapping has to be a JSON object of target to column numbers.")
			}
		case "values":
			if err := decodePart(raw, &out.values); err != nil {
				return nil, ErrBadRequest("The values have to be a JSON object of target to what each word means here.")
			}
		case "people":
			if err := decodePart(raw, &out.people); err != nil {
				return nil, ErrBadRequest("The people have to say, per name, which member they are or that an account is made.")
			}
		}
	}
	if out.data == nil {
		return nil, ErrBadRequest("The upload has no part named file.")
	}
	return out, nil
}

// decodePart reads one JSON part of the upload; an absent part is not an error.
func decodePart(raw []byte, into any) error {
	if strings.TrimSpace(string(raw)) == "" {
		return nil
	}
	return json.Unmarshal(raw, into)
}

// importPreview is what the file holds, and the mapping to start from.
type importPreview struct {
	Preview csvio.Preview `json:"preview"`
	Mapping csvio.Mapping `json:"mapping"`
	Targets []string      `json:"targets"`
}

func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	up, err := s.readImportFile(w, r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	preview, mapping, err := s.CSV.Preview(r.Context(), r.PathValue("projectKey"), up.data, up.mapping)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, importPreview{Preview: *preview, Mapping: mapping, Targets: csvio.Targets})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	up, err := s.readImportFile(w, r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	req := csvio.ImportRequest{
		Mapping: up.mapping, Values: up.values, People: up.people,
		DryRun: r.URL.Query().Get("dryRun") == "true", Source: up.name,
	}
	report, lsn, err := s.CSV.Import(r.Context(), r.PathValue("projectKey"), up.data, req, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"report": report})
}

// Clone and move.

func (s *Server) handleCloneIssue(w http.ResponseWriter, r *http.Request) {
	var req issue.CloneOptions
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			respondError(w, r, err)
			return
		}
	}
	made, lsn, err := s.Issues.Clone(r.Context(), r.PathValue("issueKey"), req, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"issue": made})
}

type moveIssueRequest struct {
	ProjectKey string     `json:"projectKey"`
	StatusID   *uuid.UUID `json:"statusId,omitempty"`
}

func (s *Server) handleMoveIssue(w http.ResponseWriter, r *http.Request) {
	var req moveIssueRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.ProjectKey == "" {
		respondError(w, r, ErrBadRequest("Say which project the issue moves to."))
		return
	}
	// Moving out of one project and into another takes writing in both.
	if !PermsFrom(r.Context()).Can(perm.IssueWrite, project.NormalizeKey(req.ProjectKey)) {
		respondError(w, r, ErrForbidden("You cannot file issues in "+req.ProjectKey+"."))
		return
	}
	moved, lsn, err := s.Issues.Move(r.Context(), r.PathValue("issueKey"), issue.MoveInput{ProjectKey: req.ProjectKey, StatusID: req.StatusID}, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": moved})
}
