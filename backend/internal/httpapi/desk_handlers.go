package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/issue"
)

// The agent side of the desk: request types, goals, the queue, and the clocks
// on an issue.

func (s *Server) handleListRequestTypes(w http.ResponseWriter, r *http.Request) {
	found, err := s.Desk.RequestTypes(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requestTypes": found})
}

type createRequestTypeRequest struct {
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	IssueTypeID     uuid.UUID  `json:"issueTypeId"`
	Priority        string     `json:"priority"`
	Category        string     `json:"category"`
	DetailsTemplate string     `json:"detailsTemplate"`
	TeamID          *uuid.UUID `json:"teamId"`
}

func (s *Server) handleCreateRequestType(w http.ResponseWriter, r *http.Request) {
	var req createRequestTypeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	created, lsn, err := s.Desk.CreateRequestType(r.Context(), r.PathValue("projectKey"), desk.RequestTypeInput{
		Name: req.Name, Description: req.Description, IssueTypeID: req.IssueTypeID, Priority: issue.Priority(req.Priority),
		Category: req.Category, DetailsTemplate: req.DetailsTemplate, TeamID: req.TeamID,
	}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"requestType": created})
}

// updateRequestTypeRequest changes what is sent and leaves out what is not;
// clearTeam takes the team away, which a missing teamId cannot say.
type updateRequestTypeRequest struct {
	Name            *string    `json:"name"`
	Description     *string    `json:"description"`
	Category        *string    `json:"category"`
	DetailsTemplate *string    `json:"detailsTemplate"`
	IssueTypeID     *uuid.UUID `json:"issueTypeId"`
	Priority        *string    `json:"priority"`
	TeamID          *uuid.UUID `json:"teamId"`
	ClearTeam       bool       `json:"clearTeam"`
}

func (s *Server) handleUpdateRequestType(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("requestTypeID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid request type id."))
		return
	}
	var req updateRequestTypeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in := desk.RequestTypeUpdate{
		Name: req.Name, Description: req.Description, Category: req.Category, DetailsTemplate: req.DetailsTemplate,
		IssueTypeID: req.IssueTypeID, TeamID: req.TeamID, ClearTeam: req.ClearTeam,
	}
	if req.Priority != nil {
		p := issue.Priority(*req.Priority)
		in.Priority = &p
	}
	updated, lsn, err := s.Desk.UpdateRequestType(r.Context(), id, in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"requestType": updated})
}

func (s *Server) handleDeleteRequestType(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("requestTypeID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid request type id."))
		return
	}
	lsn, err := s.Desk.DeleteRequestType(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	found, err := s.Desk.Policies(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"policies": found})
}

type updatePolicyRequest struct {
	Goals          map[issue.Priority]int `json:"goals"`
	PauseStatusIDs *[]uuid.UUID           `json:"pauseStatusIds"`
	// UseCalendar counts the goal in the project's business hours only.
	UseCalendar *bool `json:"useCalendar,omitempty"`
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("policyID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid policy id."))
		return
	}
	var req updatePolicyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Desk.UpdatePolicy(r.Context(), id, desk.PolicyInput{Goals: req.Goals, PauseStatusIDs: req.PauseStatusIDs, UseCalendar: req.UseCalendar}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"policy": updated})
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Desk.Queue(r.Context(), r.PathValue("projectKey"), desk.QueueFilter(r.URL.Query().Get("filter")), userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rows": rows})
}

func (s *Server) handleIssueTimers(w http.ResponseWriter, r *http.Request) {
	timers, err := s.Desk.TimersFor(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	if timers == nil {
		timers = []desk.Timer{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"timers": timers})
}

// handleAddNote posts an internal note, which the customer is never shown.
// It takes what a comment takes: a document, or text to wrap in one.
func (s *Server) handleAddNote(w http.ResponseWriter, r *http.Request) {
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
	note, lsn, err := s.Issues.AddNote(r.Context(), r.PathValue("issueKey"), body, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"comment": note})
}

// The customer's side: the portal.

func (s *Server) handlePortalDesks(w http.ResponseWriter, r *http.Request) {
	desks, err := s.Desk.Desks(r.Context(), actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"desks": desks})
}

type portalRaiseRequest struct {
	RequestTypeID uuid.UUID `json:"requestTypeId"`
	Summary       string    `json:"summary"`
	Description   string    `json:"description"`
}

func (s *Server) handlePortalRaise(w http.ResponseWriter, r *http.Request) {
	var req portalRaiseRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.RequestTypeID == uuid.Nil {
		respondError(w, r, ErrBadRequest("What kind of request is this?"))
		return
	}
	raised, lsn, err := s.Desk.Raise(r.Context(), desk.RaiseInput{
		RequestTypeID: req.RequestTypeID, Summary: req.Summary, Description: req.Description,
	}, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"request": raised})
}

func (s *Server) handlePortalRequests(w http.ResponseWriter, r *http.Request) {
	found, err := s.Desk.MyRequests(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requests": found})
}

func (s *Server) handlePortalRequest(w http.ResponseWriter, r *http.Request) {
	found, err := s.Desk.Request(r.Context(), r.PathValue("issueKey"), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, found)
}

type portalReplyRequest struct {
	Text string `json:"text"`
}

func (s *Server) handlePortalReply(w http.ResponseWriter, r *http.Request) {
	var req portalReplyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.Text == "" {
		respondError(w, r, ErrBadRequest("A reply needs some text."))
		return
	}
	comment, lsn, err := s.Desk.Reply(r.Context(), r.PathValue("issueKey"), req.Text, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"comment": comment})
}

// requireAgent keeps customers on the portal. A customer holds no role in the
// permission table, so most of the agent API would refuse them anyway; this
// makes the refusal uniform, and covers the reads that ask for no permission.
func requireAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if strings.HasPrefix(r.URL.Path, "/api/v1/portal/") {
			next.ServeHTTP(w, r)
			return
		}
		if p == nil || !p.Role.IsAgent() {
			respondError(w, r, &APIError{Status: http.StatusForbidden, Code: "portal_only",
				Message: "Customers use the portal."})
			return
		}
		next.ServeHTTP(w, r)
	})
}
