package csvio

import "testing"

// A customer's words end up in an agent's spreadsheet, where a leading "=" is
// a program rather than a summary.
func TestACellIsTextNotAFormula(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"The printer is on fire", "The printer is on fire"},
		{"=HYPERLINK(\"http://evil\",\"Details\")", "'=HYPERLINK(\"http://evil\",\"Details\")"},
		{"+1 555 0100", "'+1 555 0100"},
		{"-5", "'-5"},
		{"@everyone", "'@everyone"},
		{"", ""},
	} {
		if got := Cell(c.in); got != c.want {
			t.Errorf("Cell(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
