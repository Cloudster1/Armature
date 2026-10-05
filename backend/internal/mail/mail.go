// Package mail is the one way a message leaves the product by mail. The desk,
// the inbox and the digest all address their words through it.
package mail

import (
	"context"
	"crypto/tls"
	"github.com/armature/armature/backend/internal/observability"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Mail is one message to one address. MessageID and ReplyTo are set by the
// sender so that an answer can find its way back; most mails have neither.
type Mail struct {
	To        string
	Subject   string
	Body      string
	MessageID string
	ReplyTo   string
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

// Mailer sends one message. The SMTP mailer is the real one; tests keep what
// would have been sent.
type Mailer interface {
	Send(ctx context.Context, m Mail) error
}

// SMTPMailer sends through a plain SMTP relay, which in development is
// Mailpit and in production whatever the operator points it at.
type SMTPMailer struct {
	Addr string
	From string
	// InsecureTLS accepts any certificate the relay offers on STARTTLS: an
	// expired or self-signed one stops no mail, and protects none either.
	InsecureTLS bool
}

func (m SMTPMailer) Send(ctx context.Context, msg Mail) error {
	// The envelope sender is the bare address; the display name belongs in
	// the header only, and a relay refuses it anywhere else.
	sender := m.From
	if parsed, err := mail.ParseAddress(m.From); err == nil {
		sender = parsed.Address
	}
	headers := []string{
		"From: " + headerValue(m.From),
		"To: " + headerValue(msg.To),
		"Subject: " + headerValue(msg.Subject),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		// Says this is a machine's mail, so auto-responders leave it alone.
		"Auto-Submitted: auto-generated",
	}
	if msg.MessageID != "" {
		headers = append(headers, "Message-ID: "+headerValue(msg.MessageID))
	}
	if msg.ReplyTo != "" {
		headers = append(headers, "Reply-To: "+headerValue(msg.ReplyTo))
	}
	text := strings.Join(append(headers, "", msg.Body), "\r\n")
	err := m.deliver(sender, msg.To, []byte(text))
	observability.Current().Mailed(err)
	return err
}

// deliver is smtp.SendMail without authentication, spelled out because
// SendMail has no way to be told which certificates to accept.
func (m SMTPMailer) deliver(sender, to string, text []byte) error {
	c, err := smtp.Dial(m.Addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		host, _, _ := net.SplitHostPort(m.Addr)
		if err := c.StartTLS(&tls.Config{ServerName: host, InsecureSkipVerify: m.InsecureTLS}); err != nil {
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
