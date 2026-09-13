package mailin

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Message is what the desk needs to know about a mail: who sent it, who it was
// to, what it answers, and the words, without the quoted history.
type Message struct {
	// MessageID is without its angle brackets; empty when the mail has none.
	MessageID  string
	InReplyTo  string
	References []string
	// From is the bare address, lowercased.
	From string
	// Recipients are To, Cc and Delivered-To, bare and lowercased.
	Recipients []string
	Subject    string
	// Text is the best body: the plain part, else the HTML with its tags
	// stripped. Quoted history is still in it; StripQuotes takes it out.
	Text string
	// AutoSubmitted says a machine sent it, which nothing should answer.
	AutoSubmitted bool
	// AuthResults is what the receiving server made of the sender: the
	// Authentication-Results header, lowercased, empty when it checked nothing.
	AuthResults string
}

// SenderFailedChecks says the receiving server checked who sent this and found
// the claim false. No header at all is not a failure; nobody checked.
func (m *Message) SenderFailedChecks() bool {
	return strings.Contains(m.AuthResults, "dmarc=fail") ||
		(strings.Contains(m.AuthResults, "spf=fail") && strings.Contains(m.AuthResults, "dkim=fail"))
}

// Parse reads a raw mail. It is forgiving about everything but the shape: a
// mail with no headers at all is refused.
func Parse(raw []byte) (*Message, error) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("read the mail: %w", err)
	}
	decoder := &mime.WordDecoder{CharsetReader: charsetReader}
	subject, err := decoder.DecodeHeader(m.Header.Get("Subject"))
	if err != nil {
		subject = m.Header.Get("Subject")
	}
	out := &Message{
		MessageID:  bareID(m.Header.Get("Message-ID")),
		InReplyTo:  bareID(m.Header.Get("In-Reply-To")),
		Subject:    strings.TrimSpace(subject),
		References: ids(m.Header.Get("References")),
	}
	if from, err := mail.ParseAddress(m.Header.Get("From")); err == nil {
		out.From = strings.ToLower(from.Address)
	}
	out.AuthResults = strings.ToLower(strings.Join(m.Header["Authentication-Results"], " "))
	for _, name := range []string{"To", "Cc", "Delivered-To", "X-Original-To"} {
		for _, value := range m.Header[name] {
			if list, err := mail.ParseAddressList(value); err == nil {
				for _, a := range list {
					out.Recipients = append(out.Recipients, strings.ToLower(a.Address))
				}
			}
		}
	}
	auto := strings.ToLower(strings.TrimSpace(m.Header.Get("Auto-Submitted")))
	precedence := strings.ToLower(strings.TrimSpace(m.Header.Get("Precedence")))
	out.AutoSubmitted = (auto != "" && auto != "no") || precedence == "bulk" || precedence == "list" || precedence == "junk"

	body, err := io.ReadAll(m.Body)
	if err != nil {
		return nil, err
	}
	plain, rich := bodyText(textproto(m.Header), body)
	if plain != "" {
		out.Text = plain
	} else {
		out.Text = stripTags(rich)
	}
	return out, nil
}

// headerGetter is what both a message and a part offer.
type headerGetter interface{ Get(string) string }

func textproto(h mail.Header) headerGetter { return h }

// bodyText walks the parts and returns the first plain text and the first HTML
// it finds, so the caller can prefer the plain one.
func bodyText(h headerGetter, body []byte) (plain, rich string) {
	mediaType, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil {
		mediaType, params = "text/plain", map[string]string{}
	}
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return "", ""
		}
		reader := multipart.NewReader(bytes.NewReader(body), boundary)
		for {
			part, err := reader.NextPart()
			if err != nil {
				return plain, rich
			}
			data, err := io.ReadAll(part)
			if err != nil {
				return plain, rich
			}
			p, r := bodyText(part.Header, data)
			if plain == "" {
				plain = p
			}
			if rich == "" {
				rich = r
			}
			if plain != "" {
				return plain, rich
			}
		}
	case mediaType == "text/plain":
		return decodeText(h, body, params["charset"]), ""
	case mediaType == "text/html":
		return "", decodeText(h, body, params["charset"])
	}
	return "", ""
}

// decodeText undoes the transfer encoding and the charset.
func decodeText(h headerGetter, body []byte, charset string) string {
	var r io.Reader = bytes.NewReader(body)
	switch strings.ToLower(strings.TrimSpace(h.Get("Content-Transfer-Encoding"))) {
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, bytes.NewReader(bytes.Map(func(c rune) rune {
			if c == '\r' || c == '\n' || c == ' ' {
				return -1
			}
			return c
		}, body)))
	}
	decoded, err := io.ReadAll(r)
	if err != nil {
		decoded = body
	}
	text, err := charsetReader(charset, bytes.NewReader(decoded))
	if err != nil {
		return string(decoded)
	}
	out, _ := io.ReadAll(text)
	return string(out)
}

// charsetReader handles what a desk's mail is written in without pulling in a
// table of every encoding: UTF-8 as it is, the Latin-1 family byte by byte,
// and anything else taken as it came, which is a known limit.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		if !utf8.Valid(data) {
			return bytes.NewReader(latin1(data)), nil
		}
		return bytes.NewReader(data), nil
	case "iso-8859-1", "latin1", "iso-8859-15", "windows-1252", "cp1252":
		return bytes.NewReader(latin1(data)), nil
	}
	return bytes.NewReader(data), nil
}

// windows1252 is the part of that code page that Latin-1 leaves undefined and
// mail written on Windows uses: the curly quotes and the dashes.
var windows1252 = map[byte]rune{
	0x80: '\u20ac', 0x82: '\u201a', 0x84: '\u201e', 0x85: '\u2026', 0x91: '\u2018', 0x92: '\u2019',
	0x93: '\u201c', 0x94: '\u201d', 0x95: '\u2022', 0x96: '\u2013', 0x97: '\u2014', 0x99: '\u2122',
}

func latin1(data []byte) []byte {
	var b strings.Builder
	b.Grow(len(data) * 2)
	for _, c := range data {
		if r, ok := windows1252[c]; ok {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(rune(c))
	}
	return []byte(b.String())
}

var (
	blockTags  = regexp.MustCompile(`(?is)<\s*(br|/p|/div|/li|/tr|/h[1-6]|/blockquote)\b[^>]*>`)
	dropBlocks = regexp.MustCompile(`(?is)<\s*(style|script|head)\b.*?</\s*(style|script|head)\s*>`)
	anyTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	blankRuns  = regexp.MustCompile(`\n{3,}`)
)

// stripTags turns HTML into lines of text: block ends become line breaks,
// everything else in angle brackets goes, and entities are spelled out.
func stripTags(rich string) string {
	if rich == "" {
		return ""
	}
	text := dropBlocks.ReplaceAllString(rich, "")
	text = blockTags.ReplaceAllString(text, "\n")
	text = anyTag.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

func bareID(raw string) string {
	raw = strings.TrimSpace(raw)
	return strings.TrimSuffix(strings.TrimPrefix(raw, "<"), ">")
}

func ids(raw string) []string {
	var out []string
	for _, field := range strings.Fields(raw) {
		if id := bareID(field); id != "" {
			out = append(out, id)
		}
	}
	return out
}

var (
	keyInSubject = regexp.MustCompile(`\[([A-Z][A-Z0-9]*-[0-9]+)\]`)
	keyInID      = regexp.MustCompile(`^([A-Z][A-Z0-9]*-[0-9]+)\.`)
)

// IssueKeyIn says which request a mail is about: the tag in the subject first,
// else the message it answers, when that was one the desk sent, whose ids
// begin with the key.
func IssueKeyIn(subject, inReplyTo string, references []string) string {
	if m := keyInSubject.FindStringSubmatch(subject); m != nil {
		return m[1]
	}
	candidates := append([]string{inReplyTo}, references...)
	for i := len(candidates) - 1; i >= 0; i-- {
		if m := keyInID.FindStringSubmatch(candidates[i]); m != nil {
			return m[1]
		}
	}
	return ""
}

// ErrNoText is returned when nothing is left of a mail once the quotes go.
var ErrNoText = errors.New("the mail says nothing new")
