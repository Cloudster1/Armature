package issue

import (
	"encoding/json"
	"strings"
	"testing"
)

// The markup in a real export: headings in most descriptions, bullets in half
// of them, the odd bold word, a link and a block of shell.
func TestWikiMarkupIsReadAsFarAsTheDocumentAllows(t *testing.T) {
	raw := WikiDocument(strings.Join([]string{
		"h2. What was done",
		"",
		" * Three nodes, one of them spare",
		" ** and a note under one",
		" * Certificates renewed",
		"",
		"The whole thing is *automated* now, see [the runbook|https://example.test/run].",
		"",
		"{code}",
		"sudo iptables -I FORWARD 2 -j ACCEPT",
		"{code}",
	}, "\n"))
	if raw == nil {
		t.Fatal("the markup came back as nothing")
	}
	if err := ValidateDocument(raw, "description"); err != nil {
		t.Fatalf("the document this read cannot be shown: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	kinds := blockKinds(doc)
	for _, want := range []string{"heading", "bulletList", "paragraph", "codeBlock"} {
		if !contains(kinds, want) {
			t.Errorf("blocks = %v, want a %s among them", kinds, want)
		}
	}
	text := string(raw)
	if !strings.Contains(text, `"level":2`) {
		t.Error("the heading lost its level")
	}
	if !strings.Contains(text, `"type":"bold"`) || !strings.Contains(text, `"href":"https://example.test/run"`) {
		t.Errorf("the marks inside the line were lost: %s", text)
	}
	if !strings.Contains(text, "sudo iptables") {
		t.Error("the block of code was lost")
	}
}

// A deeper heading than this can show is flattened rather than dropped, and a
// mention of somebody another tracker knew stays as words.
func TestWikiMarkupKeepsWhatItCannotShow(t *testing.T) {
	raw := WikiDocument("h5. Deep\n\n[~ada.byrne] looked at it.\n\n||a||b||\n|1|2|")
	if err := ValidateDocument(raw, "description"); err != nil {
		t.Fatalf("cannot be shown: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"level":3`) {
		t.Errorf("a deep heading was not flattened: %s", text)
	}
	if !strings.Contains(text, "@ada.byrne") {
		t.Errorf("a mention was lost: %s", text)
	}
	if !strings.Contains(text, "a | b") || !strings.Contains(text, "1 | 2") {
		t.Errorf("a table lost its cells: %s", text)
	}
	if WikiDocument("   ") != nil {
		t.Error("empty markup became a document")
	}
}

func blockKinds(doc map[string]any) []string {
	var kinds []string
	for _, block := range doc["content"].([]any) {
		kinds = append(kinds, block.(map[string]any)["type"].(string))
	}
	return kinds
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
