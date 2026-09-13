// Package mail is the one way a message leaves the product by mail. The desk,
// the inbox and the digest all address their words through it.
package mail

import (
	"context"
	"github.com/armature/armature/backend/internal/observability"
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
	err := smtp.SendMail(m.Addr, nil, sender, []string{msg.To}, []byte(text))
	observability.Current().Mailed(err)
	return err
}
