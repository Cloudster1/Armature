package events_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/events"
)

// servicesRoot is where the services live, seen from this package.
const servicesRoot = ".."

// notListed names every topic the code publishes that Topics leaves out on purpose, with why.
var notListed = map[string]string{
	events.TopicAbsenceRecorded: "whose days away are private, so only an endpoint taking everything gets them",
	events.TopicAbsenceChanged:  "whose days away are private, so only an endpoint taking everything gets them",
	events.TopicAbsenceRemoved:  "whose days away are private, so only an endpoint taking everything gets them",
	events.TopicAccessRequested: "which names a person and a project kept from them, so it stays inside for the project's administrators",
}

// A topic offered for subscription is one something publishes, and a topic the code
// publishes is offered, so an endpoint never waits on silence or misses what it asked for.
func TestEveryTopicIsPublishedAndEveryPublishedTopicIsListed(t *testing.T) {
	fset := token.NewFileSet()
	files, err := sources(fset, servicesRoot)
	if err != nil {
		t.Fatal(err)
	}
	values := topicConstants(files["events"])
	// published is where each topic is published, by its value.
	published := map[string][]string{}
	for name, at := range publishedTopics(files) {
		if topic, ok := values[name]; ok {
			published[topic] = append(published[topic], at...)
		}
	}

	for _, topic := range events.Topics {
		if len(published[topic]) == 0 {
			t.Errorf("%s is in events.Topics, so an endpoint can take it, but nothing publishes it. Publish it, or take it off the list.", topic)
		}
	}
	var topics []string
	for topic := range published {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	for _, topic := range topics {
		listed := slices.Contains(events.Topics, topic)
		_, left := notListed[topic]
		switch {
		case !listed && !left:
			t.Errorf("%s publishes %s, which events.Topics does not list, so no endpoint can choose it. List it, or add it to notListed with why.", published[topic][0], topic)
		case listed && left:
			t.Errorf("%s is in events.Topics and in notListed; take it off one.", topic)
		}
	}
	for topic := range notListed {
		if len(published[topic]) == 0 {
			t.Errorf("%s is in notListed, but nothing publishes it any more; take it off.", topic)
		}
	}
}

// The check must see each shape a publisher takes, or the test above passes by seeing nothing.
func TestTheCheckFindsEachShapeOfPublisher(t *testing.T) {
	src := `package x
func direct(ctx context.Context, tx db.DBTX) error { return events.Emit(ctx, tx, events.TopicA, nil) }
func wrapper(ctx context.Context, tx db.DBTX, topic string) error { return events.Emit(ctx, tx, topic, nil) }
func (s *Service) wrapped(ctx context.Context, tx db.DBTX) error { return wrapper(ctx, tx, events.TopicB) }
func (s *Service) method(ctx context.Context, tx db.DBTX, topic string) error { return wrapper(ctx, tx, topic) }
func (s *Service) viaMethod(ctx context.Context, tx db.DBTX) error { return s.method(ctx, tx, events.TopicC) }
func (s *Service) handed(ctx context.Context) error { return s.Handle(ctx, events.Event{Topic: events.TopicD}) }
func (s *Service) consumer(e events.Event) bool { return e.Topic == events.TopicE || strings.HasPrefix(events.TopicF, "x") }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x/x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	published := publishedTopics(map[string][]*parsed{"x": {{rel: "x/x.go", fset: fset, file: file}}})
	var got []string
	for name := range published {
		got = append(got, name)
	}
	sort.Strings(got)
	if want := []string{"TopicA", "TopicB", "TopicC", "TopicD"}; !slices.Equal(got, want) {
		t.Fatalf("found %v, want %v", got, want)
	}
}

type parsed struct {
	rel  string
	fset *token.FileSet
	file *ast.File
}

// sources parses the services' code by package directory, leaving out tests.
func sources(fset *token.FileSet, root string) (map[string][]*parsed, error) {
	out := map[string][]*parsed{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		out[dir] = append(out[dir], &parsed{rel: rel, fset: fset, file: file})
		return nil
	})
	return out, err
}

// topicConstants maps each Topic constant of the events package to its value.
func topicConstants(files []*parsed) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		for _, decl := range f.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Topic") || i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						out[name.Name], _ = strconv.Unquote(lit.Value)
					}
				}
			}
		}
	}
	return out
}

// emitterKey names a function by package directory and name; a method goes by its name alone.
type emitterKey struct{ dir, name string }

// publishedTopics finds, for each events constant, where it is published: as the topic of
// events.Emit, of a function that hands its own parameter on to it, or of an events.Event built.
func publishedTopics(files map[string][]*parsed) map[string][]string {
	// emitters says which argument of a function is the topic it publishes.
	emitters := map[emitterKey]int{}
	topicArg := func(dir string, call *ast.CallExpr) (ast.Expr, bool) {
		var key emitterKey
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok && x.Name == "events" && fn.Sel.Name == "Emit" {
				key = emitterKey{"events", "Emit"}
			} else {
				key = emitterKey{dir, fn.Sel.Name}
			}
		case *ast.Ident:
			key = emitterKey{dir, fn.Name}
		default:
			return nil, false
		}
		i, ok := emitters[key]
		if key.dir == "events" && key.name == "Emit" {
			i, ok = 2, true
		}
		if !ok || i >= len(call.Args) {
			return nil, false
		}
		return call.Args[i], true
	}

	// A wrapper of a wrapper is found on a later pass.
	for grew := true; grew; {
		grew = false
		for dir, fs := range files {
			for _, f := range fs {
				for _, decl := range f.file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Body == nil {
						continue
					}
					key := emitterKey{dir, fn.Name.Name}
					if _, known := emitters[key]; known {
						continue
					}
					params := paramNames(fn)
					ast.Inspect(fn.Body, func(n ast.Node) bool {
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}
						arg, ok := topicArg(dir, call)
						if !ok {
							return true
						}
						if id, ok := arg.(*ast.Ident); ok {
							if i := slices.Index(params, id.Name); i >= 0 {
								emitters[key] = i
								grew = true
							}
						}
						return true
					})
				}
			}
		}
	}

	out := map[string][]string{}
	note := func(f *parsed, e ast.Expr) {
		if sel, ok := e.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "events" && strings.HasPrefix(sel.Sel.Name, "Topic") {
				out[sel.Sel.Name] = append(out[sel.Sel.Name], f.fset.Position(e.Pos()).String())
			}
		}
	}
	for dir, fs := range files {
		for _, f := range fs {
			ast.Inspect(f.file, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					if arg, ok := topicArg(dir, n); ok {
						note(f, arg)
					}
				case *ast.CompositeLit:
					if sel, ok := n.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Event" {
						if x, ok := sel.X.(*ast.Ident); ok && x.Name == "events" {
							for _, elt := range n.Elts {
								if kv, ok := elt.(*ast.KeyValueExpr); ok {
									if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Topic" {
										note(f, kv.Value)
									}
								}
							}
						}
					}
				}
				return true
			})
		}
	}
	return out
}

func paramNames(fn *ast.FuncDecl) []string {
	var out []string
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			out = append(out, "_")
		}
		for _, name := range field.Names {
			out = append(out, name.Name)
		}
	}
	return out
}
