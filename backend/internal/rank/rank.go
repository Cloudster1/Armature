// Package rank produces sortable strings that can always be placed between two
// neighbours.
//
// A board needs a stable order that survives a card being dragged anywhere.
// Integer positions would mean renumbering every card below the insertion
// point, which is a write per card and a lost update away from two cards
// claiming the same slot. A lexicographic rank instead gives each card a string
// whose ordinary string comparison is its position, so inserting between two
// cards is one row update and never touches anything else.
package rank

import (
	"errors"
	"fmt"
	"strings"
)

// alphabet is the digit set, in ascending byte order so that string comparison
// and numeric comparison agree. Digits before letters, upper before lower, is
// exactly ASCII order.
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

const (
	first = '0'
	last  = 'z'
	// mid is the digit used to seed the very first rank, leaving room on both
	// sides so that the first card can be moved either way without a rebalance.
	mid = 'V'
)

// ErrOutOfOrder is returned when the two bounds are not in ascending order.
var ErrOutOfOrder = errors.New("rank bounds are out of order")

// index returns a digit's numeric value.
func index(c byte) int { return strings.IndexByte(alphabet, c) }

// Initial returns the rank for the first card in an empty list.
func Initial() string { return string(mid) }

// Between returns a rank that sorts strictly after a and strictly before b.
//
// An empty a means "before everything" and an empty b means "after
// everything", so appending is Between(lastRank, "") and prepending is
// Between("", firstRank).
//
// The ranks are read as fractional numbers in base 62: "AV" is the digits A and
// V after an implicit radix point. Because the digit set is in ascending byte
// order, comparing the strings and comparing the numbers give the same answer,
// and finding a rank in between is finding a number in between.
func Between(a, b string) (string, error) {
	if a == "" && b == "" {
		return Initial(), nil
	}
	if err := validate(a); err != nil {
		return "", err
	}
	if err := validate(b); err != nil {
		return "", err
	}
	if a != "" && b != "" && a >= b {
		return "", fmt.Errorf("%w: %q is not before %q", ErrOutOfOrder, a, b)
	}

	base := len(alphabet)
	var out []byte

	for i := 0; ; i++ {
		// Past its end, a reads as zero and b as one past the highest digit,
		// which is exactly what "before everything" and "after everything"
		// mean at this position.
		da := 0
		if i < len(a) {
			da = index(a[i])
		}
		db := base
		if i < len(b) {
			db = index(b[i])
		}

		if da == db {
			out = append(out, alphabet[da])
			continue
		}

		if middle := (da + db) / 2; middle > da {
			// There is a digit strictly between them, so this is the answer.
			return string(append(out, alphabet[middle])), nil
		}

		// The two digits are adjacent, leaving no room at this position. Keep
		// a's digit, which fixes the result below b whatever follows, and then
		// find any tail that puts the result above a.
		out = append(out, alphabet[da])
		for j := i + 1; ; j++ {
			if j >= len(a) {
				// a has run out, so any non-zero digit is greater than it.
				return string(append(out, alphabet[base/2])), nil
			}
			dj := index(a[j])
			if dj+1 >= base {
				// The highest digit: nothing fits above it here, so carry it
				// and try the next position.
				out = append(out, alphabet[dj])
				continue
			}
			return string(append(out, alphabet[dj+(base-dj)/2])), nil
		}
	}
}

// digitAt returns the i-th digit of s, or fallback when s is shorter.
func digitAt(s string, i int, fallback byte) byte {
	if i < len(s) {
		return s[i]
	}
	return fallback
}

func validate(s string) error {
	if s == "" {
		return nil
	}
	for i := 0; i < len(s); i++ {
		if index(s[i]) < 0 {
			return fmt.Errorf("rank %q contains an invalid character %q", s, s[i])
		}
	}
	// A rank ending in the lowest digit has no room below its last position,
	// which would break the midpoint walk. Ranks are never produced this way.
	if s[len(s)-1] == first {
		return fmt.Errorf("rank %q must not end in %q", s, string(first))
	}
	return nil
}

// Sequential returns a fixed width rank for the n-th item in a list.
//
// Cards created in order get ranks in that order without any of them growing
// longer, which is what keeps a long-lived project's ranks short. Hexadecimal
// digits are used because they are a contiguous, ascending run of the alphabet,
// so a fixed width value compares numerically. The trailing digit keeps the
// result clear of the rule that a rank must not end in the lowest digit.
func Sequential(n int64) string {
	return fmt.Sprintf("%010x", n) + string(mid)
}

// Sequence returns n ranks in ascending order, for seeding a list.
func Sequence(n int) ([]string, error) {
	out := make([]string, 0, n)
	previous := ""
	for range n {
		next, err := Between(previous, "")
		if err != nil {
			return nil, err
		}
		out = append(out, next)
		previous = next
	}
	return out, nil
}
