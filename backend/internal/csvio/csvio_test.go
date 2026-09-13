package csvio

import (
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
)

func TestReadAndGuess(t *testing.T) {
	headers, rows, err := Read([]byte(byteOrderMark + "Title,Issue Type,Story Points,Tags\nFix it,Bug,3,\"a; b\"\n"))
	if err != nil || len(headers) != 4 || headers[0] != "Title" || len(rows) != 1 {
		t.Fatalf("read: %v %v %v", headers, rows, err)
	}
	if rows[0].Line != 2 {
		t.Errorf("the row says it is on line %d, want the line it is really on", rows[0].Line)
	}
	m := Guess(headers)
	if len(m["summary"]) != 1 || m["summary"][0] != 0 || m["type"][0] != 1 || m["estimate"][0] != 2 || m["labels"][0] != 3 {
		t.Errorf("guess = %v", m)
	}
	if _, _, err := Read([]byte("")); err == nil {
		t.Error("an empty file was accepted")
	}
	big := "summary\n" + strings.Repeat("x\n", MaxImportRows+1)
	if _, _, err := Read([]byte(big)); err == nil {
		t.Error("a file over the row cap was accepted")
	}
}

// A Jira export names five columns Labels, so a mapping has to say which
// column it means and let one target take several.
func TestAJiraHeaderMapsItself(t *testing.T) {
	headers := strings.Split("Summary,Issue key,Issue Type,Status,Priority,Assignee,Reporter,Created,Updated,Resolved,Due Date,Description,Labels,Labels,Labels,Original Estimate,Remaining Estimate", ",")
	m := Guess(headers)
	for target, want := range map[string]int{
		"summary": 0, "key": 1, "type": 2, "status": 3, "priority": 4, "assignee": 5,
		"reporter": 6, "created": 7, "updated": 8, "resolved": 9, "due": 10, "description": 11,
		"timeEstimate": 15, "timeRemaining": 16,
	} {
		if len(m[target]) != 1 || m[target][0] != want {
			t.Errorf("%s mapped to %v, want column %d", target, m[target], want)
		}
	}
	if len(m["labels"]) != 3 {
		t.Errorf("labels mapped to %v, want all three columns of that name", m["labels"])
	}
	if err := check(m, headers); err != nil {
		t.Errorf("the guessed mapping was refused: %v", err)
	}
}

func TestAMappingIsCheckedBeforeAnythingIsRead(t *testing.T) {
	headers := []string{"Title", "Priority"}
	if err := check(Mapping{"priority": {1}}, headers); err == nil {
		t.Error("a mapping with no summary was accepted")
	}
	if err := check(Mapping{"summary": {0}, "priority": {0, 1}}, headers); err == nil {
		t.Error("two columns were accepted for a target that holds one value")
	}
	if err := check(Mapping{"summary": {0}, "colour": {1}}, headers); err == nil {
		t.Error("a target nothing can fill was accepted")
	}
	if err := check(Mapping{"summary": {0}, "priority": {9}}, headers); err == nil {
		t.Error("a column the file does not have was accepted")
	}
}

// Every tracker writes a date its own way, and this one file writes two.
func TestTimesAndDurationsAsOtherTrackersWriteThem(t *testing.T) {
	want := time.Date(2026, time.September, 9, 8, 37, 0, 0, time.UTC)
	if at, err := parseMoment("09/Sep/26 08:37"); err != nil || !at.Equal(want) {
		t.Errorf("Jira's own format = %v %v", at, err)
	}
	if at, err := parseMoment("2026-09-09 06:37:21.0"); err != nil || at.Hour() != 6 {
		t.Errorf("a timestamp with a tenth of a second = %v %v", at, err)
	}
	if _, err := parseMoment("last Tuesday"); err == nil {
		t.Error("a phrase was read as a date")
	}
	if day, err := parseDay("09/Sep/26 08:37"); err != nil || day.Hour() != 0 {
		t.Errorf("a day kept its time: %v %v", day, err)
	}

	// Jira writes seconds; people write the working week.
	if m, err := parseMinutes("57600"); err != nil || m != 960 {
		t.Errorf("seconds = %d %v, want 960 minutes", m, err)
	}
	if m, err := parseMinutes("2w 3d 4h"); err != nil || m != 6480 {
		t.Errorf("a spelled length = %d %v, want 6480 minutes", m, err)
	}
	if _, err := parseMinutes("ages"); err == nil {
		t.Error("a word was accepted as a length of time")
	}
}

func TestCellsAndFieldValues(t *testing.T) {
	due := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	i := &issue.Issue{Key: "CP-1", Summary: "Fix, please", Priority: issue.PriorityHigh, DueDate: &due, Labels: []issue.LabelRef{{Name: "a"}, {Name: "b"}}}
	if cell(i, "key") != "CP-1" || cell(i, "labels") != "a; b" || cell(i, "due") != "2026-09-30" || cell(i, "assignee") != "" || cell(i, "priority") != "high" {
		t.Errorf("cells = %q %q %q %q", cell(i, "key"), cell(i, "labels"), cell(i, "due"), cell(i, "assignee"))
	}
	if raw, err := fieldValue(field.Field{Name: "Cost", Kind: field.Number}, "12.5"); err != nil || string(raw) != "12.5" {
		t.Errorf("number = %s %v", raw, err)
	}
	if _, err := fieldValue(field.Field{Name: "Cost", Kind: field.Number}, "lots"); err == nil {
		t.Error("a word was accepted as a number")
	}
	if raw, _ := fieldValue(field.Field{Name: "Paid", Kind: field.Checkbox}, "yes"); string(raw) != "true" {
		t.Errorf("checkbox = %s", raw)
	}
	if raw, err := fieldValue(field.Field{Name: "Ship", Kind: field.Date}, "09/Sep/26 08:37"); err != nil || string(raw) != `"2026-09-09"` {
		t.Errorf("a date the file wrote its own way = %s %v", raw, err)
	}
	if _, err := fieldValue(field.Field{Name: "Ship", Kind: field.Date}, "30/09/2026"); err == nil {
		t.Error("a day whose order nobody can tell was accepted")
	}
}
