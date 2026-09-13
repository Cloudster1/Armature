package openapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

type Priority string

type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Thing struct {
	ID        string          `json:"id"`
	Summary   string          `json:"summary"`
	Body      json.RawMessage `json:"body,omitempty"`
	Assignee  *Ref            `json:"assignee,omitempty"`
	Parent    *Ref            `json:"parent"`
	Priority  Priority        `json:"priority"`
	Tags      []string        `json:"tags"`
	Counts    map[string]int  `json:"counts,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	hidden    int
	Skipped   int `json:"-"`
}

type patch struct{ Set bool }

type Edit struct {
	Summary *string `json:"summary,omitempty"`
	Due     patch   `json:"due,omitempty"`
}

func doc(b *Builder) *Document {
	return &Document{Components: Components{Schemas: b.Components()}}
}

func TestStructsBecomeNamedComponents(t *testing.T) {
	b := NewBuilder()
	b.Enums[reflect.TypeOf(Priority(""))] = []string{"low", "high"}
	s := b.SchemaOf(Thing{})
	if s.Ref != "#/components/schemas/Thing" {
		t.Fatalf("got %+v", s)
	}
	thing := b.Components()["Thing"]
	if _, ok := b.Components()["Ref"]; !ok {
		t.Fatal("the nested struct should be a component too")
	}
	want := map[string]string{
		"id": "string", "summary": "string", "priority": "string", "tags": "array", "createdAt": "string",
	}
	for name, typ := range want {
		if got := thing.Properties[name].Type; got != typ {
			t.Errorf("%s: got %v want %s", name, got, typ)
		}
	}
	if thing.Properties["createdAt"].Format != "date-time" {
		t.Error("time.Time should be a date-time string")
	}
	if len(thing.Properties["priority"].Enum) != 2 {
		t.Error("the enum values should be listed")
	}
	// A pointer with omitempty is absent, not null; a pointer without is nullable.
	if thing.Properties["assignee"].Ref == "" {
		t.Errorf("assignee should be a plain reference, got %+v", thing.Properties["assignee"])
	}
	if len(thing.Properties["parent"].OneOf) != 2 {
		t.Errorf("parent should be nullable, got %+v", thing.Properties["parent"])
	}
	required := strings.Join(thing.Required, ",")
	for _, name := range []string{"id", "summary", "parent", "priority", "tags", "createdAt"} {
		if !strings.Contains(required, name) {
			t.Errorf("%s should be required (%s)", name, required)
		}
	}
	for _, name := range []string{"body", "assignee", "counts", "hidden", "Skipped"} {
		if strings.Contains(required, name) {
			t.Errorf("%s should not be required (%s)", name, required)
		}
	}
	if _, ok := thing.Properties["hidden"]; ok {
		t.Error("unexported fields are not written")
	}
	if _, ok := thing.Properties["Skipped"]; ok {
		t.Error("json:\"-\" fields are not written")
	}
}

func TestOverridesReplaceAwkwardTypes(t *testing.T) {
	b := NewBuilder()
	b.Overrides[reflect.TypeOf(patch{})] = Nullable(&Schema{Type: "string", Format: "date"})
	s := b.SchemaOf(Edit{})
	edit := b.Components()["Edit"]
	_ = s
	if got := edit.Properties["due"].Types(); strings.Join(got, ",") != "string,null" {
		t.Fatalf("due: got %v", got)
	}
	if len(edit.Required) != 0 {
		t.Fatalf("an edit has nothing required, got %v", edit.Required)
	}
}

func TestValidateAgainstTheBuiltSchema(t *testing.T) {
	b := NewBuilder()
	b.Enums[reflect.TypeOf(Priority(""))] = []string{"low", "high"}
	s := b.SchemaOf(Thing{})
	d := doc(b)

	good := `{"id":"1","summary":"x","parent":null,"priority":"low","tags":["a"],"createdAt":"2026-01-01T00:00:00Z","assignee":{"id":"u","name":"n"},"counts":{"a":1}}`
	if err := d.ValidateJSON(s, []byte(good)); err != nil {
		t.Fatalf("a well formed value should pass: %v", err)
	}
	cases := map[string]string{
		"missing required":  `{"id":"1","summary":"x","parent":null,"priority":"low","createdAt":"2026-01-01T00:00:00Z"}`,
		"wrong type":        `{"id":1,"summary":"x","parent":null,"priority":"low","tags":[],"createdAt":"2026-01-01T00:00:00Z"}`,
		"bad enum":          `{"id":"1","summary":"x","parent":null,"priority":"urgent","tags":[],"createdAt":"2026-01-01T00:00:00Z"}`,
		"null where not":    `{"id":"1","summary":"x","parent":null,"priority":"low","tags":null,"createdAt":"2026-01-01T00:00:00Z"}`,
		"nested wrong":      `{"id":"1","summary":"x","parent":{"id":"p"},"priority":"low","tags":[],"createdAt":"2026-01-01T00:00:00Z"}`,
		"map value wrong":   `{"id":"1","summary":"x","parent":null,"priority":"low","tags":[],"createdAt":"2026-01-01T00:00:00Z","counts":{"a":"one"}}`,
		"fraction as count": `{"id":"1","summary":"x","parent":null,"priority":"low","tags":[],"createdAt":"2026-01-01T00:00:00Z","counts":{"a":1.5}}`,
	}
	for name, raw := range cases {
		if err := d.ValidateJSON(s, []byte(raw)); err == nil {
			t.Errorf("%s should fail", name)
		}
	}
	// Extra properties are allowed: a client must tolerate a server that says more.
	if err := d.ValidateJSON(s, []byte(strings.Replace(good, `"id":"1"`, `"id":"1","extra":true`, 1))); err != nil {
		t.Errorf("extra properties should pass: %v", err)
	}
}

func TestNullable(t *testing.T) {
	if got := Nullable(&Schema{Type: "string"}).Types(); strings.Join(got, ",") != "string,null" {
		t.Fatalf("got %v", got)
	}
	if got := Nullable(Nullable(&Schema{Type: "string"})).Types(); strings.Join(got, ",") != "string,null" {
		t.Fatalf("nullable twice stays nullable once: %v", got)
	}
	if got := Nullable(&Schema{Ref: "#/components/schemas/X"}); len(got.OneOf) != 2 {
		t.Fatalf("a reference is made nullable with oneOf, got %+v", got)
	}
}

func TestDocumentRendersDeterministically(t *testing.T) {
	b := NewBuilder()
	b.SchemaOf(Thing{})
	d := doc(b)
	d.OpenAPI = "3.1.0"
	d.Paths = map[string]PathItem{"/things": {"get": &Operation{OperationID: "listThings", Responses: map[string]*Response{"200": {Description: "ok"}}}}}
	first, err := d.MarshalIndent()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := d.MarshalIndent()
	if string(first) != string(second) {
		t.Fatal("two renderings differ")
	}
	if !strings.Contains(string(first), `"$ref": "#/components/schemas/Ref"`) {
		t.Fatalf("the reference should be rendered: %s", first)
	}
}

// A raw JSON field can be promised to be more than "some JSON".
func TestFieldOverridesNameOneField(t *testing.T) {
	b := NewBuilder()
	b.FieldOverrides["Thing.body"] = &Schema{Type: "object", Description: "a document"}
	b.SchemaOf(Thing{})
	thing := b.Components()["Thing"]
	if thing.Properties["body"].Type != "object" {
		t.Fatalf("body should be overridden, got %+v", thing.Properties["body"])
	}
	if thing.Properties["summary"].Type != "string" {
		t.Fatal("other fields are untouched")
	}
}
