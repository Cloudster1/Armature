package csvio

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

// Targets are what an imported column may fill. A custom field is named as
// field:Name, and field:new:Name:kind defines one before the run.
var Targets = []string{
	"key", "summary", "description", "type", "status", "priority",
	"assignee", "reporter", "parent", "epic", "created", "updated", "resolved",
	"due", "start", "estimate", "timeEstimate", "timeRemaining", "labels",
	"sprint", "fixVersions", "affectsVersions", "components", "team",
	"comments", "worklogs",
	"links:blocks", "links:blockedBy", "links:relates", "links:duplicates", "links:duplicatedBy",
}

// manyValued are the targets a file may fill from several columns at once,
// which is how Jira writes labels, sprints, comments and worklogs.
var manyValued = map[string]bool{
	"labels": true, "sprint": true, "comments": true, "worklogs": true,
	"fixVersions": true, "affectsVersions": true, "components": true,
	"links:blocks": true, "links:blockedBy": true, "links:relates": true,
	"links:duplicates": true, "links:duplicatedBy": true,
}

// Mapping says which columns of the file fill which target, by position. A
// name would not do: a Jira export has five columns called Labels.
type Mapping map[string][]int

// Row is one record of the file, with the line it started on so a refusal
// points at a place a person can look rather than at a record number.
type Row struct {
	Cells []string
	Line  int
}

// Column is one column of the file as the person mapping it sees it.
type Column struct {
	At      int      `json:"at"`
	Name    string   `json:"name"`
	Samples []string `json:"samples"`
}

// Word is one distinct value of a mapped column, and what it becomes here.
type Word struct {
	Target string `json:"target"`
	Value  string `json:"value"`
	Count  int    `json:"count"`
	// Means is what this value matches here, empty when nothing does.
	Means string `json:"means"`
}

// PersonFound is one name the file uses for somebody, and the member it looks
// like. Jira writes a username, and a username is not an address.
type PersonFound struct {
	Name   string     `json:"name"`
	Count  int        `json:"count"`
	Member *uuid.UUID `json:"member,omitempty"`
	Email  string     `json:"email,omitempty"`
}

// Preview is what the file holds, so the mapping can be made before anything
// is written.
type Preview struct {
	Columns []Column      `json:"columns"`
	Total   int           `json:"total"`
	Words   []Word        `json:"words"`
	People  []PersonFound `json:"people"`
}

// byteOrderMark is what a spreadsheet writes before the first header.
var byteOrderMark = string(rune(0xFEFF))

// Read parses a file into headers and rows, refusing one too big to import.
func Read(data []byte) ([]string, []Row, error) {
	if len(data) > MaxImportBytes {
		return nil, nil, fmt.Errorf("the file is over %d MB; split it", MaxImportBytes>>20)
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true

	var (
		headers []string
		rows    []Row
	)
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("the file is not CSV: %v", err)
		}
		line, _ := r.FieldPos(0)
		if headers == nil {
			headers = make([]string, len(record))
			for n, h := range record {
				headers[n] = strings.TrimSpace(strings.TrimPrefix(h, byteOrderMark))
			}
			continue
		}
		rows = append(rows, Row{Cells: record, Line: line})
		if len(rows) > MaxImportRows {
			return nil, nil, fmt.Errorf("the file has over %d rows; import at most that many at a time", MaxImportRows)
		}
	}
	if headers == nil {
		return nil, nil, errors.New("the file is empty")
	}
	return headers, rows, nil
}

// Guess maps headers to targets by name, case aside, so a file exported from
// here or out of Jira mostly maps itself.
func Guess(headers []string) Mapping {
	m := Mapping{}
	for at, h := range headers {
		target, ok := aliases[strings.ToLower(strings.TrimSpace(h))]
		if !ok {
			continue
		}
		if _, taken := m[target]; taken && !manyValued[target] {
			continue
		}
		m[target] = append(m[target], at)
	}
	return m
}

// aliases are the header names other trackers write, Jira's among them.
var aliases = map[string]string{
	"key": "key", "issue key": "key",
	"summary": "summary", "title": "summary", "name": "summary",
	"description": "description",
	"type":        "type", "issue type": "type", "issuetype": "type",
	"status": "status", "priority": "priority",
	"assignee": "assignee", "reporter": "reporter",
	"parent": "parent", "parent key": "parent",
	"created": "created", "created date": "created",
	"updated": "updated", "updated date": "updated",
	"resolved": "resolved", "resolution date": "resolved",
	"due": "due", "due date": "due", "duedate": "due",
	"start": "start", "start date": "start", "custom field (target start)": "start",
	"estimate": "estimate", "story points": "estimate", "points": "estimate",
	"custom field (story points)": "estimate",
	"original estimate":           "timeEstimate",
	"remaining estimate":          "timeRemaining",
	"labels":                      "labels", "label": "labels", "tags": "labels",
	// What Jira calls the rest of an issue.
	"custom field (epic link)":       "epic",
	"custom field (parent link)":     "epic",
	"sprint":                         "sprint",
	"custom field (team)":            "team",
	"fix version/s":                  "fixVersions",
	"affects version/s":              "affectsVersions",
	"component/s":                    "components",
	"comment":                        "comments",
	"log work":                       "worklogs",
	"outward issue link (blocks)":    "links:blocks",
	"inward issue link (blocks)":     "links:blockedBy",
	"outward issue link (relates)":   "links:relates",
	"inward issue link (relates)":    "links:relates",
	"outward issue link (duplicate)": "links:duplicates",
	"inward issue link (duplicate)":  "links:duplicatedBy",
}

// check refuses a mapping the file or the product cannot honour, before any
// row is read.
func check(mapping Mapping, headers []string) error {
	if len(mapping["summary"]) == 0 {
		return errors.New("map a column to the summary; every issue needs one")
	}
	for target, positions := range mapping {
		if len(positions) == 0 {
			delete(mapping, target)
			continue
		}
		if !contains(Targets, target) && !strings.HasPrefix(target, "field:") {
			return fmt.Errorf("%q is not something a column can fill", target)
		}
		if len(positions) > 1 && !manyValued[target] {
			return fmt.Errorf("%s is filled by one column, and %d were given", target, len(positions))
		}
		for _, at := range positions {
			if at < 0 || at >= len(headers) {
				return fmt.Errorf("the file has no column %d", at+1)
			}
		}
	}
	return nil
}

// values reads every cell a target is mapped to, in column order.
func (r Row) values(mapping Mapping, target string) []string {
	var out []string
	for _, at := range mapping[target] {
		if at >= len(r.Cells) {
			continue
		}
		if v := strings.TrimSpace(r.Cells[at]); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// value is the first cell a target is mapped to, which is all a single valued
// target ever has.
func (r Row) value(mapping Mapping, target string) string {
	if v := r.values(mapping, target); len(v) > 0 {
		return v[0]
	}
	return ""
}
