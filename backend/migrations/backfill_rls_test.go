package migrations

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// org forced row security on its owner until 00932, and a cluster migrates as a
// plain owner, so a migration reading org without lifting that first read nothing.
var (
	readsOrg     = regexp.MustCompile(`(?i)\b(from|join)\s+(public\.)?org\b`)
	liftsOrg     = regexp.MustCompile(`(?i)alter\s+table\s+(public\.)?org\s+no\s+force\s+row\s+level\s+security`)
	orgRowSecure = regexp.MustCompile(`(?i)alter\s+table\s+(public\.)?org\s+(no\s+)?force\s+row\s+level\s+security`)
	lineComment  = regexp.MustCompile(`--[^\n]*`)
)

// The repair that reran the backfills written before the rule was.
const ownerBackfillsRepair = "00926_backfills_reach_every_org.sql"

// From this migration on nothing hides a row from the owner, so the ones after
// it read org freely and the rule holds only for those before.
const rowSecurityDropped = "00932_drop_row_security.sql"

// Files that read org without lifting its row security. A backfill is listed
// with the migration that repaired it; a trigger body runs as the tenant.
var readsOrgUnlifted = map[string]string{
	"00005_issue_hierarchy.sql":              ownerBackfillsRepair,
	"00007_workflow_scopes.sql":              "",
	"00033_automation.sql":                   ownerBackfillsRepair,
	"00049_local_users.sql":                  "",
	"00909_holiday_calendars.sql":            ownerBackfillsRepair,
	"00921_attachment_tombstone_trigger.sql": "",
	"00930_last_administrator.sql":           "",
}

// upSection is the migration's Up half with its comments taken out, the part
// the owner runs on the way forward.
func upSection(t *testing.T, name string) string {
	t.Helper()
	raw, err := fs.ReadFile(FS, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	text := string(raw)
	if i := strings.Index(text, "-- +goose Down"); i >= 0 {
		text = text[:i]
	}
	return lineComment.ReplaceAllString(text, "")
}

// liftsAroundItsReads says whether org's row security is lifted before the
// first read of org and forced again by the end.
func liftsAroundItsReads(up string) bool {
	read := readsOrg.FindStringIndex(up)
	lift := liftsOrg.FindStringIndex(up)
	if read == nil {
		return true
	}
	if lift == nil || lift[0] > read[0] {
		return false
	}
	all := orgRowSecure.FindAllStringSubmatch(up, -1)
	return all[len(all)-1][2] == ""
}

func TestBackfillsReadingOrgLiftItsRowSecurity(t *testing.T) {
	names, err := fs.Glob(FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(FS, rowSecurityDropped); err != nil {
		t.Fatalf("%s, which ends the rule, is missing: %v", rowSecurityDropped, err)
	}
	for _, name := range names {
		if name >= rowSecurityDropped {
			continue
		}
		up := upSection(t, name)
		if liftsAroundItsReads(up) {
			continue
		}
		repair, listed := readsOrgUnlifted[name]
		if !listed {
			t.Errorf("%s reads org without ALTER TABLE org NO FORCE ROW LEVEL SECURITY before it and FORCE after it; "+
				"a database migrated as a plain owner would see no organization, so lift it around the backfill as 00904 does", name)
			continue
		}
		if repair == "" {
			continue
		}
		if _, err := fs.Stat(FS, repair); err != nil {
			t.Errorf("%s reads org with its row security forced, and %s, which should repeat that backfill for every organization, is missing", name, repair)
			continue
		}
		if !liftsAroundItsReads(upSection(t, repair)) || readsOrg.FindStringIndex(upSection(t, repair)) == nil {
			t.Errorf("%s should repeat the backfills of %s with org's row security lifted, and does not", repair, name)
		}
	}
}

// The check itself: a read before any lift is caught, one inside a lift is not.
func TestLiftsAroundItsReads(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want bool
	}{
		{"no read", "CREATE TABLE x (id int);", true},
		{"member table is not org", "SELECT 1 FROM org_member;", true},
		{"bare read", "INSERT INTO t SELECT id FROM org;", false},
		{"join", "SELECT 1 FROM t JOIN org o ON o.id = t.org_id;", false},
		{"lifted and forced again", "ALTER TABLE org NO FORCE ROW LEVEL SECURITY;\nINSERT INTO t SELECT id FROM org;\nALTER TABLE org FORCE ROW LEVEL SECURITY;", true},
		{"lifted after the read", "INSERT INTO t SELECT id FROM org;\nALTER TABLE org NO FORCE ROW LEVEL SECURITY;\nALTER TABLE org FORCE ROW LEVEL SECURITY;", false},
		{"left lifted", "ALTER TABLE org NO FORCE ROW LEVEL SECURITY;\nINSERT INTO t SELECT id FROM org;", false},
	}
	for _, c := range cases {
		if got := liftsAroundItsReads(c.sql); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
