package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// actorFrom builds the issue service's notion of who is acting.
func actorFrom(r *http.Request) issue.Actor {
	p := PrincipalFrom(r.Context())
	if p == nil {
		return issue.Actor{}
	}
	return issue.Actor{UserID: p.User.ID, OrgRole: p.Role}
}

type createIssueRequest struct {
	ProjectKey  string          `json:"projectKey,omitempty"`
	TypeID      *uuid.UUID      `json:"typeId,omitempty"`
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description,omitempty"`
	Priority    string          `json:"priority,omitempty"`
	AssigneeID  *uuid.UUID      `json:"assigneeId,omitempty"`
	ParentKey   string          `json:"parentKey,omitempty"`
	StartDate   datePatch       `json:"startDate,omitempty"`
	DueDate     datePatch       `json:"dueDate,omitempty"`
	// TeamID, Estimate and SprintID let the plan file an issue where it will
	// sit in one request. Committing to a sprint takes the sprint permission.
	TeamID   *uuid.UUID `json:"teamId,omitempty"`
	Estimate *float64   `json:"estimate,omitempty"`
	SprintID *uuid.UUID `json:"sprintId,omitempty"`
	// ComponentIDs put the issue in the project's parts as it is filed; the
	// first with a default assignee takes it when nobody was named.
	ComponentIDs []uuid.UUID `json:"componentIds,omitempty"`
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	var req createIssueRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	// The project can come from the path or the body; the path wins, because
	// that is the resource being posted to.
	projectKey := r.PathValue("projectKey")
	if projectKey == "" {
		projectKey = req.ProjectKey
	}
	if projectKey == "" {
		respondError(w, r, ErrBadRequest("Which project should this issue go in?"))
		return
	}
	// A project named in the path was checked by the route; one named in the
	// body is checked here, or filing into any project would take writing in one.
	if r.PathValue("projectKey") == "" {
		perms := PermsFrom(r.Context())
		named := project.NormalizeKey(projectKey)
		if !perms.Can(perm.Read, named) {
			respondError(w, r, ErrNotFound("That project was not found."))
			return
		}
		if !perms.Can(perm.IssueWrite, named) {
			respondError(w, r, forbidden(perm.IssueWrite))
			return
		}
	}

	in := issue.CreateInput{
		ProjectKey:   projectKey,
		Summary:      req.Summary,
		Description:  req.Description,
		Priority:     issue.Priority(req.Priority),
		AssigneeID:   req.AssigneeID,
		ParentKey:    req.ParentKey,
		StartDate:    req.StartDate.Value,
		DueDate:      req.DueDate.Value,
		TeamID:       req.TeamID,
		Estimate:     req.Estimate,
		SprintID:     req.SprintID,
		ComponentIDs: req.ComponentIDs,
	}
	if req.TypeID != nil {
		in.TypeID = *req.TypeID
	}
	if req.SprintID != nil && !PermsFrom(r.Context()).Can(perm.SprintManage, project.NormalizeKey(projectKey)) {
		respondError(w, r, forbidden(perm.SprintManage))
		return
	}

	created, lsn, err := s.Issues.Create(r.Context(), in, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"issue": created})
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	found, err := s.Issues.ByKey(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": found})
}

type updateIssueRequest struct {
	Summary     *string    `json:"summary,omitempty"`
	Description docPatch   `json:"description,omitempty"`
	Priority    *string    `json:"priority,omitempty"`
	TypeID      *uuid.UUID `json:"typeId,omitempty"`
	// Assignee and DueDate accept an explicit null to clear the field, which a
	// plain omitted field cannot express.
	Assignee assigneePatch `json:"assigneeId,omitempty"`
	DueDate  datePatch     `json:"dueDate,omitempty"`
	// ReporterID hands authorship to another member.
	ReporterID *uuid.UUID `json:"reporterId,omitempty"`
	// The time fields are minutes; null clears one.
	TimeEstimateMinutes  minutesPatch `json:"timeEstimateMinutes,omitempty"`
	TimeRemainingMinutes minutesPatch `json:"timeRemainingMinutes,omitempty"`
}

// minutesPatch is a whole number of minutes, or null to clear, or absent.
type minutesPatch struct {
	Set   bool
	Value *int
}

func (m *minutesPatch) UnmarshalJSON(data []byte) error {
	m.Set = true
	if string(data) == "null" {
		m.Value = nil
		return nil
	}
	var raw float64
	if err := jsonUnmarshal(data, &raw); err != nil || raw != float64(int(raw)) {
		return errors.New("this has to be a whole number of minutes")
	}
	v := int(raw)
	m.Value = &v
	return nil
}

type assigneePatch struct {
	Set   bool
	Value *uuid.UUID
}

// docPatch tells "leave the description alone" apart from "clear it". A plain
// pointer cannot: the decoder turns a JSON null into a nil pointer, which reads
// as not mentioned, and a description could then never be cleared.
type docPatch struct {
	Set   bool
	Value json.RawMessage
}

func (d *docPatch) UnmarshalJSON(data []byte) error {
	d.Set = true
	if string(data) == "null" {
		d.Value = nil
		return nil
	}
	d.Value = append(json.RawMessage(nil), data...)
	return nil
}

func (a *assigneePatch) UnmarshalJSON(data []byte) error {
	a.Set = true
	if string(data) == "null" {
		a.Value = nil
		return nil
	}
	var id uuid.UUID
	if err := jsonUnmarshal(data, &id); err != nil {
		return err
	}
	a.Value = &id
	return nil
}

// datePatch is a calendar day. It accepts a bare date as well as a timestamp,
// because a client that only knows a day should not have to invent a time.
// uuidPatch tells "leave it alone" apart from "clear it" for an id, which is
// the difference between not mentioning a team and taking work off one.
type uuidPatch struct {
	Set   bool
	Value *uuid.UUID
}

func (u *uuidPatch) UnmarshalJSON(data []byte) error {
	u.Set = true
	if string(data) == "null" {
		u.Value = nil
		return nil
	}
	var raw uuid.UUID
	if err := jsonUnmarshal(data, &raw); err != nil {
		return errors.New("this has to be an id")
	}
	u.Value = &raw
	return nil
}

// numberPatch is the same idea for a number: an explicit null clears it, an
// omitted field leaves it alone, and the two are not the same thing.
type numberPatch struct {
	Set   bool
	Value *float64
}

func (n *numberPatch) UnmarshalJSON(data []byte) error {
	n.Set = true
	if string(data) == "null" {
		n.Value = nil
		return nil
	}
	var raw float64
	if err := jsonUnmarshal(data, &raw); err != nil {
		return errors.New("this has to be a number, such as 5 or 2.5")
	}
	n.Value = &raw
	return nil
}

type datePatch struct {
	Set   bool
	Value *time.Time
}

// dateLayouts are tried in order. The plain day comes first: it is what the
// planning client sends, and what a date means when nobody said otherwise.
var dateLayouts = []string{"2006-01-02", time.RFC3339}

func (d *datePatch) UnmarshalJSON(data []byte) error {
	d.Set = true
	if string(data) == "null" {
		d.Value = nil
		return nil
	}

	var raw string
	if err := jsonUnmarshal(data, &raw); err != nil {
		return errors.New("a date has to be a string such as 2026-01-31")
	}
	for _, layout := range dateLayouts {
		t, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		// The time of day is dropped on purpose: keeping the caller's clock
		// would make "the 4th" fall on different days in two time zones.
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		d.Value = &day
		return nil
	}
	return fmt.Errorf("%q is not a date; use YYYY-MM-DD", raw)
}

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	var req updateIssueRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	in := issue.UpdateInput{
		Summary: req.Summary,
		TypeID:  req.TypeID,
	}
	if req.Description.Set {
		value := req.Description.Value
		in.Description = &value
	}
	if req.Priority != nil {
		priority := issue.Priority(*req.Priority)
		in.Priority = &priority
	}
	if req.Assignee.Set {
		value := req.Assignee.Value
		in.Assignee = &value
	}
	if req.DueDate.Set {
		value := req.DueDate.Value
		in.DueDate = &value
	}
	in.ReporterID = req.ReporterID
	if req.TimeEstimateMinutes.Set {
		value := req.TimeEstimateMinutes.Value
		in.TimeEstimate = &value
	}
	if req.TimeRemainingMinutes.Set {
		value := req.TimeRemainingMinutes.Value
		in.TimeRemaining = &value
	}

	updated, lsn, err := s.Issues.Update(r.Context(), r.PathValue("issueKey"), in, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}

func (s *Server) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Issues.Delete(r.Context(), r.PathValue("issueKey"), actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleListTransitions returns the moves this caller may make right now.
func (s *Server) handleListTransitions(w http.ResponseWriter, r *http.Request) {
	transitions, err := s.Issues.Transitions(r.Context(), r.PathValue("issueKey"), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	if transitions == nil {
		transitions = []workflow.Transition{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"transitions": transitions})
}

type transitionRequest struct {
	TransitionID uuid.UUID  `json:"transitionId"`
	Comment      string     `json:"comment,omitempty"`
	AssigneeID   *uuid.UUID `json:"assigneeId,omitempty"`
	Resolution   string     `json:"resolution,omitempty"`
}

func (s *Server) handleTransitionIssue(w http.ResponseWriter, r *http.Request) {
	var req transitionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.TransitionID == uuid.Nil {
		respondError(w, r, ErrBadRequest("Which transition should be taken?"))
		return
	}

	updated, lsn, err := s.Issues.Transition(r.Context(), r.PathValue("issueKey"), issue.TransitionInput{
		TransitionID: req.TransitionID,
		Comment:      req.Comment,
		Assignee:     req.AssigneeID,
		Resolution:   req.Resolution,
	}, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}

type commentRequest struct {
	Body json.RawMessage `json:"body,omitempty"`
	// Text is a convenience for clients that have no rich text editor yet; it
	// is wrapped into a document server side.
	Text string `json:"text,omitempty"`
}

// document returns the body to store, accepting either a rich text document or
// plain text.
func (c commentRequest) document() (json.RawMessage, error) {
	if len(c.Body) > 0 {
		return c.Body, nil
	}
	if strings.TrimSpace(c.Text) == "" {
		return nil, ErrBadRequest("A comment needs some text.")
	}
	return issue.TextDocument(c.Text), nil
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	var req commentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	body, err := req.document()
	if err != nil {
		respondError(w, r, err)
		return
	}

	comment, lsn, err := s.Issues.AddComment(r.Context(), r.PathValue("issueKey"), body, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"comment": comment})
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	// Notes are for agents; a customer reading their own request through the
	// agent API would be refused before this anyway.
	comments, err := s.Issues.Comments(r.Context(), r.PathValue("issueKey"), PrincipalFrom(r.Context()).Role.IsAgent())
	if err != nil {
		respondError(w, r, err)
		return
	}
	if comments == nil {
		comments = []issue.Comment{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"comments": comments})
}

func (s *Server) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("commentID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid comment id."))
		return
	}

	var req commentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	body, err := req.document()
	if err != nil {
		respondError(w, r, err)
		return
	}

	comment, lsn, err := s.Issues.EditComment(r.Context(), id, body, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"comment": comment})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("commentID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid comment id."))
		return
	}
	lsn, err := s.Issues.DeleteComment(r.Context(), id, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleIssueHistory(w http.ResponseWriter, r *http.Request) {
	history, err := s.Issues.History(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	if history == nil {
		history = []issue.HistoryEntry{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"history": history})
}

func (s *Server) handleIssueChildren(w http.ResponseWriter, r *http.Request) {
	children, err := s.Issues.Children(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"children": children})
}

// handleIssueHierarchy answers everything the issue's place in the tree needs
// in one request: what is above it, what is under it, and how that is going.
func (s *Server) handleIssueHierarchy(w http.ResponseWriter, r *http.Request) {
	hierarchy, err := s.Issues.Hierarchy(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, hierarchy)
}

// parentRequest carries the new parent. A null is a deliberate detach, which
// an omitted field cannot express.
type parentRequest struct {
	ParentKey *string `json:"parentKey"`
}

func (s *Server) handleSetParent(w http.ResponseWriter, r *http.Request) {
	var req parentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	updated, lsn, err := s.Issues.SetParent(r.Context(), r.PathValue("issueKey"), req.ParentKey, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}

func (s *Server) handleProjectHierarchy(w http.ResponseWriter, r *http.Request) {
	tree, err := s.Issues.Tree(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tree": tree})
}

var _ = auth.RoleMember
