package git

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

const (
	oidA = "1111111111111111111111111111111111111111"
	oidB = "2222222222222222222222222222222222222222"
	zero = "0000000000000000000000000000000000000000"
)

func pkts(lines ...string) []byte {
	var out bytes.Buffer
	for _, l := range lines {
		if l == flushPkt {
			out.WriteString(flushPkt)
			continue
		}
		out.Write(pktLine(l))
	}
	return out.Bytes()
}

// A push is its commands, what the client asked for, and the pack after them,
// which is left as it came for the host.
func TestAPushIsReadAsItsCommands(t *testing.T) {
	body := append(pkts(
		"shallow "+oidA+"\n",
		oidA+" "+oidB+" refs/heads/CP-1-fix\x00report-status side-band-64k push-options agent=git/2.43\n",
		zero+" "+oidB+" refs/heads/CP-1-more\n",
		flushPkt,
		"ci.skip\n",
		flushPkt,
	), []byte("PACK...")...)
	push, err := ParsePush(body)
	if err != nil {
		t.Fatal(err)
	}
	want := []RefUpdate{{Old: oidA, New: oidB, Ref: "refs/heads/CP-1-fix"}, {Old: zero, New: oidB, Ref: "refs/heads/CP-1-more"}}
	if !slices.Equal(push.Updates, want) {
		t.Errorf("updates = %v, want %v", push.Updates, want)
	}
	if !push.Asked("report-status") || !push.Asked("side-band-64k") || push.Asked("atomic") {
		t.Errorf("capabilities = %v", push.Capabilities)
	}
	if string(push.Pack) != "PACK..." {
		t.Errorf("pack = %q, want what followed the push options", push.Pack)
	}
	if push.Updates[0].Creates() || !push.Updates[1].Creates() || push.Updates[0].Deletes() {
		t.Error("a creation is told from an update by its zero old id")
	}
}

// What git would never send is refused rather than guessed at.
func TestAPushThatIsNotGitsIsRefused(t *testing.T) {
	for name, body := range map[string][]byte{
		"nothing":            nil,
		"no command":         []byte(flushPkt),
		"a short length":     []byte("00"),
		"a length too long":  []byte("00ff" + oidA),
		"not hex":            []byte("zzzz"),
		"a broken command":   pkts("not a command\n", flushPkt),
		"an id that is none": pkts("xyz "+oidB+" refs/heads/a\n", flushPkt),
		"no flush":           pkts(oidA + " " + oidB + " refs/heads/a\n"),
	} {
		if _, err := ParsePush(body); !errors.Is(err, ErrBadPush) {
			t.Errorf("%s: err = %v, want ErrBadPush", name, err)
		}
	}
}

// A signed push carries its commands inside a certificate, which the proxy
// does not open; it says so, so the handler can refuse it whole.
func TestASignedPushIsNoticed(t *testing.T) {
	push, err := ParsePush(pkts("push-cert\x00report-status\n", "certificate version 0.1\n", flushPkt))
	if err != nil || !push.Certificate {
		t.Errorf("push = %+v, %v, want a certificate", push, err)
	}
}

// unband reads a side-band answer back into what band one and band two said.
func unband(t *testing.T, b []byte) (data, progress string) {
	t.Helper()
	for len(b) > 0 {
		payload, rest, flush, err := readPkt(b)
		if err != nil {
			t.Fatal(err)
		}
		b = rest
		if flush {
			continue
		}
		switch payload[0] {
		case bandData:
			data += string(payload[1:])
		case bandProgress:
			progress += string(payload[1:])
		}
	}
	return data, progress
}

// Every ref is refused with its reason, in the report git asked for, and the
// summary goes where git prints "remote:".
func TestARefusalIsReportedAsGitAskedForIt(t *testing.T) {
	updates := []RefUpdate{{Old: oidA, New: oidB, Ref: "refs/heads/main"}, {Old: oidA, New: oidB, Ref: "refs/heads/CP-1-fix"}}
	reasons := map[string]string{"refs/heads/main": "main is the default branch.", "refs/heads/CP-1-fix": "Another ref\nwas refused."}
	status := "unpack ok\n" + "ng refs/heads/main main is the default branch.\n" + "ng refs/heads/CP-1-fix Another ref was refused.\n"

	plain := RefusalReport(&PushRequest{Updates: updates, Capabilities: []string{"report-status"}}, reasons, "Refused.")
	if want := string(pkts("unpack ok\n", "ng refs/heads/main main is the default branch.\n", "ng refs/heads/CP-1-fix Another ref was refused.\n", flushPkt)); string(plain) != want {
		t.Errorf("without a side band = %q, want %q", plain, want)
	}

	banded := RefusalReport(&PushRequest{Updates: updates, Capabilities: []string{"report-status-v2", "side-band-64k"}}, reasons, "Armature refused this push.")
	data, progress := unband(t, banded)
	if data != string(pkts(strings.Split(status, "\n")[0]+"\n", "ng refs/heads/main main is the default branch.\n", "ng refs/heads/CP-1-fix Another ref was refused.\n", flushPkt)) {
		t.Errorf("band one = %q", data)
	}
	if progress != "Armature refused this push.\n" {
		t.Errorf("band two = %q", progress)
	}
	if !bytes.HasSuffix(banded, []byte(flushPkt)) {
		t.Error("a side band answer ends with a flush")
	}

	if RefusalReport(&PushRequest{Updates: updates}, reasons, "") != nil {
		t.Error("a client that asked for no report was sent one")
	}
}

// A long report is cut into packets the side band can carry.
func TestALongReportFitsTheSmallSideBand(t *testing.T) {
	var updates []RefUpdate
	reasons := map[string]string{}
	for i := range 40 {
		ref := "refs/heads/" + strings.Repeat("b", i+1)
		updates = append(updates, RefUpdate{Old: oidA, New: oidB, Ref: ref})
		reasons[ref] = strings.Repeat("Refused. ", 5)
	}
	banded := RefusalReport(&PushRequest{Updates: updates, Capabilities: []string{"report-status", "side-band"}}, reasons, "")
	for b := banded; len(b) > 0; {
		payload, rest, _, err := readPkt(b)
		if err != nil {
			t.Fatal(err)
		}
		if len(payload)+pktHeaderLen > smallBandPkt {
			t.Fatalf("a packet of %d bytes on the small side band", len(payload)+pktHeaderLen)
		}
		b = rest
	}
	if data, _ := unband(t, banded); strings.Count(data, "ng refs/heads/") != len(updates) {
		t.Errorf("the report lost refs: %q", data)
	}
}

func TestAnErrPacketIsOneLine(t *testing.T) {
	if got := string(ErrPkt("No signed\npushes.")); got != "001aERR No signed pushes.\n" {
		t.Errorf("ErrPkt = %q", got)
	}
}
