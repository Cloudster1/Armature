package issue

import (
	"encoding/json"
	"testing"
)

// The history of a move names each dropped value the way the field showed it,
// so a reader can tell what was there before.
func TestFieldValueTextReadsAStoredValue(t *testing.T) {
	cases := []struct {
		name   string
		stored string
		want   string
	}{
		{"text", `"Acme"`, "Acme"},
		{"whole number", `120`, "120"},
		{"fraction", `12.5`, "12.5"},
		{"large number stays plain", `1500000`, "1500000"},
		{"date", `"2026-03-01"`, "2026-03-01"},
		{"choice", `"Web"`, "Web"},
		{"ticked box", `true`, "Yes"},
		{"link", `"https://example.test/spec"`, "https://example.test/spec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FieldValueText(json.RawMessage(tc.stored)); got != tc.want {
				t.Errorf("FieldValueText(%s) = %q, want %q", tc.stored, got, tc.want)
			}
		})
	}
}
