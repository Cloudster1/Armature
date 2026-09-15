//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/arrange"
	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/automation"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bulk"
	"github.com/armature/armature/backend/internal/calendar"
	"github.com/armature/armature/backend/internal/component"
	"github.com/armature/armature/backend/internal/csvio"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/filter"
	"github.com/armature/armature/backend/internal/freshness"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/httpapi"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/notify"
	"github.com/armature/armature/backend/internal/oidc"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/privacy"
	"github.com/armature/armature/backend/internal/profile"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
	"github.com/armature/armature/backend/internal/theme"
	"github.com/armature/armature/backend/internal/version"
	"github.com/armature/armature/backend/internal/webhook"
	"github.com/armature/armature/backend/internal/workflow"
)

// apiServer runs the real router, with the real middleware chain, against the
// real database. Testing the handlers in isolation would skip exactly the parts
// most likely to be wrong: authentication, tenant scoping and the error shape.
type apiServer struct {
	*httptest.Server
	harness *harness
	// api is the server behind the listener, for a test that swaps a part of it.
	api *httpapi.Server
	// mailer keeps what the API and the notifier would have mailed.
	mailer   *fakeMailer
	notifier *desk.Notifier
}

const testCookieName = "armature_session_test"

func newAPIServer(t *testing.T, h *harness) *apiServer {
	t.Helper()
	// Every test's requests come from one address; the door's brakes are per
	// address and would otherwise count the whole suite as one caller.
	httpapi.ResetThrottles()

	engine := workflow.NewEngine(workflow.NewDefaultRegistry())
	workflowStore := workflow.NewStore()
	issues := issue.NewService(h.cluster, engine, workflowStore)
	sprints := sprint.NewService(h.cluster)
	plans := plan.NewService(issues, sprints)
	milestones := milestone.NewService(h.cluster)
	plans.WithMilestones(milestones)
	sprints.CountWith(plans)
	plans.WithTeams(team.NewService(h.cluster))
	projects := project.NewService(h.cluster, board.Provisioner{}, report.Provisioner{})
	workflowAdmin := workflow.NewAdmin(h.cluster, workflowStore).WithRegistry(engine.Registry()).WithObserver(board.Follower{})
	attachments := attachment.NewService(h.cluster, h.attachmentStore(t), issues)
	deskService := desk.NewService(h.cluster, issues).WithAttachments(attachments)
	mailer := &fakeMailer{}
	labels := label.NewService(h.cluster)
	testLog := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	webhooks := webhook.NewService(h.cluster, testLog)

	accounts := h.authService()
	themes := theme.NewService(h.cluster, h.attachmentStore(t))
	srv := &httpapi.Server{
		Mailer:     mailer,
		Profiles:   profile.NewService(h.attachmentStore(t), accounts),
		Privacy:    privacy.NewService(h.cluster).WithAvatars(profile.NewService(h.attachmentStore(t), accounts)).WithThemes(themes),
		Themes:     themes,
		Auth:       accounts,
		Fields:     field.NewService(h.cluster),
		Arrange:    arrange.NewService(h.cluster),
		Labels:     labels,
		Notify:     notify.NewService(h.cluster),
		Automation: automation.NewService(h.cluster, issues, labels, testLog).WithMailer(mailer).WithWebhooks(webhooks),
		Webhooks:   webhooks,
		Filters:    filter.NewService(h.cluster),
		Audit:      audit.NewService(h.cluster),
		Calendar:   calendar.NewService(h.cluster),
		Bulk:       bulk.NewService(issues, labels),
		CSV: csvio.NewService(h.cluster, issues, labels, field.NewService(h.cluster), accounts).
			WithPlanning(sprints, version.NewService(h.cluster), component.NewService(h.cluster), team.NewService(h.cluster)),
		Attachments: attachments,
		Projects:    projects,
		Templates:   template.NewService(h.cluster, projects, workflowAdmin).WithDesk(deskService),
		Git:         git.NewService(h.cluster, issues),
		Desk:        deskService,
		Reports:     report.NewService(h.cluster, plans).WithSprints(sprints),
		AppBaseURL:  "http://app.test",
		Issues:      issues,
		Boards:      board.NewService(h.cluster, issues),
		Plans:       plans,
		Sprints:     sprints,
		Milestones:  milestones,
		Versions:    version.NewService(h.cluster),
		Components:  component.NewService(h.cluster),
		Teams:       team.NewService(h.cluster),
		Perms:       perm.NewStore(h.cluster),
		Workflow:    &httpapi.WorkflowDeps{Engine: engine, Store: workflowStore, Admin: workflowAdmin},
		DB:          h.cluster,
		Telemetry:   h.tel,
		Fresh:       freshness.NewMemoryTracker(30 * time.Second),
		Log:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Secure:      false,
		CookieName:  testCookieName,
		SessionTTL:  time.Hour,
	}

	ts := httptest.NewServer(srv.Routes([]string{"http://localhost:5173"}))
	// The identity provider flow needs to know where the browser comes back
	// to, which is only known once the server is listening.
	srv.OIDC = oidc.NewService(h.cluster, ts.URL+"/api/v1/auth/oidc/callback")
	t.Cleanup(ts.Close)
	notifier := desk.NewNotifier(h.cluster, nil, mailer, "http://app.test", srv.Log)
	return &apiServer{Server: ts, harness: h, api: srv, mailer: mailer, notifier: notifier}
}

// client is an HTTP client with its own cookie jar, so each one behaves like a
// separate browser and sessions cannot bleed between tests.
type client struct {
	t    *testing.T
	http *http.Client
	base string
	// bearer, when set, is sent instead of relying on the cookie jar.
	bearer string
}

func (s *apiServer) client(t *testing.T) *client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &client{
		t:    t,
		http: &http.Client{Jar: jar, Timeout: 10 * time.Second},
		base: s.URL,
	}
}

// response is a decoded API response, kept deliberately loose so tests can
// assert on the parts they care about.
type response struct {
	Status  int
	Header  http.Header
	Body    map[string]any
	Raw     string
	Cookies []*http.Cookie
}

// Error returns the error envelope, or nil for a successful response.
func (r response) Error() map[string]any {
	if e, ok := r.Body["error"].(map[string]any); ok {
		return e
	}
	return nil
}

// ErrorCode returns the machine readable code, or "" when there is no error.
func (r response) ErrorCode() string {
	if e := r.Error(); e != nil {
		if code, ok := e["code"].(string); ok {
			return code
		}
	}
	return ""
}

func (c *client) do(method, path string, body any) response {
	c.t.Helper()

	var reader io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			reader = bytes.NewBufferString(v) // raw, for malformed-input tests
		default:
			encoded, err := json.Marshal(v)
			if err != nil {
				c.t.Fatal(err)
			}
			reader = bytes.NewReader(encoded)
		}
	}

	req, err := http.NewRequestWithContext(context.Background(), method, c.base+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}

	out := response{Status: resp.StatusCode, Header: resp.Header, Raw: string(raw), Cookies: resp.Cookies()}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &out.Body); err != nil {
			c.t.Fatalf("%s %s returned a body that is not JSON: %q", method, path, raw)
		}
	}
	apiContract.observe(c.t, method, path, resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	return out
}

// raw sends a request built by the caller, observed by the contract like
// every other call, and returns the response with its body read.
func (c *client) raw(req *http.Request) (*http.Response, []byte) {
	c.t.Helper()
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	apiContract.observe(c.t, req.Method, req.URL.RequestURI(), resp.StatusCode, resp.Header.Get("Content-Type"), body)
	return resp, body
}

// patch is do with PATCH, for symmetry with the rest.
func (c *client) patch(path string, body any) response { return c.do(http.MethodPatch, path, body) }

// noRedirects makes the client hand back a 302 rather than follow it, which
// is what a test of the sign-in redirects needs.
func (c *client) noRedirects() *client {
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

func (c *client) get(path string) response            { return c.do(http.MethodGet, path, nil) }
func (c *client) post(path string, body any) response { return c.do(http.MethodPost, path, body) }
func (c *client) put(path string, body any) response  { return c.do(http.MethodPut, path, body) }
func (c *client) delete(path string) response         { return c.do(http.MethodDelete, path, nil) }

// signup registers a new account through the API and leaves the client signed
// in, exactly as a browser would be.
func (c *client) signup(t *testing.T, h *harness, name string) response {
	t.Helper()
	resp := c.post("/api/v1/auth/signup", map[string]string{
		"email":    h.email(t, name),
		"password": testPassword,
		"name":     name,
		"orgName":  name + " Company",
		"orgSlug":  h.orgSlug(t, name),
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("signup returned %d: %s", resp.Status, resp.Raw)
	}
	return resp
}

// tokenOf is the secret at the end of a share URL, the one place it is shown.
func tokenOf(shareURL string) string {
	return shareURL[strings.LastIndex(shareURL, "/")+1:]
}

// firstDashboard is the project's own dashboard as the API hands it over,
// which for a fresh project is the overview it was provisioned with.
func (c *client) firstDashboard(t *testing.T, projectKey string) map[string]any {
	t.Helper()
	resp := c.get("/api/v1/projects/" + projectKey + "/dashboards")
	boards, ok := resp.Body["dashboards"].([]any)
	if !ok || len(boards) == 0 {
		t.Fatalf("dashboards of %s: %d %s", projectKey, resp.Status, resp.Raw)
	}
	return boards[0].(map[string]any)
}

// principalField digs a value out of the principal in a response body.
func principalField(t *testing.T, r response, keys ...string) any {
	t.Helper()
	var current any = r.Body
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("expected an object at %q, got %T", key, current)
		}
		current = m[key]
	}
	return current
}
