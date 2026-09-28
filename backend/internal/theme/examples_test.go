package theme

import (
	"testing"

	"github.com/google/uuid"
)

// An example that the editor could not save would be a trap, so every one
// passes the same validation a typed theme does.
func TestEveryExampleIsAValidTheme(t *testing.T) {
	seen := map[string]bool{}
	for _, example := range Examples() {
		if example.Key == "" || example.Name == "" || seen[example.Key] {
			t.Errorf("example %q needs a key and a name of its own", example.Name)
		}
		seen[example.Key] = true
		spec := example.Spec
		if err := Validate(&spec, uuid.Nil, nil); err != nil {
			t.Errorf("%s does not validate: %v", example.Name, err)
		}
		if len(spec.Colors.Light) == 0 || len(spec.Colors.Dark) == 0 {
			t.Errorf("%s leaves one of the palettes empty", example.Name)
		}
	}
	if ExampleByKey("deep-tech") == nil || ExampleByKey("nothing") != nil {
		t.Error("ExampleByKey does not find what is there, or finds what is not")
	}
}
