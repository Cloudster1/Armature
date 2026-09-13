package httpapi

import (
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/armature/armature/backend/internal/openapi"
)

// mcpTool is one marked row of the table as an assistant sees it: a name, a
// sentence, and one flat input schema covering path, query and body.
type mcpTool struct {
	Name        string
	Description string
	Method      string
	Path        string
	ReadOnly    bool
	Input       *openapi.Schema
	pathParams  []string
	query       []param
	hasBody     bool
}

// toolCatalog is derived from the table once; the table is what a unit test
// holds the router to, so the tools cannot drift from the routes.
var toolCatalog = sync.OnceValue(func() []mcpTool {
	b := openapi.NewBuilder()
	declareEnums(b)
	declareOverrides(b)
	var out []mcpTool
	for _, op := range operations {
		if op.tool == "" {
			continue
		}
		out = append(out, mcpTool{
			Name:        op.tool,
			Description: op.toolHelp,
			Method:      op.method,
			Path:        op.path,
			ReadOnly:    op.method == http.MethodGet,
			Input:       toolInput(b, op),
			pathParams:  pathParams(op.path),
			query:       op.query,
			hasBody:     op.request != nil || op.raw,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
})

func toolByName(name string) (mcpTool, bool) {
	for _, t := range toolCatalog() {
		if t.Name == name {
			return t, true
		}
	}
	return mcpTool{}, false
}

// toolInput flattens what the HTTP operation takes in three places into one
// object, which is the only shape a tool call has.
func toolInput(b *openapi.Builder, op operation) *openapi.Schema {
	s := &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{}}
	for _, name := range pathParams(op.path) {
		s.Properties[name] = pathParamSchema(name)
		s.Required = append(s.Required, name)
	}
	for _, q := range op.query {
		schema := q.schema
		if schema == nil {
			schema = &openapi.Schema{Type: "string"}
		}
		copied := *schema
		if q.repeated {
			copied = openapi.Schema{Type: "array", Items: schema}
		}
		if q.description != "" {
			copied.Description = q.description
		}
		s.Properties[q.name] = &copied
	}
	if op.request != nil {
		body := b.SchemaOf(op.request)
		if name, ok := strings.CutPrefix(body.Ref, "#/components/schemas/"); ok {
			body = b.Components()[name]
		}
		for key, each := range body.Properties {
			s.Properties[key] = each
		}
		s.Required = append(s.Required, body.Required...)
	}
	return b.SelfContained(s)
}
