package field

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNormalizeByKind(t *testing.T) {
	cases := []struct {
		name    string
		kind    Kind
		options []string
		in      string
		want    string
		display string
		bad     bool
	}{
		{"text is trimmed", Text, nil, `"  Acme  "`, `"Acme"`, "Acme", false},
		{"blank text clears", Text, nil, `"   "`, "", "", false},
		{"text refuses a number", Text, nil, `5`, "", "", true},
		{"number", Number, nil, `12.5`, `12.5`, "12.5", false},
		{"number typed as text", Number, nil, `"7"`, `7`, "7", false},
		{"number refuses words", Number, nil, `"seven"`, "", "", true},
		{"date", Date, nil, `"2026-03-01"`, `"2026-03-01"`, "2026-03-01", false},
		{"date refuses a time", Date, nil, `"2026-03-01T10:00:00Z"`, "", "", true},
		{"select takes an option", Select, []string{"Web", "Mobile"}, `"Web"`, `"Web"`, "Web", false},
		{"select refuses anything else", Select, []string{"Web", "Mobile"}, `"Desk"`, "", "", true},
		{"ticked checkbox", Checkbox, nil, `true`, `true`, "Yes", false},
		{"unticked checkbox clears", Checkbox, nil, `false`, "", "", false},
		{"checkbox refuses text", Checkbox, nil, `"yes"`, "", "", true},
		{"url", URL, nil, `"https://example.test/spec"`, `"https://example.test/spec"`, "https://example.test/spec", false},
		{"url needs a scheme", URL, nil, `"example.test"`, "", "", true},
		{"null clears", Text, nil, `null`, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, shown, err := normalize(Field{Name: "F", Kind: tc.kind, Options: tc.options}, json.RawMessage(tc.in))
			if tc.bad {
				if !errors.Is(err, ErrBadValue) {
					t.Fatalf("want ErrBadValue, got %v (value %s)", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("value: got %s want %s", got, tc.want)
			}
			if shown != tc.display {
				t.Errorf("display: got %q want %q", shown, tc.display)
			}
		})
	}
}

func TestUnknownKindIsRefused(t *testing.T) {
	if _, _, err := normalize(Field{Kind: "colour"}, json.RawMessage(`"red"`)); !errors.Is(err, ErrBadKind) {
		t.Fatalf("want ErrBadKind, got %v", err)
	}
	if Kind("colour").Valid() {
		t.Fatal("an unknown kind must not be valid")
	}
}

func TestCleanOptions(t *testing.T) {
	got, err := cleanOptions(Select, []string{" Web", "Mobile", "Web", "", "Mobile "})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "Web" || got[1] != "Mobile" {
		t.Fatalf("got %v", got)
	}
	if _, err := cleanOptions(Select, []string{" "}); !errors.Is(err, ErrBadValue) {
		t.Fatalf("a select with no options must be refused, got %v", err)
	}
	// Other kinds carry no options, whatever the caller sent.
	if got, _ := cleanOptions(Text, []string{"a"}); len(got) != 0 {
		t.Fatalf("text keeps no options, got %v", got)
	}
}

// An option can be taken away after it was chosen; the answer stays readable.
func TestDisplayShowsAChoiceNoLongerOffered(t *testing.T) {
	f := Field{Name: "Platform", Kind: Select, Options: []string{"Web"}}
	if got := display(f, json.RawMessage(`"Desk"`)); got != "Desk" {
		t.Fatalf("got %q, want the choice without quotes", got)
	}
	if got := display(f, json.RawMessage(`"Web"`)); got != "Web" {
		t.Fatalf("got %q", got)
	}
}
