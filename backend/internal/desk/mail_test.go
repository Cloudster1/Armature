package desk

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/mailin"
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

// Every mail about a request points at one id that names the request, so a
// customer's client files the receipt, the replies and the resolution together.
func TestMailsAboutOneRequestShareAThread(t *testing.T) {
	n := &Notifier{inbox: "support@armature.test"}
	a := &about{id: uuid.New(), key: "HELP-1", summary: "Cannot print"}
	link := requestLink("http://app.test", "acme", a.key)
	first := n.mail(a, "ann@example.test", raisedMessage(a.key, a.summary, link))
	second := n.mail(a, "ann@example.test", replyMessage(a.key, a.summary, "Grace", "Try again.", link))

	root := first.InReplyTo
	if !strings.HasPrefix(root, "<HELP-1.") || !strings.HasSuffix(root, "@armature.test>") {
		t.Fatalf("In-Reply-To = %q, want an id that begins with the key", root)
	}
	for _, m := range []Mail{first, second} {
		if m.InReplyTo != root || len(m.References) != 1 || m.References[0] != root {
			t.Errorf("mail %q: In-Reply-To %q, References %q, want both to name %q", m.Subject, m.InReplyTo, m.References, root)
		}
	}
	if first.MessageID == second.MessageID || first.MessageID == root || second.MessageID == root {
		t.Errorf("ids %q, %q and the thread %q should all differ", first.MessageID, second.MessageID, root)
	}
	// A worker that restarted names the same thread for the same request.
	again := (&Notifier{inbox: "support@armature.test"}).mail(&about{id: a.id, key: a.key}, "bob@example.test", Message{Subject: "x"})
	if again.InReplyTo != root {
		t.Errorf("the thread moved from %q to %q between two notifiers", root, again.InReplyTo)
	}
	// The same key in another organization is another request.
	if other := n.mail(&about{id: uuid.New(), key: a.key}, "ann@example.test", Message{Subject: "x"}); other.InReplyTo == root {
		t.Errorf("two requests share the thread %q", root)
	}
}

// A customer's client answers with the thread in its headers and often a
// subject of its own; the reply still finds its request.
func TestAReplyInTheThreadFindsItsRequest(t *testing.T) {
	n := &Notifier{inbox: "support@armature.test"}
	sent := n.mail(&about{id: uuid.New(), key: "HELP-12"}, "ann@example.test", Message{Subject: "[HELP-12] replied"})
	raw := strings.Join([]string{
		"From: ann@example.test",
		"To: support@armature.test",
		"Subject: Re: about my printer",
		"Message-ID: <answer-1@example.test>",
		"In-Reply-To: " + sent.MessageID,
		"References: " + strings.Join(append(sent.References, sent.MessageID), " "),
		"",
		"Thanks, it prints.",
	}, "\r\n")
	m, err := mailin.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := mailin.IssueKeyIn(m.Subject, m.InReplyTo, m.References); got != "HELP-12" {
		t.Errorf("the reply was matched to %q, want HELP-12", got)
	}
	// A client that keeps only References still lands on the request.
	if len(m.References) == 0 {
		t.Fatalf("the reply's References were lost: %q", raw)
	}
	if got := mailin.IssueKeyIn(m.Subject, "", m.References[:1]); got != "HELP-12" {
		t.Errorf("the thread alone matched %q, want HELP-12", got)
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
