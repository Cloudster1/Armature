package buildinfo

import "testing"

func TestCurrentPrefersTheLinkerAndNeverLeavesVersionEmpty(t *testing.T) {
	Version, Commit, BuiltAt = "", "", ""
	if got := Current().Version; got == "" {
		t.Fatalf("an untagged build still needs a version to show, got %q", got)
	}
	Version, Commit, BuiltAt = "1.2.3", "abc", "2026-09-28T00:00:00Z"
	got := Current()
	if got.Version != "1.2.3" || got.Commit != "abc" || got.BuiltAt != "2026-09-28T00:00:00Z" {
		t.Fatalf("linker values were not kept: %+v", got)
	}
}

func TestATagReadsAsTheVersionWithoutItsPrefix(t *testing.T) {
	Version, Commit, BuiltAt = "v0.2.0", "", ""
	if got := Current().Version; got != "0.2.0" {
		t.Fatalf("the v of a tag should not reach the version, got %q", got)
	}
}
