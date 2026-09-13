//go:build integration

package test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/assistant"
)

// fakeProvider speaks the Messages API shape back: it asks for a search first,
// then answers with navigate once it has seen the search's result.
func fakeProvider(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "secret" {
			http.Error(w, `{"error":{"message":"wrong door"}}`, http.StatusUnauthorized)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			System   string `json:"system"`
			Tools    []struct{ Name string }
			Messages []struct {
				Role    string
				Content []struct {
					Type    string
					Content string
				}
			}
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("provider got %s", raw)
		}
		seen = append(seen, string(raw))
		last := req.Messages[len(req.Messages)-1]
		w.Header().Set("Content-Type", "application/json")
		if last.Role == "user" && last.Content[0].Type == "tool_result" {
			answer := `{"to":"/search","query":"type = Bug AND statusCategory != done","text":"Open bugs, newest first. The search shows them."}`
			if strings.Contains(last.Content[0].Content, "only read") {
				answer = `{"text":"This token cannot write, so nothing was changed."}`
			}
			io.WriteString(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"t2","name":"navigate","input":`+answer+`}]}`)
			return
		}
		io.WriteString(w, `{"stop_reason":"tool_use","content":[{"type":"text","text":"Let me look."},{"type":"tool_use","id":"t1","name":"search_issues","input":{"q":"type = Bug AND statusCategory != done","limit":3}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// A model answers behind the same door, as the asking user: it looks with the
// MCP tools under the caller's credential and ends by naming a place.
func TestAModelAnswersWithTheCallersOwnTools(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "asker")
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Asked", "key": "ASK"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	types := owner.get("/api/v1/issue-types")
	bug := find(t, types.Body["issueTypes"], "name", "Bug")["id"].(string)
	owner.post("/api/v1/projects/ASK/issues", map[string]any{"summary": "a bug", "typeId": bug})

	t.Run("without a provider the door says so", func(t *testing.T) {
		if status := owner.get("/api/v1/assistant"); status.Body["configured"] != false {
			t.Fatalf("status = %s", status.Raw)
		}
		if resp := owner.post("/api/v1/assistant/ask", map[string]any{"question": "where are the bugs"}); resp.Status != http.StatusServiceUnavailable || resp.ErrorCode() != "assistant_unavailable" {
			t.Fatalf("ask without a provider: %d %s", resp.Status, resp.Raw)
		}
	})

	provider, seen := fakeProvider(t)
	api.api.Assistant = assistant.HTTPAsker{URL: provider.URL, Key: "secret", Model: "fake"}

	t.Run("with one it looks, then points", func(t *testing.T) {
		if status := owner.get("/api/v1/assistant"); status.Body["configured"] != true {
			t.Fatalf("status = %s", status.Raw)
		}
		resp := owner.post("/api/v1/assistant/ask", map[string]any{"question": "where are the open bugs", "projectKey": "ASK",
			"places": []map[string]any{{"id": "page:search", "title": "Search", "sentence": "Search every project.", "path": "/search"}}})
		if resp.Status != http.StatusOK {
			t.Fatalf("ask: %d %s", resp.Status, resp.Raw)
		}
		answer := obj(t, resp, "answer")
		if answer["to"] != "/search" || answer["query"] != "type = Bug AND statusCategory != done" || !strings.Contains(answer["text"].(string), "Open bugs") {
			t.Errorf("answer = %v", answer)
		}
		if len(*seen) != 2 {
			t.Fatalf("the provider was called %d times", len(*seen))
		}
		if !strings.Contains((*seen)[0], `"name":"search_issues"`) || !strings.Contains((*seen)[0], "Places:") || !strings.Contains((*seen)[0], "project ASK") {
			t.Error("the first call lacked the tools, the places or the project")
		}
		if !strings.Contains((*seen)[1], `total\":1`) {
			t.Errorf("the tool did not run as the caller: %s", (*seen)[1])
		}
	})

	t.Run("a read token's model cannot write either", func(t *testing.T) {
		made := owner.post("/api/v1/tokens", map[string]any{"name": "reader", "scopes": []string{"read"}})
		reader := api.client(t)
		reader.bearer = principalField(t, made, "token", "secret").(string)
		*seen = nil
		writing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			*seen = append(*seen, string(raw))
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(string(raw), "tool_result") {
				io.WriteString(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"Nothing was changed."}]}`)
				return
			}
			io.WriteString(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"t1","name":"create_issue","input":{"projectKey":"ASK","summary":"from a model"}}]}`)
		}))
		defer writing.Close()
		api.api.Assistant = assistant.HTTPAsker{URL: writing.URL, Model: "fake"}
		resp := reader.post("/api/v1/assistant/ask", map[string]any{"question": "file a bug"})
		if resp.Status != http.StatusOK {
			t.Fatalf("ask: %d %s", resp.Status, resp.Raw)
		}
		if !strings.Contains((*seen)[1], "only read") {
			t.Errorf("the model's write was not refused: %s", (*seen)[1])
		}
		if listed := owner.get("/api/v1/projects/ASK/issues"); listed.Body["total"].(float64) != 1 {
			t.Errorf("the model wrote through a read token: %s", listed.Raw)
		}
	})

	t.Run("a question is required and a broken provider is a sentence", func(t *testing.T) {
		if resp := owner.post("/api/v1/assistant/ask", map[string]any{"question": "  "}); resp.Status != http.StatusBadRequest {
			t.Errorf("empty question: %d %s", resp.Status, resp.Raw)
		}
		api.api.Assistant = assistant.HTTPAsker{URL: provider.URL, Key: "wrong", Model: "fake"}
		if resp := owner.post("/api/v1/assistant/ask", map[string]any{"question": "anything"}); resp.Status != http.StatusBadGateway || resp.ErrorCode() != "assistant_failed" {
			t.Errorf("a refused provider: %d %s", resp.Status, resp.Raw)
		}
	})
}

// What a model wants to change is proposed, not done: the words it read were
// written by people, and a person confirms the change.
func TestAModelsChangeWaitsForTheReader(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "proposer")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Proposed", "key": "PRP"}), http.StatusCreated, "a project")

	writing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(raw), "tool_result") {
			io.WriteString(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"t2","name":"navigate","input":{"text":"I propose filing it."}}]}`)
			return
		}
		io.WriteString(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"t1","name":"create_issue","input":{"projectKey":"PRP","summary":"from a model"}}]}`)
	}))
	defer writing.Close()
	api.api.Assistant = assistant.HTTPAsker{URL: writing.URL, Model: "fake"}

	answered := want(t, owner.post("/api/v1/assistant/ask", map[string]any{"question": "file a bug"}), http.StatusOK, "ask")
	proposals := list(t, answered, "proposals")
	if len(proposals) != 1 || proposals[0].(map[string]any)["tool"] != "create_issue" {
		t.Fatalf("proposals = %s", answered.Raw)
	}
	if listed := owner.get("/api/v1/projects/PRP/issues"); listed.Body["total"].(float64) != 0 {
		t.Fatalf("the model filed an issue by itself: %s", listed.Raw)
	}

	// The reader confirms it, through the same door any client would use.
	want(t, owner.post("/api/v1/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "create_issue", "arguments": proposals[0].(map[string]any)["arguments"]},
	}), http.StatusOK, "confirming the proposal")
	h.waitForPrimary(t)
	if listed := owner.get("/api/v1/projects/PRP/issues"); listed.Body["total"].(float64) != 1 {
		t.Fatalf("confirming did not file the issue: %s", listed.Raw)
	}
}
