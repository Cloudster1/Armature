//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// MCP is the same table, dispatched in process: a tool call answers what the
// HTTP call answers, under the same credential and the same refusals.
func TestAnAssistantCallsTheAPIAsTools(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "mcp")
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Tools", "key": "MCP"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	types := owner.get("/api/v1/issue-types")
	bug := find(t, types.Body["issueTypes"], "name", "Bug")["id"].(string)
	first := owner.post("/api/v1/projects/MCP/issues", map[string]any{"summary": "a bug to find", "typeId": bug})
	firstKey := obj(t, first, "issue")["key"].(string)

	token := owner.post("/api/v1/tokens", map[string]any{"name": "assistant"})
	agent := api.client(t)
	agent.bearer = principalField(t, token, "token", "secret").(string)

	rpc := func(c *client, id int, method string, params any) response {
		body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			body["params"] = params
		}
		return c.post("/api/v1/mcp", body)
	}
	result := func(t *testing.T, resp response) map[string]any {
		t.Helper()
		if resp.Status != http.StatusOK {
			t.Fatalf("rpc answered %d: %s", resp.Status, resp.Raw)
		}
		if e, ok := resp.Body["error"]; ok && e != nil {
			t.Fatalf("rpc error: %v", e)
		}
		out, _ := resp.Body["result"].(map[string]any)
		return out
	}
	call := func(t *testing.T, c *client, name string, args map[string]any) map[string]any {
		t.Helper()
		return result(t, rpc(c, 9, "tools/call", map[string]any{"name": name, "arguments": args}))
	}
	text := func(res map[string]any) string {
		return res["content"].([]any)[0].(map[string]any)["text"].(string)
	}

	t.Run("it introduces itself and lists the tools", func(t *testing.T) {
		init := result(t, rpc(agent, 1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "0"}}))
		if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["name"] != "armature" {
			t.Fatalf("initialize = %v", init)
		}
		if resp := agent.post("/api/v1/mcp", map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); resp.Status != http.StatusAccepted {
			t.Fatalf("a notification answered %d: %s", resp.Status, resp.Raw)
		}
		tools := result(t, rpc(agent, 2, "tools/list", nil))["tools"].([]any)
		names := map[string]bool{}
		var readOnlyGet, readOnlyCreate any
		for _, each := range tools {
			tool := each.(map[string]any)
			names[tool["name"].(string)] = true
			if tool["name"] == "get_issue" {
				readOnlyGet = tool["annotations"].(map[string]any)["readOnlyHint"]
			}
			if tool["name"] == "create_issue" {
				readOnlyCreate = tool["annotations"].(map[string]any)["readOnlyHint"]
			}
		}
		if !names["get_issue"] || !names["create_issue"] || !names["search_issues"] {
			t.Fatalf("tools = %v", names)
		}
		if readOnlyGet != true || readOnlyCreate != false {
			t.Errorf("readOnlyHint: get_issue %v, create_issue %v", readOnlyGet, readOnlyCreate)
		}
	})

	t.Run("a tool answers what the HTTP call answers", func(t *testing.T) {
		res := call(t, agent, "get_issue", map[string]any{"issueKey": firstKey})
		if res["isError"] == true {
			t.Fatalf("get_issue: %s", text(res))
		}
		structured := res["structuredContent"].(map[string]any)["issue"].(map[string]any)
		direct := obj(t, agent.get("/api/v1/issues/"+firstKey), "issue")
		if structured["key"] != direct["key"] || structured["summary"] != direct["summary"] {
			t.Errorf("the tool says %v, the call says %v", structured["summary"], direct["summary"])
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(text(res)), &parsed); err != nil {
			t.Errorf("the text is not the JSON body: %v", err)
		}
	})

	t.Run("a search takes an NQL query", func(t *testing.T) {
		res := call(t, agent, "search_issues", map[string]any{"q": "project = MCP AND type = Bug", "limit": 5})
		found := res["structuredContent"].(map[string]any)
		if found["total"].(float64) != 1 {
			t.Fatalf("search found %v", found["total"])
		}
		res = call(t, agent, "search_issues", map[string]any{"q": "this is not a query"})
		if res["isError"] != true {
			t.Errorf("a bad query was not an error: %s", text(res))
		}
	})

	t.Run("a write lands and is read back at once", func(t *testing.T) {
		res := call(t, agent, "create_issue", map[string]any{"projectKey": "MCP", "summary": "made by a tool"})
		if res["isError"] == true {
			t.Fatalf("create_issue: %s", text(res))
		}
		key := res["structuredContent"].(map[string]any)["issue"].(map[string]any)["key"].(string)
		if got := call(t, agent, "get_issue", map[string]any{"issueKey": key}); got["isError"] == true {
			t.Fatalf("the new issue is not read back: %s", text(got))
		}
		if listed := owner.get("/api/v1/projects/MCP/issues"); listed.Body["total"].(float64) != 2 {
			t.Errorf("the project has %v issues after the tool wrote one", listed.Body["total"])
		}
	})

	t.Run("a wrong key is an error the model reads, not a failure", func(t *testing.T) {
		res := call(t, agent, "get_issue", map[string]any{"issueKey": "MCP-999"})
		if res["isError"] != true || !strings.HasSuffix(text(res), ".") {
			t.Errorf("unknown issue: %v", res)
		}
	})

	t.Run("a read token is refused a writing tool", func(t *testing.T) {
		made := owner.post("/api/v1/tokens", map[string]any{"name": "reader", "scopes": []string{"read"}})
		reader := api.client(t)
		reader.bearer = principalField(t, made, "token", "secret").(string)
		if res := call(t, reader, "get_issue", map[string]any{"issueKey": firstKey}); res["isError"] == true {
			t.Fatalf("a read token cannot read: %s", text(res))
		}
		res := call(t, reader, "create_issue", map[string]any{"projectKey": "MCP", "summary": "not allowed"})
		if res["isError"] != true || !strings.Contains(text(res), "only read") {
			t.Errorf("a read token wrote: %v", res)
		}
	})

	t.Run("the openapi document is a resource", func(t *testing.T) {
		list := result(t, rpc(agent, 3, "resources/list", nil))["resources"].([]any)
		uri := list[0].(map[string]any)["uri"].(string)
		read := result(t, rpc(agent, 4, "resources/read", map[string]any{"uri": uri}))
		contents := read["contents"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(contents, `"/mcp"`) {
			t.Error("the document does not describe the endpoint itself")
		}
	})

	t.Run("the endpoint refuses what is not a request", func(t *testing.T) {
		if resp := agent.post("/api/v1/mcp", "not json"); resp.Status != http.StatusBadRequest {
			t.Errorf("malformed JSON answered %d", resp.Status)
		}
		nobody := api.client(t)
		if resp := nobody.post("/api/v1/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}); resp.Status != http.StatusUnauthorized {
			t.Errorf("no credential answered %d", resp.Status)
		}
		unknown := rpc(agent, 5, "nothing/here", nil)
		if code := unknown.Body["error"].(map[string]any)["code"].(float64); code != -32601 {
			t.Errorf("an unknown method answered %v", unknown.Body["error"])
		}
	})
}
