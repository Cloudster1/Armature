package mailin

import "testing"

func TestStripQuotesKeepsWhatWasWritten(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"gmail", "Still broken.\n\nOn Mon, 1 Sep 2026 at 10:00, Armature <no-reply@armature.test> wrote:\n> We have your request\n> HELP-1", "Still broken."},
		{"wrapped", "Still broken.\n\nOn Mon, 1 Sep 2026 at 10:00, Armature\n<no-reply@armature.test> wrote:\n> quoted", "Still broken."},
		{"outlook", "Thanks, that worked.\r\n\r\nFrom: Armature <no-reply@armature.test>\r\nSent: Monday\r\nSubject: [HELP-1] replied\r\n\r\nquoted", "Thanks, that worked."},
		{"original message", "Yes please.\n-----Original Message-----\nFrom: x@y.z\nquoted", "Yes please."},
		{"german", "Passt.\n\nAm 01.09.2026 um 10:00 schrieb Armature:\n> quoted", "Passt."},
		{"signature", "Done.\n-- \nAda\nLovelace Ltd", "Done."},
		{"brackets only", "> old\n> older\nNew line\n> quoted again", "New line"},
		{"phone", "ok\n\nSent from my iPhone", "ok"},
		{"nothing new", "> only quotes\n>\n> here", ""},
	}
	for _, c := range cases {
		if got := StripQuotes(c.in); got != c.want {
			t.Errorf("%s: StripQuotes = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIssueKeyIsReadFromTheSubjectThenTheThread(t *testing.T) {
	if got := IssueKeyIn("Re: [HELP-12] Grace replied to your request", "", nil); got != "HELP-12" {
		t.Errorf("subject: %q", got)
	}
	if got := IssueKeyIn("Re: your request", "HELP-3.abc@armature.test", nil); got != "HELP-3" {
		t.Errorf("in-reply-to: %q", got)
	}
	if got := IssueKeyIn("Re: your request", "unrelated@elsewhere", []string{"HELP-7.def@armature.test", "HELP-8.ghi@armature.test"}); got != "HELP-8" {
		t.Errorf("references, newest last: %q", got)
	}
	if got := IssueKeyIn("hello", "", nil); got != "" {
		t.Errorf("nothing: %q", got)
	}
}
