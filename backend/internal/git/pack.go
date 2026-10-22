package git

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1" //nolint:gosec // git names objects by sha1; nothing here relies on it resisting collisions.
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
)

// The object ids git writes, in hex, and the pack's own framing.
const (
	sha1Hex       = 40
	sha256Hex     = 64
	packSignature = "PACK"
	// packHeaderLen is the signature, the version and the object count.
	packHeaderLen = 12
	// MaxUnpackedPush bounds what a push's objects may inflate to while the
	// proxy reads them, so a small pack cannot make it inflate gigabytes.
	MaxUnpackedPush = 512 << 20
)

// The kinds of entry a pack holds, as its headers number them.
const (
	objCommit   = 1
	objOfsDelta = 6
	objRefDelta = 7
)

// ErrBadPack is a pack the proxy cannot read, which it refuses rather than
// forwarding unchecked.
var ErrBadPack = errors.New("the pushed pack could not be read")

// PackCommits answers the commits a pack carries, by id, with their parents.
// A commit stored as a delta of an object it cannot resolve is left out.
func PackCommits(pack []byte, hexLen int) (map[string][]string, error) {
	commits := map[string][]string{}
	if len(pack) == 0 {
		return commits, nil
	}
	if len(pack) < packHeaderLen || string(pack[:4]) != packSignature {
		return nil, fmt.Errorf("%w: it does not start as a pack", ErrBadPack)
	}
	count := binary.BigEndian.Uint32(pack[8:packHeaderLen])
	r := bytes.NewReader(pack)
	if _, err := r.Seek(packHeaderLen, io.SeekStart); err != nil {
		return nil, err
	}
	// Commits are kept by where they start and by id, for deltas made of them.
	byOffset := map[int64][]byte{}
	byID := map[string][]byte{}
	var inflated int64
	for range count {
		start := int64(len(pack)) - int64(r.Len())
		kind, size, err := entryHeader(r)
		if err != nil {
			return nil, err
		}
		if inflated += size; inflated > MaxUnpackedPush {
			return nil, fmt.Errorf("%w: it unpacks to more than %d MiB", ErrBadPack, MaxUnpackedPush>>20)
		}
		var base []byte
		switch kind {
		case objOfsDelta:
			back, err := ofsDeltaDistance(r)
			if err != nil {
				return nil, err
			}
			base = byOffset[start-back]
		case objRefDelta:
			id := make([]byte, hexLen/2)
			if _, err := io.ReadFull(r, id); err != nil {
				return nil, fmt.Errorf("%w: a delta's base is cut short", ErrBadPack)
			}
			base = byID[hex.EncodeToString(id)]
		}
		// Only a commit, or a delta of one, is worth keeping; the rest is
		// inflated to find where the next entry starts.
		keep := kind == objCommit || base != nil
		body, err := inflate(r, size, keep)
		if err != nil {
			return nil, err
		}
		if !keep {
			continue
		}
		if base != nil {
			if body, err = applyDelta(base, body); err != nil {
				return nil, err
			}
		}
		id := objectID(hexLen, "commit", body)
		byOffset[start], byID[id] = body, body
		commits[id] = parentsOf(body)
	}
	return commits, nil
}

// entryHeader reads an entry's kind and inflated size.
func entryHeader(r *bytes.Reader) (int, int64, error) {
	c, err := r.ReadByte()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: an entry is cut short", ErrBadPack)
	}
	kind := int(c>>4) & 0x7
	size := int64(c & 0x0f)
	for shift := 4; c&0x80 != 0; shift += 7 {
		if c, err = r.ReadByte(); err != nil || shift > 56 {
			return 0, 0, fmt.Errorf("%w: an entry's size is cut short", ErrBadPack)
		}
		size |= int64(c&0x7f) << shift
	}
	return kind, size, nil
}

// ofsDeltaDistance reads how far back an offset delta's base starts.
func ofsDeltaDistance(r *bytes.Reader) (int64, error) {
	c, err := r.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("%w: a delta's base is cut short", ErrBadPack)
	}
	back := int64(c & 0x7f)
	for c&0x80 != 0 {
		if c, err = r.ReadByte(); err != nil || back > 1<<48 {
			return 0, fmt.Errorf("%w: a delta's base is cut short", ErrBadPack)
		}
		back = (back+1)<<7 | int64(c&0x7f)
	}
	return back, nil
}

// inflate reads one zlib stream of the size the header promised, from a
// reader that zlib consumes byte by byte, so the next entry starts after it.
func inflate(r *bytes.Reader, size int64, keep bool) ([]byte, error) {
	z, err := zlib.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadPack, err)
	}
	defer z.Close()
	var out bytes.Buffer
	sink := io.Discard
	if keep {
		sink = &out
	}
	n, err := io.Copy(sink, io.LimitReader(z, size+1))
	if err != nil || n != size {
		return nil, fmt.Errorf("%w: an entry is not the size it says", ErrBadPack)
	}
	// Reading on to the end checks the stream's checksum and steps past it.
	if _, err := io.Copy(io.Discard, z); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadPack, err)
	}
	return out.Bytes(), nil
}

// applyDelta rebuilds an object from its base and git's copy and insert
// instructions.
func applyDelta(base, delta []byte) ([]byte, error) {
	bad := fmt.Errorf("%w: a delta does not fit its base", ErrBadPack)
	r := bytes.NewReader(delta)
	baseSize, err := binary.ReadUvarint(r)
	if err != nil || baseSize != uint64(len(base)) {
		return nil, bad
	}
	size, err := binary.ReadUvarint(r)
	if err != nil || size > MaxUnpackedPush {
		return nil, bad
	}
	out := make([]byte, 0, size)
	for r.Len() > 0 {
		op, _ := r.ReadByte()
		switch {
		case op&0x80 != 0:
			var offset, length uint64
			for i := range 4 {
				if op&(1<<i) != 0 {
					b, err := r.ReadByte()
					if err != nil {
						return nil, bad
					}
					offset |= uint64(b) << (8 * i)
				}
			}
			for i := range 3 {
				if op&(1<<(4+i)) != 0 {
					b, err := r.ReadByte()
					if err != nil {
						return nil, bad
					}
					length |= uint64(b) << (8 * i)
				}
			}
			if length == 0 {
				length = 0x10000
			}
			if offset+length > uint64(len(base)) {
				return nil, bad
			}
			out = append(out, base[offset:offset+length]...)
		case op != 0:
			insert := make([]byte, op)
			if _, err := io.ReadFull(r, insert); err != nil {
				return nil, bad
			}
			out = append(out, insert...)
		default:
			return nil, bad
		}
	}
	if uint64(len(out)) != size {
		return nil, bad
	}
	return out, nil
}

// objectID is git's name for an object: the hash of its kind, size and body.
func objectID(hexLen int, kind string, body []byte) string {
	var h hash.Hash = sha1.New() //nolint:gosec // see the import.
	if hexLen == sha256Hex {
		h = sha256.New()
	}
	h.Write([]byte(kind + " " + strconv.Itoa(len(body)) + "\x00"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// parentsOf reads the parent lines of a commit's header.
func parentsOf(commit []byte) []string {
	var parents []string
	for line := range strings.SplitSeq(string(commit), "\n") {
		if line == "" {
			break
		}
		if id, ok := strings.CutPrefix(line, "parent "); ok {
			parents = append(parents, id)
		}
	}
	return parents
}

// FastForward says whether new descends from old through the commits a push
// carries; a history it cannot follow is not shown to be one.
func FastForward(commits map[string][]string, old, new string) bool {
	seen := map[string]bool{}
	queue := []string{new}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		if at == old {
			return true
		}
		if seen[at] {
			continue
		}
		seen[at] = true
		queue = append(queue, commits[at]...)
	}
	return false
}
