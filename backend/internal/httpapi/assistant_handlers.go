package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/armature/armature/backend/internal/assistant"
)

// AsksPerMinute brakes one address: a model call is the dearest thing the api does.
const AsksPerMinute = 20

// A question carries the places the client can offer; both go into the model's
// prompt, so both are bounded and kept to this application.
const (
	maxPlaces     = 40
	maxPlaceRunes = 200
)

var asks = newThrottle(AsksPerMinute, time.Minute)

type assistantStatus struct {
	Configured bool `json:"configured"`
}

type askRequest struct {
	Question   string            `json:"question"`
	ProjectKey string            `json:"projectKey,omitempty"`
	Places     []assistant.Place `json:"places,omitempty"`
}

// proposal is a change the model asked for. It is the reader's to make, so it
// is described and handed back rather than done.
type proposal struct {
	Tool      string                     `json:"tool"`
	Says      string                     `json:"says"`
	Arguments map[string]json.RawMessage `json:"arguments,omitempty"`
}

func (s *Server) handleAssistantStatus(w http.ResponseWriter, r *http.Request) {
	_, configured := s.Assistant.(assistant.HTTPAsker)
	respondJSON(w, r, http.StatusOK, assistantStatus{Configured: configured})
}

// handleAsk lends the model the MCP tools as the caller: it reads what the
// caller could read, and what it would change comes back as a proposal.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var req askRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		respondError(w, r, ErrBadRequest("Ask a question first."))
		return
	}
	places, apiErr := safePlaces(req.Places)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if !asks.gate(w, r, "Too many questions from here. Wait a minute and ask again.") {
		return
	}
	if s.Assistant == nil {
		respondError(w, r, assistant.ErrUnavailable)
		return
	}
	runner := &toolRunner{s: s, r: r, proposals: []proposal{}}
	answer, err := s.Assistant.Ask(r.Context(), assistant.Question{Text: req.Question, ProjectKey: req.ProjectKey, Places: places}, runner)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"answer": answer, "proposals": runner.proposals})
}

// safePlaces keeps the client's places to paths of this application and to one
// line each: they are written into the model's prompt.
func safePlaces(places []assistant.Place) ([]assistant.Place, *APIError) {
	if len(places) > maxPlaces {
		return nil, ErrBadRequest("That is more places than a question needs.")
	}
	out := make([]assistant.Place, 0, len(places))
	for _, p := range places {
		if !strings.HasPrefix(p.Path, "/") || strings.HasPrefix(p.Path, "//") || strings.ContainsAny(p.Path, "\\\r\n") {
			return nil, ErrBadRequest("A place is a path of this application, such as /projects.")
		}
		p.ID, p.Title, p.Sentence = oneLine(p.ID), oneLine(p.Title), oneLine(p.Sentence)
		out = append(out, p)
	}
	return out, nil
}

func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
	if runes := []rune(s); len(runes) > maxPlaceRunes {
		return string(runes[:maxPlaceRunes])
	}
	return s
}

// toolRunner is the MCP tool catalogue lent to the model on behalf of one
// request, and the proposals the model made along the way.
type toolRunner struct {
	s         *Server
	r         *http.Request
	proposals []proposal
}

func (t *toolRunner) Tools() []assistant.Tool {
	out := []assistant.Tool{}
	for _, tool := range toolCatalog() {
		input, _ := json.Marshal(tool.Input)
		out = append(out, assistant.Tool{Name: tool.Name, Description: tool.Description, Input: input})
	}
	return out
}

// Call reads with a tool, or proposes a change. Text the model has read may
// come from anybody, so nothing it says can make a change on its own.
func (t *toolRunner) Call(ctx context.Context, name string, args json.RawMessage) (string, bool) {
	var arguments map[string]json.RawMessage
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return "The arguments were not an object.", true
		}
	}
	tool, known := toolByName(name)
	if !known {
		return "There is no tool " + name + ". Ask for the names.", true
	}
	if !tool.ReadOnly {
		if writeRefused(tool.Method, PrincipalFrom(t.r.Context())) {
			return ErrReadOnlyToken().Message, true
		}
		t.proposals = append(t.proposals, proposal{Tool: name, Says: tool.Description, Arguments: arguments})
		return "Proposed. Nothing changes until the reader confirms it, so tell them what you propose rather than that it is done.", false
	}
	result, rpcErr := t.s.callTool(t.r.WithContext(ctx), toolCallParams{Name: name, Arguments: arguments})
	if rpcErr != nil {
		return rpcErr.Message, true
	}
	out, ok := result.(toolResult)
	if !ok || len(out.Content) == 0 {
		return "Done.", false
	}
	return out.Content[0].Text, out.IsError
}
