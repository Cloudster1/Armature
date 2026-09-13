// Package mailin reads the desk's mailbox: a POP3 client small enough to be
// written here, and a parser for what it fetches. It knows nothing about
// issues; the desk decides what a mail means.
package mailin

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Inbox is where replies wait. POP3 is the real one; tests hand in a slice.
type Inbox interface {
	// Poll hands over every message whose uid skip does not know, deletes
	// the ones handle consumed, and ends the session, which is what makes
	// the deletions real.
	Poll(ctx context.Context, skip func(uid string) bool, handle func(uid string, raw []byte) (consumed bool)) error
}

// POP3 is a mailbox reached by the six commands every provider serves. TLS is
// implicit on the connection when set; STARTTLS is not needed for Mailpit or
// for the providers a desk would use.
type POP3 struct {
	Addr     string
	User     string
	Password string
	TLS      bool
	Timeout  time.Duration
}

const defaultTimeout = 30 * time.Second

func (p POP3) Poll(ctx context.Context, skip func(string) bool, handle func(string, []byte) bool) error {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	c, err := dial(ctx, p.Addr, p.TLS, timeout)
	if err != nil {
		return err
	}
	defer c.close()
	if _, err := c.cmd("USER " + p.User); err != nil {
		return err
	}
	if _, err := c.cmd("PASS " + p.Password); err != nil {
		return err
	}
	if _, err := c.cmd("UIDL"); err != nil {
		return err
	}
	listing, err := c.multiline()
	if err != nil {
		return err
	}
	type entry struct{ number, uid string }
	var entries []entry
	for _, line := range strings.Split(strings.TrimSpace(string(listing)), "\r\n") {
		number, uid, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && number != "" && uid != "" {
			entries = append(entries, entry{number, uid})
		}
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
		if skip(e.uid) {
			continue
		}
		if _, err := c.cmd("RETR " + e.number); err != nil {
			return err
		}
		raw, err := c.multiline()
		if err != nil {
			return err
		}
		if handle(e.uid, raw) {
			if _, err := c.cmd("DELE " + e.number); err != nil {
				return err
			}
		}
	}
	// QUIT is what commits the deletions; a dropped connection leaves the
	// box as it was, which is the right way round.
	_, err = c.cmd("QUIT")
	return err
}

// conn is one POP3 session.
type conn struct {
	c       net.Conn
	r       *bufio.Reader
	timeout time.Duration
}

func dial(ctx context.Context, addr string, useTLS bool, timeout time.Duration) (*conn, error) {
	d := net.Dialer{Timeout: timeout}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("reach the mailbox at %s: %w", addr, err)
	}
	if useTLS {
		host, _, _ := net.SplitHostPort(addr)
		t := tls.Client(raw, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := t.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, fmt.Errorf("secure the connection to %s: %w", addr, err)
		}
		raw = t
	}
	c := &conn{c: raw, r: bufio.NewReader(raw), timeout: timeout}
	if _, err := c.response(); err != nil {
		c.close()
		return nil, fmt.Errorf("the mailbox at %s did not greet: %w", addr, err)
	}
	return c, nil
}

func (c *conn) close() { _ = c.c.Close() }

// cmd sends one line and reads the status line, which is +OK or -ERR.
func (c *conn) cmd(line string) (string, error) {
	_ = c.c.SetDeadline(time.Now().Add(c.timeout))
	if _, err := c.c.Write([]byte(line + "\r\n")); err != nil {
		return "", err
	}
	status, err := c.response()
	if err != nil {
		verb, _, _ := strings.Cut(line, " ")
		return "", fmt.Errorf("%s: %w", verb, err)
	}
	return status, nil
}

func (c *conn) response() (string, error) {
	_ = c.c.SetDeadline(time.Now().Add(c.timeout))
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.HasPrefix(line, "+OK") {
		return line, nil
	}
	if strings.HasPrefix(line, "-ERR") {
		return "", errors.New(strings.TrimSpace(strings.TrimPrefix(line, "-ERR")))
	}
	return "", fmt.Errorf("unexpected answer %q", line)
}

// MaxMessageBytes bounds one mail: the box is somebody else's to fill.
const MaxMessageBytes = 10 << 20

// multiline reads to the lone dot and undoes the byte stuffing that protects
// lines beginning with one.
func (c *conn) multiline() ([]byte, error) {
	var out []byte
	for {
		if len(out) > MaxMessageBytes {
			return nil, fmt.Errorf("a mail larger than %d bytes was left in the box", MaxMessageBytes)
		}
		_ = c.c.SetDeadline(time.Now().Add(c.timeout))
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == ".\r\n" || line == ".\n" {
			return out, nil
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		out = append(out, line...)
	}
}
