package nql

import (
	"strings"
	"testing"
)

func TestCompleteKnowsWhereTheCaretIs(t *testing.T) {
	cases := []struct {
		text   string
		slot   Slot
		field  string
		prefix string
		from   int
		want   string // a word that must be offered
		not    string // a word that must not be
	}{
		{"", SlotField, "", "", 0, "status", ""},
		{"sta", SlotField, "", "sta", 0, "status", "summary"},
		{"status ", SlotOperator, "status", "", 7, "IN", "<"},
		{"priority ", SlotOperator, "priority", "", 9, ">=", ""},
		{"status = ", SlotValue, "status", "", 9, "", ""},
		{"statusCategory = ", SlotValue, "statusCategory", "", 17, "done", ""},
		{"statusCategory = do", SlotValue, "statusCategory", "do", 17, "done", "todo"},
		{"assignee = ", SlotValue, "assignee", "", 11, "currentUser()", ""},
		{"status = Done ", SlotKeyword, "", "", 14, "AND", ""},
		{"status = Done AND ", SlotField, "", "", 18, "assignee", ""},
		{"status IN (", SlotValue, "status", "", 11, "", ""},
		{"status IN (Done, ", SlotValue, "status", "", 17, "", ""},
		{"due ", SlotOperator, "due", "", 4, "IS EMPTY", ""},
		{"due IS ", SlotKeyword, "", "", 7, "NOT EMPTY", ""},
		{"due IS EMPTY ", SlotKeyword, "", "", 13, "ORDER BY", ""},
		{"status = Done ORDER ", SlotKeyword, "", "", 20, "BY", ""},
		{"status = Done ORDER BY ", SlotField, "", "", 23, "updated", "description"},
		{"status = Done ORDER BY updated ", SlotKeyword, "", "", 31, "DESC", ""},
		{`"Story points" `, SlotOperator, "Story points", "", 15, "~", ""},
		{`summary ~ "print`, SlotValue, "summary", "print", 10, "", ""},
		{`(status = Done) `, SlotKeyword, "", "", 16, "OR", ""},
	}
	for _, c := range cases {
		got := Complete(c.text, len([]rune(c.text)))
		if got.Slot != c.slot || got.Field != c.field || got.Prefix != c.prefix || got.From != c.from || got.To != len([]rune(c.text)) {
			t.Errorf("%q: slot %s field %q prefix %q from %d to %d", c.text, got.Slot, got.Field, got.Prefix, got.From, got.To)
		}
		if c.want != "" && !hasWord(got.Words, c.want) {
			t.Errorf("%q: %q missing from %v", c.text, c.want, got.Words)
		}
		if c.not != "" && hasWord(got.Words, c.not) {
			t.Errorf("%q: %q offered in %v", c.text, c.not, got.Words)
		}
	}
	if got := Complete(`summary ~ "print`, 16); !got.Quoted || got.From != 10 {
		t.Errorf("an open quote is the word being typed: %+v", got)
	}
	// The caret in the middle of the text completes what is before it.
	if got := Complete("status = Done AND assignee = x", 3); got.Slot != SlotField || got.Prefix != "sta" {
		t.Errorf("mid-text caret: %+v", got)
	}
}

func hasWord(words []string, w string) bool {
	for _, each := range words {
		if strings.EqualFold(each, w) {
			return true
		}
	}
	return false
}
