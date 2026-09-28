package theme

import (
	"testing"
)

// The theme files kept under shipped/ are offered as examples and are what
// somebody imports to try the format. Each has to be a package this version
// reads, with a description, and a spec the editor would save; the examples
// test then validates it like the rest.
func TestEveryShippedThemeFileIsAnExample(t *testing.T) {
	examples := shipped()
	if len(examples) < 2 {
		t.Fatalf("shipped %d themes, want the files under shipped/", len(examples))
	}
	for _, e := range examples {
		if e.Description == "" {
			t.Errorf("%s has no description for the Themes page", e.Key)
		}
		if ExampleByKey(e.Key) == nil {
			t.Errorf("%s is not offered as an example", e.Key)
		}
	}
}
