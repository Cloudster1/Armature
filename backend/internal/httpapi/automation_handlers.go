package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/armature/armature/backend/internal/automation"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/webhook"
)

// Rules: what a project, or the organization, does by itself. Configuring a
// project's rules is administering the project; the organization's, the tenant.

// maxIncomingBytes bounds what an incoming hook may post.
const maxIncomingBytes = 256 << 10

func (s *Server) handleAutomationCatalog(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"catalog": automation.Catalog(), "topics": webhookTopics()})
}

func (s *Server) handleListProjectRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Automation.List(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) handleCreateProjectRule(w http.ResponseWriter, r *http.Request) {
	s.createRule(w, r, r.PathValue("projectKey"))
}

func (s *Server) handleListOrgRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Automation.List(r.Context(), "")
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) handleCreateOrgRule(w http.ResponseWriter, r *http.Request) {
	s.createRule(w, r, "")
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request, projectKey string) {
	var req automation.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	rule, lsn, err := s.Automation.Create(r.Context(), projectKey, req, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"rule": rule})
}

// ruleFor loads a rule addressed by id and checks the caller may configure
// where it lives: the project, or the organization for a rule with none.
func (s *Server) ruleFor(w http.ResponseWriter, r *http.Request) (*automation.Rule, bool) {
	id, apiErr := pathUUID(r, "ruleID", "rule")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return nil, false
	}
	rule, err := s.Automation.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return nil, false
	}
	perms := PermsFrom(r.Context())
	allowed := perms.CanInOrg(perm.OrgAdminister)
	if rule.ProjectKey != "" {
		allowed = perms.Can(perm.ProjectAdminister, rule.ProjectKey)
	}
	if !allowed {
		respondError(w, r, ErrForbidden("Only an administrator of where this rule lives can touch it."))
		return nil, false
	}
	return rule, true
}

func (s *Server) handleGetRule(w http.ResponseWriter, r *http.Request) {
	rule, ok := s.ruleFor(w, r)
	if !ok {
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rule": rule})
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	rule, ok := s.ruleFor(w, r)
	if !ok {
		return
	}
	var req automation.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Automation.Update(r.Context(), rule.ID, req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"rule": updated})
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	rule, ok := s.ruleFor(w, r)
	if !ok {
		return
	}
	lsn, err := s.Automation.Delete(r.Context(), rule.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleRuleRuns(w http.ResponseWriter, r *http.Request) {
	rule, ok := s.ruleFor(w, r)
	if !ok {
		return
	}
	runs, err := s.Automation.Runs(r.Context(), rule.ID, queryInt(r, "limit", automation.DefaultRuns, 1, 200))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"runs": runs})
}

// runRuleRequest names the issue to run against; empty runs with none.
type runRuleRequest struct {
	IssueKey string `json:"issueKey,omitempty"`
}

func (s *Server) handleRunRule(w http.ResponseWriter, r *http.Request) {
	rule, ok := s.ruleFor(w, r)
	if !ok {
		return
	}
	var req runRuleRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			respondError(w, r, err)
			return
		}
	}
	run, err := s.Automation.RunNow(r.Context(), rule.ID, req.IssueKey, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"run": run})
}

// handleIncomingHook is what a rule's address answers. The token is the whole
// of the authorization: whoever knows the address may call it.
func (s *Server) handleIncomingHook(w http.ResponseWriter, r *http.Request) {
	rule, orgID, err := s.Automation.ByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		respondError(w, r, ErrNotFound("There is no rule at this address."))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxIncomingBytes))
	if err != nil {
		respondError(w, r, ErrBadRequest("The body is too large; keep it under 256 KB."))
		return
	}
	if len(body) > 0 && !json.Valid(body) {
		respondError(w, r, ErrBadRequest("The body has to be JSON."))
		return
	}
	if err := s.Automation.Incoming(r.Context(), rule, orgID, body); err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, incomingReceipt{Rule: rule.Name, Accepted: true})
}

type incomingReceipt struct {
	Rule     string `json:"rule"`
	Accepted bool   `json:"accepted"`
}

// Webhooks: where the organization's events go. Administering the tenant.

func webhookTopics() []string {
	out := []string{webhook.TopicAny}
	for _, t := range events.Topics {
		if !strings.HasPrefix(t, "automation.") {
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	endpoints, err := s.Webhooks.List(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"webhooks": endpoints})
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req webhook.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Webhooks.Create(r.Context(), req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"webhook": made})
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "endpointID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req webhook.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Webhooks.Update(r.Context(), id, req)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"webhook": updated})
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "endpointID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Webhooks.Delete(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleRotateWebhookEndpointSecret(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "endpointID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	rotated, lsn, err := s.Webhooks.RotateSecret(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"webhook": rotated})
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "endpointID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	delivery, lsn, err := s.Webhooks.Test(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"delivery": delivery})
}

func (s *Server) handleWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "endpointID", "webhook")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	deliveries, err := s.Webhooks.Deliveries(r.Context(), id, queryInt(r, "limit", webhook.DefaultDeliveries, 1, 200))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"deliveries": deliveries})
}

func (s *Server) handleRedeliverWebhook(w http.ResponseWriter, r *http.Request) {
	deliveryID, apiErr := pathUUID(r, "deliveryID", "delivery")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	delivery, lsn, err := s.Webhooks.Redeliver(r.Context(), deliveryID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"delivery": delivery})
}
