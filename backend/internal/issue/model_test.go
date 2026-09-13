package issue

import (
	"errors"
	"testing"
)

func TestParseKey(t *testing.T) {
	tests := []struct {
		in      string
		project string
		num     int64
		wantErr bool
	}{
		{in: "NOJ-1", project: "NOJ", num: 1},
		{in: "NOJ-142", project: "NOJ", num: 142},
		{in: "noj-142", project: "NOJ", num: 142},
		{in: "  NOJ-142  ", project: "NOJ", num: 142},
		{in: "ABCDEFGHIJ-999999", project: "ABCDEFGHIJ", num: 999999},
		{in: "A1B2-7", project: "A1B2", num: 7},

		{in: "NOJ", wantErr: true},
		{in: "NOJ-", wantErr: true},
		{in: "-142", wantErr: true},
		{in: "NOJ-0", wantErr: true},   // issue numbers start at one
		{in: "NOJ-007", wantErr: true}, // no leading zeroes
		{in: "N-1", wantErr: true},     // project keys are at least two long
		{in: "1NOJ-1", wantErr: true},  // and start with a letter
		{in: "NOJ-1-2", wantErr: true},
		{in: "NOJ 142", wantErr: true},
		{in: "", wantErr: true},
	}

	for _, tt := range tests {
		project, num, err := ParseKey(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseKey(%q) = %q/%d, want an error", tt.in, project, num)
			} else if !errors.Is(err, ErrInvalidKey) {
				t.Errorf("ParseKey(%q) error = %v, want ErrInvalidKey", tt.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseKey(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if project != tt.project || num != tt.num {
			t.Errorf("ParseKey(%q) = %q/%d, want %q/%d", tt.in, project, num, tt.project, tt.num)
		}
	}
}

func TestKeyRoundTrip(t *testing.T) {
	for _, key := range []string{"NOJ-1", "PLATFORM-4321", "A1-9"} {
		project, num, err := ParseKey(key)
		if err != nil {
			t.Fatalf("ParseKey(%q): %v", key, err)
		}
		if got := FormatKey(project, num); got != key {
			t.Errorf("round trip of %q produced %q", key, got)
		}
	}
}

func TestPriorityValid(t *testing.T) {
	for _, p := range []Priority{PriorityLowest, PriorityLow, PriorityMedium, PriorityHigh, PriorityHighest} {
		if !p.Valid() {
			t.Errorf("%q should be valid", p)
		}
	}
	for _, p := range []Priority{"", "urgent", "High"} {
		if p.Valid() {
			t.Errorf("%q should not be valid", p)
		}
	}
}
