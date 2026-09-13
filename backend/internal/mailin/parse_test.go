package mailin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) *Message {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return m
}

func TestParseReadsTheHeadersThatMatter(t *testing.T) {
	m := fixture(t, "plain.eml")
	if m.From != "ada@example.com" || m.MessageID != "abc123@example.com" || m.InReplyTo != "HELP-1.0190@armature.test" {
		t.Errorf("headers = %+v", m)
	}
	if len(m.Recipients) != 1 || m.Recipients[0] != "support@armature.test" {
		t.Errorf("recipients = %v", m.Recipients)
	}
	if IssueKeyIn(m.Subject, m.InReplyTo, m.References) != "HELP-1" {
		t.Errorf("key from %q", m.Subject)
	}
	if got := StripQuotes(m.Text); got != "Still broken, sorry." {
		t.Errorf("text = %q", got)
	}
	if m.AutoSubmitted {
		t.Error("a person's mail was taken for a machine's")
	}
}

func TestParseDecodesEncodingsAndCharsets(t *testing.T) {
	qp := fixture(t, "quoted-printable.eml")
	if qp.Subject != "Re: [HELP-2] ändern" {
		t.Errorf("encoded-word subject = %q", qp.Subject)
	}
	if got := StripQuotes(qp.Text); got != "Schön, danke \u2013 passt." {
		t.Errorf("quoted-printable text = %q", got)
	}
	if len(qp.Recipients) != 3 {
		t.Errorf("recipients = %v, want To and Cc", qp.Recipients)
	}
	b64 := fixture(t, "base64-latin1.eml")
	if b64.Text != "Café is closed." {
		t.Errorf("base64 latin1 text = %q", b64.Text)
	}
}

func TestParsePrefersPlainAndFallsBackToHTML(t *testing.T) {
	if alt := fixture(t, "alternative.eml"); alt.Text != "The plain words." {
		t.Errorf("alternative = %q", alt.Text)
	}
	if mixed := fixture(t, "mixed.eml"); mixed.Text != "See the file." {
		t.Errorf("mixed = %q", mixed.Text)
	}
	rich := fixture(t, "html-only.eml")
	if rich.Text != "First line & more\nSecond\nthird" {
		t.Errorf("html = %q", rich.Text)
	}
	if strings.Contains(rich.Text, "color") {
		t.Error("the style block leaked into the text")
	}
}

func TestParseKnowsAMachineWhenItSeesOne(t *testing.T) {
	if !fixture(t, "auto.eml").AutoSubmitted {
		t.Error("an auto reply was not marked")
	}
	if fixture(t, "auto.eml").MessageID != "" {
		t.Error("a missing id should be empty")
	}
}

func TestParseRefusesWhatIsNotAMail(t *testing.T) {
	if _, err := Parse([]byte("")); err == nil {
		t.Error("an empty body was taken for a mail")
	}
}
