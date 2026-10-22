//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// A description and a comment are written in Markdown, as a client without
// the editor's document sends them, and read back as the document it makes.
func TestADescriptionAndACommentCanBeWrittenInMarkdown(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	key := h.projectKey(t, "DMD")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "markdown writer", auth.RoleOwner))
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Markdown", "key": key}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	description := func(body map[string]any) string {
		t.Helper()
		raw, _ := json.Marshal(body["issue"].(map[string]any)["description"])
		return string(raw)
	}

	issued := want(t, owner.post("/api/v1/projects/"+key+"/issues", map[string]any{
		"summary":             "written in Markdown",
		"descriptionMarkdown": "### Describe the bug\n\nIt **fails**:\n\n- one\n- two\n\n```go\nfmt.Println()\n```\n\nSee [the docs](https://example.com/docs).",
	}), http.StatusCreated, "an issue in Markdown")
	issueKey := issued.Body["issue"].(map[string]any)["key"].(string)
	got := description(issued.Body)
	for _, part := range []string{`"type":"heading","attrs":{"level":3}`, `"text":"Describe the bug"`, `"type":"bold"`, `"type":"bulletList"`, `"type":"codeBlock","attrs":{"language":"go"}`, `"href":"https://example.com/docs"`} {
		if !strings.Contains(got, part) {
			t.Errorf("the description lacks %s: %s", part, got)
		}
	}
	if strings.Contains(got, "###") {
		t.Errorf("the heading was kept as its Markdown: %s", got)
	}

	rewritten := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+issueKey, map[string]any{"descriptionMarkdown": "Now *shorter*."}), http.StatusOK, "a Markdown edit")
	if got := description(rewritten.Body); !strings.Contains(got, `"type":"italic"`) || strings.Contains(got, "Describe the bug") {
		t.Errorf("the rewritten description is %s", got)
	}
	cleared := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+issueKey, map[string]any{"descriptionMarkdown": ""}), http.StatusOK, "an empty Markdown edit")
	if _, still := cleared.Body["issue"].(map[string]any)["description"]; still {
		t.Errorf("empty Markdown should clear the description: %s", cleared.Raw)
	}
	both := map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "x"}}}}}
	refused := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+issueKey, map[string]any{"description": both, "descriptionMarkdown": "x"}), http.StatusBadRequest, "a description sent twice")
	if !strings.Contains(refused.Raw, "not both") {
		t.Errorf("refusal = %s", refused.Raw)
	}
	want(t, owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "twice", "description": both, "descriptionMarkdown": "x"}), http.StatusBadRequest, "an issue filed with two descriptions")

	comment := want(t, owner.post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"bodyMarkdown": "Fixed in `v2`, see **notes**."}), http.StatusCreated, "a comment in Markdown")
	raw, _ := json.Marshal(comment.Body["comment"].(map[string]any)["body"])
	if !strings.Contains(string(raw), `"type":"code"`) || !strings.Contains(string(raw), `"type":"bold"`) {
		t.Errorf("the comment's body is %s", raw)
	}
	want(t, owner.post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"bodyMarkdown": "  \n"}), http.StatusBadRequest, "a blank comment in Markdown")
	want(t, owner.post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"bodyMarkdown": "x", "body": both}), http.StatusBadRequest, "a comment sent twice")
}
