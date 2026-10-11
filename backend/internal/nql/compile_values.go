package nql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// keyPattern is the issue package's, repeated here because the issue package
// is what imports this one.
var keyPattern = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-([1-9][0-9]{0,17})$`)

// keys binds issue keys into a template with two slots, project key and number.
func (c *compiler) keys(values []Value, template string) (string, error) {
	var parts []string
	for _, v := range values {
		if v.Kind != VWord && v.Kind != VString {
			return "", errAt(v.Pos, "%q is not an issue key. Write it like PROJ-12.", v.String())
		}
		m := keyPattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(v.Text)))
		if m == nil {
			return "", errAt(v.Pos, "%q is not an issue key. Write it like PROJ-12.", v.Text)
		}
		num, _ := strconv.ParseInt(m[2], 10, 64)
		parts = append(parts, fmt.Sprintf(template, c.bind(m[1]), c.bind(num)))
	}
	return anyOf(parts), nil
}

// texts reads plain values, normalized, refusing functions and numbers.
func (c *compiler) texts(def fieldDef, values []Value, normalize func(string) string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		text, err := plainText(def, v)
		if err != nil {
			return nil, err
		}
		out = append(out, normalize(text))
	}
	return out, nil
}

func plainText(def fieldDef, v Value) (string, error) {
	switch v.Kind {
	case VWord, VString:
		return v.Text, nil
	case VNumber:
		return v.Text, nil
	case VFunction:
		return "", errAt(v.Pos, "%s() cannot be compared with %s. Write the value itself.", v.Text, def.name)
	default:
		return "", errAt(v.Pos, "%q is not a value %s takes.", v.Text, def.name)
	}
}

func number(def fieldDef, v Value) (float64, error) {
	if v.Kind != VNumber {
		return 0, errAt(v.Pos, "%s is a number, and %s is not one.", def.name, v)
	}
	n, err := strconv.ParseFloat(v.Text, 64)
	if err != nil {
		return 0, errAt(v.Pos, "%q is not a number.", v.Text)
	}
	return n, nil
}

func withParens(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + "()"
	}
	return out
}

func anyOf(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func contains(list []string, s string) bool {
	for _, each := range list {
		if each == s {
			return true
		}
	}
	return false
}
