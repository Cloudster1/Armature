package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/oidc"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/privacy"
	"github.com/armature/armature/backend/internal/profile"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/render"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
	"github.com/armature/armature/backend/internal/tenant"
	"github.com/armature/armature/backend/internal/theme"
	"github.com/armature/armature/backend/internal/version"
	"github.com/armature/armature/backend/internal/webhook"
	"github.com/armature/armature/backend/internal/workflow"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxLogger
	ctxPrincipal
	ctxPerms
	ctxWriteRecord
)

// RequestIDFrom returns the id assigned to this request.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxRequestID).(string)
	return id
}

func loggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxLogger).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// PrincipalFrom returns the authenticated caller, if any.
// PermsFrom returns what the caller may do. An unauthenticated caller holds an
// empty set, which refuses everything rather than defaulting to anything.
func PermsFrom(ctx context.Context) perm.Set {
	set, _ := ctx.Value(ctxPerms).(perm.Set)
	return set
}

func PrincipalFrom(ctx context.Context) *auth.Principal {
	p, _ := ctx.Value(ctxPrincipal).(*auth.Principal)
	return p
}

// requestID assigns every request an id, echoes it back, and puts it on the
// request logger so that one user-visible identifier ties a support ticket to
// the exact log lines.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), ctxRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush and Unwrap keep server-sent events and http.ResponseController working
// through the wrapper.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logging attaches a request-scoped logger and writes one access line per
// request.
func logging(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			log := base.With("request_id", RequestIDFrom(r.Context()))
			if traceID := observability.TraceIDFrom(r.Context()); traceID != "" {
				log = log.With("trace_id", traceID)
			}
			ctx := context.WithValue(r.Context(), ctxLogger, log)

			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r.WithContext(ctx))

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			attrs := []any{
				"method", r.Method,
				"path", redactPath(r.URL.Path),
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes", rec.bytes,
			}
			if p := PrincipalFrom(ctx); p != nil {
				attrs = append(attrs, "user_id", p.User.ID)
				if p.Org != nil {
					attrs = append(attrs, "org", p.Org.Slug)
				}
			}
			log.Info("request", attrs...)
		})
	}
}

// recovery turns a panic into a 500 rather than a dropped connection, and keeps
// the process alive.
func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				// http.ErrAbortHandler is a deliberate abort, not a bug.
				if v == http.ErrAbortHandler {
					panic(v)
				}
				loggerFrom(r.Context()).Error("panic in handler", "panic", v, "path", r.URL.Path)
				respondError(w, r, ErrInternal(nil))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// writeRecord carries the write ahead log position produced by a handler back
// up to the middleware, which persists it so the caller's next read is not
// served by a lagging replica.
type writeRecord struct{ lsn db.LSN }

// NoteWrite records the position of a write made while handling this request.
// Handlers call it after any mutation; the freshness middleware does the rest.
func NoteWrite(ctx context.Context, lsn db.LSN) {
	if rec, ok := ctx.Value(ctxWriteRecord).(*writeRecord); ok && lsn > rec.lsn {
		rec.lsn = lsn
	}
}

// Server holds everything the handlers need.
type Server struct {
	Auth       *auth.Service
	Projects   *project.Service
	Issues     *issue.Service
	Boards     *board.Service
	Plans      *plan.Service
	Sprints    *sprint.Service
	Milestones *milestone.Service
	Versions   *version.Service
	Components *component.Service
	Teams      *team.Service
	// Templates makes projects the way a template says; Projects makes them
	// plain. Both are kept because the seed and the tests use the plain one.
	Templates *template.Service
	Git       *git.Service
	Desk      *desk.Service
	Reports   *report.Service
	// Fields are what a project records beyond the standard columns;
	// Attachments are the files on an issue and the store behind them.
	Fields      *field.Service
	Attachments *attachment.Service
	Labels      *label.Service
	// Arrange says which of an issue's fields are shown, where, and in what
	// order, per project and per issue type.
	Arrange *arrange.Service
	Perms   *perm.Store
	// Notify is the inbox and the preferences behind it.
	Notify *notify.Service
	// Automation runs rules; Webhooks posts events out. Both are the worker's
	// too, so a rule run by hand takes the stream's path.
	Automation *automation.Service
	Webhooks   *webhook.Service
	// Filters are saved queries; Bulk edits many issues at once; CSV moves
	// them in and out as files.
	Filters *filter.Service
	// Themes are people's own redefinitions of the interface and the files behind them.
	Themes *theme.Service
	Bulk   *bulk.Service
	CSV    *csvio.Service
	// Audit reads the organization's record of who did what; Calendar lays a
	// project's dated things over a month.
	Audit    *audit.Service
	Calendar *calendar.Service
	// OIDC is nil in a deployment with no identity provider support compiled in.
	OIDC     *oidc.Service
	Workflow *WorkflowDeps
	// Profiles keeps people's pictures; nil in a deployment without a bucket.
	Profiles *profile.Service
	// Privacy serves a person's rights over their data: export, erasure, and
	// an organization letting a member go.
	Privacy *privacy.Service
	// Renderer prints a page as a PDF. Nil means export answers that it is not set up.
	Renderer render.Renderer
	// Assistant is the model behind Ask; Unavailable when none is configured.
	Assistant assistant.Asker
	// Mailer sends the portal's codes from the request path. Nil means mail is
	// off, and the door says so.
	Mailer desk.Mailer

	DB    *db.Cluster
	Fresh freshnessTracker
	Log   *slog.Logger
	// Telemetry counts and traces requests; nil serves without either.
	Telemetry  *observability.Telemetry
	Secure     bool
	CookieName string
	// AppBaseURL is where the browser is sent back to after a sign-in that
	// left this application, which the API cannot infer from the request.
	AppBaseURL string
	SessionTTL time.Duration
	// CheckOrigin also refuses a cookie-carried write from an origin that is not
	// this application's; off in development, where the dev server has its own.
	CheckOrigin bool
	// TrustedProxies are the only peers whose X-Forwarded-For is believed;
	// empty means the connection's own address is the caller's.
	TrustedProxies []netip.Prefix

	// handler is the built router, kept so a tool call can be run through the
	// same chain as any request.
	handler http.Handler
}

// WorkflowDeps bundles the workflow engine with the store that loads workflows
// for it, since the handlers always need both together.
type WorkflowDeps struct {
	Engine *workflow.Engine
	Store  *workflow.Store
	// Admin writes configuration. Nil in a deployment that does not allow it.
	Admin *workflow.Admin
}

// freshnessTracker is the subset of the freshness package this layer needs,
// declared here so that httpapi depends on behaviour rather than a concrete
// implementation.
type freshnessTracker interface {
	Note(ctx context.Context, key string, lsn db.LSN)
	Required(ctx context.Context, key string) db.LSN
}

// readOnlyToken refuses a write from a token made to read, before any handler,
// so the promise holds for every route including ones added later.
func readOnlyToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A transport carries reads and writes alike; the rule is applied to
		// the calls it carries, which run through this chain again.
		if !transports[r.URL.Path] && writeRefused(r.Method, PrincipalFrom(r.Context())) {
			respondError(w, r, ErrReadOnlyToken())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// transports are the POSTs that only carry other calls: MCP and Ask.
var transports = map[string]bool{mcpAPIPrefix + "/mcp": true, mcpAPIPrefix + "/assistant/ask": true}

// writeRefused says whether a method changes something and the caller's token
// was made not to.
func writeRefused(method string, p *auth.Principal) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return p != nil && p.TokenID != nil && p.HasScope(auth.ScopeRead)
}

// authenticate resolves the caller from a session cookie or bearer token and
// puts the principal, the tenant scope and the freshness pin on the context.
//
// It never rejects an unauthenticated request: that is requireAuth's job, so
// that endpoints like login and health can share the same chain.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rec := &writeRecord{}
		ctx = context.WithValue(ctx, ctxWriteRecord, rec)

		if secret := credentialFrom(r, s.CookieName); secret != "" {
			principal, err := s.Auth.Authenticate(ctx, secret)
			switch {
			case err == nil:
				ctx = context.WithValue(ctx, ctxPrincipal, principal)
				if principal.Org != nil {
					ctx = tenant.WithOrg(ctx, tenant.Org{ID: principal.Org.ID, Slug: principal.Org.Slug})

					// One more round trip per authenticated request, so that
					// every later check is a question of memory. Asking the
					// database inside each handler instead would be a query per
					// guard, and there are several on some routes.
					set, err := s.Perms.ResolveFor(ctx, principal.Org.ID,
						principal.User.ID, principal.Role == auth.RoleOwner)
					if err != nil {
						respondError(w, r, err)
						return
					}
					// A key holds less than the person who made it, and the
					// narrowing happens here so that every guard below reads
					// it without knowing a key was involved.
					if principal.TokenID != nil {
						set = set.AsKey(principal.TokenProjects)
					}
					ctx = context.WithValue(ctx, ctxPerms, set)
				}
				// A session that came through an open door sees one desk.
				if principal.PortalDeskID != nil {
					ctx = desk.WithScope(ctx, desk.Scope{ProjectID: *principal.PortalDeskID, ProjectKey: principal.PortalDesk})
				}
				// Pin this caller's reads to their own most recent write.
				if key := freshnessKey(principal); key != "" {
					if lsn := s.Fresh.Required(ctx, key); lsn != 0 {
						ctx = db.PinLSN(ctx, lsn)
					}
				}
			case errors.Is(err, auth.ErrInvalidToken):
				// An unknown, expired or revoked credential simply means the
				// caller is anonymous; requireAuth turns that into a 401 where
				// it matters, and endpoints like login still work with a stale
				// cookie present. Clearing the cookie stops the browser
				// resending a dead token on every subsequent request.
				if _, cookieErr := r.Cookie(s.CookieName); cookieErr == nil {
					s.clearSessionCookie(w)
				}
			default:
				// Any other refusal is a decision about this specific caller,
				// such as a deactivated account. Degrading it to "anonymous"
				// would answer 401, which reads as an expired session and
				// invites the user to keep retrying a password that will never
				// work. Answer it directly instead.
				respondError(w, r, err)
				return
			}
		}

		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)

		// Persist any write this request made, after the handler has finished.
		if rec.lsn != 0 {
			if p := PrincipalFrom(ctx); p != nil {
				if key := freshnessKey(p); key != "" {
					s.Fresh.Note(context.WithoutCancel(ctx), key, rec.lsn)
				}
			}
		}
	})
}

// requireSession refuses an API key an act that must be done by somebody at a
// keyboard, in the shape account erasure already refuses one.
func requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := PrincipalFrom(r.Context()); p != nil && p.TokenID != nil {
			respondError(w, r, ErrSessionOnly())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAuth rejects anonymous callers.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PrincipalFrom(r.Context()) == nil {
			respondError(w, r, ErrUnauthorized(""))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireOrg rejects a caller who is authenticated but has not selected an
// organization, which would otherwise reach the database with no tenant scope.
func requireOrg(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if p == nil {
			respondError(w, r, ErrUnauthorized(""))
			return
		}
		if !p.InOrg() {
			respondError(w, r, toAPIError(tenant.ErrNoTenant))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requirePerm gates a route on a permission held over the whole organization.
// It is the right guard for anything that is not about one project.
func requirePerm(required perm.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFrom(r.Context())
			if p == nil || !p.InOrg() {
				respondError(w, r, ErrUnauthorized(""))
				return
			}
			if !PermsFrom(r.Context()).CanInOrg(required) {
				respondError(w, r, forbidden(required))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireProjectPerm gates a route on a permission held in the project the path
// names, whether that comes from a grant over the project or over the whole
// organization.
func requireProjectPerm(required perm.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFrom(r.Context())
			if p == nil || !p.InOrg() {
				respondError(w, r, ErrUnauthorized(""))
				return
			}
			key := project.NormalizeKey(chi.URLParam(r, "projectKey"))
			if !PermsFrom(r.Context()).Can(required, key) {
				respondError(w, r, forbidden(required))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireSomewhere gates a route that names no project in its path, such as one
// addressed by a board id. Holding the permission somewhere is enough to try;
// whether it applies to the thing being touched is the handler's own check.
func requireSomewhere(required perm.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFrom(r.Context())
			if p == nil || !p.InOrg() {
				respondError(w, r, ErrUnauthorized(""))
				return
			}
			if !PermsFrom(r.Context()).CanSomewhere(required) {
				respondError(w, r, forbidden(required))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireIssuePerm gates a route addressed by an issue key. The project is the
// prefix of the key, so the check is still exact without a database lookup.
func requireIssuePerm(required perm.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFrom(r.Context())
			if p == nil || !p.InOrg() {
				respondError(w, r, ErrUnauthorized(""))
				return
			}
			projectKey, _, err := issue.ParseKey(chi.URLParam(r, "issueKey"))
			if err != nil {
				respondError(w, r, toAPIError(err))
				return
			}
			if !PermsFrom(r.Context()).Can(required, projectKey) {
				respondError(w, r, forbidden(required))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// forbidden says which permission was missing. A refusal that does not name
// what was needed leaves somebody guessing which role to ask for.
func forbidden(required perm.Permission) *APIError {
	return ErrForbidden("You need the " + string(required) + " permission to do that.")
}

// credentialFrom pulls a credential from the Authorization header first, then
// the session cookie, so that API clients are never affected by cookie policy.
func credentialFrom(r *http.Request, cookieName string) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if token, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(token)
		}
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

// freshnessKey scopes the read-your-writes pin. Sessions and API tokens each
// get their own, so one user's browser tab does not force primary reads for
// their unrelated automation.
func freshnessKey(p *auth.Principal) string {
	switch {
	case p == nil:
		return ""
	case p.SessionID != nil:
		return "s:" + p.SessionID.String()
	case p.TokenID != nil:
		return "t:" + p.TokenID.String()
	default:
		return ""
	}
}

// clientIP returns the caller's address as clientAddress settled it.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// clientAddress believes X-Forwarded-For only from a proxy the operator named,
// and then only its right-most hop that is not another trusted proxy.
func (s *Server) clientAddress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if peer := forwardedFor(r, s.TrustedProxies); peer != "" {
			r.RemoteAddr = net.JoinHostPort(peer, "0")
		}
		next.ServeHTTP(w, r)
	})
}

// forwardedFor is the client a trusted proxy vouches for, or empty when the
// peer is not a trusted proxy or said nothing.
func forwardedFor(r *http.Request, trusted []netip.Prefix) string {
	if !isTrusted(clientIP(r), trusted) {
		return ""
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if _, err := netip.ParseAddr(hop); err != nil {
			return ""
		}
		if !isTrusted(hop, trusted) {
			return hop
		}
	}
	return ""
}

func isTrusted(address string, trusted []netip.Prefix) bool {
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// contextWithTimeout bounds a handler's own dependency calls.
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// sharedPrefix is where a shared dashboard's token sits in a path.
const sharedPrefix = "/api/v1/shared/"

// redactPath keeps a shared dashboard's token out of the access log, where
// a path is written whole; the token is the whole of the authorization.
func redactPath(path string) string {
	for _, prefix := range tokenPrefixes {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := path[len(prefix):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			return prefix + "{token}" + rest[i:]
		}
		return prefix + "{token}"
	}
	return path
}

// tokenPrefixes are the paths whose next segment is a secret: a link to a
// shared dashboard, a rule's address, and the rating link a mail carries.
var tokenPrefixes = []string{sharedPrefix, "/api/v1/automation/hooks/", "/api/v1/csat/"}

// requireFeature refuses to make something for a page the project does not
// have. Reads stay open, so what exists is still shown; only making more is
// refused, and the answer says where to turn the page on.
func (s *Server) requireFeature(feature project.Feature) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := project.NormalizeKey(chi.URLParam(r, "projectKey"))
			p, err := s.Projects.ByKey(r.Context(), key)
			if err != nil {
				respondError(w, r, err)
				return
			}
			if !p.Has(feature) {
				respondError(w, r, &project.FeatureOffError{Key: p.Key, Feature: feature})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
