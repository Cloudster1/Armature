package mailin

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakePOP3 speaks enough of the protocol to be polled, and remembers what it
// was told.
type fakePOP3 struct {
	messages map[string]string // number -> raw
	uids     []string          // "number uid"
	badPass  bool
	mu       sync.Mutex
	commands []string
}

func (f *fakePOP3) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		w("+OK fake ready")
		r := bufio.NewReader(c)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			f.mu.Lock()
			f.commands = append(f.commands, line)
			f.mu.Unlock()
			verb, arg, _ := strings.Cut(line, " ")
			switch verb {
			case "USER":
				w("+OK")
			case "PASS":
				if f.badPass {
					w("-ERR wrong password")
				} else {
					w("+OK")
				}
			case "UIDL":
				w("+OK")
				for _, u := range f.uids {
					w(u)
				}
				w(".")
			case "RETR":
				w("+OK")
				for _, l := range strings.Split(f.messages[arg], "\r\n") {
					if strings.HasPrefix(l, ".") {
						l = "." + l
					}
					w(l)
				}
				w(".")
			case "DELE":
				w("+OK")
			case "QUIT":
				w("+OK bye")
				return
			default:
				w("-ERR what")
			}
		}
	}()
	return ln.Addr().String()
}

func TestPollFetchesUnknownMailAndDeletesWhatWasConsumed(t *testing.T) {
	f := &fakePOP3{
		messages: map[string]string{
			"1": "Subject: one\r\n\r\n.starts with a dot\r\nbody",
			"2": "Subject: two\r\n\r\nbody two",
			"3": "Subject: three\r\n\r\nbody three",
		},
		uids: []string{"1 uid-a", "2 uid-b", "3 uid-c"},
	}
	addr := f.serve(t)
	got := map[string]string{}
	err := POP3{Addr: addr, User: "u", Password: "p"}.Poll(context.Background(),
		func(uid string) bool { return uid == "uid-b" },
		func(uid string, raw []byte) bool {
			got[uid] = string(raw)
			return uid == "uid-a"
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.Contains(got["uid-a"], "\r\n.starts with a dot\r\n") || got["uid-c"] == "" {
		t.Errorf("fetched %q, want a and c with the dot unstuffed", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	joined := strings.Join(f.commands, "|")
	if !strings.Contains(joined, "USER u|PASS p|UIDL|RETR 1|DELE 1|RETR 3|QUIT") {
		t.Errorf("commands = %s, want only the consumed message deleted and a QUIT", joined)
	}
}

func TestPollReportsARefusedPassword(t *testing.T) {
	f := &fakePOP3{badPass: true}
	err := POP3{Addr: f.serve(t), User: "u", Password: "wrong"}.Poll(context.Background(),
		func(string) bool { return false }, func(string, []byte) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "PASS") {
		t.Errorf("err = %v, want the refused PASS", err)
	}
}
