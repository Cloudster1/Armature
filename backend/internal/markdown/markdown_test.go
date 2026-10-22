package markdown

import (
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/issue"
)

func TestMarkdownKeepsWhatAnIssueBodyHolds(t *testing.T) {
	body := "## User story\n\nAs **somebody**, I want `code` and [docs](docs/api.md).\n\n" +
		"#### Deep heading\n\n- [x] done\n- [ ] open\n\n1. first\n2. second\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nfmt.Println()\n```\n\n> quoted ~~gone~~\n\nSee https://example.com and [anchor](#here)."
	doc := Document(body, "https://github.com/o/r/blob/abc/")
	if err := issue.ValidateDocument(doc, "description"); err != nil {
		t.Fatalf("not a description: %v\n%s", err, doc)
	}
	text := string(doc)
	for _, want := range []string{
		`"type":"heading","attrs":{"level":2}`,
		`"type":"heading","attrs":{"level":3}`,
		`"type":"bold"`,
		`"type":"code"`,
		`"href":"https://github.com/o/r/blob/abc/docs/api.md"`,
		`"text":"[x] done"`,
		`"text":"[ ] open"`,
		`"type":"orderedList"`,
		`"text":"a | b"`,
		`"text":"1 | 2"`,
		`"type":"codeBlock","attrs":{"language":"go"}`,
		`"type":"blockquote"`,
		`"type":"strike"`,
		`"href":"https://example.com"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the description lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"href":"#here"`) {
		t.Errorf("an anchor became a link the tracker would refuse:\n%s", text)
	}
}

func TestAnEmptyBodyIsNoDescription(t *testing.T) {
	if doc := Document("  \n\n", ""); doc != nil {
		t.Errorf("an empty body became %s", doc)
	}
}
