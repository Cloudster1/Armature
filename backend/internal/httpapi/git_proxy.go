package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/oauth"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/project"
)

// The git proxy's brakes, per token: a clone or a fetch starts with one
// request for the refs, and a push is one post.
const (
	GitFetchesPerMinute = 60
	GitPushesPerMinute  = 30
	gitBrakeWindow      = time.Minute
)

// gitPathPrefix is where the proxy answers, and gitLegs the three requests of
// git's smart HTTP below a repository's path.
const gitPathPrefix = "/api/v1/git/"

var gitLegs = []git.Leg{git.LegRefs, git.LegUploadPack, git.LegReceivePack}

// gitTransfer says whether a path is one of the proxy's, which judge reads and
// writes themselves and take the token from basic auth as git sends it.
func gitTransfer(path string) bool {
	if !strings.HasPrefix(path, gitPathPrefix) || strings.Count(strings.TrimPrefix(path, gitPathPrefix), "/") < 3 {
		return false
	}
	for _, leg := range gitLegs {
		if strings.HasSuffix(path, "/"+string(leg)) {
			return true
		}
	}
	return false
}

// gitBrakes hold the proxy's rate per token, one set per server.
type gitBrakes struct {
	once            sync.Once
	fetches, pushes *throttle
}

func (s *Server) brakes() *gitBrakes {
	s.gitBrakes.once.Do(func() {
		s.gitBrakes.fetches = newThrottle(GitFetchesPerMinute, gitBrakeWindow)
		s.gitBrakes.pushes = newThrottle(GitPushesPerMinute, gitBrakeWindow)
	})
	return &s.gitBrakes
}

// gitCall is a proxy request that may go on: who, where, and what they may push.
type gitCall struct {
	principal *auth.Principal
	repo      *git.Repository
	// mayEdit is the person's right to edit a project's issues through this
	// token, which git.write stands in for in an app's own scopes.
	mayEdit func(projectKey string) bool
}

// gitRefuse answers in a way git prints: a plain sentence, which the client
// shows as "remote:" before its own error.
func gitRefuse(w http.ResponseWriter, status int, sentence string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="Armature", charset="UTF-8"`)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	if sentence != "" {
		_, _ = io.WriteString(w, sentence+"\n")
	}
}

// gitCaller lets a request through to a repository, or answers why not. A push
// also needs git.write from an app, a key that may write, and a right to edit.
func (s *Server) gitCaller(w http.ResponseWriter, r *http.Request, push bool) (*gitCall, bool) {
	p := PrincipalFrom(r.Context())
	switch {
	case p == nil && r.Header.Get("Authorization") == "":
		// The challenge alone: git asks its credential helper and comes back.
		gitRefuse(w, http.StatusUnauthorized, "")
		return nil, false
	case p == nil:
		gitRefuse(w, http.StatusUnauthorized, "That token is not valid any more. Get a new one and try again.")
		return nil, false
	case p.TokenID == nil:
		gitRefuse(w, http.StatusUnauthorized, "Clone and push with a token, sent as a bearer token or as the password.")
		return nil, false
	case !p.Role.IsAgent():
		gitRefuse(w, http.StatusForbidden, "Customers use the portal.")
		return nil, false
	case p.App != nil && !p.HasScope(oauth.ScopeRead):
		gitRefuse(w, http.StatusForbidden, p.App.Name+" was not allowed to read. Connect it again.")
		return nil, false
	}
	key := project.NormalizeKey(chi.URLParam(r, "projectKey"))
	owner, err := url.PathUnescape(chi.URLParam(r, "owner"))
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")
	if err != nil || !PermsFrom(r.Context()).Can(perm.Read, key) {
		gitRefuse(w, http.StatusNotFound, "That repository was not found in "+key+". Check the project key and the repository's owner/name.")
		return nil, false
	}
	repo, err := s.Git.ProxyRepository(r.Context(), key, owner+"/"+name)
	switch {
	case errors.Is(err, git.ErrNotFound):
		gitRefuse(w, http.StatusNotFound, "That repository was not found in "+key+". Check the project key and the repository's owner/name.")
		return nil, false
	case errors.Is(err, git.ErrAmbiguousName):
		gitRefuse(w, http.StatusConflict, "Two hosts carry "+owner+"/"+name+" in "+key+", so Armature cannot tell which to reach. A project administrator disconnects one.")
		return nil, false
	case err != nil:
		s.Log.Error("git proxy: find repository", "error", err)
		gitRefuse(w, http.StatusInternalServerError, "Armature could not read the repository. Try again in a moment.")
		return nil, false
	case !repo.HasToken:
		gitRefuse(w, http.StatusConflict, repo.Name+" has no access token in Armature, so Armature cannot reach it. A project administrator adds one under the project's repositories.")
		return nil, false
	}

	editable := personPermsFrom(r.Context()).AsKey(p.TokenProjects)
	call := &gitCall{principal: p, repo: repo, mayEdit: func(projectKey string) bool { return editable.Can(perm.IssueWrite, projectKey) }}
	if !push {
		return call, true
	}
	switch {
	case p.App != nil && !p.HasScope(oauth.ScopeGitWrite):
		gitRefuse(w, http.StatusForbidden, oauth.MissingScope(p.App.Name, oauth.ScopeGitWrite, p.Scopes))
		return nil, false
	case p.App == nil && p.HasScope(auth.ScopeRead):
		gitRefuse(w, http.StatusForbidden, ErrReadOnlyToken().Message)
		return nil, false
	case !call.mayEdit(key):
		gitRefuse(w, http.StatusForbidden, "You may not edit the issues of "+key+", so you may not push to its repositories. A project administrator can give you a role that does.")
		return nil, false
	}
	return call, true
}

// tokenBrake keys a brake by the token, so one app's run cannot slow another's.
func tokenBrake(w http.ResponseWriter, t *throttle, call *gitCall, sentence string) bool {
	key := call.principal.TokenID.String()
	if t.over(key, time.Now()) {
		gitRefuse(w, http.StatusTooManyRequests, sentence)
		return false
	}
	t.record(key)
	return true
}

// transferContext gives a clone or a push its own limit, and the connection
// the deadlines to match.
func transferContext(w http.ResponseWriter, r *http.Request) (context.Context, context.CancelFunc) {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(git.TransferTimeout)
	_ = rc.SetReadDeadline(deadline)
	_ = rc.SetWriteDeadline(deadline)
	return context.WithDeadline(r.Context(), deadline)
}

func (s *Server) handleGitRefs(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	push := service == string(git.LegReceivePack)
	call, ok := s.gitCaller(w, r, push)
	if !ok {
		return
	}
	if !push && service != string(git.LegUploadPack) {
		gitRefuse(w, http.StatusBadRequest, "Armature speaks git's smart protocol only. Use git 1.6.6 or later.")
		return
	}
	// A fetch starts here; a push is braked at its post, so asking for the
	// refs before it costs nothing.
	if !push && !tokenBrake(w, s.brakes().fetches, call, "This token has fetched too often in the last minute. Wait a minute and try again.") {
		return
	}
	ctx, cancel := transferContext(w, r)
	defer cancel()
	resp, err := s.Git.Forward(ctx, call.repo, git.LegRefs, "service="+service, r.Header, nil)
	s.relayGit(w, call.repo, resp, err)
}

func (s *Server) handleGitUploadPack(w http.ResponseWriter, r *http.Request) {
	call, ok := s.gitCaller(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := transferContext(w, r)
	defer cancel()
	resp, err := s.Git.Forward(ctx, call.repo, git.LegUploadPack, "", r.Header, r.Body)
	s.relayGit(w, call.repo, resp, err)
}

func (s *Server) handleGitReceivePack(w http.ResponseWriter, r *http.Request) {
	call, ok := s.gitCaller(w, r, true)
	if !ok {
		return
	}
	if !tokenBrake(w, s.brakes().pushes, call, "This token has pushed too often in the last minute. Wait a minute and push again.") {
		return
	}
	ctx, cancel := transferContext(w, r)
	defer cancel()

	body, whole, err := readPush(r)
	if err != nil {
		gitRefuse(w, http.StatusBadRequest, "The push could not be read. Push again.")
		return
	}
	push, err := git.ParsePush(body)
	if err != nil {
		gitRefuse(w, http.StatusBadRequest, "The push could not be read: "+err.Error()+". Push again with a current git.")
		return
	}
	rec := git.PushRecord{Actor: call.principal.User.ID, IP: clientIP(r)}
	if app := call.principal.App; app != nil {
		rec.AppID, rec.AppName = &app.ClientID, app.Name
	}

	if push.Certificate {
		rec.Outcome = "refused"
		s.recordPush(ctx, call.repo, rec)
		w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(git.ErrPkt("Armature does not forward signed pushes. Push without --signed."))
		return
	}
	var refs []git.PushedRef
	allowed := false
	switch {
	case !whole:
		refs = refuseAll(push, "This push is larger than "+itoa(git.MaxPushBytes>>20)+" MiB, the most Armature forwards at once. Push fewer commits at a time.")
	default:
		refs, allowed, err = s.Git.CheckPush(ctx, call.repo, push, call.mayEdit)
		if err != nil {
			s.Log.Error("git proxy: check push", "error", err)
			gitRefuse(w, http.StatusInternalServerError, "Armature could not check the push. Try again in a moment.")
			return
		}
	}
	rec.Refs = refs
	if !allowed {
		rec.Outcome = "refused"
		s.recordPush(ctx, call.repo, rec)
		answerRefusal(w, push, refs)
		return
	}

	header := r.Header.Clone()
	// The body is forwarded as it was read, unpacked.
	header.Del("Content-Encoding")
	resp, err := s.Git.Forward(ctx, call.repo, git.LegReceivePack, "", header, bytes.NewReader(body))
	rec.Outcome = "forwarded"
	if resp != nil {
		rec.HostStatus = resp.StatusCode
	}
	s.recordPush(ctx, call.repo, rec)
	s.relayGit(w, call.repo, resp, err)
}

// readPush reads a push up to the cap, unpacking it if git compressed it, and
// says whether it was all there; the rest is drained so git reads the answer.
func readPush(r *http.Request) ([]byte, bool, error) {
	var in io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		z, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, false, err
		}
		defer z.Close()
		in = z
	}
	body, err := io.ReadAll(io.LimitReader(in, git.MaxPushBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(body) <= git.MaxPushBytes {
		return body, true, nil
	}
	_, _ = io.Copy(io.Discard, r.Body)
	return body, false, nil
}

func refuseAll(push *git.PushRequest, reason string) []git.PushedRef {
	refs := make([]git.PushedRef, 0, len(push.Updates))
	for _, u := range push.Updates {
		refs = append(refs, git.PushedRef{Ref: u.Ref, Old: u.Old, New: u.New, Refused: reason})
	}
	return refs
}

// answerRefusal tells git every ref was refused and why, as receive-pack
// would; a client that asked for no report hears the first reason instead.
func answerRefusal(w http.ResponseWriter, push *git.PushRequest, refs []git.PushedRef) {
	reasons := map[string]string{}
	first := ""
	for _, ref := range refs {
		reasons[ref.Ref] = ref.Refused
		if first == "" && !strings.HasPrefix(ref.Refused, "Another ref") {
			first = ref.Refused
		}
	}
	report := git.RefusalReport(push, reasons, "Armature refused this push: "+first)
	if report == nil {
		gitRefuse(w, http.StatusForbidden, first)
		return
	}
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(report)
}

func (s *Server) recordPush(ctx context.Context, repo *git.Repository, rec git.PushRecord) {
	lsn, err := s.Git.RecordPush(ctx, repo, rec)
	if err != nil {
		s.Log.Error("git proxy: audit a push", "error", err, "repository", repo.Name)
		return
	}
	NoteWrite(ctx, lsn)
}

// relayedHeaders are what the host says that git needs to hear; its cookies
// and its challenges never reach the client.
var relayedHeaders = []string{"Content-Type", "Cache-Control", "Expires", "Pragma"}

// relayGit streams the host's answer back, or says in a sentence why the host
// could not be had. A refusal of the token is Armature's to fix, not the caller's.
func (s *Server) relayGit(w http.ResponseWriter, repo *git.Repository, resp *http.Response, err error) {
	if err != nil {
		s.Log.Warn("git proxy: reach the host", "error", err, "repository", repo.Name)
		gitRefuse(w, http.StatusBadGateway, "Armature could not reach the host of "+repo.Name+". Try again in a moment.")
		return
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		gitRefuse(w, http.StatusBadGateway, "The host refused Armature's token for "+repo.Name+". A project administrator renews it under the project's repositories.")
		return
	case resp.StatusCode == http.StatusNotFound:
		gitRefuse(w, http.StatusBadGateway, "The host has no "+repo.Name+", or Armature's token cannot see it. A project administrator checks the repository's name and token.")
		return
	case resp.StatusCode/100 != 2:
		gitRefuse(w, http.StatusBadGateway, "The host of "+repo.Name+" answered "+itoa(resp.StatusCode)+". Try again in a moment.")
		return
	}
	for _, name := range relayedHeaders {
		if value := resp.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher := http.NewResponseController(w)
	buf := make([]byte, gitRelayChunk)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			_ = flusher.Flush()
		}
		if err != nil {
			return
		}
	}
}

// gitRelayChunk is how much of the host's answer is passed on at a time;
// each piece is flushed, so git sees progress as the host sends it.
const gitRelayChunk = 32 << 10
