package git

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"
	"testing"
)

const (
	objTree = 2
	objBlob = 3
)

// packBuilder writes a pack the way git does, entry by entry; the trailer's
// checksum is left out, since nothing here reads it.
type packBuilder struct {
	buf     bytes.Buffer
	count   uint32
	offsets []int
}

func (p *packBuilder) entry(kind int, body []byte, prefix []byte) int {
	at := packHeaderLen + p.buf.Len()
	size := len(body)
	c := byte(kind<<4) | byte(size&0x0f)
	size >>= 4
	for size > 0 {
		p.buf.WriteByte(c | 0x80)
		c = byte(size & 0x7f)
		size >>= 7
	}
	p.buf.WriteByte(c)
	p.buf.Write(prefix)
	z := zlib.NewWriter(&p.buf)
	_, _ = z.Write(body)
	_ = z.Close()
	p.count++
	p.offsets = append(p.offsets, at)
	return at
}

func (p *packBuilder) object(kind int, body string) int { return p.entry(kind, []byte(body), nil) }

// ofsDelta stores body as a delta of the entry at base that inserts it whole.
func (p *packBuilder) ofsDelta(base int, baseLen int, body string) int {
	at := packHeaderLen + p.buf.Len()
	back := at - base
	enc := []byte{byte(back & 0x7f)}
	for back >>= 7; back > 0; back >>= 7 {
		back--
		enc = append([]byte{byte(0x80 | back&0x7f)}, enc...)
	}
	return p.entry(objOfsDelta, insertDelta(baseLen, body), enc)
}

func (p *packBuilder) refDelta(baseID string, baseLen int, body string) int {
	id, _ := hex.DecodeString(baseID)
	return p.entry(objRefDelta, insertDelta(baseLen, body), id)
}

// insertDelta copies the base's first byte and inserts the rest, so applying
// it needs both instructions.
func insertDelta(baseLen int, body string) []byte {
	out := binary.AppendUvarint(nil, uint64(baseLen))
	out = binary.AppendUvarint(out, uint64(len(body)))
	out = append(out, 0x80|0x10, 1) // copy one byte from offset zero
	rest := []byte(body[1:])
	for len(rest) > 0 {
		n := min(len(rest), 0x7f)
		out = append(out, byte(n))
		out = append(out, rest[:n]...)
		rest = rest[n:]
	}
	return out
}

func (p *packBuilder) bytes() []byte {
	head := []byte(packSignature)
	head = binary.BigEndian.AppendUint32(head, 2)
	head = binary.BigEndian.AppendUint32(head, p.count)
	return append(head, p.buf.Bytes()...)
}

func commitText(parents ...string) string {
	out := "tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n"
	for _, p := range parents {
		out += "parent " + p + "\n"
	}
	return out + "author A <a@b> 1 +0000\ncommitter A <a@b> 1 +0000\n\nWork\n"
}

// The commits a push carries are read with their parents, stored whole or as
// deltas of one another, however much else the pack holds.
func TestAPacksCommitsAreReadWithTheirParents(t *testing.T) {
	onHost := "3333333333333333333333333333333333333333"
	first, second, third := commitText(onHost), "", ""
	firstID := objectID(sha1Hex, "commit", []byte(first))
	second = commitText(firstID)
	secondID := objectID(sha1Hex, "commit", []byte(second))
	third = commitText(secondID, onHost)
	thirdID := objectID(sha1Hex, "commit", []byte(third))

	var p packBuilder
	p.object(objBlob, "package plus\n")
	at := p.object(objCommit, first)
	p.object(objTree, "100644 plus.go\x00"+string(make([]byte, 20)))
	secondAt := p.ofsDelta(at, len(first), second)
	_ = secondAt
	p.refDelta(secondID, len(second), third)

	commits, err := PackCommits(p.bytes(), sha1Hex)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(commits[firstID], []string{onHost}) || !slices.Equal(commits[secondID], []string{firstID}) ||
		!slices.Equal(commits[thirdID], []string{secondID, onHost}) || len(commits) != 3 {
		t.Errorf("commits = %v", commits)
	}
	if !FastForward(commits, onHost, thirdID) {
		t.Error("work on top of the host's commit is a fast-forward")
	}
	if FastForward(commits, "4444444444444444444444444444444444444444", thirdID) {
		t.Error("work that does not reach the host's commit was taken for a fast-forward")
	}
	if FastForward(commits, onHost, "5555555555555555555555555555555555555555") {
		t.Error("a commit the pack does not carry was taken for a fast-forward")
	}
}

// An empty pack is a push that carries nothing new.
func TestAnEmptyPackCarriesNoCommits(t *testing.T) {
	commits, err := PackCommits(nil, sha1Hex)
	if err != nil || len(commits) != 0 {
		t.Errorf("commits = %v, %v", commits, err)
	}
}

// A pack the proxy cannot read is refused, never forwarded unchecked.
func TestABrokenPackIsRefused(t *testing.T) {
	var p packBuilder
	p.object(objCommit, commitText())
	whole := p.bytes()
	lying := slices.Clone(whole)
	lying[packHeaderLen] = byte(objCommit<<4) | 0x01 // says one byte, holds more

	for name, pack := range map[string][]byte{
		"not a pack":   []byte("PAKC\x00\x00\x00\x02\x00\x00\x00\x01"),
		"cut short":    whole[:len(whole)-4],
		"a wrong size": lying,
		"one entry too many": func() []byte {
			more := slices.Clone(whole)
			binary.BigEndian.PutUint32(more[8:packHeaderLen], 2)
			return more
		}(),
	} {
		if _, err := PackCommits(pack, sha1Hex); !errors.Is(err, ErrBadPack) {
			t.Errorf("%s: err = %v, want ErrBadPack", name, err)
		}
	}
}

// A pack whose objects would inflate past the bound is refused before they are.
func TestAPackThatUnpacksTooFarIsRefused(t *testing.T) {
	var p packBuilder
	p.buf.WriteByte(byte(objBlob<<4) | 0x80)
	p.buf.Write(binary.AppendUvarint(nil, MaxUnpackedPush))
	p.count = 1
	if _, err := PackCommits(p.bytes(), sha1Hex); !errors.Is(err, ErrBadPack) {
		t.Errorf("err = %v, want ErrBadPack", err)
	}
}

// A delta that reads past its base is not applied.
func TestADeltaThatDoesNotFitIsRefused(t *testing.T) {
	base := []byte("short")
	for name, delta := range map[string][]byte{
		"another base":     append(binary.AppendUvarint(nil, 9), 1, 'x'),
		"a copy past it":   {5, 10, 0x80 | 0x01 | 0x10, 2, 10},
		"a reserved op":    {5, 1, 0},
		"the wrong length": {5, 2, 1, 'x'},
	} {
		if _, err := applyDelta(base, delta); !errors.Is(err, ErrBadPack) {
			t.Errorf("%s: err = %v, want ErrBadPack", name, err)
		}
	}
}
