package git

import (
	"strings"
	"testing"
)

func TestIssueKeysAreFoundInProse(t *testing.T) {
	got := IssueKeys("CP-12 fix the thing, also CP-7 and cp-9 (not a key) and CP-12 again; PROJ2-1 too")
	want := "CP-12,CP-7,PROJ2-1"
	if strings.Join(got, ",") != want {
		t.Errorf("keys = %v, want %s", got, want)
	}
}

func TestIssueKeysIgnoreWhatOnlyLooksLikeOne(t *testing.T) {
	for _, text := range []string{"ABC-0 has no zero issue", "utf-8", "A-1 is too short a project", "cp-4 in lower case"} {
		if got := IssueKeys(text); len(got) != 0 {
			t.Errorf("%q yielded keys %v", text, got)
		}
	}
}

func TestSmartCommandsReadTransitionsAndComments(t *testing.T) {
	got := SmartCommands("CP-4 #start-progress fix the validator\n\nCP-4 #comment reproduced on staging, see #123")
	if len(got) != 2 {
		t.Fatalf("commands = %+v, want two", got)
	}
	if got[0].Transition != "start-progress" || got[0].Comment != "" {
		t.Errorf("first = %+v, want the transition", got[0])
	}
	if got[1].Comment != "reproduced on staging, see #123" || got[1].Transition != "" {
		t.Errorf("second = %+v, want the comment to the end of its line", got[1])
	}
}

// "#123" on every host is a reference to something else, never a command.
func TestSmartCommandsSkipNumbers(t *testing.T) {
	if got := SmartCommands("CP-4 closes #123 and #45"); len(got) != 0 {
		t.Errorf("commands = %+v, want none", got)
	}
}

func TestTransitionSlugMatchesHowPeopleType(t *testing.T) {
	cases := map[string]string{"Start progress": "start-progress", "Ready for  review": "ready-for-review", "Close": "close"}
	for name, want := range cases {
		if got := TransitionSlug(name); got != want {
			t.Errorf("TransitionSlug(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestBranchNameIsSafeAndShort(t *testing.T) {
	got := BranchName("CP-4", "Sign-in rejects valid passwords containing a plus sign (and more!) really quite long summary here")
	if !strings.HasPrefix(got, "CP-4-sign-in-rejects-valid-passwords") {
		t.Errorf("name = %q", got)
	}
	if len(got) > len("CP-4-")+41 {
		t.Errorf("name %q is longer than a person wants to type", got)
	}
	if strings.ContainsAny(got, " ()!") {
		t.Errorf("name %q holds characters a ref cannot", got)
	}
	if got != "CP-4-sign-in-rejects-valid-passwords" {
		t.Errorf("name = %q, want it cut at a word, not inside one", got)
	}
	if got := BranchName("CP-5", "supercalifragilisticexpialidociousness-and-then-some-more-words"); got != "CP-5-supercalifragilisticexpialidociousness" {
		t.Errorf("a first word longer than the limit is kept whole: %q", got)
	}
	if got := BranchName("CP-9", "!!!"); got != "CP-9" {
		t.Errorf("a summary with nothing usable gives %q, want the key alone", got)
	}
}
