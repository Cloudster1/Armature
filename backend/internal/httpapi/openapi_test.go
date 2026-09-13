package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
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
