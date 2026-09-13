package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/team"
)

// A team is the people inside a project who work together. A board scoped to a
// team draws that team's work, and its backlog is that work with no sprint, so
// a project with two teams has two boards and two backlogs.

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	found, err := s.Teams.List(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"teams": found})
}

func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("teamID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid team id."))
		return
	}
	found, err := s.Teams.ByID(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"team": found})
}

type teamRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	// WeeklyCapacity is left alone when absent, cleared when null.
	WeeklyCapacity numberPatch `json:"weeklyCapacity"`
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	var req teamRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	in := team.CreateInput{Name: req.Name}
	if req.Description != nil {
		in.Description = *req.Description
	}

	created, lsn, err := s.Teams.Create(r.Context(), r.PathValue("projectKey"), in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"team": created})
}

func (s *Server) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("teamID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid team id."))
		return
	}
	var req teamRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	var in team.UpdateInput
	if req.Name != "" {
		in.Name = &req.Name
	}
	in.Description = req.Description
	if req.WeeklyCapacity.Set {
		in.SetCapacity = true
		in.WeeklyCapacity = req.WeeklyCapacity.Value
	}

	updated, lsn, err := s.Teams.Update(r.Context(), id, in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"team": updated})
}

func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("teamID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid team id."))
		return
	}
	lsn, err := s.Teams.Delete(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type addTeamMemberRequest struct {
	UserID uuid.UUID `json:"userId"`
	Lead   bool      `json:"lead"`
}

func (s *Server) handleAddTeamMember(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("teamID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid team id."))
		return
	}
	var req addTeamMemberRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.UserID == uuid.Nil {
		respondError(w, r, ErrBadRequest("Who should be added to the team?"))
		return
	}

	updated, lsn, err := s.Teams.AddMember(r.Context(), id, req.UserID, req.Lead, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"team": updated})
}

func (s *Server) handleRemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("teamID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid team id."))
		return
	}
	userID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid user id."))
		return
	}

	updated, lsn, err := s.Teams.RemoveMember(r.Context(), id, userID, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"team": updated})
}

type setIssueTeamRequest struct {
	TeamID *uuid.UUID `json:"teamId"`
}

// handleSetIssueTeam hands an issue to a team. A null team is not an omission:
// it is the work going back to the project at large.
func (s *Server) handleSetIssueTeam(w http.ResponseWriter, r *http.Request) {
	var req setIssueTeamRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	updated, lsn, err := s.Issues.SetTeam(r.Context(), r.PathValue("issueKey"), req.TeamID, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}

// ------------------------------------------------------------- boards ------

func (s *Server) handleListBoards(w http.ResponseWriter, r *http.Request) {
	found, err := s.Boards.List(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"boards": found})
}

func (s *Server) handleGetBoardByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("boardID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid board id."))
		return
	}
	found, err := s.Boards.ByID(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"board": found})
}

// handleGetBacklog returns the work in a board's scope that is not in a sprint.
func (s *Server) handleGetBacklog(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("boardID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid board id."))
		return
	}
	cards, err := s.Boards.Backlog(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"cards": cards})
}

type boardRequest struct {
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Type        *board.Type     `json:"type"`
	GroupBy     *board.Grouping `json:"groupBy"`
	TeamID      uuidPatch       `json:"teamId"`
}

func (s *Server) handleCreateBoard(w http.ResponseWriter, r *http.Request) {
	var req boardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	in := board.CreateInput{Name: req.Name}
	if req.Description != nil {
		in.Description = *req.Description
	}
	if req.Type != nil {
		in.Type = *req.Type
	}
	if req.TeamID.Set {
		in.TeamID = req.TeamID.Value
	}

	created, lsn, err := s.Boards.CreateBoard(r.Context(), r.PathValue("projectKey"), in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"board": created})
}

func (s *Server) handleUpdateBoardByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("boardID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid board id."))
		return
	}
	var req boardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	var in board.UpdateBoardInput
	if req.Name != "" {
		in.Name = &req.Name
	}
	in.Description = req.Description
	in.Type = req.Type
	in.GroupBy = req.GroupBy
	if req.TeamID.Set {
		value := req.TeamID.Value
		in.TeamID = &value
	}

	updated, lsn, err := s.Boards.UpdateBoard(r.Context(), id, in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"board": updated})
}

func (s *Server) handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("boardID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid board id."))
		return
	}
	lsn, err := s.Boards.DeleteBoard(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
