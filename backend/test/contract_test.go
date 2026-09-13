//go:build integration

package test

import (
	"flag"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/armature/armature/backend/internal/httpapi"
	"github.com/armature/armature/backend/internal/openapi"
)

// The contract: every call the suite makes is checked against the OpenAPI
// document the server derives from itself. A response that does not fit its
// own description fails the test that provoked it, and a final test refuses a
// suite that left an operation unexercised. Nothing here knows what the
// endpoints do; it only knows what they promised.

type route struct {
	httpapi.Route
	pattern *regexp.Regexp
	op      *openapi.Operation
}

type contract struct {
	doc    *openapi.Document
	routes []route
	mu     sync.Mutex
	// hits counts responses per operation and status.
	hits map[string]map[int]int
}

var apiContract = newContract()

func newContract() *contract {
	doc := httpapi.Spec()
	c := &contract{doc: doc, hits: map[string]map[int]int{}}
	for _, r := range httpapi.Catalog() {
		item := doc.Paths[r.Path]
		c.routes = append(c.routes, route{Route: r, pattern: templatePattern(r.Path), op: item[strings.ToLower(r.Method)]})
	}
	// Literal paths before templated ones, so /projects/key-check is not
	// mistaken for /projects/{projectKey}.
	sort.SliceStable(c.routes, func(i, j int) bool {
		return strings.Count(c.routes[i].Path, "{") < strings.Count(c.routes[j].Path, "{")
	})
	return c
}

// templatePattern matches concrete paths against a template: {name} is one
// path segment, everything else is literal.
func templatePattern(template string) *regexp.Regexp {
	var expr strings.Builder
	expr.WriteString("^")
	rest := template
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			expr.WriteString(regexp.QuoteMeta(rest))
			break
		}
		close := strings.IndexByte(rest, '}')
		expr.WriteString(regexp.QuoteMeta(rest[:open]))
		expr.WriteString(`[^/]+`)
		rest = rest[close+1:]
	}
	expr.WriteString("$")
	return regexp.MustCompile(expr.String())
}

// key is how an operation is counted: method and template.
func (r route) key() string { return r.Method + " " + r.Path }

// find the operation a request hit.
func (c *contract) find(method, path string) *route {
	path = strings.TrimPrefix(path, "/api/v1")
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	for i := range c.routes {
		if c.routes[i].Method == method && c.routes[i].pattern.MatchString(path) {
			return &c.routes[i]
		}
	}
	return nil
}

// observe records a response and checks it against the document.
func (c *contract) observe(t *testing.T, method, path string, status int, contentType string, body []byte) {
	t.Helper()
	r := c.find(method, path)
	if r == nil {
		// A test may deliberately call something that does not exist; the
		// router's refusal is the right answer. Anything else from an
		// undocumented route is a route the document is missing.
		if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Errorf("contract: %s %s is not a documented operation", method, path)
		}
		return
	}
	c.mu.Lock()
	if c.hits[r.key()] == nil {
		c.hits[r.key()] = map[int]int{}
	}
	c.hits[r.key()][status]++
	c.mu.Unlock()

	if r.op == nil {
		t.Errorf("contract: %s has no operation in the document", r.key())
		return
	}
	if r.Binary && status == http.StatusOK {
		return
	}
	if status/100 == 3 {
		// http.Redirect writes a small HTML body for a GET; that is fine.
		return
	}
	if status == http.StatusNoContent {
		if len(body) != 0 {
			t.Errorf("contract: %s %s answered %d with a body", method, path, status)
		}
		return
	}
	resp := r.op.Responses[fmt.Sprint(status)]
	if resp == nil {
		if status/100 == 2 {
			t.Errorf("contract: %s %s answered %d, which the document does not mention", method, path, status)
			return
		}
		resp = r.op.Responses["default"]
	}
	media, ok := resp.Content["application/json"]
	if !ok || media.Schema == nil {
		if len(body) != 0 {
			t.Errorf("contract: %s %s answered %d with a body the document does not describe", method, path, status)
		}
		return
	}
	if !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("contract: %s %s answered %d as %q, want JSON", method, path, status, contentType)
		return
	}
	if err := c.doc.ValidateJSON(media.Schema, body); err != nil {
		t.Errorf("contract: %s %s answered %d with a body that does not match the document: %v\n%s", method, path, status, err, truncate(body))
	}
}

func truncate(b []byte) string {
	if len(b) > 600 {
		return string(b[:600]) + "..."
	}
	return string(b)
}

// uncovered lists the operations that were never answered successfully, and
// separately the ones never refused, so a suite can be judged on both.
func (c *contract) uncovered() (neverSucceeded, neverRefused []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.routes {
		succeeded, refused := false, false
		for status := range c.hits[r.key()] {
			switch {
			case status/100 == 3 && r.Redirect:
				// A browser facing route refuses by sending the browser to
				// the sign-in page with a reason, which is a redirect too.
				succeeded, refused = true, true
			case status/100 == 2, status/100 == 3:
				succeeded = true
			case status/100 == 4:
				refused = true
			}
		}
		if !succeeded {
			neverSucceeded = append(neverSucceeded, r.key())
		}
		if !refused {
			neverRefused = append(neverRefused, r.key())
		}
	}
	sort.Strings(neverSucceeded)
	sort.Strings(neverRefused)
	return
}

// runFiltered reports whether go test was asked for a subset, in which case
// coverage of the whole surface cannot be expected.
func runFiltered() bool {
	f := flag.Lookup("test.run")
	return f != nil && f.Value.String() != ""
}
