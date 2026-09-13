package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEveryToolIsNamedOnceAndExplained(t *testing.T) {
	seen := map[string]string{}
	for _, op := range operations {
		if op.tool == "" {
			continue
		}
		key := op.method + " " + op.path
		if prior, ok := seen[op.tool]; ok {
			t.Errorf("%s and %s share the tool name %s", prior, key, op.tool)
		}
		seen[op.tool] = key
		if !strings.HasSuffix(op.toolHelp, ".") {
			t.Errorf("%s: the tool sentence %q does not end", key, op.toolHelp)
		}
		if strings.ToLower(op.tool) != op.tool || strings.ContainsAny(op.tool, "- ") {
			t.Errorf("%s: %q is not a snake_case tool name", key, op.tool)
		}
	}
	if len(seen) < 20 {
		t.Errorf("only %d tools are marked", len(seen))
	}
}

func TestAToolRequiresItsPathAndReadsWhenItGets(t *testing.T) {
	for _, tool := range toolCatalog() {
		if tool.ReadOnly != (tool.Method == http.MethodGet) {
			t.Errorf("%s: readOnly %v for %s", tool.Name, tool.ReadOnly, tool.Method)
		}
		for _, name := range pathParams(tool.Path) {
			if _, ok := tool.Input.Properties[name]; !ok {
				t.Errorf("%s: the schema lacks the path value %s", tool.Name, name)
			}
			required := false
			for _, r := range tool.Input.Required {
				required = required || r == name
			}
			if !required {
				t.Errorf("%s: %s is not required", tool.Name, name)
			}
		}
		encoded, err := json.Marshal(tool.Input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "#/components/") {
			t.Errorf("%s: the schema refers outside itself: %s", tool.Name, encoded)
		}
	}
	create, _ := toolByName("create_issue")
	if _, ok := create.Input.Properties["summary"]; !ok {
		t.Error("create_issue does not take a summary")
	}
	search, _ := toolByName("search_issues")
	if q := search.Input.Properties["q"]; q == nil || q.Type != "string" {
		t.Error("search_issues does not take q as a string")
	}
	if status := search.Input.Properties["status"]; status == nil || status.Type != "array" {
		t.Error("a repeated query parameter is not an array")
	}
}

func TestTheEndpointSpeaksJSONRPC(t *testing.T) {
	s := &Server{}
	call := func(body string) (int, rpcResponse) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.handleMCP(rec, req)
		var resp rpcResponse
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("not a JSON-RPC answer: %s", rec.Body.String())
			}
		}
		return rec.Code, resp
	}

	status, resp := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if status != http.StatusOK || resp.Error != nil || !strings.Contains(string(resp.Result), MCPProtocolVersion) {
		t.Fatalf("initialize: %d %+v", status, resp)
	}
	if string(resp.ID) != "1" {
		t.Errorf("the id %s was not echoed", resp.ID)
	}

	status, resp = call(`{"jsonrpc":"2.0","id":"a","method":"tools/list"}`)
	if status != http.StatusOK || !strings.Contains(string(resp.Result), `"get_issue"`) {
		t.Fatalf("tools/list: %d %s", status, resp.Result)
	}

	if status, _ = call(`{"jsonrpc":"2.0","method":"notifications/initialized"}`); status != http.StatusAccepted {
		t.Errorf("a notification answers %d", status)
	}
	if _, resp = call(`{"jsonrpc":"2.0","id":2,"method":"nothing/here"}`); resp.Error == nil || resp.Error.Code != rpcMethodNotFound {
		t.Errorf("an unknown method answers %+v", resp.Error)
	}
	if _, resp = call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"no_such_tool"}}`); resp.Error == nil || resp.Error.Code != rpcInvalidParams {
		t.Errorf("an unknown tool answers %+v", resp.Error)
	}
	if _, resp = call(`[{"jsonrpc":"2.0","id":4,"method":"ping"}]`); resp.Error == nil || resp.Error.Code != rpcInvalidRequest {
		t.Errorf("a batch answers %+v", resp.Error)
	}
	if status, _ = call(`not json`); status != http.StatusBadRequest {
		t.Errorf("malformed JSON answers %d", status)
	}
}

func TestAToolCallBecomesTheHTTPCallItStandsFor(t *testing.T) {
	tool, _ := toolByName("search_issues")
	outer := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", nil)
	outer.Header.Set("Authorization", "Bearer armature_pat_x")
	inner, err := tool.request(outer, map[string]json.RawMessage{
		"q": json.RawMessage(`"type = Bug"`), "limit": json.RawMessage(`5`), "status": json.RawMessage(`["a","b"]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inner.Method != http.MethodGet || inner.URL.Path != "/api/v1/issues" {
		t.Errorf("inner call is %s %s", inner.Method, inner.URL.Path)
	}
	q := inner.URL.Query()
	if q.Get("q") != "type = Bug" || q.Get("limit") != "5" || len(q["status"]) != 2 {
		t.Errorf("query = %s", inner.URL.RawQuery)
	}
	if inner.Header.Get("Authorization") != "Bearer armature_pat_x" {
		t.Error("the credential did not travel")
	}

	get, _ := toolByName("get_issue")
	if _, err := get.request(outer, nil); err == nil {
		t.Error("a missing path value was accepted")
	}
	if _, err := get.request(outer, map[string]json.RawMessage{"issueKey": json.RawMessage(`"CP-1"`), "extra": json.RawMessage(`1`)}); err == nil {
		t.Error("an argument a GET cannot carry was accepted")
	}

	create, _ := toolByName("create_issue")
	inner, err = create.request(outer, map[string]json.RawMessage{"projectKey": json.RawMessage(`"CP"`), "summary": json.RawMessage(`"hello"`)})
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 64)
	n, _ := inner.Body.Read(body)
	if inner.URL.Path != "/api/v1/projects/CP/issues" || !strings.Contains(string(body[:n]), `"summary":"hello"`) {
		t.Errorf("create call: %s %s", inner.URL.Path, body[:n])
	}
}
