//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A description is written, rewritten and cleared through the API. Clearing
// is the case that matters: a JSON null has to reach the service as "clear",
// not vanish into "not mentioned".
func TestADescriptionCanBeWrittenAndCleared(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "describer")
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Described", "key": "DSC"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	doc := func(text string) map[string]any {
		return map[string]any{"type": "doc", "content": []any{
			map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}},
		}}
	}

	issued := owner.post("/api/v1/projects/DSC/issues", map[string]any{"summary": "with words", "description": doc("first draft")})
	if issued.Status != http.StatusCreated {
		t.Fatalf("create issue: %d %s", issued.Status, issued.Raw)
	}
	key := issued.Body["issue"].(map[string]any)["key"].(string)
	if issued.Body["issue"].(map[string]any)["description"] == nil {
		t.Fatalf("the description given at creation is missing: %s", issued.Raw)
	}

	edited := owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{"description": doc("second draft")})
	if edited.Status != http.StatusOK {
		t.Fatalf("edit: %d %s", edited.Status, edited.Raw)
	}

	cleared := owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{"description": nil})
	if cleared.Status != http.StatusOK {
		t.Fatalf("clear: %d %s", cleared.Status, cleared.Raw)
	}
	if _, still := cleared.Body["issue"].(map[string]any)["description"]; still {
		t.Fatalf("a null should clear the description, but it is still there: %s", cleared.Raw)
	}
	again := owner.get("/api/v1/issues/" + key)
	if _, still := again.Body["issue"].(map[string]any)["description"]; still {
		t.Fatalf("the cleared description came back on read: %s", again.Raw)
	}

	// Not mentioning the description leaves it alone, which is the other half.
	owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{"description": doc("third")})
	untouched := owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{"summary": "renamed"})
	if untouched.Body["issue"].(map[string]any)["description"] == nil {
		t.Fatalf("an edit that does not mention the description must not clear it: %s", untouched.Raw)
	}

	history := owner.get("/api/v1/issues/" + key + "/history")
	if !strings.Contains(history.Raw, `"to":"cleared"`) {
		t.Fatalf("the changelog should say the description was cleared: %s", history.Raw)
	}

	// The database holds only what the renderer can show, and says why not.
	refused := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{"description": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "table"}}}}), http.StatusUnprocessableEntity, "a table")
	if !strings.Contains(refused.Raw, `holds a \"table\", which this tracker cannot show`) {
		t.Fatalf("refusal = %s", refused.Raw)
	}
	// An editor that sends an empty paragraph has cleared the description.
	emptied := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{"description": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph"}}}}), http.StatusOK, "an empty paragraph")
	if _, still := emptied.Body["issue"].(map[string]any)["description"]; still {
		t.Fatalf("an empty document should clear the description: %s", emptied.Raw)
	}

	// Naming somebody in the description brings them onto the issue.
	slug := principalField(t, owner.get("/api/v1/auth/me"), "principal", "org", "slug").(string)
	var orgID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM org WHERE slug = $1`, slug).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	colleague := h.joinExisting(t, &workspace{orgID: orgID}, h.email(t, "named"), "member")
	mention := map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{
		map[string]any{"type": "text", "text": "Ask "},
		map[string]any{"type": "mention", "attrs": map[string]any{"id": colleague.String(), "label": "Invited Person"}},
	}}}}
	want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{"description": mention}), http.StatusOK, "a mention in the description")
	watchers := want(t, owner.get("/api/v1/issues/"+key+"/watchers"), http.StatusOK, "watchers").Raw
	if !strings.Contains(watchers, colleague.String()) {
		t.Fatalf("the person named in the description is not watching: %s", watchers)
	}
}
