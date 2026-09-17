package httpapi

import (
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/armature/armature/backend/internal/arrange"
	"github.com/armature/armature/backend/internal/assistant"
	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/automation"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bulk"
	"github.com/armature/armature/backend/internal/calendar"
	"github.com/armature/armature/backend/internal/component"
	"github.com/armature/armature/backend/internal/csvio"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/filter"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/notify"
	"github.com/armature/armature/backend/internal/oidc"
	"github.com/armature/armature/backend/internal/openapi"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
	"github.com/armature/armature/backend/internal/version"
	"github.com/armature/armature/backend/internal/webhook"
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
	set(perm.Role(""), "global_administrator", "project_administrator", "scrum_master", "user", "reader")
	set(auth.OrgRole(""), "owner", "admin", "member", "customer")
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

// operations is the table. Order is by area, then by path.
var operations = []operation{
	// Health, outside authentication.
	{method: "GET", path: "/healthz", handler: "handleLiveness", tag: "health", summary: "Whether the process is running.", public: true,
		responses: ok(env{"status": ""})},
	{method: "GET", path: "/readyz", handler: "handleReadiness", tag: "health", summary: "Whether the process can serve traffic, and how the replicas are doing.", public: true,
		responses: map[int]any{200: env{"status": "", "routing": db.Stats{}}, 503: env{"status": ""}}},
	{method: "GET", path: "/openapi.json", handler: "handleOpenAPI", tag: "health", summary: "This document.", public: true,
		responses: ok(openapi.Document{})},

	// Signing up and in.
	{method: "GET", path: "/auth/signup", handler: "handleSignupOpen", tag: "auth", summary: "Whether a new organization may be created by signing up.", public: true,
		responses: ok(env{"open": true})},
	{method: "POST", path: "/auth/signup", handler: "handleSignup", tag: "auth", summary: "Create an account and its organization, and sign in.", public: true,
		request: signupRequest{}, responses: created(env{"principal": auth.Principal{}})},
	{method: "POST", path: "/auth/login", handler: "handleLogin", tag: "auth", summary: "Sign in with email and password.", public: true,
		request: loginRequest{}, responses: ok(env{"principal": auth.Principal{}})},
	{method: "POST", path: "/auth/invites/accept", handler: "handleAcceptInvite", tag: "auth", summary: "Join an organization with an invitation token.", public: true,
		request: acceptInviteRequest{}, responses: ok(env{"principal": auth.Principal{}})},
	{method: "POST", path: "/auth/invites/preview", handler: "handlePreviewInvite", tag: "auth", summary: "Where an invitation leads and whom it is for, before accepting it.", public: true,
		request: previewInviteRequest{}, responses: ok(env{"invite": auth.InvitePreview{}})},
	{method: "GET", path: "/auth/oidc/{orgSlug}/start", handler: "handleOIDCStart", tag: "auth", summary: "Begin signing in through the organization's identity provider.", public: true, redirect: true,
		query: []param{{name: "next", description: "Where to land afterwards."}}, responses: map[int]any{}},
	{method: "GET", path: "/auth/oidc/callback", handler: "handleOIDCCallback", tag: "auth", summary: "Where the identity provider sends the browser back.", public: true, redirect: true,
		query: []param{{name: "state"}, {name: "code"}, {name: "error"}}, responses: map[int]any{}},
	{method: "GET", path: "/desk/{orgSlug}", handler: "handleDeskEntry", tag: "portal", summary: "The desk behind a public address, and which of its desks let people in without a code.", public: true,
		responses: ok(env{"name": "", "slug": "", "open": []desk.OpenDoor{}})},
	{method: "POST", path: "/desk/{orgSlug}/codes", handler: "handlePortalCode", tag: "portal", summary: "Mail a one-time code to an address.", public: true,
		request: portalCodeRequest{}, responses: map[int]any{202: env{"expiresInSeconds": 0}}},
	{method: "POST", path: "/desk/{orgSlug}/sessions", handler: "handlePortalSession", tag: "portal", summary: "Enter the portal with a mailed code, or with a name and address at a desk whose door is open.", public: true,
		request: portalSessionRequest{}, responses: ok(env{"principal": auth.Principal{}})},
	{method: "POST", path: "/unwatch", handler: "handleUnwatch", tag: "portal", summary: "Stop following a request, with the token a mail carried.", public: true,
		request: unwatchRequest{}, responses: none()},
	{method: "POST", path: "/auth/logout", handler: "handleLogout", tag: "auth", summary: "End the session.", responses: none()},
	{method: "PATCH", path: "/auth/me", handler: "handleUpdateProfile", tag: "auth", summary: "Change your name, time zone or language.", request: updateProfileRequest{}, responses: ok(env{"principal": auth.Principal{}})},
	{method: "POST", path: "/auth/me/avatar", handler: "handleSetAvatar", tag: "auth", summary: "Put a picture on your account.", multipart: true, responses: ok(env{"principal": auth.Principal{}})},
	{method: "DELETE", path: "/auth/me/avatar", handler: "handleRemoveAvatar", tag: "auth", summary: "Take your picture away.", responses: none()},
	{method: "GET", path: "/auth/me/export", handler: "handleExportMe", tag: "auth", summary: "Everything held about you, as one JSON file.", binary: true, responses: ok(nil)},
	{method: "DELETE", path: "/auth/me", handler: "handleEraseMe", tag: "auth", summary: "Erase your account: your identity goes, what you wrote stays as Former user. From a browser session only.", responses: none()},
	{method: "GET", path: "/users/{userID}/avatar", handler: "handleAvatar", tag: "auth", summary: "Somebody's picture, for anyone in an organization with them.", binary: true, responses: map[int]any{}},
	{method: "GET", path: "/auth/me", handler: "handleMe", tool: "whoami", toolHelp: "Who the token belongs to and which organization it acts in.", tag: "auth", summary: "Who is signed in, and which organizations they belong to.",
		responses: ok(env{"principal": auth.Principal{}, "organizations": []auth.Membership{}})},
	{method: "POST", path: "/auth/switch-org", handler: "handleSwitchOrg", tag: "auth", summary: "Move the session to another organization.",
		request: switchOrgRequest{}, responses: ok(env{"organization": auth.Org{}})},
	{method: "GET", path: "/tokens", handler: "handleListAPITokens", tag: "auth", summary: "The caller's personal access tokens.", responses: ok(env{"tokens": []auth.APIToken{}})},
	{method: "POST", path: "/tokens", handler: "handleCreateAPIToken", tag: "auth", summary: "Make a personal access token; the secret is shown once.",
		request: createTokenRequest{}, responses: created(env{"token": auth.APIToken{}})},
	{method: "DELETE", path: "/tokens/{tokenID}", handler: "handleRevokeAPIToken", tag: "auth", summary: "Revoke a token.", responses: none()},
	{method: "GET", path: "/invites", handler: "handleListInvites", tag: "auth", summary: "Open invitations.", responses: ok(env{"invites": []auth.Invite{}})},
	{method: "POST", path: "/invites", handler: "handleCreateInvite", tag: "auth", summary: "Invite somebody: mailed when mail is set up, and the link shown once either way.",
		request: inviteRequest{}, responses: created(env{"invite": auth.Invite{}, "token": "", "link": "", "mailed": true})},
	{method: "DELETE", path: "/invites/{inviteID}", handler: "handleRevokeInvite", tag: "auth", summary: "Withdraw an invitation.", responses: none()},

	// The portal.
	{method: "GET", path: "/portal/requests/{issueKey}/watchers", handler: "handlePortalWatchers", tag: "portal", summary: "Who follows a request.", responses: ok(env{"watchers": []issue.Watcher{}})},
	{method: "POST", path: "/portal/requests/{issueKey}/watchers", handler: "handlePortalFollow", tag: "portal", summary: "Add somebody by address to follow a request.", request: portalFollowRequest{}, responses: created(env{"watcher": issue.Watcher{}})},
	{method: "DELETE", path: "/portal/requests/{issueKey}/watchers/{userID}", handler: "handlePortalUnfollow", tag: "portal", summary: "Stop somebody following a request.", responses: none()},
	{method: "GET", path: "/portal/requests/{issueKey}/attachments", handler: "handlePortalAttachments", tag: "portal", summary: "The files on your request, the desk's included.", responses: ok(env{"attachments": []attachment.Attachment{}})},
	{method: "POST", path: "/portal/requests/{issueKey}/attachments", handler: "handlePortalAttach", tag: "portal", summary: "Put a file on your request, as a multipart part named file.", multipart: true, responses: created(env{"attachment": attachment.Attachment{}})},
	{method: "GET", path: "/portal/attachments/{attachmentID}", handler: "handlePortalAttachment", tag: "portal", summary: "The bytes of a file on your request, as a download.", binary: true,
		query: []param{{name: "inline", description: "1 to show images, PDFs and text in place."}}, responses: ok(nil)},
	{method: "DELETE", path: "/portal/attachments/{attachmentID}", handler: "handlePortalDetach", tag: "portal", summary: "Take back a file you put on your request.", responses: none()},
	{method: "GET", path: "/portal/desks", handler: "handlePortalDesks", tag: "portal", summary: "The service desks a customer may raise requests with.", responses: ok(env{"desks": []desk.Desk{}})},
	{method: "POST", path: "/portal/requests", handler: "handlePortalRaise", tag: "portal", summary: "Raise a request.",
		request: portalRaiseRequest{}, responses: created(env{"request": issue.Issue{}})},
	{method: "GET", path: "/portal/requests", handler: "handlePortalRequests", tag: "portal", summary: "The caller's own requests.", responses: ok(env{"requests": []issue.Issue{}})},
	{method: "GET", path: "/portal/requests/{issueKey}", handler: "handlePortalRequest", tag: "portal", summary: "One request, with what the desk has said to the customer.", responses: ok(desk.Request{})},
	{method: "POST", path: "/portal/requests/{issueKey}/replies", handler: "handlePortalReply", tag: "portal", summary: "Reply to the desk.",
		request: portalReplyRequest{}, responses: created(env{"comment": issue.Comment{}})},

	// Metadata behind the pickers.
	// Rules and the webhooks they, and the stream, post to.
	{method: "GET", path: "/automation/catalog", handler: "handleAutomationCatalog", tag: "automation", summary: "What a rule may watch, test and do, and the topics a webhook may subscribe to.", responses: ok(env{"catalog": []automation.Descriptor{}, "topics": []string{}})},
	{method: "GET", path: "/projects/{projectKey}/automation/rules", handler: "handleListProjectRules", tool: "list_automation_rules", toolHelp: "A project's automation rules with their triggers, conditions and actions.", tag: "automation", summary: "A project's rules.", responses: ok(env{"rules": []automation.Rule{}})},
	{method: "POST", path: "/projects/{projectKey}/automation/rules", handler: "handleCreateProjectRule", tag: "automation", summary: "Make a rule for a project.", request: automation.Input{}, responses: created(env{"rule": automation.Rule{}})},
	{method: "GET", path: "/automation/rules", handler: "handleListOrgRules", tag: "automation", summary: "The organization's rules, which watch every project.", responses: ok(env{"rules": []automation.Rule{}})},
	{method: "POST", path: "/automation/rules", handler: "handleCreateOrgRule", tag: "automation", summary: "Make a rule for the whole organization.", request: automation.Input{}, responses: created(env{"rule": automation.Rule{}})},
	{method: "GET", path: "/automation/rules/{ruleID}", handler: "handleGetRule", tag: "automation", summary: "One rule.", responses: ok(env{"rule": automation.Rule{}})},
	{method: "PATCH", path: "/automation/rules/{ruleID}", handler: "handleUpdateRule", tag: "automation", summary: "Rewrite a rule.", request: automation.Input{}, responses: ok(env{"rule": automation.Rule{}})},
	{method: "DELETE", path: "/automation/rules/{ruleID}", handler: "handleDeleteRule", tag: "automation", summary: "Remove a rule and its log.", responses: none()},
	{method: "GET", path: "/automation/rules/{ruleID}/runs", handler: "handleRuleRuns", tag: "automation", summary: "A rule's log, newest first, with why each run did what it did.",
		query: []param{{name: "limit", schema: intParam, description: "1 to 200."}}, responses: ok(env{"runs": []automation.Run{}})},
	{method: "POST", path: "/automation/rules/{ruleID}/run", handler: "handleRunRule", tool: "run_automation_rule", toolHelp: "Run a rule now against one issue, or with none, and read what it did.", tag: "automation", summary: "Run a rule now, by hand.", request: runRuleRequest{}, responses: ok(env{"run": automation.Run{}})},
	{method: "POST", path: "/automation/hooks/{token}", handler: "handleIncomingHook", tag: "automation", summary: "What an outside system posts to start a rule; the address is the authorization.", public: true, raw: true, rawNote: "Any JSON object; issueKey names the issue to act on.", responses: map[int]any{202: incomingReceipt{}}},
	{method: "GET", path: "/webhooks", handler: "handleListWebhooks", tag: "webhooks", summary: "Where the organization's events are posted.", responses: ok(env{"webhooks": []webhook.Endpoint{}})},
	{method: "POST", path: "/webhooks", handler: "handleCreateWebhook", tag: "webhooks", summary: "Add an endpoint; the secret is shown once.", request: webhook.Input{}, responses: created(env{"webhook": webhook.Endpoint{}})},
	{method: "PATCH", path: "/webhooks/{endpointID}", handler: "handleUpdateWebhook", tag: "webhooks", summary: "Change an endpoint's name, address, topics or whether it is on.", request: webhook.Input{}, responses: ok(env{"webhook": webhook.Endpoint{}})},
	{method: "DELETE", path: "/webhooks/{endpointID}", handler: "handleDeleteWebhook", tag: "webhooks", summary: "Remove an endpoint and its log.", responses: none()},
	{method: "POST", path: "/webhooks/{endpointID}/rotate-secret", handler: "handleRotateWebhookEndpointSecret", tag: "webhooks", summary: "Issue a new secret; the old one stops at once.", responses: ok(env{"webhook": webhook.Endpoint{}})},
	{method: "POST", path: "/webhooks/{endpointID}/test", handler: "handleTestWebhook", tag: "webhooks", summary: "Post a ping now and read how it went.", responses: ok(env{"delivery": webhook.Delivery{}})},
	{method: "GET", path: "/webhooks/{endpointID}/deliveries", handler: "handleWebhookDeliveries", tag: "webhooks", summary: "An endpoint's log, newest first.",
		query: []param{{name: "limit", schema: intParam, description: "1 to 200."}}, responses: ok(env{"deliveries": []webhook.Delivery{}})},
	{method: "POST", path: "/webhooks/{endpointID}/deliveries/{deliveryID}/redeliver", handler: "handleRedeliverWebhook", tag: "webhooks", summary: "Try a logged delivery again, now.", responses: ok(env{"delivery": webhook.Delivery{}})},
	// The inbox: the caller's own, always.
	{method: "GET", path: "/notifications", handler: "handleInbox", tool: "list_notifications", toolHelp: "What the caller was told about their work, newest first.", tag: "notifications", summary: "What the caller was told, newest first.",
		query:     []param{{name: "unread", schema: boolParam, description: "Only what is not read yet."}, {name: "limit", schema: intParam, description: "1 to 200."}},
		responses: ok(env{"notifications": []notify.Notification{}})},
	{method: "GET", path: "/notifications/unread-count", handler: "handleUnreadCount", tag: "notifications", summary: "How many are unread; the badge.", responses: ok(unreadCount{})},
	{method: "POST", path: "/notifications/read", handler: "handleMarkRead", tag: "notifications", summary: "Mark some, or all, read.", request: markReadRequest{}, responses: none()},
	{method: "GET", path: "/notification-preferences", handler: "handleNotificationPreferences", tag: "notifications", summary: "How the caller wants to be told.", responses: ok(env{"preferences": notify.Preferences{}})},
	{method: "PUT", path: "/notification-preferences", handler: "handleSaveNotificationPreferences", tag: "notifications", summary: "Change how the caller is told.", request: notify.Preferences{}, responses: ok(env{"preferences": notify.Preferences{}})},
	{method: "DELETE", path: "/organization", handler: "handleDeleteOrganization", tag: "organization", summary: "Delete the organization and everything in it; owners only, with its address typed back.",
		query: []param{{name: "confirm", description: "The organization's address, typed back."}}, responses: none()},
	{method: "DELETE", path: "/members/{userID}", handler: "handleRemoveMember", tag: "organization", summary: "Let a member go: their membership, grants, groups, teams and tokens here.", responses: none()},
	{method: "GET", path: "/members", handler: "handleListMembers", tool: "list_members", toolHelp: "The organization's people, with the ids other tools take for assignees.", tag: "organization", summary: "The organization's people.", responses: ok(env{"members": []memberView{}})},
	{method: "GET", path: "/issue-types", handler: "handleListIssueTypes", tool: "list_issue_types", toolHelp: "The issue types and their ids, needed to file an issue of a given type.", tag: "organization", summary: "The issue types and where each sits in the hierarchy.", responses: ok(env{"issueTypes": []issueTypeView{}})},
	{method: "GET", path: "/statuses", handler: "handleListStatuses", tool: "list_statuses", toolHelp: "The statuses workflows are built from.", tag: "organization", summary: "The statuses workflows are built from.", responses: ok(env{"statuses": []workflow.Status{}})},
	{method: "POST", path: "/statuses", handler: "handleCreateStatus", tag: "organization", summary: "Coin a status workflows can be built from.", request: createStatusRequest{}, responses: created(env{"status": workflow.Status{}})},
	{method: "GET", path: "/link-types", handler: "handleListLinkTypes", tag: "organization", summary: "The ways two issues can relate.", responses: ok(env{"linkTypes": []issue.LinkTypeRef{}})},
	{method: "GET", path: "/roles", handler: "handleListRoles", tag: "access", summary: "The five roles and what each grants.", responses: ok(env{"roles": []roleView{}})},
	{method: "GET", path: "/access/me", handler: "handleMyAccess", tag: "access", summary: "What the caller may do, which decides which buttons to draw.",
		responses: ok(env{"grants": []perm.Grant{}, "projects": []string{}, "canAdministerOrg": false, "canCreateProject": false})},

	// Groups and role assignments.
	{method: "GET", path: "/groups", handler: "handleListGroups", tag: "access", summary: "Groups.", responses: ok(env{"groups": []perm.Group{}})},
	{method: "POST", path: "/groups", handler: "handleCreateGroup", tag: "access", summary: "Make a group.", request: createGroupRequest{}, responses: created(env{"group": perm.Group{}})},
	{method: "GET", path: "/groups/{groupID}", handler: "handleGetGroup", tag: "access", summary: "One group with its members.", responses: ok(env{"group": perm.Group{}})},
	{method: "DELETE", path: "/groups/{groupID}", handler: "handleDeleteGroup", tag: "access", summary: "Delete a group.", responses: none()},
	{method: "POST", path: "/groups/{groupID}/members", handler: "handleAddGroupMember", tag: "access", summary: "Put somebody in a group.", request: addGroupMemberRequest{}, responses: ok(env{"group": perm.Group{}})},
	{method: "DELETE", path: "/groups/{groupID}/members/{userID}", handler: "handleRemoveGroupMember", tag: "access", summary: "Take somebody out of a group.", responses: ok(env{"group": perm.Group{}})},
	{method: "GET", path: "/role-assignments", handler: "handleListAssignments", tag: "access", summary: "Who holds which role where.",
		query: []param{{name: "project", description: "Only grants over this project."}}, responses: ok(env{"assignments": []perm.Assignment{}})},
	{method: "POST", path: "/role-assignments", handler: "handleGrantRole", tag: "access", summary: "Grant a role to a person or a group.", request: grantRoleRequest{}, responses: created(env{"assignment": perm.Assignment{}})},
	{method: "DELETE", path: "/role-assignments/{assignmentID}", handler: "handleRevokeRole", tag: "access", summary: "Revoke a grant.", responses: none()},
	{method: "GET", path: "/oidc-provider", handler: "handleGetOIDCProvider", tag: "access", summary: "The organization's identity provider, if one is configured.", responses: ok(env{"provider": (*oidc.Provider)(nil)})},
	{method: "PUT", path: "/oidc-provider", handler: "handleSaveOIDCProvider", tag: "access", summary: "Configure the identity provider.", request: saveOIDCProviderRequest{}, responses: ok(env{"provider": oidc.Provider{}})},

	// Workflows and schemes.
	{method: "GET", path: "/workflows", handler: "handleListWorkflows", tag: "workflows", summary: "The organization's workflows.", responses: ok(env{"workflows": []workflow.Summary{}})},
	{method: "GET", path: "/workflows/rule-types", handler: "handleWorkflowRuleTypes", tag: "workflows", summary: "The conditions, validators and post-functions a workflow can use.",
		responses: ok(env{"ruleTypes": []workflow.RuleType{}})},
	{method: "GET", path: "/workflows/{workflowID}", handler: "handleGetWorkflow", tag: "workflows", summary: "One workflow with its rules by transition.",
		responses: ok(env{"workflow": workflow.Workflow{}, "rules": map[string][]workflow.Rule{}})},
	{method: "POST", path: "/workflows", handler: "handleCreateWorkflow", tag: "workflows", summary: "Author a workflow.", request: graphRequest{}, responses: created(env{"workflow": workflow.Workflow{}})},
	{method: "PUT", path: "/workflows/{workflowID}", handler: "handleSaveWorkflow", tag: "workflows", summary: "Save a workflow; open issues stay where they are.", request: graphRequest{}, responses: ok(env{"workflow": workflow.Workflow{}})},
	{method: "POST", path: "/workflows/{workflowID}/copy", handler: "handleCopyWorkflow", tag: "workflows", summary: "Copy a workflow under a new name.", request: copyWorkflowRequest{}, responses: created(env{"workflow": workflow.Workflow{}})},
	{method: "DELETE", path: "/workflows/{workflowID}", handler: "handleDeleteWorkflow", tag: "workflows", summary: "Delete a workflow no scheme uses.", responses: none()},
	{method: "GET", path: "/workflow-schemes", handler: "handleListSchemes", tag: "workflows", summary: "Which workflow answers for which issue type.", responses: ok(env{"schemes": []workflow.Scheme{}})},
	{method: "POST", path: "/workflow-schemes", handler: "handleCreateScheme", tag: "workflows", summary: "Make a scheme.", request: schemeRequest{}, responses: created(env{"scheme": workflow.Scheme{}})},
	{method: "PUT", path: "/workflow-schemes/{schemeID}", handler: "handleSaveScheme", tag: "workflows", summary: "Save a scheme.", request: schemeRequest{}, responses: ok(env{"scheme": workflow.Scheme{}})},
	{method: "PUT", path: "/workflow-schemes/{schemeID}/default", handler: "handleSetDefaultScheme", tag: "workflows", summary: "Make a scheme the organization's default.", responses: none()},
	{method: "DELETE", path: "/workflow-schemes/{schemeID}", handler: "handleDeleteScheme", tag: "workflows", summary: "Delete a scheme no project uses.", responses: none()},

	// Projects.
	{method: "GET", path: "/projects", handler: "handleListProjects", tool: "list_projects", toolHelp: "The projects the caller can see, with their keys.", tag: "projects", summary: "Projects.",
		query: []param{{name: "archived", schema: boolParam, description: "Include archived projects."}}, responses: ok(env{"projects": []project.Project{}})},
	{method: "GET", path: "/project-templates", handler: "handleListTemplates", tag: "projects", summary: "The templates a project can start from.", responses: ok(env{"templates": []template.Template{}})},
	{method: "GET", path: "/projects/key-check", handler: "handleCheckProjectKey", tag: "projects", summary: "Whether a key is free, or a suggestion from a name.",
		query:     []param{{name: "key", description: "A key to check."}, {name: "name", description: "A name to suggest a key for; answered with the suggestion alone."}},
		responses: ok(keyCheckResponse{})},
	{method: "POST", path: "/projects", handler: "handleCreateProject", tag: "projects", summary: "Make a project from a template.", request: createProjectRequest{}, responses: created(env{"project": project.Project{}})},
	{method: "GET", path: "/projects/{projectKey}", handler: "handleGetProject", tool: "get_project", toolHelp: "One project by its key.", tag: "projects", summary: "One project.", responses: ok(env{"project": project.Project{}})},
	{method: "PATCH", path: "/projects/{projectKey}", handler: "handleUpdateProject", tag: "projects", summary: "Edit a project.", request: updateProjectRequest{}, responses: ok(env{"project": project.Project{}})},
	{method: "DELETE", path: "/projects/{projectKey}", handler: "handleArchiveProject", tag: "projects", summary: "Archive a project.", responses: none()},
	{method: "POST", path: "/projects/{projectKey}/restore", handler: "handleRestoreProject", tag: "projects", summary: "Bring an archived project back.", responses: none()},
	{method: "GET", path: "/projects/{projectKey}/workflows", handler: "handleProjectWorkflows", tag: "workflows", summary: "Which workflow each issue type follows here, and who decided.",
		responses: ok(env{"projectKey": "", "schemeId": (*string)(nil), "assignments": []workflow.Assignment{}})},
	{method: "PUT", path: "/projects/{projectKey}/workflow-scheme", handler: "handleSetProjectScheme", tag: "workflows", summary: "Override the organization's scheme, or hand the decision back with null.",
		request: setProjectSchemeRequest{}, responses: ok(env{"project": project.Project{}})},
	{method: "PUT", path: "/projects/{projectKey}/workflow-assignments/{issueTypeID}", handler: "handleSetProjectAssignment", tag: "workflows", summary: "Decide which workflow one issue type follows here, or hand it back with null.",
		request: setAssignmentRequest{}, responses: ok(env{"projectKey": "", "schemeId": (*string)(nil), "assignments": []workflow.Assignment{}})},
	{method: "GET", path: "/projects/{projectKey}/hierarchy", handler: "handleProjectHierarchy", tool: "get_hierarchy", toolHelp: "The whole project as a tree of issues, roots first.", tag: "hierarchy", summary: "The whole project as a tree, roots first.", responses: ok(env{"tree": []issue.Node{}})},
	{method: "GET", path: "/projects/{projectKey}/plan", handler: "handleGetPlan", tool: "get_plan", toolHelp: "The project laid out against a calendar, with an optional NQL query marking what matches.", tag: "plan", summary: "The project laid out against a calendar.",
		query: []param{{name: "q", description: "An NQL query; the plan is returned whole and matched lists the keys it selects."}}, responses: ok(plan.Plan{})},

	// Issues.
	{method: "GET", path: "/issues", handler: "handleListIssues", tool: "search_issues", toolHelp: "Search issues across projects; q takes an NQL query such as assignee = currentUser() AND statusCategory != done.", tag: "issues", summary: "Search issues across projects.", query: issueListQuery, responses: ok(issue.Result{})},
	{method: "GET", path: "/projects/{projectKey}/issues", handler: "handleListIssues", tool: "list_project_issues", toolHelp: "A project's issues, filtered or narrowed by an NQL query in q.", id: "listProjectIssues", tag: "issues", summary: "A project's issues.", query: issueListQuery[1:], responses: ok(issue.Result{})},
	{method: "POST", path: "/projects/{projectKey}/issues", handler: "handleCreateIssue", tool: "create_issue", toolHelp: "File an issue in a project; typeId comes from list_issue_types.", id: "createProjectIssue", tag: "issues", summary: "File an issue in a project.", request: createIssueRequest{}, responses: created(env{"issue": issue.Issue{}})},
	{method: "POST", path: "/issues", handler: "handleCreateIssue", tag: "issues", summary: "File an issue, naming the project in the body.", request: createIssueRequest{}, responses: created(env{"issue": issue.Issue{}})},
	{method: "GET", path: "/issues/{issueKey}", handler: "handleGetIssue", tool: "get_issue", toolHelp: "One issue by its key, such as CP-12.", tag: "issues", summary: "One issue.", responses: ok(env{"issue": issue.Issue{}})},
	{method: "PATCH", path: "/issues/{issueKey}", handler: "handleUpdateIssue", tool: "update_issue", toolHelp: "Edit an issue's fields; its status moves through transition_issue instead.", tag: "issues", summary: "Edit an issue's fields; the status moves through transitions instead.", request: updateIssueRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	{method: "DELETE", path: "/issues/{issueKey}", handler: "handleDeleteIssue", tag: "issues", summary: "Delete an issue; organization administrators only.", responses: none()},
	{method: "GET", path: "/issues/{issueKey}/children", handler: "handleIssueChildren", tag: "hierarchy", summary: "The issues directly underneath.", responses: ok(env{"children": []issue.Issue{}})},
	{method: "GET", path: "/issues/{issueKey}/hierarchy", handler: "handleIssueHierarchy", tag: "hierarchy", summary: "The issue in context: above it, under it, and its roll-up.", responses: ok(issue.Hierarchy{})},
	{method: "PUT", path: "/issues/{issueKey}/parent", handler: "handleSetParent", tag: "hierarchy", summary: "Move the issue under another, or out with null.", request: parentRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	{method: "PUT", path: "/issues/{issueKey}/schedule", handler: "handleScheduleIssue", tag: "plan", summary: "Set both ends of the issue's range at once.", request: scheduleRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	{method: "PUT", path: "/issues/{issueKey}/sprint", handler: "handleSetIssueSprint", tag: "sprints", summary: "Commit the issue to a sprint, or back to the backlog with null.", request: setIssueSprintRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	{method: "PUT", path: "/issues/{issueKey}/estimate", handler: "handleSetIssueEstimate", tag: "sprints", summary: "Size the issue; null is unestimated.", request: setIssueEstimateRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	{method: "PUT", path: "/issues/{issueKey}/team", handler: "handleSetIssueTeam", tag: "teams", summary: "Hand the issue to a team, or back to the project with null.", request: setIssueTeamRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	{method: "GET", path: "/issues/{issueKey}/history", handler: "handleIssueHistory", tag: "issues", summary: "The changelog, newest first.", responses: ok(env{"history": []issue.HistoryEntry{}})},
	{method: "GET", path: "/issues/{issueKey}/links", handler: "handleListLinks", tag: "issues", summary: "How this issue relates to others.", responses: ok(env{"links": []issue.Link{}})},
	{method: "POST", path: "/issues/{issueKey}/links", handler: "handleAddLink", tag: "issues", summary: "Relate this issue to another.", request: linkRequest{}, responses: created(env{"link": issue.Link{}})},
	{method: "DELETE", path: "/issues/{issueKey}/links/{linkID}", handler: "handleDeleteLink", tag: "issues", summary: "Remove a relationship.", responses: none()},
	{method: "GET", path: "/issues/{issueKey}/transitions", handler: "handleListTransitions", tool: "list_transitions", toolHelp: "The moves the workflow allows the caller on this issue right now.", tag: "issues", summary: "The moves the workflow allows this caller right now.", responses: ok(env{"transitions": []workflow.Transition{}})},
	{method: "POST", path: "/issues/{issueKey}/transitions", handler: "handleTransitionIssue", tool: "transition_issue", toolHelp: "Move an issue through its workflow by a transition id from list_transitions.", tag: "issues", summary: "Move the issue through its workflow.", request: transitionRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	{method: "GET", path: "/issues/{issueKey}/comments", handler: "handleListComments", tool: "list_comments", toolHelp: "The comments on an issue.", tag: "comments", summary: "Comments; agents see internal notes too.", responses: ok(env{"comments": []issue.Comment{}})},
	{method: "POST", path: "/issues/{issueKey}/comments", handler: "handleAddComment", tool: "add_comment", toolHelp: "Comment on an issue; text is plain words.", tag: "comments", summary: "Comment on an issue.", request: commentRequest{}, responses: created(env{"comment": issue.Comment{}})},
	{method: "PATCH", path: "/issues/{issueKey}/comments/{commentID}", handler: "handleUpdateComment", tag: "comments", summary: "Edit a comment.", request: commentRequest{}, responses: ok(env{"comment": issue.Comment{}})},
	{method: "DELETE", path: "/issues/{issueKey}/comments/{commentID}", handler: "handleDeleteComment", tag: "comments", summary: "Delete a comment.", responses: none()},
	{method: "GET", path: "/issues/{issueKey}/watchers", handler: "handleListWatchers", tag: "issues", summary: "Who is told about an issue.", responses: ok(env{"watchers": []issue.Watcher{}})},
	{method: "POST", path: "/issues/{issueKey}/watchers", handler: "handleAddWatcher", tag: "issues", summary: "Add a watcher by account or by address, or watch it yourself.", request: addWatcherRequest{}, responses: created(env{"watcher": issue.Watcher{}})},
	{method: "DELETE", path: "/issues/{issueKey}/watchers/{userID}", handler: "handleRemoveWatcher", tag: "issues", summary: "Stop somebody watching an issue.", responses: none()},
	{method: "POST", path: "/issues/{issueKey}/notes", handler: "handleAddNote", tag: "service desk", summary: "Add an internal note the customer never sees.", request: commentRequest{}, responses: created(env{"comment": issue.Comment{}})},

	// Custom fields.
	{method: "GET", path: "/field-kinds", handler: "handleFieldKinds", tag: "fields", summary: "What a custom field can be.", responses: ok(env{"kinds": []field.KindInfo{}})},
	{method: "GET", path: "/projects/{projectKey}/fields", handler: "handleListFields", tag: "fields", summary: "A project's custom fields.", responses: ok(env{"fields": []field.Field{}})},
	{method: "POST", path: "/projects/{projectKey}/fields", handler: "handleCreateField", tag: "fields", summary: "Define a field.", request: fieldRequest{}, responses: created(env{"field": field.Field{}})},
	{method: "PATCH", path: "/fields/{fieldID}", handler: "handleUpdateField", tag: "fields", summary: "Rename a field or change its options.", request: fieldRequest{}, responses: ok(env{"field": field.Field{}})},
	{method: "DELETE", path: "/fields/{fieldID}", handler: "handleDeleteField", tag: "fields", summary: "Delete a field and every answer to it.", responses: none()},
	{method: "GET", path: "/projects/{projectKey}/issue-arrangement", handler: "handleProjectArrangement", tag: "fields", summary: "How this project arranges an issue's fields, per issue type.", responses: ok(env{"arrangements": []arrange.Arrangement{}})},
	{method: "PUT", path: "/projects/{projectKey}/issue-arrangement", handler: "handleSetProjectArrangement", tag: "fields", summary: "Arrange an issue's fields here; null places hand the type back to the organization.", request: arrangementRequest{}, responses: ok(env{"arrangements": []arrange.Arrangement{}})},
	{method: "GET", path: "/issue-arrangement", handler: "handleOrgArrangement", tag: "fields", summary: "How the organization arranges an issue's fields, per issue type.", responses: ok(env{"arrangements": []arrange.Arrangement{}})},
	{method: "PUT", path: "/issue-arrangement", handler: "handleSetOrgArrangement", tag: "fields", summary: "Arrange an issue's fields for every project; null places restore the built-in arrangement.", request: arrangementRequest{}, responses: ok(env{"arrangements": []arrange.Arrangement{}})},
	{method: "GET", path: "/issues/{issueKey}/fields", handler: "handleIssueFields", tag: "fields", summary: "Every field of the project with this issue's answer.", responses: ok(env{"values": []field.Value{}})},
	{method: "PUT", path: "/issues/{issueKey}/fields/{fieldID}", handler: "handleSetIssueField", tag: "fields", summary: "Answer a field, or clear it with null.", request: setIssueFieldRequest{}, responses: ok(env{"value": field.Value{}})},

	// Labels and time.
	{method: "GET", path: "/labels", handler: "handleListLabels", tool: "list_labels", toolHelp: "The organization's labels and how often each is used.", tag: "labels", summary: "The organization's labels, with how often each is used.", responses: ok(env{"labels": []label.Label{}})},
	{method: "POST", path: "/labels", handler: "handleCreateLabel", tag: "labels", summary: "Coin a label.", request: labelRequest{}, responses: created(env{"label": label.Label{}})},
	{method: "PATCH", path: "/labels/{labelID}", handler: "handleUpdateLabel", tag: "labels", summary: "Rename or recolour a label everywhere.", request: labelRequest{}, responses: ok(env{"label": label.Label{}})},
	{method: "DELETE", path: "/labels/{labelID}", handler: "handleDeleteLabel", tag: "labels", summary: "Remove a label from the organization and every issue.", responses: none()},
	{method: "PUT", path: "/issues/{issueKey}/labels", handler: "handleSetIssueLabels", tag: "labels", summary: "Make the issue carry exactly these labels, by name; new words are coined.", request: setIssueLabelsRequest{}, responses: ok(env{"labels": []issue.LabelRef{}})},
	{method: "GET", path: "/issues/{issueKey}/worklogs", handler: "handleListWorklogs", tag: "time", summary: "The time logged on an issue.", responses: ok(env{"worklogs": []issue.Worklog{}})},
	{method: "POST", path: "/issues/{issueKey}/worklogs", handler: "handleLogWork", tag: "time", summary: "Log time; what remains comes down by it.", request: worklogRequest{}, responses: created(env{"worklog": issue.Worklog{}})},
	{method: "PATCH", path: "/worklogs/{worklogID}", handler: "handleUpdateWorklog", tag: "time", summary: "Correct an entry of your own.", request: worklogRequest{}, responses: ok(env{"worklog": issue.Worklog{}})},
	{method: "DELETE", path: "/worklogs/{worklogID}", handler: "handleDeleteWorklog", tag: "time", summary: "Remove an entry of your own.", responses: none()},

	// Attachments.
	{method: "GET", path: "/issues/{issueKey}/attachments", handler: "handleListAttachments", tag: "attachments", summary: "The files on an issue.", responses: ok(env{"attachments": []attachment.Attachment{}})},
	{method: "POST", path: "/issues/{issueKey}/attachments", handler: "handleUploadAttachment", tag: "attachments", summary: "Upload a file, as a multipart part named file.", multipart: true, responses: created(env{"attachment": attachment.Attachment{}})},
	{method: "GET", path: "/attachments/{attachmentID}", handler: "handleDownloadAttachment", tag: "attachments", summary: "The bytes, as a download.", binary: true,
		query: []param{{name: "inline", description: "1 to show images, PDFs and text in place."}}, responses: ok(nil)},
	{method: "DELETE", path: "/attachments/{attachmentID}", handler: "handleDeleteAttachment", tag: "attachments", summary: "Remove a file.", responses: none()},

	// Boards.
	{method: "GET", path: "/projects/{projectKey}/board", handler: "handleGetBoard", tag: "boards", summary: "The board the project opens on.", responses: ok(env{"board": board.Board{}})},
	{method: "PATCH", path: "/projects/{projectKey}/board", handler: "handleUpdateBoard", tag: "boards", summary: "Change how the opening board groups its cards.", request: boardSettingsRequest{}, responses: none()},
	{method: "POST", path: "/projects/{projectKey}/board/move", handler: "handleMoveCard", tag: "boards", summary: "Drop a card on a swimlane, which takes the workflow transition.", request: moveCardRequest{}, responses: ok(board.MoveResult{})},
	{method: "POST", path: "/projects/{projectKey}/board/swimlanes", handler: "handleAddSwimlane", tag: "boards", summary: "Add a swimlane.", request: swimlaneRequest{}, responses: created(env{"swimlane": board.Swimlane{}})},
	{method: "PUT", path: "/projects/{projectKey}/board/swimlanes", handler: "handleReorderSwimlanes", tag: "boards", summary: "Set the left to right order.", request: reorderSwimlanesRequest{}, responses: none()},
	{method: "PATCH", path: "/projects/{projectKey}/board/swimlanes/{swimlaneID}", handler: "handleUpdateSwimlane", tag: "boards", summary: "Change a swimlane's name, states or limit.", request: updateSwimlaneRequest{}, responses: none()},
	{method: "DELETE", path: "/projects/{projectKey}/board/swimlanes/{swimlaneID}", handler: "handleDeleteSwimlane", tag: "boards", summary: "Remove a swimlane.", responses: none()},
	{method: "GET", path: "/projects/{projectKey}/boards", handler: "handleListBoards", tool: "list_boards", toolHelp: "A project's boards.", tag: "boards", summary: "A project's boards.", responses: ok(env{"boards": []board.Summary{}})},
	{method: "POST", path: "/projects/{projectKey}/boards", handler: "handleCreateBoard", tag: "boards", summary: "Add a board, scrum or kanban, for the project or a team.", request: boardRequest{}, responses: created(env{"board": board.Board{}})},
	{method: "GET", path: "/boards/{boardID}", handler: "handleGetBoardByID", tool: "get_board", toolHelp: "One board with its columns and cards.", tag: "boards", summary: "One board.", responses: ok(env{"board": board.Board{}})},
	{method: "GET", path: "/sprints/{sprintID}/board", handler: "handleGetSprintBoard", tool: "get_sprint_board", toolHelp: "A sprint's board, showing only what is committed to it.", tag: "boards", summary: "The sprint's board: its stream's board showing only what is committed to it.", responses: ok(env{"board": board.Board{}})},
	{method: "PATCH", path: "/boards/{boardID}", handler: "handleUpdateBoardByID", tag: "boards", summary: "Edit a board.", request: boardRequest{}, responses: ok(env{"board": board.Board{}})},
	{method: "DELETE", path: "/boards/{boardID}", handler: "handleDeleteBoard", tag: "boards", summary: "Delete a board that is not the project's last.", responses: none()},
	{method: "GET", path: "/boards/{boardID}/backlog", handler: "handleGetBacklog", tag: "boards", summary: "The work the board's stream has not committed to a sprint.", responses: ok(env{"cards": []board.Card{}})},

	// Milestones.
	{method: "GET", path: "/projects/{projectKey}/milestones", handler: "handleListMilestones", tool: "list_milestones", toolHelp: "A project's milestones, each with its progress.", tag: "milestones", summary: "A project's milestones, each with its progress.",
		query: []param{{name: "closed", schema: boolParam, description: "Include closed milestones."}}, responses: ok(env{"milestones": []milestone.Milestone{}})},
	{method: "POST", path: "/projects/{projectKey}/milestones", handler: "handleCreateMilestone", tag: "milestones", summary: "Set a milestone.", request: milestoneRequest{}, responses: created(env{"milestone": milestone.Milestone{}})},
	{method: "PATCH", path: "/milestones/{milestoneID}", handler: "handleUpdateMilestone", tag: "milestones", summary: "Edit a milestone.", request: milestoneRequest{}, responses: ok(env{"milestone": milestone.Milestone{}})},
	{method: "POST", path: "/milestones/{milestoneID}/close", handler: "handleCloseMilestone", tag: "milestones", summary: "Close a milestone; what was assigned stays for the record.", responses: ok(env{"milestone": milestone.Milestone{}})},
	{method: "POST", path: "/milestones/{milestoneID}/reopen", handler: "handleReopenMilestone", tag: "milestones", summary: "Reopen a closed milestone.", responses: ok(env{"milestone": milestone.Milestone{}})},
	{method: "DELETE", path: "/milestones/{milestoneID}", handler: "handleDeleteMilestone", tag: "milestones", summary: "Delete a milestone; its issues are left unassigned.", responses: none()},
	{method: "PUT", path: "/issues/{issueKey}/milestone", handler: "handleSetIssueMilestone", tag: "milestones", summary: "Make the issue count towards a milestone, or towards none with null.", request: setIssueMilestoneRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	// The desk's extras.
	{method: "GET", path: "/projects/{projectKey}/articles", handler: "handleListArticles", tool: "list_articles", toolHelp: "A service desk's knowledge base, drafts included.", tag: "desk", summary: "A desk's articles, published and drafts.", responses: ok(env{"articles": []desk.Article{}})},
	{method: "POST", path: "/projects/{projectKey}/articles", handler: "handleCreateArticle", tag: "desk", summary: "Write an article.", request: desk.ArticleInput{}, responses: created(env{"article": desk.Article{}})},
	{method: "PATCH", path: "/articles/{articleID}", handler: "handleUpdateArticle", tag: "desk", summary: "Edit, publish or unpublish an article.", request: desk.ArticleInput{}, responses: ok(env{"article": desk.Article{}})},
	{method: "DELETE", path: "/articles/{articleID}", handler: "handleDeleteArticle", tag: "desk", summary: "Remove an article.", responses: none()},
	{method: "GET", path: "/portal/desks/{projectKey}/articles", handler: "handlePortalArticles", tag: "portal", summary: "The published articles that match, offered before a request is raised.",
		query: []param{{name: "q", description: "Words to look for; empty lists the first few."}}, responses: ok(env{"articles": []desk.Article{}})},
	{method: "GET", path: "/portal/articles/{articleID}", handler: "handlePortalArticle", tag: "portal", summary: "One published article.", responses: ok(env{"article": desk.Article{}})},
	{method: "GET", path: "/projects/{projectKey}/canned-responses", handler: "handleListCanned", tag: "desk", summary: "The replies a desk's agents keep ready.", responses: ok(env{"responses": []desk.CannedResponse{}})},
	{method: "POST", path: "/projects/{projectKey}/canned-responses", handler: "handleCreateCanned", tag: "desk", summary: "Keep a reply ready; {{customer.name}}, {{issue.key}}, {{issue.summary}} and {{agent.name}} are filled in.", request: desk.CannedInput{}, responses: created(env{"response": desk.CannedResponse{}})},
	{method: "PATCH", path: "/canned-responses/{responseID}", handler: "handleUpdateCanned", tag: "desk", summary: "Change a canned response.", request: desk.CannedInput{}, responses: ok(env{"response": desk.CannedResponse{}})},
	{method: "DELETE", path: "/canned-responses/{responseID}", handler: "handleDeleteCanned", tag: "desk", summary: "Remove a canned response.", responses: none()},
	{method: "POST", path: "/canned-responses/{responseID}/render", handler: "handleRenderCanned", tag: "desk", summary: "A canned response with its placeholders filled for one request.", request: renderRequest{}, responses: ok(rendered{})},
	{method: "GET", path: "/csat/{token}", handler: "handleRatingPage", tag: "portal", summary: "What the rating link is about; the token is the whole of the authorization.", public: true, responses: ok(env{"rating": desk.RatingPage{}})},
	{method: "POST", path: "/csat/{token}", handler: "handleRate", tag: "portal", summary: "Rate a resolved request, 1 to 5, once.", public: true, request: rateRequest{}, responses: ok(env{"rating": desk.Rating{}})},
	{method: "GET", path: "/issues/{issueKey}/csat", handler: "handleIssueRating", tag: "desk", summary: "What the customer said of a resolved request.", responses: ok(env{"rating": desk.Rating{}})},
	{method: "GET", path: "/projects/{projectKey}/business-calendar", handler: "handleGetCalendar", tag: "desk", summary: "When the desk is open, for goals that count only those hours.", responses: ok(env{"calendar": desk.Calendar{}})},
	{method: "PUT", path: "/projects/{projectKey}/business-calendar", handler: "handleSaveCalendar", tag: "desk", summary: "Set when the desk is open.", request: desk.Calendar{}, responses: ok(env{"calendar": desk.Calendar{}})},
	// The organization's record, its shared fields, a project's health and its month.
	{method: "GET", path: "/audit", handler: "handleAuditLog", tool: "list_audit_log", toolHelp: "Who did what to the organization: roles, groups, projects, workflows, tokens and sign-ins, newest first.", tag: "audit", summary: "The audit log, newest first, with the actions it holds for a filter.",
		query:     []param{{name: "action", description: "One action, such as role.granted."}, {name: "actor", description: "A user id."}, {name: "from", description: "The first day, YYYY-MM-DD."}, {name: "to", description: "The last day, YYYY-MM-DD, inclusive."}, {name: "before", description: "The createdAt of the last row shown, for the next page."}, {name: "limit", description: "Rows per page, at most 200."}},
		responses: ok(env{"entries": []audit.Row{}, "actions": []string{}})},
	{method: "GET", path: "/audit/export", handler: "handleAuditExport", tag: "audit", summary: "The same log as a CSV file.", binary: true,
		query:     []param{{name: "action", description: "One action."}, {name: "actor", description: "A user id."}, {name: "from", description: "The first day, YYYY-MM-DD."}, {name: "to", description: "The last day, YYYY-MM-DD, inclusive."}},
		responses: ok(nil)},
	{method: "GET", path: "/fields", handler: "handleListOrgFields", tag: "fields", summary: "The fields every project of the organization has.", responses: ok(env{"fields": []field.Field{}})},
	{method: "POST", path: "/fields", handler: "handleCreateOrgField", tag: "fields", summary: "Define a field for every project.", request: fieldRequest{}, responses: created(env{"field": field.Field{}})},
	{method: "POST", path: "/fields/{fieldID}/promote", handler: "handlePromoteField", tag: "fields", summary: "Make a project's field the organization's, folding same-named fields into it.", responses: ok(env{"field": field.Field{}})},
	{method: "GET", path: "/projects/{projectKey}/status-updates", handler: "handleListStatusUpdates", tool: "list_status_updates", toolHelp: "How a project has said it is doing, newest first.", tag: "projects", summary: "The project's status updates, newest first.", responses: ok(env{"updates": []project.StatusUpdate{}})},
	{method: "POST", path: "/projects/{projectKey}/status-updates", handler: "handlePostStatusUpdate", tool: "post_status_update", toolHelp: "Say how a project is doing: on_track, at_risk or off_track, with a note and an optional target date.", tag: "projects", summary: "Post how the project is doing.", request: statusUpdateRequest{}, responses: created(env{"update": project.StatusUpdate{}})},
	{method: "GET", path: "/projects/{projectKey}/calendar", handler: "handleCalendarMonth", tag: "plan", summary: "Everything dated in a month: issues, sprints, milestones and versions.",
		query:     []param{{name: "month", description: "YYYY-MM; this month when left out."}},
		responses: ok(env{"month": calendar.Month{}})},

	// Saved filters, and what a selection or a file does to many issues.
	{method: "GET", path: "/filters", handler: "handleListFilters", tool: "list_filters", toolHelp: "The caller's saved filters and the organization's shared ones, each a named query.", tag: "filters", summary: "Saved filters the caller may see: theirs, then the shared ones.", responses: ok(env{"filters": []filter.Filter{}})},
	{method: "POST", path: "/filters", handler: "handleCreateFilter", tag: "filters", summary: "Save a query under a name.", request: filter.Input{}, responses: created(env{"filter": filter.Filter{}})},
	{method: "GET", path: "/filters/{filterID}", handler: "handleGetFilter", tag: "filters", summary: "One saved filter.", responses: ok(env{"filter": filter.Filter{}})},
	{method: "PATCH", path: "/filters/{filterID}", handler: "handleUpdateFilter", tag: "filters", summary: "Change a saved filter; the owner's to do.", request: filter.Input{}, responses: ok(env{"filter": filter.Filter{}})},
	{method: "DELETE", path: "/filters/{filterID}", handler: "handleDeleteFilter", tag: "filters", summary: "Remove a saved filter; the owner's to do.", responses: none()},
	{method: "PUT", path: "/filters/{filterID}/star", handler: "handleStarFilter", tag: "filters", summary: "Keep a filter close.", responses: ok(env{"filter": filter.Filter{}})},
	{method: "DELETE", path: "/filters/{filterID}/star", handler: "handleUnstarFilter", tag: "filters", summary: "Let a filter go from the starred ones.", responses: ok(env{"filter": filter.Filter{}})},
	{method: "PUT", path: "/filters/{filterID}/subscription", handler: "handleSubscribeFilter", tag: "filters", summary: "Have the result mailed daily or weekly.", request: filter.Subscription{}, responses: ok(env{"filter": filter.Filter{}})},
	{method: "DELETE", path: "/filters/{filterID}/subscription", handler: "handleUnsubscribeFilter", tag: "filters", summary: "Stop the mail.", responses: ok(env{"filter": filter.Filter{}})},
	{method: "GET", path: "/filters/{filterID}/issues", handler: "handleRunFilter", tool: "run_filter", toolHelp: "The issues a saved filter matches right now, as the caller.", tag: "filters", summary: "What a saved filter matches, as the caller.",
		query: []param{{name: "limit", schema: intParam, description: "1 to 200."}, {name: "offset", schema: intParam}}, responses: ok(issue.Result{})},
	{method: "GET", path: "/issues/suggest", handler: "handleSuggest", tag: "issues", summary: "What the search bar could say next: words the query takes at the caret, and the issues the words so far find.",
		query:     []param{{name: "q", description: "The text so far."}, {name: "at", description: "The caret's position in q, in characters; the end when absent."}, {name: "project", description: "A project key, to scope names and issues."}, {name: "limit", description: "Words and issues to offer, at most 20."}},
		responses: ok(issue.Suggestions{})},
	{method: "GET", path: "/issues/export", handler: "handleExportIssues", tag: "issues", summary: "The issues a query matches, as a CSV file with the chosen columns.", binary: true,
		query:     []param{{name: "q", description: "An NQL query; without one, everything the caller can see."}, {name: "project", description: "A project key to stay inside."}, {name: "columns", description: "Comma-separated column names; the default is key, summary, type, status, priority, assignee, created."}},
		responses: ok(nil)},
	{method: "POST", path: "/issues/bulk", handler: "handleBulkEdit", tool: "bulk_edit_issues", toolHelp: "Change many issues at once: priority, assignee, sprint, milestone, team, labels, or a transition by name; each refusal is named.", tag: "issues", summary: "Change many issues at once; one refused leaves the rest done.", request: bulkRequest{}, responses: ok(bulk.Result{})},
	{method: "POST", path: "/projects/{projectKey}/import/preview", handler: "handleImportPreview", tag: "issues", summary: "A CSV file's columns, the words in them and the people it names.", multipart: true, parts: []string{"mapping"}, responses: ok(importPreview{})},
	{method: "POST", path: "/projects/{projectKey}/import", handler: "handleImport", tag: "issues", summary: "Make an issue per row of a CSV file, or say what would be made.", multipart: true, parts: []string{"mapping", "values", "people"},
		query: []param{{name: "dryRun", schema: boolParam, description: "Report without making anything."}}, responses: ok(env{"report": csvio.ImportReport{}})},
	{method: "POST", path: "/issues/{issueKey}/clone", handler: "handleCloneIssue", tag: "issues", summary: "Copy an issue, with its links and subtasks on request.", request: issue.CloneOptions{}, responses: created(env{"issue": issue.Issue{}})},
	{method: "POST", path: "/issues/{issueKey}/move", handler: "handleMoveIssue", tag: "issues", summary: "Put an issue in another project; it keeps its history and its old address still finds it.", request: moveIssueRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	// Versions and components.
	{method: "GET", path: "/projects/{projectKey}/versions", handler: "handleListVersions", tool: "list_versions", toolHelp: "A project's versions with how far each has got; archived ones on request.", tag: "versions", summary: "A project's versions, unreleased first.",
		query: []param{{name: "archived", schema: boolParam, description: "Include archived versions."}}, responses: ok(env{"versions": []version.Version{}})},
	{method: "POST", path: "/projects/{projectKey}/versions", handler: "handleCreateVersion", tag: "versions", summary: "Add a version.", request: versionRequest{}, responses: created(env{"version": version.Version{}})},
	{method: "PATCH", path: "/versions/{versionID}", handler: "handleUpdateVersion", tag: "versions", summary: "Edit a version.", request: versionRequest{}, responses: ok(env{"version": version.Version{}})},
	{method: "POST", path: "/versions/{versionID}/release", handler: "handleReleaseVersion", tag: "versions", summary: "Mark a version shipped.", responses: ok(env{"version": version.Version{}})},
	{method: "POST", path: "/versions/{versionID}/unrelease", handler: "handleUnreleaseVersion", tag: "versions", summary: "Take a release back.", responses: ok(env{"version": version.Version{}})},
	{method: "POST", path: "/versions/{versionID}/archive", handler: "handleArchiveVersion", tag: "versions", summary: "Put a released version away.", responses: ok(env{"version": version.Version{}})},
	{method: "DELETE", path: "/versions/{versionID}", handler: "handleDeleteVersion", tag: "versions", summary: "Remove a version; issues stop naming it.", responses: none()},
	{method: "GET", path: "/versions/{versionID}/notes", handler: "handleReleaseNotes", tag: "versions", summary: "What the version fixed, done issues by type.", responses: ok(env{"notes": version.Notes{}})},
	{method: "PUT", path: "/issues/{issueKey}/versions", handler: "handleSetIssueVersions", tag: "versions", summary: "Name what fixes the issue and what it affects, in full.", request: setIssueVersionsRequest{}, responses: ok(env{"issue": issue.Issue{}})},
	{method: "GET", path: "/projects/{projectKey}/components", handler: "handleListComponents", tool: "list_components", toolHelp: "A project's components with their leads and default assignees.", tag: "components", summary: "A project's components.", responses: ok(env{"components": []component.Component{}})},
	{method: "POST", path: "/projects/{projectKey}/components", handler: "handleCreateComponent", tag: "components", summary: "Add a component.", request: componentRequest{}, responses: created(env{"component": component.Component{}})},
	{method: "PATCH", path: "/components/{componentID}", handler: "handleUpdateComponent", tag: "components", summary: "Edit a component.", request: componentRequest{}, responses: ok(env{"component": component.Component{}})},
	{method: "DELETE", path: "/components/{componentID}", handler: "handleDeleteComponent", tag: "components", summary: "Remove a component; issues leave it.", responses: none()},
	{method: "PUT", path: "/issues/{issueKey}/components", handler: "handleSetIssueComponents", tag: "components", summary: "Put the issue in these components and no others.", request: setIssueComponentsRequest{}, responses: ok(env{"issue": issue.Issue{}})},

	// Sprints.
	{method: "GET", path: "/projects/{projectKey}/sprints", handler: "handleListSprints", tool: "list_sprints", toolHelp: "A project's sprints.", tag: "sprints", summary: "A project's sprints.",
		query: []param{{name: "closed", schema: boolParam, description: "Include completed sprints."}, {name: "team", schema: uuidParam, description: "Only one team's."}}, responses: ok(env{"sprints": []sprint.Sprint{}})},
	{method: "POST", path: "/projects/{projectKey}/sprints", handler: "handleCreateSprint", tag: "sprints", summary: "Plan a sprint.", request: sprintRequest{}, responses: created(env{"sprint": sprint.Sprint{}})},
	{method: "PATCH", path: "/sprints/{sprintID}", handler: "handleUpdateSprint", tag: "sprints", summary: "Edit a sprint.", request: sprintRequest{}, responses: ok(env{"sprint": sprint.Sprint{}})},
	{method: "POST", path: "/sprints/{sprintID}/start", handler: "handleStartSprint", tag: "sprints", summary: "Start a sprint; one runs per team at a time.", responses: ok(env{"sprint": sprint.Sprint{}})},
	{method: "POST", path: "/sprints/{sprintID}/complete", handler: "handleCompleteSprint", tag: "sprints", summary: "Complete a sprint and carry unfinished work onward.", request: completeSprintRequest{}, responses: ok(env{"report": sprint.Report{}})},
	{method: "DELETE", path: "/sprints/{sprintID}", handler: "handleDeleteSprint", tag: "sprints", summary: "Delete a sprint that has not started.", responses: none()},

	// Teams.
	{method: "GET", path: "/projects/{projectKey}/teams", handler: "handleListTeams", tool: "list_teams", toolHelp: "A project's teams.", tag: "teams", summary: "A project's teams.", responses: ok(env{"teams": []team.Team{}})},
	{method: "POST", path: "/projects/{projectKey}/teams", handler: "handleCreateTeam", tag: "teams", summary: "Form a team.", request: teamRequest{}, responses: created(env{"team": team.Team{}})},
	{method: "GET", path: "/teams/{teamID}", handler: "handleGetTeam", tag: "teams", summary: "One team with its members.", responses: ok(env{"team": team.Team{}})},
	{method: "PATCH", path: "/teams/{teamID}", handler: "handleUpdateTeam", tag: "teams", summary: "Edit a team.", request: teamRequest{}, responses: ok(env{"team": team.Team{}})},
	{method: "DELETE", path: "/teams/{teamID}", handler: "handleDeleteTeam", tag: "teams", summary: "Delete a team carrying no work.", responses: none()},
	{method: "POST", path: "/teams/{teamID}/members", handler: "handleAddTeamMember", tag: "teams", summary: "Put somebody on the team.", request: addTeamMemberRequest{}, responses: ok(env{"team": team.Team{}})},
	{method: "DELETE", path: "/teams/{teamID}/members/{userID}", handler: "handleRemoveTeamMember", tag: "teams", summary: "Take somebody off the team.", responses: ok(env{"team": team.Team{}})},

	// Repositories and what flows in from them.
	{method: "GET", path: "/projects/{projectKey}/repositories", handler: "handleListRepositories", tag: "git", summary: "The repositories connected to a project.", responses: ok(env{"repositories": []git.Repository{}})},
	{method: "POST", path: "/projects/{projectKey}/repositories", handler: "handleConnectRepository", tag: "git", summary: "Connect a repository; the webhook secret is shown once.",
		request: repositoryRequest{}, responses: created(env{"repository": git.Repository{}, "webhookUrl": ""})},
	{method: "PATCH", path: "/repositories/{repositoryID}", handler: "handleUpdateRepository", tag: "git", summary: "Edit a connection.", request: repositoryRequest{}, responses: ok(env{"repository": git.Repository{}})},
	{method: "POST", path: "/repositories/{repositoryID}/rotate-secret", handler: "handleRotateWebhookSecret", tag: "git", summary: "Issue a new webhook secret.", responses: ok(env{"repository": git.Repository{}, "webhookUrl": ""})},
	{method: "DELETE", path: "/repositories/{repositoryID}", handler: "handleDisconnectRepository", tag: "git", summary: "Disconnect a repository.", responses: none()},
	{method: "POST", path: "/git/webhooks/{repositoryID}", handler: "handleWebhook", tag: "git", summary: "What a git host calls; the delivery proves itself with the repository's secret.", public: true, raw: true, responses: ok(git.Receipt{})},
	{method: "GET", path: "/issues/{issueKey}/development", handler: "handleIssueDevelopment", tag: "git", summary: "The commits, branches, pull requests and CI runs that name the issue.", responses: ok(git.Development{})},
	{method: "POST", path: "/issues/{issueKey}/branches", handler: "handleCreateBranch", tag: "git", summary: "Have the host make a branch for the issue, named for it and mapped to it.", request: createBranchRequest{}, responses: created(env{"branch": git.Branch{}})},

	// Dashboards.
	{method: "GET", path: "/projects/{projectKey}/dashboards", handler: "handleListDashboards", tool: "list_dashboards", toolHelp: "A project's dashboards with their widgets.", tag: "dashboards", summary: "A project's dashboards with their widgets.", responses: ok(env{"dashboards": []report.Dashboard{}})},
	{method: "POST", path: "/projects/{projectKey}/dashboards", handler: "handleCreateDashboard", tag: "dashboards", summary: "Add a dashboard.", request: createDashboardRequest{}, responses: created(env{"dashboard": report.Dashboard{}})},
	{method: "GET", path: "/projects/{projectKey}/report-kinds", handler: "handleReportKinds", tag: "dashboards", summary: "The reports that suit this kind of project.", responses: ok(env{"kinds": []report.KindInfo{}})},
	{method: "GET", path: "/projects/{projectKey}/reports/{kind}", handler: "handleReport", tool: "get_report", toolHelp: "One report computed now; kind is a widget kind such as status_breakdown, throughput or velocity.", tag: "dashboards", summary: "One report, computed now.",
		query: []param{{name: "days", schema: intParam, description: "The window, 1 to 365."}, {name: "team", schema: uuidParam},
			{name: "sprint", schema: uuidParam, description: "One sprint's burndown, running or over."},
			{name: "milestone", schema: uuidParam, description: "One milestone's progress, open or closed, instead of every open one's."},
			{name: "version", schema: uuidParam, description: "One version's progress, released or not, instead of every unreleased one's."},
			{name: "q", description: "An NQL query the counted issues must match; the dashboard's filter, composed on the page."},
			{name: "groupBy", description: "For a chart: status, statusCategory, type, priority, assignee, team, milestone, label, fixVersion or component."},
			{name: "splitBy", description: "For a stacked chart: a second field from the same list."},
			{name: "measure", description: "For a chart: count or points."},
			{name: "shape", description: "For a chart: bar, stacked, donut or line."},
			{name: "interval", description: "For a line: week or month."},
			{name: "series", description: "For a line: created, resolved or open."}}, responses: ok(reportBody)},
	{method: "PATCH", path: "/dashboards/{dashboardID}", handler: "handleRenameDashboard", tag: "dashboards", summary: "Rename a dashboard.", request: renameDashboardRequest{}, responses: none()},
	{method: "DELETE", path: "/dashboards/{dashboardID}", handler: "handleDeleteDashboard", tag: "dashboards", summary: "Delete a dashboard that is not the project's last.", responses: none()},
	{method: "POST", path: "/dashboards/{dashboardID}/widgets", handler: "handleAddWidget", tag: "dashboards", summary: "Add a widget.", request: widgetRequest{}, responses: created(env{"widget": report.Widget{}})},
	{method: "GET", path: "/dashboard-templates", handler: "handleListDashboardTemplates", tag: "dashboards", summary: "What a dashboard may start from: the built-ins that suit the project, then the organization's own.",
		query: []param{{name: "project", description: "The project the dashboard is for, so only templates that suit its kind are offered."}}, responses: ok(env{"templates": []report.Template{}})},
	{method: "POST", path: "/dashboards/{dashboardID}/template", handler: "handleSaveDashboardTemplate", tag: "dashboards", summary: "Keep a dashboard's arrangement as a template for the organization.", request: saveTemplateRequest{}, responses: created(env{"template": report.Template{}})},
	{method: "DELETE", path: "/dashboard-templates/{templateID}", handler: "handleDeleteDashboardTemplate", tag: "dashboards", summary: "Remove one of the organization's own templates; dashboards made from it stay.", responses: none()},
	{method: "GET", path: "/dashboards/{dashboardID}/shares", handler: "handleListShares", tag: "dashboards", summary: "The live links to a dashboard.", responses: ok(env{"shares": []report.Share{}})},
	{method: "POST", path: "/dashboards/{dashboardID}/shares", handler: "handleCreateShare", tag: "dashboards", summary: "Make a link that opens the dashboard without a sign-in; the address is in this answer and nowhere afterwards.",
		request: createShareRequest{}, responses: created(env{"share": report.Share{}, "url": ""})},
	{method: "DELETE", path: "/dashboards/{dashboardID}/shares/{shareID}", handler: "handleRevokeShare", tag: "dashboards", summary: "Revoke a link.", responses: none()},
	{method: "GET", path: "/shared/{token}", handler: "handleShared", tag: "dashboards", summary: "A shared dashboard: its arrangement, whose it is, and the filter the link froze.", public: true, responses: ok(report.SharedView{})},
	{method: "GET", path: "/shared/{token}/widgets/{widgetID}", handler: "handleSharedWidget", tag: "dashboards", summary: "One widget of a shared dashboard, computed now with the link's own filter.", public: true, responses: ok(reportBody)},
	{method: "GET", path: "/shared/{token}/pdf", handler: "handleSharedPDF", tag: "dashboards", summary: "A shared dashboard printed as a PDF, for whoever holds the link.", public: true, binary: true, responses: map[int]any{}},
	{method: "GET", path: "/dashboards/{dashboardID}/pdf", handler: "handleDashboardPDF", tag: "dashboards", summary: "The dashboard printed as a PDF, filtered as the reader has it.", binary: true,
		query: []param{{name: "q", description: "The dashboard's live filter, so the print shows what the reader sees."}}, responses: map[int]any{}},
	{method: "PUT", path: "/dashboards/{dashboardID}/widgets", handler: "handleReorderWidgets", tag: "dashboards", summary: "Set the widgets' order.", request: reorderWidgetsRequest{}, responses: none()},
	{method: "PATCH", path: "/widgets/{widgetID}", handler: "handleUpdateWidget", tag: "dashboards", summary: "Change a widget's title, width or parameters.", request: widgetRequest{}, responses: ok(env{"widget": report.Widget{}})},
	{method: "DELETE", path: "/widgets/{widgetID}", handler: "handleRemoveWidget", tag: "dashboards", summary: "Remove a widget.", responses: none()},

	// The service desk's agent side.
	{method: "GET", path: "/projects/{projectKey}/request-types", handler: "handleListRequestTypes", tag: "service desk", summary: "What customers can ask a desk for.", responses: ok(env{"requestTypes": []desk.RequestType{}})},
	{method: "POST", path: "/projects/{projectKey}/request-types", handler: "handleCreateRequestType", tag: "service desk", summary: "Add a request type.", request: createRequestTypeRequest{}, responses: created(env{"requestType": desk.RequestType{}})},
	{method: "PATCH", path: "/request-types/{requestTypeID}", handler: "handleUpdateRequestType", tag: "service desk", summary: "Change a request type: its category, what it becomes, or the team it lands with.", request: updateRequestTypeRequest{}, responses: ok(env{"requestType": desk.RequestType{}})},
	{method: "DELETE", path: "/request-types/{requestTypeID}", handler: "handleDeleteRequestType", tag: "service desk", summary: "Remove a request type.", responses: none()},
	{method: "GET", path: "/projects/{projectKey}/sla-policies", handler: "handleListPolicies", tag: "service desk", summary: "The desk's goals per priority.", responses: ok(env{"policies": []desk.Policy{}})},
	{method: "PATCH", path: "/sla-policies/{policyID}", handler: "handleUpdatePolicy", tag: "service desk", summary: "Change goals or which statuses pause the clock.", request: updatePolicyRequest{}, responses: ok(env{"policy": desk.Policy{}})},
	{method: "GET", path: "/projects/{projectKey}/queue", handler: "handleQueue", tag: "service desk", summary: "The agents' queue, most urgent first.",
		query: []param{{name: "filter", schema: &openapi.Schema{Type: "string", Enum: []string{"open", "unassigned", "mine", "breached", "all"}}}}, responses: ok(env{"rows": []desk.QueueRow{}})},
	{method: "GET", path: "/issues/{issueKey}/timers", handler: "handleIssueTimers", tag: "service desk", summary: "The clocks on a request.", responses: ok(env{"timers": []desk.Timer{}})},

	// The Model Context Protocol: this API as tools for an assistant.
	{method: "POST", path: "/mcp", handler: "handleMCP", id: "mcp", tag: "mcp", summary: "The Model Context Protocol endpoint: the marked operations of this API as tools.",
		raw: true, rawNote: "A JSON-RPC 2.0 request as the Model Context Protocol defines it.", responses: map[int]any{200: rpcResponse{}, 202: nil}},
	{method: "GET", path: "/assistant", handler: "handleAssistantStatus", tag: "assistant", summary: "Whether a model is configured to answer questions.", responses: ok(assistantStatus{})},
	{method: "POST", path: "/assistant/ask", handler: "handleAsk", tag: "assistant", summary: "Ask a model where something is; it looks with the caller's own tools.", request: askRequest{}, responses: ok(env{"answer": assistant.Answer{}, "proposals": []proposal{}})},
}

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
