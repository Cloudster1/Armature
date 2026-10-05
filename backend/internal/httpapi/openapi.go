package httpapi

import (
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/armature/armature/backend/internal/arrange"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/calendar"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/openapi"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/theme"
	"github.com/armature/armature/backend/internal/workflow"
)

// The API described in terms of the code that serves it.
//
// Every route in Routes has a row here, naming the request type the handler
// decodes and the type it responds with, and a test refuses a router and a
// table that disagree. The document is then derived by reflection, so a field
// added to an issue appears in the specification the moment it appears in the
// response, and nothing is written twice.

// env is a response envelope: the one or two keys a handler wraps its result
// in. A nil value is an untyped JSON value; a typed nil pointer is nullable.
type env map[string]any

// param is a query parameter.
type param struct {
	name, description string
	schema            *openapi.Schema
	repeated          bool
}

// operation is one row of the table.
type operation struct {
	method, path string
	handler      string
	// id names the operation for clients when the handler's name would not
	// be unique, as when one handler serves two paths.
	id      string
	summary string
	tag     string
	query   []param
	// request is the JSON body type; multipart says the body is a file upload
	// instead; raw says any JSON is accepted.
	request   any
	multipart bool
	// parts names the other form parts a multipart handler reads, so a client
	// generated from the document can send them.
	parts []string
	raw   bool
	// responses maps a status to its envelope or bare type; nil is no body.
	responses map[int]any
	// public routes need no session; binary ones answer with a file.
	public bool
	binary bool
	// redirect routes answer with a Location header rather than a body.
	redirect bool
	// tool names the operation for an assistant and toolHelp is the sentence
	// the model reads; empty means the operation is not offered as a tool.
	tool, toolHelp string
	// rawNote describes a raw body; the git webhook keeps the default.
	rawNote string
}

// Spec builds the OpenAPI document from the table.
func Spec() *openapi.Document {
	b := openapi.NewBuilder()
	declareEnums(b)
	declareOverrides(b)

	doc := &openapi.Document{
		OpenAPI: "3.1.0",
		Info: openapi.Info{
			Title:   "Armature",
			Version: "1",
			Description: "The tracker's HTTP API. Every endpoint answers JSON; errors share one envelope. " +
				"Sign in with a session cookie or an API token sent as a bearer token.",
		},
		Servers: []openapi.Server{{URL: "/api/v1", Description: "This deployment"}},
		Paths:   map[string]openapi.PathItem{},
		Components: openapi.Components{
			SecuritySchemes: map[string]openapi.SecurityScheme{
				"session": {Type: "apiKey", In: "cookie", Name: "armature_session", Description: "The cookie a sign-in sets."},
				"token":   {Type: "http", Scheme: "bearer", Description: "A personal access token from POST /tokens."},
			},
		},
		Security: []map[string][]string{{"session": {}}, {"token": {}}},
	}

	errorSchema := b.Schema(reflect.TypeOf(errorEnvelope{}))
	tags := map[string]bool{}
	for _, op := range operations {
		item := doc.Paths[op.path]
		if item == nil {
			item = openapi.PathItem{}
			doc.Paths[op.path] = item
		}
		o := &openapi.Operation{
			OperationID: op.operationID(),
			Summary:     op.summary,
			Tags:        []string{op.tag},
			Responses:   map[string]*openapi.Response{},
		}
		tags[op.tag] = true
		for _, name := range pathParams(op.path) {
			o.Parameters = append(o.Parameters, openapi.Parameter{
				Name: name, In: "path", Required: true, Schema: pathParamSchema(name),
			})
		}
		for _, q := range op.query {
			schema := q.schema
			if schema == nil {
				schema = &openapi.Schema{Type: "string"}
			}
			if q.repeated {
				schema = &openapi.Schema{Type: "array", Items: schema}
			}
			o.Parameters = append(o.Parameters, openapi.Parameter{Name: q.name, In: "query", Description: q.description, Schema: schema})
		}
		switch {
		case op.multipart:
			body := &openapi.Schema{Type: "object", Required: []string{"file"},
				Properties: map[string]*openapi.Schema{"file": {Type: "string", Format: "binary"}}}
			for _, part := range op.parts {
				body.Properties[part] = &openapi.Schema{Type: "string", Description: "A JSON object, sent as a form part."}
			}
			o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
				"multipart/form-data": {Schema: body},
			}}
		case op.raw:
			o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
				"application/json": {Schema: &openapi.Schema{Description: op.rawDescription()}},
			}}
		case op.request != nil:
			o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
				"application/json": {Schema: b.SchemaOf(op.request)},
			}}
		}
		for status, body := range op.responses {
			r := &openapi.Response{Description: http.StatusText(status)}
			switch {
			case op.binary && status == http.StatusOK:
				r.Content = map[string]openapi.MediaType{"*/*": {Schema: &openapi.Schema{Type: "string", Format: "binary"}}}
			case body != nil:
				r.Content = map[string]openapi.MediaType{"application/json": {Schema: envelopeSchema(b, body)}}
			}
			o.Responses[itoa(status)] = r
		}
		if op.redirect {
			o.Responses["302"] = &openapi.Response{Description: "Found: the browser is sent on."}
		}
		o.Responses["default"] = &openapi.Response{Description: "An error, in the one shape every endpoint uses.",
			Content: map[string]openapi.MediaType{"application/json": {Schema: errorSchema}}}
		if op.public {
			o.Security = []map[string][]string{}
		}
		item[strings.ToLower(op.method)] = o
	}
	for _, tag := range openapi.SortedKeys(tags) {
		doc.Tags = append(doc.Tags, openapi.Tag{Name: tag})
	}
	doc.Components.Schemas = b.Components()
	return doc
}

// rawDescription says what a raw body is when the row does not.
func (op operation) rawDescription() string {
	if op.rawNote != "" {
		return op.rawNote
	}
	return "The payload the git host sends."
}

// operationID is what a client calls the operation: the handler's name unless
// the table says otherwise.
func (op operation) operationID() string {
	if op.id != "" {
		return op.id
	}
	id := strings.TrimPrefix(op.handler, "handle")
	return strings.ToLower(id[:1]) + id[1:]
}

// docSchema is a rich text document: what descriptions and comments are.
var docSchema = &openapi.Schema{
	Type:        "object",
	Description: "A rich text document.",
	Properties: map[string]*openapi.Schema{
		"type":    {Type: "string", Enum: []string{"doc"}},
		"content": {Type: "array", Items: &openapi.Schema{Description: "A node of the document."}},
	},
	Required: []string{"type", "content"},
}

// answerSchema is what a custom field's value can be.
var answerSchema = &openapi.Schema{OneOf: []*openapi.Schema{{Type: "string"}, {Type: "number"}, {Type: "boolean"}}}

// keyCheckResponse is either a verdict on a key or a suggestion from a name.
type keyCheckResponse struct {
	Key        string `json:"key,omitempty"`
	Valid      *bool  `json:"valid,omitempty"`
	Available  *bool  `json:"available,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// errorEnvelope is how every failure is written.
type errorEnvelope struct {
	Error APIError `json:"error"`
}

// envelopeSchema describes a response body: an env becomes an inline object
// with every key required, anything else is the type itself.
func envelopeSchema(b *openapi.Builder, body any) *openapi.Schema {
	e, ok := body.(env)
	if !ok {
		return b.SchemaOf(body)
	}
	s := &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{}}
	for _, key := range openapi.SortedKeys(e) {
		schema := b.SchemaOf(e[key])
		if schema == nil {
			schema = &openapi.Schema{Description: "A JSON value."}
		}
		s.Properties[key] = schema
		s.Required = append(s.Required, key)
	}
	return s
}

var pathParamPattern = regexp.MustCompile(`\{(\w+)\}`)

func pathParams(path string) []string {
	var out []string
	for _, m := range pathParamPattern.FindAllStringSubmatch(path, -1) {
		out = append(out, m[1])
	}
	return out
}

// pathParamSchema types a path parameter by its name: ids are uuids, keys and
// slugs are strings.
func pathParamSchema(name string) *openapi.Schema {
	if strings.HasSuffix(name, "ID") {
		return &openapi.Schema{Type: "string", Format: "uuid"}
	}
	if strings.HasSuffix(name, "Key") {
		return &openapi.Schema{Type: "string", Description: "An issue key such as CP-12, or a project key such as CP."}
	}
	return &openapi.Schema{Type: "string"}
}

func itoa(n int) string { return strconv.Itoa(n) }

// declareEnums tells the builder which named string types are closed lists.
func declareEnums(b *openapi.Builder) {
	set := func(v any, values ...string) { b.Enums[reflect.TypeOf(v)] = values }
	set(issue.Priority(""), "lowest", "low", "medium", "high", "highest")
	set(workflow.StatusCategory(""), "todo", "in_progress", "done")
	set(workflow.RuleKind(""), "condition", "validator", "postfunction")
	set(workflow.OptionKind(""), "text", "roles")
	set(board.Type(""), "scrum", "kanban")
	set(board.Grouping(""), "none", "assignee", "priority", "type")
	set(project.Kind(""), "software", "service", "business")
	set(sprint.State(""), "future", "active", "closed")
	set(auth.OrgRole(""), "owner", "admin", "member", "customer")
	set(auth.SignInMethod(""), "password", "provider", "none")
	set(git.HostKind(""), "github", "gitlab", "gitea")
	set(desk.QueueFilter(""), "open", "unassigned", "mine", "breached", "all")
	set(desk.Metric(""), "first_response", "resolution")
	set(field.Kind(""), "text", "number", "date", "select", "checkbox", "url")
	set(arrange.Scope(""), "builtin", "organization", "project")
	// The vocabulary comes from the package rather than a list written twice,
	// so a slot the server knows is one the client is made to draw.
	slots := make([]string, 0)
	for _, slot := range arrange.Slots() {
		slots = append(slots, string(slot))
	}
	b.Enums[reflect.TypeOf(arrange.Slot(""))] = slots
	areas := make([]string, 0)
	for _, area := range arrange.Areas() {
		areas = append(areas, string(area))
	}
	b.Enums[reflect.TypeOf(arrange.Area(""))] = areas
	set(project.Health(""), "on_track", "at_risk", "off_track")
	features := make([]string, 0)
	for _, f := range project.AllFeatures() {
		features = append(features, string(f))
	}
	b.Enums[reflect.TypeOf(project.Feature(""))] = features
	set(calendar.Kind(""), "issue", "sprint", "milestone", "version")
	b.FieldOverrides["Label.color"] = &openapi.Schema{Type: "string", Enum: label.Colors}
	b.FieldOverrides["LabelRef.color"] = &openapi.Schema{Type: "string", Enum: label.Colors}
	b.FieldOverrides["Backdrop.fit"] = &openapi.Schema{Type: "string", Enum: theme.BackdropFits}
	// Every kind of report, whichever kind of project it suits.
	seen := map[string]bool{}
	kinds := []string{}
	for _, pk := range []project.Kind{project.KindSoftware, project.KindBusiness, project.KindService} {
		for _, k := range report.Kinds(pk) {
			if !seen[string(k.Kind)] {
				seen[string(k.Kind)] = true
				kinds = append(kinds, string(k.Kind))
			}
		}
	}
	b.Enums[reflect.TypeOf(report.Kind(""))] = kinds
}

// declareOverrides describes the patch types, whose JSON is a plain value or
// null and nothing like their Go shape.
func declareOverrides(b *openapi.Builder) {
	uuidOrNull := openapi.Nullable(&openapi.Schema{Type: "string", Format: "uuid"})
	b.Overrides[reflect.TypeOf(datePatch{})] = openapi.Nullable(&openapi.Schema{Type: "string", Format: "date", Description: "A day, YYYY-MM-DD; null clears it."})
	b.Overrides[reflect.TypeOf(numberPatch{})] = openapi.Nullable(&openapi.Schema{Type: "number", Description: "A number; null clears it, leaving it out keeps it."})
	b.Overrides[reflect.TypeOf(uuidPatch{})] = uuidOrNull
	b.Overrides[reflect.TypeOf(assigneePatch{})] = uuidOrNull
	b.Overrides[reflect.TypeOf(leadPatch{})] = uuidOrNull
	b.Overrides[reflect.TypeOf(numberPatch{})] = openapi.Nullable(&openapi.Schema{Type: "number"})
	b.Overrides[reflect.TypeOf(docPatch{})] = openapi.Nullable(&openapi.Schema{Type: "object", Description: "A rich text document; null clears it."})
	b.Overrides[reflect.TypeOf(minutesPatch{})] = openapi.Nullable(&openapi.Schema{Type: "integer", Description: "Minutes; null clears it."})
	b.Overrides[reflect.TypeOf(perm.Set{})] = &openapi.Schema{Type: "object"}
	// Raw JSON fields that are always a document, or always an answer.
	for _, field := range []string{"Issue.description", "Comment.body", "CreateIssueRequest.description", "CommentRequest.body"} {
		b.FieldOverrides[field] = docSchema
	}
	b.FieldOverrides["Value.value"] = answerSchema
	// The sprint reference on an issue names the sprint's state as text,
	// because the issue package cannot import the sprint package; the values
	// are the sprint states all the same.
	b.FieldOverrides["SprintRef.state"] = &openapi.Schema{Type: "string", Enum: []string{"future", "active", "closed"}}
	b.FieldOverrides["SetIssueFieldRequest.value"] = openapi.Nullable(answerSchema)
	b.Overrides[reflect.TypeOf(reportUnion{})] = &openapi.Schema{OneOf: []*openapi.Schema{
		b.SchemaOf(report.Breakdown{}), b.SchemaOf(report.ThroughputReport{}), b.SchemaOf(report.WorkloadReport{}),
		b.SchemaOf(report.CycleTimeReport{}), b.SchemaOf(report.EpicsReport{}), b.SchemaOf(report.SprintReport{}),
		b.SchemaOf(report.VelocityReport{}), b.SchemaOf(report.SLAReport{}),
		b.SchemaOf(report.BurndownReport{}), b.SchemaOf(report.SprintHistoryReport{}),
		b.SchemaOf(report.ChartReport{}), b.SchemaOf(report.ChartSeriesReport{}), b.SchemaOf(report.MilestonesReport{}),
		b.SchemaOf(report.VersionsReport{}), b.SchemaOf(report.CumulativeFlowReport{}), b.SchemaOf(report.ControlChartReport{}),
		b.SchemaOf(report.CreatedResolvedReport{}), b.SchemaOf(report.AverageAgeReport{}), b.SchemaOf(report.ResolutionReport{}),
		b.SchemaOf(report.ReleaseBurndownReport{}), b.SchemaOf(report.CSATReport{}),
	}}
}

// Shorthands for the table.
var (
	uuidParam = &openapi.Schema{Type: "string", Format: "uuid"}
	boolParam = &openapi.Schema{Type: "boolean"}
	intParam  = &openapi.Schema{Type: "integer"}
	dateParam = &openapi.Schema{Type: "string", Format: "date"}
)

func none() map[int]any            { return map[int]any{204: nil} }
func ok(body any) map[int]any      { return map[int]any{200: body} }
func created(body any) map[int]any { return map[int]any{201: body} }

// issueListQuery is what both issue lists filter on.
var issueListQuery = []param{
	{name: "project", description: "Project key; only on the cross-project list."},
	{name: "text", description: "Words the summary contains."},
	{name: "q", description: "An NQL query, such as assignee = currentUser() AND statusCategory != done ORDER BY priority DESC. Its ORDER BY outranks orderBy."},
	{name: "assignee", description: "A user id, \"me\", or \"none\"."},
	{name: "reporter", description: "A user id or \"me\"."},
	{name: "status", schema: uuidParam, repeated: true},
	{name: "category", schema: &openapi.Schema{Type: "string", Enum: []string{"todo", "in_progress", "done"}}, repeated: true},
	{name: "type", schema: uuidParam, repeated: true},
	{name: "priority", schema: &openapi.Schema{Type: "string", Enum: []string{"lowest", "low", "medium", "high", "highest"}}, repeated: true},
	{name: "label", schema: uuidParam, repeated: true, description: "Issues carrying any of these labels."},
	{name: "milestone", schema: uuidParam, description: "Issues counting towards this milestone."},
	{name: "orderBy", description: "created, updated, priority, key, summary or status."},
	{name: "order", description: "asc or desc; desc unless said."},
	{name: "limit", schema: intParam, description: "1 to 200."},
	{name: "offset", schema: intParam},
}

// reportBody is whichever report the kind produces, answered bare.
var reportBody = reportUnion{}

// reportUnion stands in for "one of the reports"; the builder is told to
// describe it as such below rather than as a struct.
type reportUnion struct{}

// Route is one operation as the integration suite needs it: how to recognise
// a call to it, and what it may answer with.
type Route struct {
	Method, Path, ID string
	Statuses         []int
	Public, Binary   bool
	Redirect         bool
}

// Catalog lists every operation in the table.
func Catalog() []Route {
	out := make([]Route, 0, len(operations))
	for _, op := range operations {
		r := Route{Method: op.method, Path: op.path, Public: op.public, Binary: op.binary, Redirect: op.redirect}
		r.ID = op.operationID()
		for status := range op.responses {
			r.Statuses = append(r.Statuses, status)
		}
		out = append(out, r)
	}
	return out
}

// handleOpenAPI serves the document this process was built from.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, Spec())
}
