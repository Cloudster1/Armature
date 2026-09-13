package issue

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const richDoc = `{"type":"doc","content":[
 {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Plan"}]},
 {"type":"paragraph","content":[{"type":"text","text":"Ask ","marks":[{"type":"bold"}]},{"type":"mention","attrs":{"id":"0f6d1a5e-1b6c-4c3e-9d2a-2b7c9e1f0a11","label":"Ada Lovelace"}},{"type":"text","text":" first."}]},
 {"type":"bulletList","content":[
  {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]},
  {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"},{"type":"hardBreak"},{"type":"text","text":"more"}]}]}]},
 {"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]}]},
 {"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},
 {"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1\ny := 2"}]},
 {"type":"paragraph","content":[{"type":"text","text":"site","marks":[{"type":"link","attrs":{"href":"https://example.test"}}]}]}
]}`

func TestValidateDocumentAcceptsWhatTheRendererShows(t *testing.T) {
	if err := ValidateDocument(json.RawMessage(richDoc), "description"); err != nil {
		t.Fatalf("a document of every allowed node was refused: %v", err)
	}
	if err := ValidateDocument(TextDocument("plain words"), "comment"); err != nil {
		t.Fatalf("a plain text document was refused: %v", err)
	}
}

func TestValidateDocumentRefusesInASentence(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"type":"doc","content":[{"type":"table"}]}`, `The description holds a "table", which this tracker cannot show; take it out.`},
		{`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"underline"}]}]}]}`, `uses a "underline" style`},
		{`{"type":"doc","content":[{"type":"heading","attrs":{"level":5},"content":[{"type":"text","text":"x"}]}]}`, `beyond level 3`},
		{`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}]}]}`, `not a web or mail address`},
		{`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"nope","label":"x"}}]}]}`, `without saying who`},
		{`{"type":"paragraph"}`, `must be a document`},
		{`not json`, `not a document this tracker can read`},
	}
	for _, c := range cases {
		err := ValidateDocument(json.RawMessage(c.body), "description")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.body, err, c.want)
		}
	}
	deep := strings.Repeat(`{"type":"blockquote","content":[`, maxDocDepth+2) + `{"type":"paragraph"}` + strings.Repeat(`]}`, maxDocDepth+2)
	if err := ValidateDocument(json.RawMessage(`{"type":"doc","content":[`+deep+`]}`), "comment"); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Errorf("a deep nest was accepted: %v", err)
	}
	big := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + strings.Repeat("a", MaxDocBytes) + `"}]}]}`
	if err := ValidateDocument(json.RawMessage(big), "comment"); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("a megabyte was accepted: %v", err)
	}
}

func TestEmptyDocumentsAndMentions(t *testing.T) {
	for _, body := range []string{``, `null`, `{"type":"doc","content":[]}`, `{"type":"doc","content":[{"type":"paragraph"},{"type":"paragraph","content":[]}]}`} {
		if !IsEmptyDocument(json.RawMessage(body)) {
			t.Errorf("%q should be empty", body)
		}
	}
	if IsEmptyDocument(TextDocument("x")) || IsEmptyDocument(json.RawMessage(richDoc)) {
		t.Error("a document with words is not empty")
	}
	ids := MentionIDs(json.RawMessage(richDoc))
	if len(ids) != 1 || ids[0] != uuid.MustParse("0f6d1a5e-1b6c-4c3e-9d2a-2b7c9e1f0a11") {
		t.Errorf("mentions = %v", ids)
	}
}

// Mail and search read the document the way a person would have typed it.
func TestPlainTextReadsLikeTyping(t *testing.T) {
	got := PlainText(json.RawMessage(richDoc))
	want := strings.Join([]string{
		"Plan",
		"",
		"Ask @Ada Lovelace first.",
		"",
		"- one",
		"- two",
		"  more",
		"",
		"1. first",
		"",
		"> quoted",
		"",
		"```",
		"x := 1",
		"y := 2",
		"```",
		"",
		"site",
	}, "\n")
	if got != want {
		t.Errorf("PlainText =\n%s\nwant\n%s", got, want)
	}
	if got := PlainText(TextDocument("two\nlines")); got != "two\nlines" {
		t.Errorf("a plain document keeps its lines: %q", got)
	}
}
