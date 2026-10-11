package csvio

import (
	"strings"
	"testing"
)

// The last row of a cut export is all that tells a spreadsheet reader the
// file is not everything, so it names both counts and what to do.
func TestACutExportEndsWithANoteThatSaysSo(t *testing.T) {
	note := truncationNote(ExportRows, 7321)
	for _, want := range []string{"5000", "7321", "narrow"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note %q does not say %q", note, want)
		}
	}
	if Cell(note) != note {
		t.Errorf("the note %q reads as a formula in a spreadsheet", note)
	}
}
