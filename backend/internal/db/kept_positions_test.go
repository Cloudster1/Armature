package db

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// servicesRoot is where the services live, seen from this package.
const servicesRoot = ".."

// discardsAllowed names every write whose position is thrown away on purpose,
// keyed by file and function, with why no read of its caller depends on it.
var discardsAllowed = map[string]string{
	// Nobody waits to read these: they run from a timer, the outbox or a mailbox.
	"audit/audit.go (*Consumer).Handle":             "an event consumer; no request waits on it",
	"automation/runner.go (*Service).run":           "a rule run from an event; no request waits on it",
	"desk/inbound_retry.go (*Inbound).claim":        "mail fetched from a mailbox; no request waits on it",
	"desk/inbound_retry.go (*Inbound).fail":         "mail fetched from a mailbox; no request waits on it",
	"desk/inbound.go (*Inbound).finish":             "mail fetched from a mailbox; no request waits on it",
	"desk/inbound_raise.go (*Inbound).raise":        "mail fetched from a mailbox; no request waits on it",
	"desk/inbound_raise.go (*Inbound).attach":       "mail fetched from a mailbox; no request waits on it",
	"desk/mail.go (*Notifier).Handle":               "an event consumer; no request waits on it",
	"desk/watch.go (*Watch).Once":                   "the breach watch; no request waits on it",
	"events/consumer.go (*Consumer).deadLetter":     "an event consumer; no request waits on it",
	"events/relay.go (*Relay).once":                 "the outbox relay; no request waits on it",
	"filter/subscriptions.go (*Subscriptions).send": "a scheduled mail; no request waits on it",
	"notify/digest.go (*Digest).send":               "a scheduled mail; no request waits on it",
	"notify/fanout.go (*FanOut).deliver":            "an event consumer; no request waits on it",
	"privacy/retention.go (*Retention).prune":       "a scheduled sweep; no request waits on it",
	"report/flow.go (*Service).WriteFlowSnapshot":   "the nightly worker; no request waits on it",
	"sprint/snapshot.go (*Service).Snapshot":        "the nightly worker; no request waits on it",
	"webhook/send.go (*Service).pause":              "the delivery worker; no request waits on it",

	// Called by another machine, which reads nothing back through a session.
	"automation/runner.go (*Service).Incoming":   "an incoming hook answers its caller and keeps no session",
	"git/receive.go (*Service).Record":           "a forge's push answers the forge, which keeps no session",
	"git/smart_once.go (*Service).forgetCommand": "part of a forge's push, which keeps no session",
	"git/smart_once.go (*Service).recordCommand": "part of a forge's push, which keeps no session",
	"desk/extras.go (*Service).InviteRating":     "no handler calls it; the tests that do read on the primary",
	"oauth/clients.go (*Service).Register":       "a partner registering itself keeps no session; the client is read on the primary",
	"oauth/token.go (*Service).Token":            "a connected app keeps no session; its tokens are read through Authenticate, on the primary",
	"oauth/token.go (*Service).Revoke":           "a connected app keeps no session; its tokens are read through Authenticate, on the primary",

	// Read back only on the primary, through ReadAdmin or WriteAdmin.
	"auth/service.go (*Service).Logout":                 "sessions are read through Authenticate, on the primary",
	"auth/login.go (*Service).Login":                    "a rehashed password is read only at sign-in, on the primary",
	"auth/portal.go (*Service).IssuePortalCode":         "the code is checked through WriteAdmin, on the primary",
	"auth/recovery.go (*Service).RequestPasswordReset":  "the link is read back only through WriteAdmin, on the primary",
	"auth/secondfactor.go (*Service).issueChallenge":    "the challenge is read back only through WriteAdmin, on the primary",
	"auth/secondfactor.go (*Service).StartSecondFactor": "the secret is read back only through WriteAdmin and ReadAdmin, on the primary",
	"auth/recovery.go (*Service).ResetPassword":         "the new password is read only at sign-in, on the primary",
	"auth/profile.go (*Service).UpdateProfile":          "the owner reads it back through Authenticate, on the primary",
	"auth/profile.go (*Service).SetAvatarURL":           "the owner reads it back through Authenticate, on the primary",
	"oidc/service.go (*Service).Start":                  "the login is read back by Exchange through WriteAdmin",
	"oidc/service.go (*Service).Exchange":               "it consumes the login; nothing reads it afterwards",
	"oidc/signin.go (*Service).signIn":                  "sessions are read through Authenticate, on the primary",
	"oidc/signin.go (*Service).noteJoinRequest":         "the caller is refused and holds no session to pin",
	"privacy/service.go (*Service).Erase":               "the person is signed out in the same answer",
	"theme/service.go (*Service).EraseOwner":            "part of an erasure, whose person is signed out",
	"report/flow.go (*Service).reconstructMissingDays":  "the report reads what it rebuilt on the primary, in the same request",
	"report/service.go (*Service).Dashboards":           "the list reads what it made on the primary, in the same request",

	// Stamps of use, which nobody needs to the second.
	"auth/service.go (*Service).authenticateSession":  "a session's last sight, touched at most once a minute",
	"auth/service.go (*Service).authenticateAPIToken": "a token's last use, which the tokens page shows to the minute",

	// A later write in the same request carries a position past this one.
	"auth/accounts.go (*Service).UserForAddress":      "the watcher added next carries the later position the handler notes",
	"desk/companies.go (*Service).raisersCompany":     "the request raised next carries the later position the handler notes",
	"auth/import_member.go (*Service).ImportedMember": "the import's later writes carry the position the handler notes",
	"demo/plant.go plant":                             "the demo's record, written last, carries the position the handler notes",
}

// A write whose position is thrown away cannot be noted by its handler, and the
// caller's next read may come from a replica that has not seen it (issue #100).
func TestEveryServiceKeepsThePositionItsWriteReturns(t *testing.T) {
	found, err := discardedPositions(servicesRoot)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, site := range found {
		seen[site.key] = true
		if _, ok := discardsAllowed[site.key]; !ok {
			t.Errorf("%s throws away the position its write returns. Return it so the handler can call NoteWrite, or add %q to discardsAllowed with why no read of the caller's depends on it.", site.at, site.key)
		}
	}
	var stale []string
	for key := range discardsAllowed {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("%s no longer throws a position away; take it off discardsAllowed.", key)
	}
}

// The check must recognise the shapes a discarded position takes, or the test
// above passes by seeing nothing.
func TestTheCheckFindsADiscardedPosition(t *testing.T) {
	src := `package x
func (s *Service) Kept(ctx context.Context) (LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx DBTX) error { return nil })
}
func (s *Service) Assigned(ctx context.Context) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx DBTX) error { return nil })
	return err
}
func (s *Service) InIf(ctx context.Context) error {
	if _, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx DBTX) error { return nil }); err != nil {
		return err
	}
	return nil
}
func Plain(w io.Writer) { _, _ = w.Write(nil) }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x/x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, site := range discardsIn(fset, "x/x.go", file) {
		keys = append(keys, site.key)
	}
	want := []string{"x/x.go (*Service).Assigned", "x/x.go (*Service).InIf"}
	if strings.Join(keys, ", ") != strings.Join(want, ", ") {
		t.Fatalf("found %v, want %v", keys, want)
	}
}

type discardSite struct {
	key string // file and function, which is what the allowlist names
	at  string // file and line, for the message
}

// discardedPositions walks the services, leaving out this package and tests.
func discardedPositions(root string) ([]discardSite, error) {
	fset := token.NewFileSet()
	var out []discardSite
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "db" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		out = append(out, discardsIn(fset, rel, file)...)
		return nil
	})
	return out, err
}

func discardsIn(fset *token.FileSet, rel string, file *ast.File) []discardSite {
	var out []discardSite
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		key := rel + " " + funcName(fn)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			var call ast.Expr
			switch s := n.(type) {
			case *ast.AssignStmt:
				if len(s.Rhs) == 1 && len(s.Lhs) > 0 && isBlank(s.Lhs[0]) {
					call = s.Rhs[0]
				}
			case *ast.ExprStmt:
				call = s.X
			}
			if call != nil && isClusterWrite(call) {
				out = append(out, discardSite{key: key, at: fmt.Sprintf("%s:%d", rel, fset.Position(n.Pos()).Line)})
			}
			return true
		})
	}
	return out
}

// isClusterWrite matches Write and WriteAdmin by their shape, a context and a
// function literal, so an io.Writer's Write is not mistaken for one.
func isClusterWrite(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Write" && sel.Sel.Name != "WriteAdmin") {
		return false
	}
	_, isFunc := call.Args[1].(*ast.FuncLit)
	return isFunc
}

func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := fn.Recv.List[0].Type
	prefix := ""
	if star, ok := recv.(*ast.StarExpr); ok {
		prefix, recv = "*", star.X
	}
	if id, ok := recv.(*ast.Ident); ok {
		return "(" + prefix + id.Name + ")." + fn.Name.Name
	}
	return fn.Name.Name
}
