package csvio

import (
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/issue"
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

// The export reads the list a page at a time, and a page the list would cut
// back to its default would leave issues out of the file.
func TestAnExportPageIsOneTheListGivesWhole(t *testing.T) {
	if exportPage <= 0 || exportPage > issue.MaxPageLimit {
		t.Errorf("an export page is %d issues, want 1 to the list's %d", exportPage, issue.MaxPageLimit)
	}
	if ExportRows < exportPage {
		t.Errorf("an export holds %d issues, fewer than one page of %d", ExportRows, exportPage)
	}
}
