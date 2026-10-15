// Package mail is the one way a message leaves the product by mail. The desk,
// the inbox and the digest all address their words through it.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync/atomic"
	"time"

	"github.com/armature/armature/backend/internal/observability"
)

// Mail is one message to one address. The ids and ReplyTo are set by the
// sender so that an answer can find its way back; most mails have none.
type Mail struct {
	To        string
	Subject   string
	Body      string
	MessageID string
	ReplyTo   string
	// InReplyTo and References name the thread a mail belongs to, so a
	// reader's client files mails about one thing together.
	InReplyTo  string
	References []string
}

// headerValue keeps one header on one line. A name or a subject is somebody
// else's words, and a newline in them would start a header of their choosing.
func headerValue(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r < 0x20 {
			return ' '
		}
		return r
	}, s)
}

// encodedText is a header of free text that any relay takes: RFC 2047 words
// when it is not ASCII, one to a line, so a long Japanese subject stays short.
func encodedText(s string) string {
	plain := headerValue(s)
	encoded := mime.QEncoding.Encode("utf-8", plain)
	if encoded == plain {
		return plain
	}
	// An encoded value is nothing but encoded words, so this space is always
	// one the encoder put between two of them.
	return strings.ReplaceAll(encoded, "?= =?", "?=\r\n =?")
}

// address writes a mailbox with its display name encoded when it needs to be;
// what does not parse goes out as it was, on one line.
func address(s string) string {
	parsed, err := mail.ParseAddress(s)
	if err != nil {
		return headerValue(s)
	}
	if parsed.Name == "" {
		return parsed.Address
	}
	return parsed.String()
}

// quotedPrintable keeps the body seven bit, which every relay passes.
func quotedPrintable(s string) []byte {
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

// Mailer sends one message. The SMTP mailer is the real one; tests keep what
// would have been sent.
type Mailer interface {
	Send(ctx context.Context, m Mail) error
}

// TLS is how the connection to the relay is protected.
type TLS string

const (
	// StartTLS upgrades the connection whenever the relay offers to, and
	// sends a login only once it has.
	StartTLS TLS = "starttls"
	// ImplicitTLS speaks TLS from the first byte, as a relay on port 465 does.
	ImplicitTLS TLS = "implicit"
	// NoTLS never upgrades, for a relay beside the product that offers a
	// certificate nobody can check.
	NoTLS TLS = "none"
)

// SMTPMailer sends through an SMTP relay, which in development is Mailpit and
// in production whatever the operator points it at. It is shared by pointer,
// so a login the relay refuses is reported once, not once per mail.
type SMTPMailer struct {
	Addr string
	From string
	// User and Password are the login sent as AUTH PLAIN, over TLS only;
	// without a user nothing is sent.
	User     string
	Password string
	// TLS is StartTLS when empty.
	TLS TLS
	// InsecureTLS accepts any certificate the relay offers: an expired or
	// self-signed one stops no mail, and protects none either.
	InsecureTLS bool
	// Log is told of a refused login; nil is slog's default.
	Log *slog.Logger

	refused atomic.Bool
}

func (m *SMTPMailer) Send(ctx context.Context, msg Mail) error {
	// The envelope sender is the bare address; the display name belongs in
	// the header only, and a relay refuses it anywhere else.
	sender := m.From
	if parsed, err := mail.ParseAddress(m.From); err == nil {
		sender = parsed.Address
	}
	headers := []string{
		"From: " + address(m.From),
		"To: " + address(msg.To),
		"Subject: " + encodedText(msg.Subject),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
		// Says this is a machine's mail, so auto-responders leave it alone.
		"Auto-Submitted: auto-generated",
	}
	if msg.MessageID != "" {
		headers = append(headers, "Message-ID: "+headerValue(msg.MessageID))
	}
	if msg.InReplyTo != "" {
		headers = append(headers, "In-Reply-To: "+headerValue(msg.InReplyTo))
	}
	if len(msg.References) > 0 {
		refs := make([]string, len(msg.References))
		for i, r := range msg.References {
			refs[i] = headerValue(r)
		}
		headers = append(headers, "References: "+strings.Join(refs, "\r\n "))
	}
	if msg.ReplyTo != "" {
		headers = append(headers, "Reply-To: "+address(msg.ReplyTo))
	}
	text := append([]byte(strings.Join(append(headers, "", ""), "\r\n")), quotedPrintable(msg.Body)...)
	err := m.deliver(sender, msg.To, text)
	observability.Current().Mailed(err)
	return err
}

// deliver is smtp.SendMail spelled out, because SendMail cannot be told which
// certificates to accept or that TLS comes before the first byte.
func (m *SMTPMailer) deliver(sender, to string, text []byte) error {
	c, err := m.dial()
	if err != nil {
		return err
	}
	defer c.Close()
	if m.User != "" {
		if err := m.login(c); err != nil {
			return err
		}
	}
	if err := c.Mail(sender); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(text); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// dial connects and secures the connection as TLS says. A login is never sent
// in the clear, so a relay that offers no upgrade is not handed one.
func (m *SMTPMailer) dial() (*smtp.Client, error) {
	host, _, _ := net.SplitHostPort(m.Addr)
	cfg := &tls.Config{ServerName: host, InsecureSkipVerify: m.InsecureTLS, MinVersion: tls.VersionTLS12}
	if m.TLS == ImplicitTLS {
		conn, err := tls.Dial("tcp", m.Addr, cfg)
		if err != nil {
			return nil, err
		}
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
		}
		return c, err
	}
	c, err := smtp.Dial(m.Addr)
	if err != nil {
		return nil, err
	}
	if m.TLS == NoTLS {
		return c, nil
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(cfg); err != nil {
			c.Close()
			return nil, err
		}
	} else if m.User != "" {
		c.Close()
		return nil, fmt.Errorf("The mail server at %s offers no TLS, so the login was not sent; set ARMATURE_SMTP_TLS to implicit if it speaks TLS from the start.", m.Addr)
	}
	return c, nil
}

// login sends AUTH PLAIN. A refused one is logged when it starts being
// refused and again only after a login has gone through in between.
func (m *SMTPMailer) login(c *smtp.Client) error {
	host, _, _ := net.SplitHostPort(m.Addr)
	err := c.Auth(smtp.PlainAuth("", m.User, m.Password, host))
	var answer *textproto.Error
	if errors.As(err, &answer) && answer.Code == codeLoginRefused {
		refused := fmt.Errorf("The mail server at %s refused the login for %s; check ARMATURE_SMTP_USER and ARMATURE_SMTP_PASSWORD.", m.Addr, m.User)
		if m.refused.CompareAndSwap(false, true) {
			m.logger().Error(refused.Error(), "answer", answer.Error())
		}
		return refused
	}
	if err == nil {
		m.refused.Store(false)
	}
	return err
}

// codeLoginRefused is the relay's answer to a user or password it does not take.
const codeLoginRefused = 535

func (m *SMTPMailer) logger() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}
