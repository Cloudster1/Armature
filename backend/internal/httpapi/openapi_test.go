package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/armature/armature/backend/internal/webhook"
)

// routed lists every method and path the router serves, in the form the
// operation table uses.
func routed(t *testing.T) map[string]bool {
	t.Helper()
	s := &Server{Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	out := map[string]bool{}
	router, ok := s.Routes(nil).(chi.Routes)
	if !ok {
		t.Fatal("the router is not walkable")
	}
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		if strings.HasPrefix(route, "/api/v1") {
			route = strings.TrimPrefix(route, "/api/v1")
		}
		out[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The table and the router are two lists of the same thing; a route in one
// and not the other is a route nobody documented or nobody serves.
func TestEveryRouteIsInTheTableAndBack(t *testing.T) {
	served := routed(t)
	described := map[string]bool{}
	for _, op := range operations {
		key := op.method + " " + op.path
		if described[key] {
			t.Errorf("%s appears twice in the table", key)
		}
		described[key] = true
		if !served[key] {
			t.Errorf("%s is in the table but the router does not serve it", key)
		}
	}
	for key := range served {
		if !described[key] {
			t.Errorf("%s is served but not in the table", key)
		}
	}
}

func TestEveryOperationIsWellFormed(t *testing.T) {
	ids := map[string]string{}
	for _, op := range operations {
		key := op.method + " " + op.path
		if op.summary == "" || op.tag == "" || op.handler == "" {
			t.Errorf("%s lacks a summary, a tag or a handler", key)
		}
		if op.responses == nil {
			t.Errorf("%s says nothing about its responses", key)
		}
		if (op.method == "GET" || op.method == "DELETE") && (op.request != nil || op.multipart || op.raw) {
			t.Errorf("%s takes a body", key)
		}
		for status := range op.responses {
			if status < 200 || status > 599 {
				t.Errorf("%s answers %d", key, status)
			}
		}
		// Clients are generated with one function per id, so two operations
		// with one id would collapse into one function.
		if prior, ok := ids[op.operationID()]; ok {
			t.Errorf("%s and %s share the operation id %s", prior, key, op.operationID())
		}
		ids[op.operationID()] = key
	}
}

func TestTheDocumentBuildsAndReferencesResolve(t *testing.T) {
	doc := Spec()
	if len(doc.Paths) == 0 || len(doc.Components.Schemas) == 0 {
		t.Fatal("an empty document")
	}
	encoded, err := doc.MarshalIndent()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range refsIn(encoded) {
		if _, ok := doc.Components.Schemas[name]; !ok {
			t.Errorf("reference to %q has no component", name)
		}
	}
	for _, want := range []string{"Issue", "Project", "Board", "Sprint", "Team", "Dashboard", "Field", "Attachment", "Principal", "APIError"} {
		if _, ok := doc.Components.Schemas[want]; !ok {
			t.Errorf("component %s is missing", want)
		}
	}
	issue := doc.Components.Schemas["Issue"]
	if issue.Properties["priority"].Enum == nil {
		t.Error("the priority should be an enum")
	}
	if got := issue.Properties["key"].Type; got != "string" {
		t.Errorf("issue.key is %v", got)
	}
}

// An installation is one team's, so what the document and the assistant's tools
// say to a reader speaks of everyone and all projects, never an organization.
func TestTheDocumentSaysEveryoneNotTheOrganization(t *testing.T) {
	encoded, err := Spec().MarshalIndent()
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatal(err)
	}
	for _, words := range readWords(doc, "") {
		if strings.Contains(strings.ToLower(words), "organization") {
			t.Errorf("the document says %q", words)
		}
	}
	for _, op := range operations {
		if strings.Contains(strings.ToLower(op.toolHelp), "organization") {
			t.Errorf("the tool %s says %q", op.tool, op.toolHelp)
		}
	}
}

// readWords collects what a reader of the document reads: summaries,
// descriptions, tags and the values an enum allows, but not names or ids.
func readWords(node any, key string) []string {
	var out []string
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			out = append(out, readWords(child, k)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, readWords(child, key)...)
		}
	case string:
		switch key {
		case "summary", "description", "tags", "enum", "name":
			out = append(out, v)
		}
	}
	return out
}

func refsIn(encoded []byte) []string {
	var out []string
	marker := []byte(`"$ref": "#/components/schemas/`)
	for {
		i := bytes.Index(encoded, marker)
		if i < 0 {
			return out
		}
		encoded = encoded[i+len(marker):]
		end := bytes.IndexByte(encoded, '"')
		out = append(out, string(encoded[:end]))
	}
}

// The checked in copy is what clients are generated from; it has to be the
// document this code produces, or the client and the server disagree.
func TestTheCheckedInDocumentIsCurrent(t *testing.T) {
	// The Makefile mounts the repository's api directory into the toolchain
	// container and says where; from a plain go test it is three levels up.
	path := os.Getenv("ARMATURE_OPENAPI_FILE")
	if path == "" {
		path = filepath.Join("..", "..", "..", "api", "openapi.json")
	}
	checkedIn, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run make openapi)", path, err)
	}
	generated, err := Spec().MarshalIndent()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(checkedIn), bytes.TrimSpace(generated)) {
		t.Fatal("api/openapi.json is out of date; run make openapi and commit the result")
	}
}

// A receiver is generated from the document like any client, so what a
// delivery carries, body and headers, is described there.
func TestTheDocumentDescribesAWebhookDelivery(t *testing.T) {
	doc := Spec()
	delivery := doc.Webhooks["event"]["post"]
	if delivery == nil {
		t.Fatal("the document describes no webhook delivery")
	}
	body, err := doc.Resolve(delivery.RequestBody.Content["application/json"].Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"version", "id", "topic", "orgId", "occurredAt", "payload"} {
		if !slices.Contains(body.Required, field) {
			t.Errorf("the envelope does not promise %s", field)
		}
	}
	if !body.Properties["orgId"].Deprecated {
		t.Error("the envelope's orgId is not marked deprecated")
	}
	if got := body.Properties["payload"].Type; got != "object" {
		t.Errorf("the payload is %v, want an object", got)
	}
	headers := map[string]bool{}
	for _, p := range delivery.Parameters {
		if p.In == "header" && p.Required {
			headers[p.Name] = true
		}
	}
	for _, name := range []string{webhook.HeaderEvent, webhook.HeaderDelivery, webhook.HeaderVersion, webhook.HeaderSignature, webhook.HeaderTimestamp, webhook.HeaderTimestampedSignature} {
		if !headers[name] {
			t.Errorf("the delivery does not list the header %s", name)
		}
	}
}
