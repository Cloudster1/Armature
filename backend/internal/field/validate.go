package field

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MaxTextLength is the longest text a field holds. A field is a line, not a
// document; the description is for documents.
const MaxTextLength = 500

// MaxOptions is how many choices a select field may offer.
const MaxOptions = 50

// normalize checks a raw JSON value against the field's kind and returns it in
// its canonical form together with how it reads. A JSON null, or nothing at
// all, means the value is being cleared and comes back as nil.
func normalize(f Field, raw json.RawMessage) (json.RawMessage, string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, "", nil
	}
	switch f.Kind {
	case Text:
		s, err := asString(raw)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %s takes text", ErrBadValue, f.Name)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, "", nil
		}
		if len([]rune(s)) > MaxTextLength {
			return nil, "", fmt.Errorf("%w: %s takes at most %d characters", ErrBadValue, f.Name, MaxTextLength)
		}
		return marshal(s), s, nil
	case Number:
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil {
			// A number typed into a text box arrives as a string; accept it
			// rather than make every client convert first.
			s, serr := asString(raw)
			if serr != nil {
				return nil, "", fmt.Errorf("%w: %s takes a number", ErrBadValue, f.Name)
			}
			s = strings.TrimSpace(s)
			if s == "" {
				return nil, "", nil
			}
			n, err = strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, "", fmt.Errorf("%w: %s takes a number, such as 12 or 2.5", ErrBadValue, f.Name)
			}
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, "", fmt.Errorf("%w: %s takes a number", ErrBadValue, f.Name)
		}
		return marshal(n), strconv.FormatFloat(n, 'f', -1, 64), nil
	case Date:
		s, err := asString(raw)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %s takes a date such as 2026-01-31", ErrBadValue, f.Name)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, "", nil
		}
		day, err := time.Parse("2006-01-02", s)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %s takes a date such as 2026-01-31", ErrBadValue, f.Name)
		}
		formatted := day.Format("2006-01-02")
		return marshal(formatted), formatted, nil
	case Select:
		s, err := asString(raw)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %s takes one of its options", ErrBadValue, f.Name)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, "", nil
		}
		for _, option := range f.Options {
			if option == s {
				return marshal(s), s, nil
			}
		}
		return nil, "", fmt.Errorf("%w: %s has to be one of %s", ErrBadValue, f.Name, strings.Join(f.Options, ", "))
	case Checkbox:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, "", fmt.Errorf("%w: %s is either true or false", ErrBadValue, f.Name)
		}
		if !b {
			// Unticked is the same as never answered, so the row goes away
			// rather than storing a false that means nothing.
			return nil, "", nil
		}
		return marshal(true), "Yes", nil
	case URL:
		s, err := asString(raw)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %s takes a web address", ErrBadValue, f.Name)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, "", nil
		}
		parsed, err := url.Parse(s)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, "", fmt.Errorf("%w: %s takes a web address starting with http:// or https://", ErrBadValue, f.Name)
		}
		return marshal(s), s, nil
	}
	return nil, "", ErrBadKind
}

// display renders a stored value the way normalize would have. A value that
// no longer fits, such as a choice removed from a select field's options, is
// still what somebody said and is shown as they said it.
func display(f Field, raw json.RawMessage) string {
	_, shown, err := normalize(f, raw)
	if err == nil {
		return shown
	}
	if s, err := asString(raw); err == nil {
		return s
	}
	return string(raw)
}

// cleanOptions tidies the choices of a select field: trimmed, no blanks, no
// duplicates, and not too many. Any other kind has none.
func cleanOptions(kind Kind, options []string) ([]string, error) {
	if kind != Select {
		return []string{}, nil
	}
	out := make([]string, 0, len(options))
	seen := map[string]bool{}
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" || seen[option] {
			continue
		}
		seen[option] = true
		out = append(out, option)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: a select field needs at least one option", ErrBadValue)
	}
	if len(out) > MaxOptions {
		return nil, fmt.Errorf("%w: a select field offers at most %d options", ErrBadValue, MaxOptions)
	}
	return out, nil
}

func asString(raw json.RawMessage) (string, error) {
	var s string
	err := json.Unmarshal(raw, &s)
	return s, err
}

func marshal(v any) json.RawMessage {
	encoded, err := json.Marshal(v)
	if err != nil {
		// Only strings, numbers and booleans reach here; none can fail.
		return json.RawMessage("null")
	}
	return encoded
}
