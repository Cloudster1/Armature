package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/testcert"
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

// fakeRelay offers STARTTLS with an expired certificate, as the relay did that
// stopped the portal's codes, and keeps the body of what it was handed.
func fakeRelay(t *testing.T) (addr string, got <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	cfg := &tls.Config{Certificates: []tls.Certificate{testcert.Expired(t)}}
	bodies := make(chan string, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		var c net.Conn = raw
		defer func() { c.Close() }()
		r := bufio.NewReader(c)
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		w("220 fake ready")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			verb, _, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
			switch strings.ToUpper(verb) {
			case "EHLO":
				w("250-fake")
				w("250 STARTTLS")
			case "STARTTLS":
				w("220 go ahead")
				c = tls.Server(raw, cfg)
				r = bufio.NewReader(c)
			case "MAIL", "RCPT":
				w("250 ok")
			case "DATA":
				w("354 go on")
				var body strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					body.WriteString(l)
				}
				bodies <- body.String()
				w("250 queued")
			case "QUIT":
				w("221 bye")
				return
			default:
				w("502 what")
			}
		}
	}()
	return ln.Addr().String(), bodies
}

func TestARelayWithAnExpiredCertificateIsRefused(t *testing.T) {
	addr, got := fakeRelay(t)
	m := SMTPMailer{Addr: addr, From: "Armature <no-reply@armature.test>"}
	err := m.Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("an expired certificate should be refused, got %v", err)
	}
	select {
	case body := <-got:
		t.Fatalf("the relay should not have been handed the mail, got %q", body)
	default:
	}
}

func TestInsecureTLSSendsThroughAnExpiredCertificate(t *testing.T) {
	addr, got := fakeRelay(t)
	m := SMTPMailer{Addr: addr, From: "Armature <no-reply@armature.test>", InsecureTLS: true}
	if err := m.Send(context.Background(), Mail{To: "ann@example.test", Subject: "Code", Body: "123456"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if body := <-got; !strings.Contains(body, "123456") {
		t.Errorf("the relay got %q, want the code in it", body)
	}
}

// sent hands a mail to the fake relay and reads back what it was given, the
// way a receiving server would.
func sent(t *testing.T, from string, m Mail) (string, *netmail.Message) {
	t.Helper()
	addr, got := fakeRelay(t)
	if err := (SMTPMailer{Addr: addr, From: from, InsecureTLS: true}).Send(context.Background(), m); err != nil {
		t.Fatalf("send: %v", err)
	}
	raw := <-got
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
