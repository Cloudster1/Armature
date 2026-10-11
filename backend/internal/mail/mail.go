// Package mail is the one way a message leaves the product by mail. The desk,
// the inbox and the digest all address their words through it.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
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
