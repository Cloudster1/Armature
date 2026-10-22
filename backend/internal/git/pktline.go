package git

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// git's pkt-line framing: four hex digits of length, the header included, and
// "0000" alone as a flush. A side band packet carries one byte of band first.
const (
	pktHeaderLen = 4
	// maxPkt is the longest pkt-line, and smallBandPkt the longest plain
	// side-band packet; a band packet spends one byte on its band.
	maxPkt           = 65520
	smallBandPkt     = 1000
	maxBandData      = maxPkt - pktHeaderLen - 1
	maxSmallBandData = smallBandPkt - pktHeaderLen - 1
	flushPkt         = "0000"
	bandData         = 1
	bandProgress     = 2
)

// zeroOID is how a command names a ref that is not there: before a creation,
// after a deletion.
func zeroOID(oid string) bool { return strings.Trim(oid, "0") == "" }

// ErrBadPush is a receive-pack request that is not one git sends.
var ErrBadPush = errors.New("the push could not be read")

// RefUpdate is one command of a push: move Ref from Old to New.
type RefUpdate struct {
	Old, New, Ref string
}

// Deletes says the command removes the ref.
func (u RefUpdate) Deletes() bool { return zeroOID(u.New) }

// Creates says the command makes a ref that is not there.
func (u RefUpdate) Creates() bool { return zeroOID(u.Old) }

// PushRequest is what a git client posts to git-receive-pack: commands, then
// the pack that carries the objects they need.
type PushRequest struct {
	Updates      []RefUpdate
	Capabilities []string
	// Certificate is set for a signed push, whose commands sit in a certificate.
	Certificate bool
	Pack        []byte
}

// Asked says whether the client asked for a capability.
func (p *PushRequest) Asked(capability string) bool {
	return slices.Contains(p.Capabilities, capability)
}

// readPkt reads the pkt-line at the start of b: its payload, what follows it,
// and whether it was a flush.
func readPkt(b []byte) (payload, rest []byte, flush bool, err error) {
	if len(b) < pktHeaderLen {
		return nil, nil, false, fmt.Errorf("%w: a pkt-line is cut short", ErrBadPush)
	}
	n, err := strconv.ParseUint(string(b[:pktHeaderLen]), 16, 16)
	if err != nil {
		return nil, nil, false, fmt.Errorf("%w: %q is not a pkt-line length", ErrBadPush, b[:pktHeaderLen])
	}
	switch {
	case n == 0:
		return nil, b[pktHeaderLen:], true, nil
	case n < pktHeaderLen || int(n) > len(b):
		return nil, nil, false, fmt.Errorf("%w: a pkt-line of %d bytes does not fit", ErrBadPush, n)
	}
	return b[pktHeaderLen:n], b[n:], false, nil
}

// ParsePush reads the commands of a receive-pack request, and leaves the pack
// after them as it came.
func ParsePush(body []byte) (*PushRequest, error) {
	push := &PushRequest{}
	rest := body
	first := true
	for {
		payload, next, flush, err := readPkt(rest)
		if err != nil {
			return nil, err
		}
		rest = next
		if flush {
			break
		}
		line := strings.TrimSuffix(string(payload), "\n")
		if first {
			if cmd, caps, found := strings.Cut(line, "\x00"); found {
				line = cmd
				push.Capabilities = strings.Fields(caps)
			}
			if line == "push-cert" {
				push.Certificate = true
				return push, nil
			}
		}
		if strings.HasPrefix(line, "shallow ") {
			// A shallow clone names where its history stops; that is not a command.
			continue
		}
		first = false
		fields := strings.Fields(line)
		if len(fields) != 3 || !isOID(fields[0]) || !isOID(fields[1]) {
			return nil, fmt.Errorf("%w: %q is not a command", ErrBadPush, line)
		}
		push.Updates = append(push.Updates, RefUpdate{Old: fields[0], New: fields[1], Ref: fields[2]})
	}
	if len(push.Updates) == 0 {
		return nil, fmt.Errorf("%w: it names no ref", ErrBadPush)
	}
	if push.Asked("push-options") {
		for {
			_, next, flush, err := readPkt(rest)
			if err != nil {
				return nil, err
			}
			rest = next
			if flush {
				break
			}
		}
	}
	push.Pack = rest
	return push, nil
}

// isOID accepts a sha1 or sha256 object id in hex.
func isOID(s string) bool {
	if len(s) != sha1Hex && len(s) != sha256Hex {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// pktLine frames one line.
func pktLine(s string) []byte {
	return fmt.Appendf(nil, "%04x%s", len(s)+pktHeaderLen, s)
}

// ErrPkt is git's way for a server to stop a client with a sentence, which
// the client prints as "remote error".
func ErrPkt(sentence string) []byte { return pktLine("ERR " + oneLine(sentence) + "\n") }

// oneLine keeps a sentence to the single line a status or an ERR has.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// RefusalReport is receive-pack's answer refusing every command, each with its
// reason, as the client asked to hear it; nil when it asked for no report.
func RefusalReport(push *PushRequest, reasons map[string]string, summary string) []byte {
	if !push.Asked("report-status") && !push.Asked("report-status-v2") {
		return nil
	}
	var status bytes.Buffer
	status.Write(pktLine("unpack ok\n"))
	for _, u := range push.Updates {
		status.Write(pktLine("ng " + u.Ref + " " + oneLine(reasons[u.Ref]) + "\n"))
	}
	status.WriteString(flushPkt)

	band := 0
	switch {
	case push.Asked("side-band-64k"):
		band = maxBandData
	case push.Asked("side-band"):
		band = maxSmallBandData
	}
	if band == 0 {
		return status.Bytes()
	}
	var out bytes.Buffer
	// What goes to band two the client prints as "remote:", before the refs.
	for line := range strings.SplitSeq(summary, "\n") {
		out.Write(pktLine(string(rune(bandProgress)) + line + "\n"))
	}
	report := status.Bytes()
	for len(report) > 0 {
		n := min(band, len(report))
		out.Write(pktLine(string(rune(bandData)) + string(report[:n])))
		report = report[n:]
	}
	out.WriteString(flushPkt)
	return out.Bytes()
}
