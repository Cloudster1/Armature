// Package mailtest is a mail relay inside the test's own process, so a test
// can prove how the mailer connects, logs in and what it hands over.
package mailtest

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"net"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/testcert"
)

// Options is the kind of relay to stand up. The zero value offers STARTTLS
// and takes mail from anybody, as the dev Mailpit does.
type Options struct {
	// Implicit speaks TLS from the first byte, as a relay on port 465 does.
	Implicit bool
	// NoStartTLS leaves STARTTLS out of what the relay offers.
	NoStartTLS bool
	// User and Password, when set, are the one login AUTH PLAIN accepts, and
	// mail is refused until it has been given.
	User, Password string
}

// Relay presents an expired certificate, as the relay did that stopped the
// portal's codes, so a mailer reaches it only with InsecureTLS.
type Relay struct {
	Addr string
	// Mails is the body of each mail the relay took, in the order taken.
	Mails <-chan string
}

// mailsKept is how many mails a test may send before it has to read them.
const mailsKept = 16

// Start listens on a port of its own until the test ends, taking any number
// of connections.
func Start(t *testing.T, o Options) *Relay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	cfg := &tls.Config{Certificates: []tls.Certificate{testcert.Expired(t)}}
	mails := make(chan string, mailsKept)
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(raw, cfg, o, mails)
		}
	}()
	return &Relay{Addr: ln.Addr().String(), Mails: mails}
}

func serve(raw net.Conn, cfg *tls.Config, o Options, mails chan<- string) {
	var c net.Conn = raw
	if o.Implicit {
		c = tls.Server(raw, cfg)
	}
	defer func() { c.Close() }()
	r := bufio.NewReader(c)
	w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
	loggedIn := o.User == ""
	login := func(encoded string) {
		given, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil && string(given) == "\x00"+o.User+"\x00"+o.Password {
			loggedIn = true
			w("235 2.7.0 authenticated")
			return
		}
		w("535 5.7.8 authentication credentials invalid")
	}
	w("220 fake ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		verb, rest, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		switch strings.ToUpper(verb) {
		case "EHLO":
			var offers []string
			if _, secure := c.(*tls.Conn); !secure && !o.NoStartTLS {
				offers = append(offers, "STARTTLS")
			}
			if o.User != "" {
				offers = append(offers, "AUTH PLAIN")
			}
			w("250-fake")
			for _, offer := range offers {
				w("250-" + offer)
			}
			w("250 8BITMIME")
		case "STARTTLS":
			w("220 go ahead")
			c = tls.Server(raw, cfg)
			r = bufio.NewReader(c)
		case "AUTH":
			mechanism, initial, _ := strings.Cut(rest, " ")
			if !strings.EqualFold(mechanism, "PLAIN") {
				w("504 5.5.4 only PLAIN")
				continue
			}
			if initial == "" {
				w("334 ")
				answer, err := r.ReadString('\n')
				if err != nil {
					return
				}
				initial = strings.TrimRight(answer, "\r\n")
			}
			login(initial)
		case "MAIL":
			if !loggedIn {
				w("530 5.7.0 authentication required")
				continue
			}
			w("250 ok")
		case "RCPT":
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
			mails <- body.String()
			w("250 queued")
		case "QUIT":
			w("221 bye")
			return
		default:
			w("502 what")
		}
	}
}
