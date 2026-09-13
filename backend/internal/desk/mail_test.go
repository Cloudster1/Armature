package desk

import (
	"strings"
	"testing"
)

func TestMailWordsTheReplyAndTheResolution(t *testing.T) {
	link := requestLink("http://app.test/", "acme", "HELP-1")
	if link != "http://app.test/desk/acme?next=%2Fportal%2Frequests%2FHELP-1" {
		t.Errorf("link = %q", link)
	}

	reply := replyMessage("HELP-1", "Cannot print", "Grace Hopper", "Try again now.", link)
	if !strings.Contains(reply.Subject, "HELP-1") || !strings.Contains(reply.Subject, "Grace Hopper") {
		t.Errorf("subject = %q", reply.Subject)
	}
	if !strings.Contains(reply.Body, "Try again now.") || !strings.Contains(reply.Body, link) {
		t.Errorf("body = %q", reply.Body)
	}

	done := resolvedMessage("HELP-1", "Cannot print", "Resolved", link)
	if !strings.Contains(done.Subject, "resolved") || !strings.Contains(done.Body, "comes back to us") {
		t.Errorf("resolved = %+v", done)
	}

	raised := raisedMessage("HELP-1", "Cannot print", link)
	if !strings.Contains(raised.Subject, "[HELP-1]") || !strings.Contains(raised.Body, link) {
		t.Errorf("raised = %+v", raised)
	}

	handled := assignedMessage("HELP-1", "Cannot print", "Grace Hopper", link)
	if !strings.Contains(handled.Subject, "Grace Hopper is handling") || !strings.Contains(handled.Body, link) {
		t.Errorf("assigned = %+v", handled)
	}
	following := followingMessage("HELP-1", "Cannot print", "Ada", link, "http://app.test/unwatch?token=abc")
	if !strings.Contains(following.Subject, "Ada added you") || !strings.Contains(following.Body, "Stop following this request: http://app.test/unwatch?token=abc") {
		t.Errorf("following = %+v", following)
	}
	if later := followingMessage("HELP-1", "Cannot print", "Ada", link, ""); strings.Contains(later.Body, "Stop following") {
		t.Errorf("a mail without a token offered a stop link: %+v", later)
	}

	code := codeMessage("Acme", "123456")
	if !strings.Contains(code.Subject, "123456") || !strings.Contains(code.Body, "ten minutes") {
		t.Errorf("code = %+v", code)
	}
}

func TestMessageIDBeginsWithTheKey(t *testing.T) {
	id := messageID("HELP-1", "armature.test")
	if !strings.HasPrefix(id, "<HELP-1.") || !strings.HasSuffix(id, "@armature.test>") {
		t.Errorf("id = %q", id)
	}
	if messageID("HELP-1", "armature.test") == id {
		t.Error("two ids for the same key should differ")
	}
}

func TestPlainTextReadsADocument(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hello "},{"type":"text","text":"there"}]}]}`
	if got := plainText(doc); got != "Hello there" {
		t.Errorf("plainText = %q", got)
	}
	if got := plainText("not json"); got != "not json" {
		t.Errorf("a non document should come back as it was, got %q", got)
	}
}
