package mail

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	netmail "net/mail"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/mail/mailtest"
)

// A subject is somebody's words: an organization's name, a person's, an issue
// summary. A newline in one of them would start a header of their choosing.
func TestAHeaderStaysOnOneLine(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Acme", "Acme"},
		{"Acme\r\nBcc: everyone@elsewhere.test", "Acme  Bcc: everyone@elsewhere.test"},
		{"Line\nbreak", "Line break"},
		{"Bell\aring", "Bell ring"},
	} {
		if got := headerValue(c.in); got != c.want {
			t.Errorf("headerValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestARelayWithAnExpiredCertificateIsRefused(t *testing.T) {
	relay := mailtest.Start(t, mailtest.Options{})
	m := &SMTPMailer{Addr: relay.Addr, From: "Armature <no-reply@armature.test>"}
	err := m.Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("an expired certificate should be refused, got %v", err)
	}
	nothingTaken(t, relay)
}

func TestInsecureTLSSendsThroughAnExpiredCertificate(t *testing.T) {
	relay := mailtest.Start(t, mailtest.Options{})
	m := &SMTPMailer{Addr: relay.Addr, From: "Armature <no-reply@armature.test>", InsecureTLS: true}
	if err := m.Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if body := <-relay.Mails; !strings.Contains(body, "123456") {
		t.Errorf("the relay got %q, want the code in it", body)
	}
}

// sent hands a mail to the fake relay and reads back what it was given, the
// way a receiving server would.
func sent(t *testing.T, from string, m Mail) (string, *netmail.Message) {
	t.Helper()
	relay := mailtest.Start(t, mailtest.Options{})
	if err := (&SMTPMailer{Addr: relay.Addr, From: from, InsecureTLS: true}).Send(context.Background(), m); err != nil {
		t.Fatalf("send: %v", err)
	}
	raw := <-relay.Mails
	parsed, err := netmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("the relay got something that is not a mail: %v\n%s", err, raw)
	}
	return raw, parsed
}

// A relay without SMTPUTF8 refuses eight bit headers, and a strict reader
// garbles them; a summary in German or Japanese has to arrive as written.
func TestNonASCIIHeadersAndBodyAreEncoded(t *testing.T) {
	subject := "[HELP-7] Bestellung für Müller, 注文"
	body := "Grüße aus Köln.\n\nこんにちは\n"
	raw, m := sent(t, "Armature Kundendienst Zürich <no-reply@armature.test>",
		Mail{To: "ann@example.test", Subject: subject, Body: body})

	for i := 0; i < len(raw); i++ {
		if raw[i] >= 0x80 {
			t.Fatalf("byte %d of the mail is not ASCII; headers and body must be encoded:\n%s", i, raw)
		}
	}
	decoder := new(mime.WordDecoder)
	if got, err := decoder.DecodeHeader(m.Header.Get("Subject")); err != nil || got != subject {
		t.Errorf("subject decodes to %q (%v), want %q; raw %q", got, err, subject, m.Header.Get("Subject"))
	}
	from, err := netmail.ParseAddress(m.Header.Get("From"))
	if err != nil || from.Name != "Armature Kundendienst Zürich" || from.Address != "no-reply@armature.test" {
		t.Errorf("From = %+v (%v), raw %q", from, err, m.Header.Get("From"))
	}
	if cte := m.Header.Get("Content-Transfer-Encoding"); cte != "quoted-printable" {
		t.Errorf("Content-Transfer-Encoding = %q, want quoted-printable", cte)
	}
	if ct := m.Header.Get("Content-Type"); !strings.Contains(strings.ToLower(ct), "charset=utf-8") {
		t.Errorf("Content-Type = %q, want UTF-8 declared", ct)
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(m.Body))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(string(decoded), "\r\n", "\n"); got != body {
		t.Errorf("body decodes to %q, want %q", got, body)
	}
}

// A long summary in Japanese is several encoded words; a relay refuses a line
// past 998 bytes, so they are folded onto lines of their own.
func TestALongEncodedSubjectIsFolded(t *testing.T) {
	subject := "[HELP-7] " + strings.Repeat("注文の確認", 60)
	raw, m := sent(t, "Armature <no-reply@armature.test>", Mail{To: "ann@example.test", Subject: subject, Body: "x"})
	head, _, _ := strings.Cut(raw, "\r\n\r\n")
	for _, line := range strings.Split(head, "\r\n") {
		if len(line) > maxLine {
			t.Errorf("a header line is %d bytes long: %q", len(line), line)
		}
	}
	if got, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject")); err != nil || got != subject {
		t.Errorf("subject decodes to %q (%v), want %q", got, err, subject)
	}
}

// maxLine is the longest line RFC 5322 lets a relay accept, without its CRLF.
const maxLine = 998

// An encoded subject is still somebody's words; a newline in it must not
// come out the other side as a header of their choosing.
func TestAnEncodedSubjectStaysOnOneLine(t *testing.T) {
	_, m := sent(t, "Armature <no-reply@armature.test>",
		Mail{To: "ann@example.test", Subject: "Müller\r\nBcc: everyone@elsewhere.test", Body: "x"})
	if bcc := m.Header.Get("Bcc"); bcc != "" {
		t.Fatalf("the subject started a header: Bcc %q", bcc)
	}
}

// Mails about one thing name the same thread, so a reader's client files
// them together; a mail about nothing in particular names none.
func TestThreadHeadersAreWritten(t *testing.T) {
	_, m := sent(t, "Armature <no-reply@armature.test>", Mail{
		To: "ann@example.test", Subject: "[HELP-7] replied", Body: "x",
		MessageID:  "<HELP-7.b@armature.test>",
		InReplyTo:  "<HELP-7.root@armature.test>",
		References: []string{"<HELP-7.root@armature.test>"},
	})
	if got := m.Header.Get("Message-ID"); got != "<HELP-7.b@armature.test>" {
		t.Errorf("Message-ID = %q", got)
	}
	if got := m.Header.Get("In-Reply-To"); got != "<HELP-7.root@armature.test>" {
		t.Errorf("In-Reply-To = %q", got)
	}
	if got := m.Header.Get("References"); got != "<HELP-7.root@armature.test>" {
		t.Errorf("References = %q", got)
	}

	_, plain := sent(t, "Armature <no-reply@armature.test>", Mail{To: "ann@example.test", Subject: "Code", Body: "123456"})
	for _, name := range []string{"In-Reply-To", "References"} {
		if got := plain.Header.Get(name); got != "" {
			t.Errorf("a mail about nothing in particular carries %s %q", name, got)
		}
	}
}

// nothingTaken fails the test if the relay was handed a mail.
func nothingTaken(t *testing.T, relay *mailtest.Relay) {
	t.Helper()
	select {
	case body := <-relay.Mails:
		t.Fatalf("the relay should not have been handed the mail, got %q", body)
	default:
	}
}

// A relay that wants a login gets it over whichever TLS it speaks, and takes
// the mail only once it has.
func TestALoginGoesOverTLS(t *testing.T) {
	for _, c := range []struct {
		name     string
		implicit bool
		tls      TLS
	}{
		{"after STARTTLS", false, StartTLS},
		{"by default, after STARTTLS", false, ""},
		{"from the first byte", true, ImplicitTLS},
	} {
		t.Run(c.name, func(t *testing.T) {
			relay := mailtest.Start(t, mailtest.Options{Implicit: c.implicit, User: "armature", Password: "secret"})
			m := &SMTPMailer{Addr: relay.Addr, From: "Armature <no-reply@armature.test>", User: "armature", Password: "secret", TLS: c.tls, InsecureTLS: true}
			if err := m.Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"}); err != nil {
				t.Fatalf("send: %v", err)
			}
			if body := <-relay.Mails; !strings.Contains(body, "123456") {
				t.Errorf("the relay got %q, want the code in it", body)
			}
		})
	}
}

// Implicit TLS checks the certificate as STARTTLS does.
func TestImplicitTLSRefusesAnExpiredCertificate(t *testing.T) {
	relay := mailtest.Start(t, mailtest.Options{Implicit: true})
	m := &SMTPMailer{Addr: relay.Addr, From: "Armature <no-reply@armature.test>", TLS: ImplicitTLS}
	err := m.Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("an expired certificate should be refused, got %v", err)
	}
	nothingTaken(t, relay)
}

// A relay that offers no upgrade would read the password in the clear, so it
// is never sent one; without a login the mail goes as it did before.
func TestALoginIsNotSentWhereSTARTTLSIsNotOffered(t *testing.T) {
	relay := mailtest.Start(t, mailtest.Options{NoStartTLS: true, User: "armature", Password: "secret"})
	m := &SMTPMailer{Addr: relay.Addr, From: "Armature <no-reply@armature.test>", User: "armature", Password: "secret"}
	err := m.Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"})
	want := "The mail server at " + relay.Addr + " offers no TLS, so the login was not sent"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want %q", err, want)
	}
	nothingTaken(t, relay)

	open := mailtest.Start(t, mailtest.Options{NoStartTLS: true})
	if err := (&SMTPMailer{Addr: open.Addr, From: "Armature <no-reply@armature.test>"}).Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"}); err != nil {
		t.Fatalf("without a login a relay with no TLS takes the mail: %v", err)
	}
	<-open.Mails
}

// None never upgrades, so a certificate nobody can check stops no mail.
func TestNoTLSSendsPlain(t *testing.T) {
	relay := mailtest.Start(t, mailtest.Options{})
	m := &SMTPMailer{Addr: relay.Addr, From: "Armature <no-reply@armature.test>", TLS: NoTLS}
	if err := m.Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if body := <-relay.Mails; !strings.Contains(body, "123456") {
		t.Errorf("the relay got %q, want the code in it", body)
	}
}

// A refused login fails every mail until it is fixed, but the operator hears
// of it once, and again only if it is refused after having worked.
func TestARefusedLoginIsSaidOnceUntilOneGoesThrough(t *testing.T) {
	relay := mailtest.Start(t, mailtest.Options{User: "armature", Password: "secret"})
	var logged bytes.Buffer
	m := &SMTPMailer{
		Addr: relay.Addr, From: "Armature <no-reply@armature.test>", User: "armature", Password: "wrong", InsecureTLS: true,
		Log: slog.New(slog.NewTextHandler(&logged, nil)),
	}
	sentence := "The mail server at " + relay.Addr + " refused the login for armature; check ARMATURE_SMTP_USER and ARMATURE_SMTP_PASSWORD."
	send := func() error {
		return m.Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"})
	}
	refusals := func() int { return strings.Count(logged.String(), "level=ERROR") }

	for i := 0; i < 2; i++ {
		if err := send(); err == nil || err.Error() != sentence {
			t.Fatalf("send %d: err = %v, want %q", i+1, err, sentence)
		}
	}
	nothingTaken(t, relay)
	if n := refusals(); n != 1 || !strings.Contains(logged.String(), "check ARMATURE_SMTP_USER and ARMATURE_SMTP_PASSWORD.") {
		t.Fatalf("logged %d errors, want the sentence once:\n%s", n, logged.String())
	}

	m.Password = "secret"
	if err := send(); err != nil {
		t.Fatalf("with the right password: %v", err)
	}
	<-relay.Mails
	m.Password = "wrong"
	if err := send(); err == nil {
		t.Fatal("the wrong password should be refused again")
	}
	if n := refusals(); n != 2 {
		t.Errorf("logged %d errors, want a second once the login broke again:\n%s", n, logged.String())
	}
}
