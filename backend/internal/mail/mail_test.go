package mail

import "testing"

// A subject is somebody's words: an organization's name, a person's, an issue
// summary. A newline in one of them would start a header of their choosing.
func TestAHeaderStaysOnOneLine(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Acme", "Acme"},
		{"Acme\r\nBcc: everyone@elsewhere.test", "Acme  Bcc: everyone@elsewhere.test"},
		{"Line\nbreak", "Line break"},
		{"Bell\aring", "Bell ring"},
	} {
		if got := headerValue(c.in); got != c.want {
			t.Errorf("headerValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
