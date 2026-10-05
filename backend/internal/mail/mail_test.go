package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
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
