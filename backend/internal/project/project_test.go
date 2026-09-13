package project

import "testing"

func TestSuggestKey(t *testing.T) {
	tests := []struct{ name, want string }{
		{"Armature", "ARMATURE"},
		{"Customer Portal", "CP"},
		{"Really Long Project Name Here", "RLPNH"},
		{"Platform", "PLATFORM"},
		{"api", "API"},
		{"Foo & Bar", "FB"},
		{"A", "AA"},
		{"Supercalifragilistic", "SUPERCALIF"},
		{"", ""},
		{"123", ""},
		{"!!!", ""},
	}
	for _, tt := range tests {
		if got := SuggestKey(tt.name); got != tt.want {
			t.Errorf("SuggestKey(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// Whatever SuggestKey produces must satisfy the database constraint, or an
// unusual project name turns into a failed insert.
func TestSuggestKeyProducesUsableKeys(t *testing.T) {
	for _, name := range []string{
		"Armature", "Customer Portal", "A", "api", "Foo & Bar",
		"Really Long Project Name With Very Many Words In It Indeed",
		"Ünïcödé Prøject",
	} {
		got := SuggestKey(name)
		if got == "" {
			continue // the caller asks the user for a key
		}
		if !ValidKey(got) {
			t.Errorf("SuggestKey(%q) = %q, which the project_key_shape constraint rejects", name, got)
		}
	}
}

func TestValidKey(t *testing.T) {
	valid := []string{"AB", "NOJ", "A1", "ABCDEFGHIJ", "X9Y8Z7"}
	invalid := []string{"", "A", "1AB", "ab", "A-B", "A B", "ABCDEFGHIJK", "A_B"}
	for _, k := range valid {
		if !ValidKey(k) {
			t.Errorf("ValidKey(%q) = false, want true", k)
		}
	}
	for _, k := range invalid {
		if ValidKey(k) {
			t.Errorf("ValidKey(%q) = true, want false", k)
		}
	}
}

func TestNormalizeKey(t *testing.T) {
	for in, want := range map[string]string{
		"  noj  ": "NOJ",
		"noj":     "NOJ",
		"NOJ":     "NOJ",
	} {
		if got := NormalizeKey(in); got != want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKindValid(t *testing.T) {
	for _, k := range []Kind{KindSoftware, KindService, KindBusiness} {
		if !k.Valid() {
			t.Errorf("%q should be valid", k)
		}
	}
	for _, k := range []Kind{"", "other", "Software"} {
		if k.Valid() {
			t.Errorf("%q should not be valid", k)
		}
	}
}
