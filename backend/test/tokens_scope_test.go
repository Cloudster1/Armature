//go:build integration

package test

import (
	"net/http"
	"testing"
)

// A read token is refused in the api, before any handler: it reads everything
// its owner can and changes nothing, on every route there is or will be.
func TestAReadTokenReadsAndNeverWrites(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "readonly")
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Read only", "key": "RDO"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	owner.post("/api/v1/projects/RDO/issues", map[string]any{"summary": "already here"})

	created := owner.post("/api/v1/tokens", map[string]any{"name": "assistant", "scopes": []string{"read"}})
	if created.Status != http.StatusCreated {
		t.Fatalf("create token: %d %s", created.Status, created.Raw)
	}
	if scopes := obj(t, created, "token")["scopes"].([]any); len(scopes) != 1 || scopes[0] != "read" {
		t.Errorf("scopes = %v, want [read]", scopes)
	}
	reader := api.client(t)
	reader.bearer = principalField(t, created, "token", "secret").(string)

	t.Run("it reads", func(t *testing.T) {
		if resp := reader.get("/api/v1/projects/RDO/issues"); resp.Status != http.StatusOK || resp.Body["total"].(float64) != 1 {
			t.Fatalf("list issues: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("it is refused a write with a sentence naming the token", func(t *testing.T) {
		resp := reader.post("/api/v1/projects/RDO/issues", map[string]any{"summary": "from a read token"})
		if resp.Status != http.StatusForbidden || resp.ErrorCode() != "read_only_token" {
			t.Fatalf("create issue with a read token: %d %s", resp.Status, resp.Raw)
		}
		if resp := reader.patch("/api/v1/issues/RDO-1", map[string]any{"summary": "renamed"}); resp.Status != http.StatusForbidden {
			t.Fatalf("patch with a read token: %d %s", resp.Status, resp.Raw)
		}
		if listed := owner.get("/api/v1/projects/RDO/issues"); listed.Body["total"].(float64) != 1 {
			t.Fatalf("the refused write landed: %s", listed.Raw)
		}
	})

	t.Run("a full token still writes", func(t *testing.T) {
		full := owner.post("/api/v1/tokens", map[string]any{"name": "ci"})
		writer := api.client(t)
		writer.bearer = principalField(t, full, "token", "secret").(string)
		if resp := writer.post("/api/v1/projects/RDO/issues", map[string]any{"summary": "from a full token"}); resp.Status != http.StatusCreated {
			t.Fatalf("create issue with a full token: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("a scope nobody enforces is not accepted", func(t *testing.T) {
		resp := owner.post("/api/v1/tokens", map[string]any{"name": "wide", "scopes": []string{"write"}})
		if resp.Status != http.StatusUnprocessableEntity {
			t.Fatalf("unknown scope: %d %s", resp.Status, resp.Raw)
		}
	})
}
